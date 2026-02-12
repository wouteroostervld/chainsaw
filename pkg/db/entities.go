package db

import (
	"database/sql"
	"fmt"
)

// Entity represents a code entity (function, type, etc.)
type Entity struct {
	ID         int64
	Name       string
	EntityType string
	ChunkID    int64
}

// Scan implements Scannable interface for Entity
func (e *Entity) Scan(rows *sql.Rows) error {
	return rows.Scan(&e.ID, &e.Name, &e.EntityType, &e.ChunkID)
}

// EntityEdge represents a graph edge between two entities
type EntityEdge struct {
	SourceEntityID int64
	TargetEntityID int64
	RelationType   string
	ChunkID        int64
	Weight         float64
}

// Scan implements Scannable interface for EntityEdge
func (e *EntityEdge) Scan(rows *sql.Rows) error {
	return rows.Scan(&e.SourceEntityID, &e.TargetEntityID, &e.RelationType, &e.ChunkID, &e.Weight)
}

// UpsertEntity inserts or updates an entity, returns the entity ID
func (db *DB) UpsertEntity(name, entityType string, chunkID int64) (int64, error) {
	// First try insert
	result, err := db.conn.Exec(`
		INSERT OR IGNORE INTO entities (name, entity_type, chunk_id)
		VALUES (?, ?, ?)
	`, name, entityType, chunkID)

	if err != nil {
		return 0, fmt.Errorf("failed to insert entity: %w", err)
	}

	// If inserted, get the new ID
	if rowsAffected, _ := result.RowsAffected(); rowsAffected > 0 {
		id, err := result.LastInsertId()
		if err != nil {
			return 0, fmt.Errorf("failed to get insert ID: %w", err)
		}
		return id, nil
	}

	// Otherwise, fetch existing ID
	var id int64
	err = db.conn.QueryRow(`
		SELECT id FROM entities
		WHERE name = ? AND entity_type = ? AND chunk_id = ?
	`, name, entityType, chunkID).Scan(&id)

	if err != nil {
		return 0, fmt.Errorf("failed to query existing entity: %w", err)
	}

	return id, nil
}

// UpsertEntityEdge inserts or updates an edge between entities
func (db *DB) UpsertEntityEdge(sourceID, targetID int64, relationType string, chunkID int64) error {
	_, err := db.conn.Exec(`
		INSERT INTO graph_edges (source_entity_id, target_entity_id, relation_type, chunk_id, weight)
		VALUES (?, ?, ?, ?, 1.0)
		ON CONFLICT(source_entity_id, target_entity_id, chunk_id) DO UPDATE SET
			relation_type = excluded.relation_type,
			weight = excluded.weight
	`, sourceID, targetID, relationType, chunkID)

	if err != nil {
		return fmt.Errorf("failed to upsert entity edge: %w", err)
	}

	return nil
}

// GetEntityByName retrieves entities by name
func (db *DB) GetEntityByName(name string) ([]*Entity, error) {
	rows, err := db.conn.Query(`
		SELECT id, name, entity_type, chunk_id
		FROM entities
		WHERE name = ?
	`, name)

	if err != nil {
		return nil, fmt.Errorf("failed to query entities: %w", err)
	}
	defer rows.Close()

	return scanEntities(rows)
}

// GetEntityEdges retrieves edges for an entity
func (db *DB) GetEntityEdges(entityID int64) ([]*EntityEdge, error) {
	rows, err := db.conn.Query(`
		SELECT source_entity_id, target_entity_id, relation_type, chunk_id, weight
		FROM graph_edges
		WHERE source_entity_id = ? OR target_entity_id = ?
		ORDER BY weight DESC
	`, entityID, entityID)

	if err != nil {
		return nil, fmt.Errorf("failed to query entity edges: %w", err)
	}
	defer rows.Close()

	return scanEntityEdges(rows)
}

// GetEntitiesByType retrieves all entities of a specific type
func (db *DB) GetEntitiesByType(entityType string) ([]*Entity, error) {
	rows, err := db.conn.Query(`
		SELECT id, name, entity_type, chunk_id
		FROM entities
		WHERE entity_type = ?
		ORDER BY name
	`, entityType)

	if err != nil {
		return nil, fmt.Errorf("failed to query entities by type: %w", err)
	}
	defer rows.Close()

	return scanEntities(rows)
}

// FindRelatedEntities finds entities related to a given entity
func (db *DB) FindRelatedEntities(entityID int64, relationType string) ([]*Entity, error) {
	query := `
		SELECT DISTINCT e.id, e.name, e.entity_type, e.chunk_id
		FROM entities e
		JOIN graph_edges g ON (g.source_entity_id = e.id OR g.target_entity_id = e.id)
		WHERE (g.source_entity_id = ? OR g.target_entity_id = ?)
		  AND e.id != ?
	`

	args := []interface{}{entityID, entityID, entityID}

	if relationType != "" {
		query += " AND g.relation_type = ?"
		args = append(args, relationType)
	}

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to find related entities: %w", err)
	}
	defer rows.Close()

	return scanEntities(rows)
}

// Helper functions

func scanEntities(rows *sql.Rows) ([]*Entity, error) {
	return scanRows[Entity](rows)
}

func scanEntityEdges(rows *sql.Rows) ([]*EntityEdge, error) {
	return scanRows[EntityEdge](rows)
}

// Legacy chunk-based operations for backwards compatibility

type GetNeighborsOptions struct {
	ChunkID   int64
	MaxDepth  int
	MinWeight float64
	Limit     int
}

type Neighbor struct {
	ChunkID      int64
	Depth        int
	TotalWeight  float64
	RelationType string
}

func (db *DB) GetNeighbors(opts GetNeighborsOptions) ([]*Neighbor, error) {
	// Return empty - entities are the new model
	return []*Neighbor{}, nil
}

func (db *DB) DeleteEdgesForChunk(chunkID int64) error {
	_, err := db.conn.Exec(`DELETE FROM graph_edges WHERE chunk_id = ?`, chunkID)
	return err
}

func (db *DB) CountEdges() (int64, error) {
	var count int64
	err := db.conn.QueryRow("SELECT COUNT(*) FROM graph_edges").Scan(&count)
	return count, err
}
