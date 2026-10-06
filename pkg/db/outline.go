package db

import (
	"database/sql"
	"fmt"
	"strings"
)

// OutlineNode is a semantic chunk of a parsed file, in the tree of chunks
type OutlineNode struct {
	Parent    int     // Index of the parent node in the same batch, -1 for the file skeleton
	StartLine int     // 1-indexed
	EndLine   int     // 1-indexed, inclusive
	Content   string  // Full content, with child nodes folded to their stubs
	Stub      string  // Text that stands in for this node in its parent's content
	ChunkIDs  []int64 // Embedded chunks holding this node's content
}

// StoreFileOutline stores the tree of semantic chunks of a file. Parents must
// come before their children. The file's previous outline must have been
// removed with DeleteChunksForFile.
func (db *DB) StoreFileOutline(fileID int64, nodes []OutlineNode) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	ids := make([]int64, len(nodes))
	for i, node := range nodes {
		var parentID int64
		if node.Parent >= 0 && node.Parent < i {
			parentID = ids[node.Parent]
		}
		result, err := tx.Exec(`
			INSERT INTO outline_nodes (file_id, parent_id, start_line, end_line, content, stub)
			VALUES (?, ?, ?, ?, ?, ?)
		`, fileID, parentID, node.StartLine, node.EndLine, node.Content, node.Stub)
		if err != nil {
			return fmt.Errorf("failed to insert outline node: %w", err)
		}
		if ids[i], err = result.LastInsertId(); err != nil {
			return fmt.Errorf("failed to get outline node ID: %w", err)
		}
		for _, chunkID := range node.ChunkIDs {
			if _, err := tx.Exec("INSERT OR REPLACE INTO chunk_nodes (chunk_id, node_id) VALUES (?, ?)", chunkID, ids[i]); err != nil {
				return fmt.Errorf("failed to map chunk to outline node: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit outline: %w", err)
	}
	return nil
}

// outlineNode is a stored node of a file's outline
type outlineNode struct {
	id, parentID       int64
	startLine, endLine int
	content, stub      string
	children           []*outlineNode
}

// loadOutline returns the outline nodes of a file by ID
func (db *DB) loadOutline(fileID int64) (map[int64]*outlineNode, error) {
	rows, err := db.conn.Query(`
		SELECT id, parent_id, start_line, end_line, content, stub
		FROM outline_nodes
		WHERE file_id = ?
		ORDER BY id
	`, fileID)
	if err != nil {
		return nil, fmt.Errorf("failed to query outline: %w", err)
	}
	defer rows.Close()

	nodes := make(map[int64]*outlineNode)
	for rows.Next() {
		n := &outlineNode{}
		if err := rows.Scan(&n.id, &n.parentID, &n.startLine, &n.endLine, &n.content, &n.stub); err != nil {
			return nil, fmt.Errorf("failed to scan outline node: %w", err)
		}
		nodes[n.id] = n
		if parent := nodes[n.parentID]; parent != nil {
			parent.children = append(parent.children, n)
		}
	}
	return nodes, rows.Err()
}

// render returns the content of a node with the given descendants shown in
// full in place of their stubs
func (n *outlineNode) render(expanded map[int64]bool) string {
	content := n.content
	for _, child := range n.children {
		if expanded[child.id] {
			content = strings.Replace(content, child.stub, strings.TrimSuffix(child.render(expanded), "\n"), 1)
		}
	}
	return content
}

// nodeForChunk returns the outline node a chunk belongs to, or 0
func (db *DB) nodeForChunk(chunkID int64) (int64, error) {
	var nodeID int64
	err := db.conn.QueryRow("SELECT node_id FROM chunk_nodes WHERE chunk_id = ?", chunkID).Scan(&nodeID)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to query outline node: %w", err)
	}
	return nodeID, nil
}

// callee is a definition called from an outline node
type callee struct {
	nodeID int64
	fileID int64
	path   string
}

// calleesOf returns the outline nodes of the definitions that are called
// from the given node. Only calls whose target is certain are followed.
func (db *DB) calleesOf(nodeID int64) ([]callee, error) {
	rows, err := db.conn.Query(`
		SELECT DISTINCT tn.node_id, n.file_id, f.path
		FROM chunk_nodes sn
		JOIN entities s ON s.chunk_id = sn.chunk_id
		JOIN graph_edges g ON g.source_entity_id = s.id AND g.relation_type = ?
		JOIN entities t ON t.id = g.target_entity_id AND t.chunk_id != 0
		JOIN chunk_nodes tn ON tn.chunk_id = t.chunk_id
		JOIN outline_nodes n ON n.id = tn.node_id
		JOIN files f ON f.id = n.file_id
		WHERE sn.node_id = ? AND tn.node_id != sn.node_id
		ORDER BY tn.node_id
	`, relationCalls, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to query callees: %w", err)
	}
	defer rows.Close()

	var callees []callee
	for rows.Next() {
		var c callee
		if err := rows.Scan(&c.nodeID, &c.fileID, &c.path); err != nil {
			return nil, fmt.Errorf("failed to scan callee: %w", err)
		}
		callees = append(callees, c)
	}
	return callees, rows.Err()
}

// attachOutlines rewrites search results for files with an outline, to the
// given view. For ViewBody a result is the whole definition that matched,
// even if it was embedded in several blocks. Otherwise all hits in one file
// become a single result carrying the folded file with the hits shown in
// full. ViewCalls adds the definitions the hits call: in place when they are
// in the same file, as related chunks when they are in another.
func (db *DB) attachOutlines(results []*SearchResult, pathFilter string, view SearchView) ([]*SearchResult, error) {
	var merged []*SearchResult
	byFile := make(map[int64]*SearchResult)
	outlines := make(map[int64]map[int64]*outlineNode)
	expanded := make(map[int64]map[int64]bool)
	related := make(map[int64]map[int64]bool) // Nodes already added as related chunk, per file

	// expand marks a node and its ancestors, so the path to it is opened up
	expand := func(fileID, nodeID int64) {
		for n := outlines[fileID][nodeID]; n != nil; n = outlines[fileID][n.parentID] {
			expanded[fileID][n.id] = true
		}
	}

	for _, result := range results {
		fileID := result.Chunk.FileID
		nodeID, err := db.nodeForChunk(result.Chunk.ChunkID)
		if err != nil {
			return nil, err
		}
		if nodeID == 0 {
			merged = append(merged, result)
			continue
		}

		if outlines[fileID] == nil {
			if outlines[fileID], err = db.loadOutline(fileID); err != nil {
				return nil, err
			}
			expanded[fileID] = make(map[int64]bool)
			related[fileID] = make(map[int64]bool)
		}
		node := outlines[fileID][nodeID]
		if node == nil {
			merged = append(merged, result)
			continue
		}
		seen := expanded[fileID][nodeID]
		hit := LineRange{Start: node.startLine, End: node.endLine}

		if view == ViewBody {
			// Another block of a definition that is already a result
			if !seen {
				expanded[fileID][nodeID] = true
				result.Outline = node.content
				result.Hits = []LineRange{hit}
				merged = append(merged, result)
			}
			continue
		}

		first := byFile[fileID]
		if first == nil {
			first = result
			byFile[fileID] = first
			merged = append(merged, first)
		}
		if !seen {
			first.Hits = append(first.Hits, hit)
		}
		expand(fileID, nodeID)

		if view != ViewCalls {
			continue
		}
		callees, err := db.calleesOf(nodeID)
		if err != nil {
			return nil, err
		}
		for _, c := range callees {
			if c.fileID == fileID {
				expand(fileID, c.nodeID)
				continue
			}
			if related[fileID][c.nodeID] || !matchesPathFilter(c.path, pathFilter) {
				continue
			}
			related[fileID][c.nodeID] = true
			chunk, err := db.outlineChunk(c.nodeID)
			if err != nil {
				return nil, err
			}
			first.RelatedChunks = append(first.RelatedChunks, &RelatedChunk{
				Chunk:        chunk,
				RelationType: relationCalls,
				Weight:       weightExact,
				Depth:        1,
			})
		}
	}

	for fileID, result := range byFile {
		for _, node := range outlines[fileID] {
			if node.parentID == 0 {
				result.Outline = node.render(expanded[fileID])
				break
			}
		}
	}
	return merged, nil
}

// outlineChunk returns an outline node in the shape of a chunk
func (db *DB) outlineChunk(nodeID int64) (*Chunk, error) {
	c := &Chunk{}
	err := db.conn.QueryRow(`
		SELECT file_id, content, start_line, end_line
		FROM outline_nodes
		WHERE id = ?
	`, nodeID).Scan(&c.FileID, &c.ContentSnippet, &c.StartLine, &c.EndLine)
	if err != nil {
		return nil, fmt.Errorf("failed to get outline node: %w", err)
	}
	return c, nil
}

// matchesPathFilter applies a SQL LIKE prefix pattern such as "/home/user/project/%"
func matchesPathFilter(path, pathFilter string) bool {
	return pathFilter == "" || strings.HasPrefix(path, strings.TrimSuffix(pathFilter, "%"))
}
