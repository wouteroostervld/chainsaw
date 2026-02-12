package db

import (
	"path/filepath"
	"testing"
)

func TestUpsertEntity(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := Open(Config{
		Path:         dbPath,
		EmbeddingDim: 384,
		SkipVecTable: true,
	})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Insert entity
	id, err := db.UpsertEntity("myFunction", "function", 1)
	if err != nil {
		t.Fatalf("Failed to insert entity: %v", err)
	}

	// Verify entity exists
	entities, err := db.GetEntityByName("myFunction")
	if err != nil {
		t.Fatalf("Failed to get entity: %v", err)
	}
	if len(entities) == 0 {
		t.Fatal("Entity not found after insert")
	}
	if entities[0].ID != id {
		t.Errorf("Expected ID %d, got %d", id, entities[0].ID)
	}
	if entities[0].EntityType != "function" {
		t.Errorf("Expected type 'function', got %s", entities[0].EntityType)
	}

	// Update entity (same name, type, chunkID should return same ID)
	id2, err := db.UpsertEntity("myFunction", "function", 1)
	if err != nil {
		t.Fatalf("Failed to upsert entity: %v", err)
	}
	if id2 != id {
		t.Errorf("Expected same ID on upsert, got %d vs %d", id, id2)
	}
}

func TestUpsertEntityEdge(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := Open(Config{
		Path:         dbPath,
		EmbeddingDim: 384,
		SkipVecTable: true,
	})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create two entities
	sourceID, err := db.UpsertEntity("funcA", "function", 1)
	if err != nil {
		t.Fatalf("Failed to insert source entity: %v", err)
	}

	targetID, err := db.UpsertEntity("funcB", "function", 2)
	if err != nil {
		t.Fatalf("Failed to insert target entity: %v", err)
	}

	// Create edge
	err = db.UpsertEntityEdge(sourceID, targetID, "calls", 1)
	if err != nil {
		t.Fatalf("Failed to insert edge: %v", err)
	}

	// Verify edge exists
	edges, err := db.GetEntityEdges(sourceID)
	if err != nil {
		t.Fatalf("Failed to get edges: %v", err)
	}
	if len(edges) == 0 {
		t.Fatal("Edge not found after insert")
	}
	if edges[0].RelationType != "calls" {
		t.Errorf("Expected relation 'calls', got %s", edges[0].RelationType)
	}
	if edges[0].TargetEntityID != targetID {
		t.Errorf("Expected target %d, got %d", targetID, edges[0].TargetEntityID)
	}
}

func TestGetEntitiesByType(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := Open(Config{
		Path:         dbPath,
		EmbeddingDim: 384,
		SkipVecTable: true,
	})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create entities of different types
	_, err = db.UpsertEntity("funcA", "function", 1)
	if err != nil {
		t.Fatalf("Failed to insert entity: %v", err)
	}

	_, err = db.UpsertEntity("funcB", "function", 2)
	if err != nil {
		t.Fatalf("Failed to insert entity: %v", err)
	}

	_, err = db.UpsertEntity("MyType", "type", 3)
	if err != nil {
		t.Fatalf("Failed to insert entity: %v", err)
	}

	// Get all functions
	functions, err := db.GetEntitiesByType("function")
	if err != nil {
		t.Fatalf("Failed to get entities by type: %v", err)
	}
	if len(functions) != 2 {
		t.Errorf("Expected 2 functions, got %d", len(functions))
	}

	// Get all types
	types, err := db.GetEntitiesByType("type")
	if err != nil {
		t.Fatalf("Failed to get entities by type: %v", err)
	}
	if len(types) != 1 {
		t.Errorf("Expected 1 type, got %d", len(types))
	}
}

func TestFindRelatedEntities(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := Open(Config{
		Path:         dbPath,
		EmbeddingDim: 384,
		SkipVecTable: true,
	})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create entities
	aID, _ := db.UpsertEntity("funcA", "function", 1)
	bID, _ := db.UpsertEntity("funcB", "function", 2)
	cID, _ := db.UpsertEntity("funcC", "function", 3)

	// Create edges: A calls B, A imports C
	db.UpsertEntityEdge(aID, bID, "calls", 1)
	db.UpsertEntityEdge(aID, cID, "imports", 1)

	// Find all entities related to A
	related, err := db.FindRelatedEntities(aID, "")
	if err != nil {
		t.Fatalf("Failed to find related entities: %v", err)
	}
	if len(related) != 2 {
		t.Errorf("Expected 2 related entities, got %d", len(related))
	}

	// Find only entities A calls
	called, err := db.FindRelatedEntities(aID, "calls")
	if err != nil {
		t.Fatalf("Failed to find called entities: %v", err)
	}
	if len(called) != 1 {
		t.Errorf("Expected 1 called entity, got %d", len(called))
	}
	if called[0].Name != "funcB" {
		t.Errorf("Expected funcB, got %s", called[0].Name)
	}
}

func TestDeleteEdgesForChunk(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := Open(Config{
		Path:         dbPath,
		EmbeddingDim: 384,
		SkipVecTable: true,
	})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create entities and edges for chunk 1
	aID, _ := db.UpsertEntity("funcA", "function", 1)
	bID, _ := db.UpsertEntity("funcB", "function", 1)
	cID, _ := db.UpsertEntity("funcC", "function", 2)

	db.UpsertEntityEdge(aID, bID, "calls", 1)
	db.UpsertEntityEdge(bID, cID, "calls", 1)

	// Delete edges for chunk 1
	err = db.DeleteEdgesForChunk(1)
	if err != nil {
		t.Fatalf("Failed to delete edges: %v", err)
	}

	// Verify chunk 1 edge is gone
	edges, err := db.GetEntityEdges(aID)
	if err != nil {
		t.Fatalf("Failed to get edges: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("Expected 0 edges for chunk 1, got %d", len(edges))
	}
}

func TestCountEdges(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := Open(Config{
		Path:         dbPath,
		EmbeddingDim: 384,
		SkipVecTable: true,
	})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Initial count
	count, err := db.CountEdges()
	if err != nil {
		t.Fatalf("Failed to count edges: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected 0 edges initially, got %d", count)
	}

	// Insert entities and edges
	aID, _ := db.UpsertEntity("funcA", "function", 1)
	bID, _ := db.UpsertEntity("funcB", "function", 2)
	cID, _ := db.UpsertEntity("funcC", "function", 3)

	db.UpsertEntityEdge(aID, bID, "calls", 1)
	db.UpsertEntityEdge(bID, cID, "calls", 2)

	count, err = db.CountEdges()
	if err != nil {
		t.Fatalf("Failed to count edges: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected 2 edges, got %d", count)
	}
}
