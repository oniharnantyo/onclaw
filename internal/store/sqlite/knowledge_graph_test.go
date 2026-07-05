package sqlite_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/memory"
	"github.com/oniharnantyo/onclaw/internal/store/sqlite"
)

func setupTestDBWithKG(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	db, cleanup := setupTestDB(t)
	// Migrate to create kg tables
	if err := sqlite.Migrate(db); err != nil {
		cleanup()
		t.Fatalf("migrate database: %v", err)
	}
	return db, cleanup
}

func TestKGStore_IngestExtraction(t *testing.T) {
	db, cleanup := setupTestDBWithKG(t)
	defer cleanup()

	store := sqlite.NewKGStore(db)
	ctx := context.Background()

	// Note: The entity IDs will be auto-generated. We need to insert them first,
	// then use the generated IDs for relations.
	// This test needs to be adjusted to handle the auto-increment IDs.

	t.Run("insert entities and relations", func(t *testing.T) {
		// First insert entities manually to get their IDs
		now := time.Now().Format(time.RFC3339)
		res1, err := db.ExecContext(ctx,
			"INSERT INTO kg_entities (type, name, agent, valid_from) VALUES (?, ?, ?, ?)",
			"Person", "Alice", "test-agent", now,
		)
		if err != nil {
			t.Fatalf("insert Alice: %v", err)
		}
		aliceID, _ := res1.LastInsertId()

		res2, err := db.ExecContext(ctx,
			"INSERT INTO kg_entities (type, name, agent, valid_from) VALUES (?, ?, ?, ?)",
			"Person", "Bob", "test-agent", now,
		)
		if err != nil {
			t.Fatalf("insert Bob: %v", err)
		}
		bobID, _ := res2.LastInsertId()

		// Now create the extraction with proper entity IDs
		extraction := &memory.Extraction{
			Agent: "test-agent",
			Entities: []memory.Entity{
				{ID: aliceID, Type: "Person", Name: "Alice"},
				{ID: bobID, Type: "Person", Name: "Bob"},
			},
			Relations: []memory.Relation{
				{FromEntity: aliceID, ToEntity: bobID, Predicate: "knows"},
			},
		}

		err = store.IngestExtraction(ctx, extraction)
		if err != nil {
			t.Fatalf("IngestExtraction: %v", err)
		}

		// Verify entities were inserted (count should be 2 + the original 2 = 4)
		var count int
		err = db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM kg_entities WHERE agent = ? AND valid_until IS NULL",
			"test-agent",
		).Scan(&count)
		if err != nil {
			t.Fatalf("count entities: %v", err)
		}
		if count != 4 { // 2 manual + 2 from IngestExtraction
			t.Errorf("expected 4 entities, got %d", count)
		}

		// Verify relation was inserted
		var relCount int
		err = db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM kg_relations WHERE agent = ? AND valid_until IS NULL",
			"test-agent",
		).Scan(&relCount)
		if err != nil {
			t.Fatalf("count relations: %v", err)
		}
		if relCount != 1 {
			t.Errorf("expected 1 relation, got %d", relCount)
		}
	})
}

func TestKGStore_IngestExtraction_SupersedesContradictoryRelation(t *testing.T) {
	db, cleanup := setupTestDBWithKG(t)
	defer cleanup()

	store := sqlite.NewKGStore(db)
	ctx := context.Background()

	now := time.Now().Format(time.RFC3339)

	// Insert entities
	res1, _ := db.ExecContext(ctx,
		"INSERT INTO kg_entities (type, name, agent, valid_from) VALUES (?, ?, ?, ?)",
		"Person", "Alice", "test-agent", now,
	)
	aliceID, _ := res1.LastInsertId()

	res2, _ := db.ExecContext(ctx,
		"INSERT INTO kg_entities (type, name, agent, valid_from) VALUES (?, ?, ?, ?)",
		"Person", "Bob", "test-agent", now,
	)
	bobID, _ := res2.LastInsertId()

	// Insert initial relation: Alice knows Bob
	_, err := db.ExecContext(ctx,
		"INSERT INTO kg_relations (from_entity, to_entity, predicate, agent, valid_from) VALUES (?, ?, ?, ?, ?)",
		aliceID, bobID, "knows", "test-agent", now,
	)
	if err != nil {
		t.Fatalf("insert initial relation: %v", err)
	}

	// Ingest extraction with different predicate - should supersede old relation
	// (implementation supersedes ALL relations between same entities)
	extraction := &memory.Extraction{
		Agent: "test-agent",
		Entities: []memory.Entity{
			{ID: aliceID, Type: "Person", Name: "Alice"},
			{ID: bobID, Type: "Person", Name: "Bob"},
		},
		Relations: []memory.Relation{
			{FromEntity: aliceID, ToEntity: bobID, Predicate: "works_with"},
		},
	}

	err = store.IngestExtraction(ctx, extraction)
	if err != nil {
		t.Fatalf("IngestExtraction: %v", err)
	}

	// Verify old "knows" relation was superseded (valid_until set)
	var oldValidUntil sql.NullString
	err = db.QueryRowContext(ctx,
		"SELECT valid_until FROM kg_relations WHERE predicate = ? AND from_entity = ? AND to_entity = ?",
		"knows", aliceID, bobID,
	).Scan(&oldValidUntil)
	if err != nil {
		t.Fatalf("query old relation: %v", err)
	}
	if !oldValidUntil.Valid {
		t.Error("expected old 'knows' relation to have valid_until set, got NULL")
	}

	// Verify new "works_with" relation exists and is current
	var newValidUntil sql.NullString
	err = db.QueryRowContext(ctx,
		"SELECT valid_until FROM kg_relations WHERE predicate = ? AND from_entity = ? AND to_entity = ?",
		"works_with", aliceID, bobID,
	).Scan(&newValidUntil)
	if err != nil {
		t.Fatalf("query new relation: %v", err)
	}
	if newValidUntil.Valid {
		t.Errorf("expected new 'works_with' relation to have NULL valid_until, got %s", newValidUntil.String)
	}
}

func TestKGStore_DedupAfterExtraction_MergesDuplicateEntities(t *testing.T) {
	db, cleanup := setupTestDBWithKG(t)
	defer cleanup()

	store := sqlite.NewKGStore(db)
	ctx := context.Background()

	now := time.Now().Format(time.RFC3339)

	// Insert duplicate entities with different casing
	res1, _ := db.ExecContext(ctx,
		"INSERT INTO kg_entities (type, name, agent, valid_from) VALUES (?, ?, ?, ?)",
		"Person", "Alice", "test-agent", now,
	)
	aliceID1, _ := res1.LastInsertId()

	res2, _ := db.ExecContext(ctx,
		"INSERT INTO kg_entities (type, name, agent, valid_from) VALUES (?, ?, ?, ?)",
		"Person", "alice", "test-agent", now, // lowercase duplicate
	)
	aliceID2, _ := res2.LastInsertId()

	res3, _ := db.ExecContext(ctx,
		"INSERT INTO kg_entities (type, name, agent, valid_from) VALUES (?, ?, ?, ?)",
		"Person", "Bob", "test-agent", now,
	)
	bobID, _ := res3.LastInsertId()

	// Add relations: aliceID1 -> bobID, aliceID2 -> bobID
	_, err := db.ExecContext(ctx,
		"INSERT INTO kg_relations (from_entity, to_entity, predicate, agent, valid_from) VALUES (?, ?, ?, ?, ?)",
		aliceID1, bobID, "knows", "test-agent", now,
	)
	if err != nil {
		t.Fatalf("insert relation from alice1: %v", err)
	}

	_, err = db.ExecContext(ctx,
		"INSERT INTO kg_relations (from_entity, to_entity, predicate, agent, valid_from) VALUES (?, ?, ?, ?, ?)",
		aliceID2, bobID, "knows", "test-agent", now,
	)
	if err != nil {
		t.Fatalf("insert relation from alice2: %v", err)
	}

	// Run dedup
	err = store.DedupAfterExtraction(ctx, "test-agent")
	if err != nil {
		t.Fatalf("DedupAfterExtraction: %v", err)
	}

	// Verify one duplicate was invalidated
	var validCount int
	err = db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM kg_entities WHERE agent = ? AND type = ? AND LOWER(TRIM(name)) = ? AND valid_until IS NULL",
		"test-agent", "Person", "alice",
	).Scan(&validCount)
	if err != nil {
		t.Fatalf("count valid Alice entities: %v", err)
	}
	if validCount != 1 {
		t.Errorf("expected 1 valid Alice entity after dedup, got %d", validCount)
	}

	// Verify relations were re-pointed to canonical entity
	var relCount int
	err = db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM kg_relations WHERE agent = ? AND predicate = ? AND to_entity = ? AND valid_until IS NULL",
		"test-agent", "knows", bobID,
	).Scan(&relCount)
	if err != nil {
		t.Fatalf("count relations to Bob: %v", err)
	}
	if relCount != 1 {
		t.Errorf("expected 1 relation to Bob after dedup (consolidated), got %d", relCount)
	}
}

func TestKGStore_SearchGraph_TraversesFromSeed(t *testing.T) {
	db, cleanup := setupTestDBWithKG(t)
	defer cleanup()

	store := sqlite.NewKGStore(db)
	ctx := context.Background()

	now := time.Now().Format(time.RFC3339)

	// Create a simple graph: Alice -> Bob -> Charlie
	res1, _ := db.ExecContext(ctx,
		"INSERT INTO kg_entities (type, name, agent, valid_from) VALUES (?, ?, ?, ?)",
		"Person", "Alice", "test-agent", now,
	)
	aliceID, _ := res1.LastInsertId()

	res2, _ := db.ExecContext(ctx,
		"INSERT INTO kg_entities (type, name, agent, valid_from) VALUES (?, ?, ?, ?)",
		"Person", "Bob", "test-agent", now,
	)
	bobID, _ := res2.LastInsertId()

	res3, _ := db.ExecContext(ctx,
		"INSERT INTO kg_entities (type, name, agent, valid_from) VALUES (?, ?, ?, ?)",
		"Person", "Charlie", "test-agent", now,
	)
	charlieID, _ := res3.LastInsertId()

	// Add relations: Alice -> Bob, Bob -> Charlie
	_, err := db.ExecContext(ctx,
		"INSERT INTO kg_relations (from_entity, to_entity, predicate, agent, valid_from) VALUES (?, ?, ?, ?, ?)",
		aliceID, bobID, "knows", "test-agent", now,
	)
	if err != nil {
		t.Fatalf("insert Alice->Bob relation: %v", err)
	}

	_, err = db.ExecContext(ctx,
		"INSERT INTO kg_relations (from_entity, to_entity, predicate, agent, valid_from) VALUES (?, ?, ?, ?, ?)",
		bobID, charlieID, "knows", "test-agent", now,
	)
	if err != nil {
		t.Fatalf("insert Bob->Charlie relation: %v", err)
	}

	// Search from Alice with max_depth=2
	query := &memory.KGQuery{
		SeedEntity: aliceID,
		Agent:      "test-agent",
		MaxDepth:   2,
		Limit:      10,
	}

	hits, err := store.SearchGraph(ctx, query)
	if err != nil {
		t.Fatalf("SearchGraph: %v", err)
	}

	// Should find Bob (distance 1) and Charlie (distance 2)
	if len(hits) < 2 {
		t.Errorf("expected at least 2 hits, got %d", len(hits))
	}

	// Verify we found entities at different depths
	foundBob := false
	foundCharlie := false
	for _, hit := range hits {
		if hit.Entity.Name == "Bob" && hit.Distance == 1 {
			foundBob = true
		}
		if hit.Entity.Name == "Charlie" && hit.Distance == 2 {
			foundCharlie = true
		}
	}
	if !foundBob {
		t.Error("expected to find Bob at distance 1")
	}
	if !foundCharlie {
		t.Error("expected to find Charlie at distance 2")
	}
}

func TestKGStore_SearchGraph_RespectsMaxDepth(t *testing.T) {
	db, cleanup := setupTestDBWithKG(t)
	defer cleanup()

	store := sqlite.NewKGStore(db)
	ctx := context.Background()

	now := time.Now().Format(time.RFC3339)

	// Create chain: Alice -> Bob -> Charlie -> Dave
	var ids []int64
	names := []string{"Alice", "Bob", "Charlie", "Dave"}
	for _, name := range names {
		res, _ := db.ExecContext(ctx,
			"INSERT INTO kg_entities (type, name, agent, valid_from) VALUES (?, ?, ?, ?)",
			"Person", name, "test-agent", now,
		)
		id, _ := res.LastInsertId()
		ids = append(ids, id)
	}

	// Chain relations
	for i := 0; i < len(ids)-1; i++ {
		_, err := db.ExecContext(ctx,
			"INSERT INTO kg_relations (from_entity, to_entity, predicate, agent, valid_from) VALUES (?, ?, ?, ?, ?)",
			ids[i], ids[i+1], "knows", "test-agent", now,
		)
		if err != nil {
			t.Fatalf("insert relation: %v", err)
		}
	}

	// Search with max_depth=1 should only find Bob
	query := &memory.KGQuery{
		SeedEntity: ids[0],
		Agent:      "test-agent",
		MaxDepth:   1,
		Limit:      10,
	}

	hits, err := store.SearchGraph(ctx, query)
	if err != nil {
		t.Fatalf("SearchGraph depth 1: %v", err)
	}

	// All hits should be at depth 1
	for _, hit := range hits {
		if hit.Distance != 1 {
			t.Errorf("expected all hits at depth 1, got hit at depth %d", hit.Distance)
		}
	}

	// Search with max_depth=2 should find Bob and Charlie
	query.MaxDepth = 2
	hits, err = store.SearchGraph(ctx, query)
	if err != nil {
		t.Fatalf("SearchGraph depth 2: %v", err)
	}

	foundDepth2 := false
	for _, hit := range hits {
		if hit.Distance == 2 {
			foundDepth2 = true
		}
	}
	if !foundDepth2 {
		t.Error("expected to find at least one hit at depth 2")
	}
}

func TestKGStore_SearchGraph_ScopesToAgent(t *testing.T) {
	db, cleanup := setupTestDBWithKG(t)
	defer cleanup()

	store := sqlite.NewKGStore(db)
	ctx := context.Background()

	now := time.Now().Format(time.RFC3339)

	// Create entities for two agents
	res1, _ := db.ExecContext(ctx,
		"INSERT INTO kg_entities (type, name, agent, valid_from) VALUES (?, ?, ?, ?)",
		"Person", "Alice", "agent1", now,
	)
	aliceID, _ := res1.LastInsertId()

	res2, _ := db.ExecContext(ctx,
		"INSERT INTO kg_entities (type, name, agent, valid_from) VALUES (?, ?, ?, ?)",
		"Person", "Bob", "agent1", now,
	)
	bobID, _ := res2.LastInsertId()

	res3, _ := db.ExecContext(ctx,
		"INSERT INTO kg_entities (type, name, agent, valid_from) VALUES (?, ?, ?, ?)",
		"Person", "Charlie", "agent2", now,
	)
	charlieID, _ := res3.LastInsertId()

	// Add relations: Alice->Bob (agent1), Bob->Charlie (agent2)
	_, err := db.ExecContext(ctx,
		"INSERT INTO kg_relations (from_entity, to_entity, predicate, agent, valid_from) VALUES (?, ?, ?, ?, ?)",
		aliceID, bobID, "knows", "agent1", now,
	)
	if err != nil {
		t.Fatalf("insert agent1 relation: %v", err)
	}

	_, err = db.ExecContext(ctx,
		"INSERT INTO kg_relations (from_entity, to_entity, predicate, agent, valid_from) VALUES (?, ?, ?, ?, ?)",
		bobID, charlieID, "knows", "agent2", now,
	)
	if err != nil {
		t.Fatalf("insert agent2 relation: %v", err)
	}

	// Search from Alice scoped to agent1 should only find Bob
	query := &memory.KGQuery{
		SeedEntity: aliceID,
		Agent:      "agent1",
		MaxDepth:   2,
		Limit:      10,
	}

	hits, err := store.SearchGraph(ctx, query)
	if err != nil {
		t.Fatalf("SearchGraph: %v", err)
	}

	// Should only find Bob (Charlie is from agent2)
	for _, hit := range hits {
		if hit.Entity.Agent != "agent1" {
			t.Errorf("expected hit from agent1, got %s", hit.Entity.Agent)
		}
	}
}

func TestKGStore_SearchGraph_ReturnsPath(t *testing.T) {
	db, cleanup := setupTestDBWithKG(t)
	defer cleanup()

	store := sqlite.NewKGStore(db)
	ctx := context.Background()

	now := time.Now().Format(time.RFC3339)

	// Create chain: Alice -> Bob -> Charlie
	res1, _ := db.ExecContext(ctx,
		"INSERT INTO kg_entities (type, name, agent, valid_from) VALUES (?, ?, ?, ?)",
		"Person", "Alice", "test-agent", now,
	)
	aliceID, _ := res1.LastInsertId()

	res2, _ := db.ExecContext(ctx,
		"INSERT INTO kg_entities (type, name, agent, valid_from) VALUES (?, ?, ?, ?)",
		"Person", "Bob", "test-agent", now,
	)
	bobID, _ := res2.LastInsertId()

	res3, _ := db.ExecContext(ctx,
		"INSERT INTO kg_entities (type, name, agent, valid_from) VALUES (?, ?, ?, ?)",
		"Person", "Charlie", "test-agent", now,
	)
	charlieID, _ := res3.LastInsertId()

	// Add relations
	var relIDs []int64
	entities := []int64{aliceID, bobID, charlieID}
	for i := 0; i < len(entities)-1; i++ {
		res, _ := db.ExecContext(ctx,
			"INSERT INTO kg_relations (from_entity, to_entity, predicate, agent, valid_from) VALUES (?, ?, ?, ?, ?)",
			entities[i], entities[i+1], "knows", "test-agent", now,
		)
		id, _ := res.LastInsertId()
		relIDs = append(relIDs, id)
	}

	// Search from Alice
	query := &memory.KGQuery{
		SeedEntity: aliceID,
		Agent:      "test-agent",
		MaxDepth:   2,
		Limit:      10,
	}

	hits, err := store.SearchGraph(ctx, query)
	if err != nil {
		t.Fatalf("SearchGraph: %v", err)
	}

	// Find the Charlie hit and verify its path
	foundCharliePath := false
	for _, hit := range hits {
		if hit.Entity.Name == "Charlie" {
			if len(hit.Path) != 2 {
				t.Errorf("expected path of length 2 for Charlie, got %d", len(hit.Path))
			} else {
				foundCharliePath = true
			}
		}
	}
	if !foundCharliePath {
		t.Error("expected to find Charlie with path")
	}
}

func TestKGStore_SearchGraph_SkipsInvalidatedRelations(t *testing.T) {
	db, cleanup := setupTestDBWithKG(t)
	defer cleanup()

	store := sqlite.NewKGStore(db)
	ctx := context.Background()

	now := time.Now().Format(time.RFC3339)

	// Create entities
	res1, _ := db.ExecContext(ctx,
		"INSERT INTO kg_entities (type, name, agent, valid_from) VALUES (?, ?, ?, ?)",
		"Person", "Alice", "test-agent", now,
	)
	aliceID, _ := res1.LastInsertId()

	res2, _ := db.ExecContext(ctx,
		"INSERT INTO kg_entities (type, name, agent, valid_from) VALUES (?, ?, ?, ?)",
		"Person", "Bob", "test-agent", now,
	)
	bobID, _ := res2.LastInsertId()

	// Insert and then invalidate a relation
	relTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	_, err := db.ExecContext(ctx,
		"INSERT INTO kg_relations (from_entity, to_entity, predicate, agent, valid_from, valid_until) VALUES (?, ?, ?, ?, ?, ?)",
		aliceID, bobID, "knows", "test-agent", relTime, now,
	)
	if err != nil {
		t.Fatalf("insert invalidated relation: %v", err)
	}

	// Search should not find Bob via the invalidated relation
	query := &memory.KGQuery{
		SeedEntity: aliceID,
		Agent:      "test-agent",
		MaxDepth:   1,
		Limit:      10,
	}

	hits, err := store.SearchGraph(ctx, query)
	if err != nil {
		t.Fatalf("SearchGraph: %v", err)
	}

	// Should find no entities since the only relation was invalidated
	if len(hits) > 0 {
		for _, hit := range hits {
			if hit.Entity.Name == "Bob" {
				t.Error("expected not to find Bob via invalidated relation")
			}
		}
	}
}

// TestKGStore_EmptyGraph verifies that querying an empty graph returns empty results (no error).
// This is explicitly required by the spec: "Empty graph: Query on empty graph returns empty results (no error)"
func TestKGStore_EmptyGraph(t *testing.T) {
	db, cleanup := setupTestDBWithKG(t)
	defer cleanup()

	store := sqlite.NewKGStore(db)
	ctx := context.Background()

	// Query on empty graph with non-existent seed - should return empty results, not error
	query := &memory.KGQuery{
		SeedEntity: 999, // non-existent entity
		Agent:      "test-agent",
		MaxDepth:   3,
		Limit:      10,
	}

	hits, err := store.SearchGraph(ctx, query)
	if err != nil {
		t.Fatalf("SearchGraph on empty graph should not error, got: %v", err)
	}

	if len(hits) != 0 {
		t.Errorf("expected 0 hits on empty graph, got %d", len(hits))
	}
}

// TestKGStore_DedupAfterExtraction_DoesNotMergeDifferentTypes verifies that dedup
// does not merge entities of different types (e.g., "Apple" as Fruit vs Company).
// This tests the "ambiguous flagging" scenario from the spec - entities that might be
// duplicates but aren't certain should NOT be merged.
func TestKGStore_DedupAfterExtraction_DoesNotMergeDifferentTypes(t *testing.T) {
	db, cleanup := setupTestDBWithKG(t)
	defer cleanup()

	store := sqlite.NewKGStore(db)
	ctx := context.Background()

	now := time.Now().Format(time.RFC3339)

	// Insert "Apple" as both Fruit and Company - these should NOT be merged (different types)
	_, err := db.ExecContext(ctx,
		"INSERT INTO kg_entities (type, name, agent, valid_from) VALUES (?, ?, ?, ?)",
		"Fruit", "Apple", "test-agent", now,
	)
	if err != nil {
		t.Fatalf("insert Apple (Fruit) failed: %v", err)
	}

	_, err = db.ExecContext(ctx,
		"INSERT INTO kg_entities (type, name, agent, valid_from) VALUES (?, ?, ?, ?)",
		"Company", "Apple", "test-agent", now,
	)
	if err != nil {
		t.Fatalf("insert Apple (Company) failed: %v", err)
	}

	// Run dedup - should NOT merge different types
	err = store.DedupAfterExtraction(ctx, "test-agent")
	if err != nil {
		t.Fatalf("DedupAfterExtraction failed: %v", err)
	}

	// Both should still exist (different types, not ambiguous)
	var entityCount int
	err = db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM kg_entities WHERE agent = ? AND valid_until IS NULL AND name = 'Apple'",
		"test-agent",
	).Scan(&entityCount)
	if err != nil {
		t.Fatalf("query Apple entity count failed: %v", err)
	}
	if entityCount != 2 {
		t.Errorf("expected 2 Apple entities (different types) to remain, got %d", entityCount)
	}
}

// TestKGStore_SearchGraph_ByEntityName verifies that SearchGraph resolves
// SeedEntityName to an entity ID internally, enabling search by name alone.
// This tests C3 fix: name resolution inside the store layer.
func TestKGStore_SearchGraph_ByEntityName(t *testing.T) {
	db, cleanup := setupTestDBWithKG(t)
	defer cleanup()

	store := sqlite.NewKGStore(db)
	ctx := context.Background()

	now := time.Now().Format(time.RFC3339)

	// Seed: Alice knows Bob
	res1, _ := db.ExecContext(ctx,
		"INSERT INTO kg_entities (type, name, agent, valid_from) VALUES (?, ?, ?, ?)",
		"Person", "Alice", "test-agent", now,
	)
	aliceID, _ := res1.LastInsertId()

	res2, _ := db.ExecContext(ctx,
		"INSERT INTO kg_entities (type, name, agent, valid_from) VALUES (?, ?, ?, ?)",
		"Person", "Bob", "test-agent", now,
	)
	bobID, _ := res2.LastInsertId()

	_, err := db.ExecContext(ctx,
		"INSERT INTO kg_relations (from_entity, to_entity, predicate, agent, valid_from) VALUES (?, ?, ?, ?, ?)",
		aliceID, bobID, "knows", "test-agent", now,
	)
	if err != nil {
		t.Fatalf("insert relation: %v", err)
	}

	t.Run("search by entity name instead of ID", func(t *testing.T) {
		query := &memory.KGQuery{
			SeedEntityName: "Alice",
			Agent:          "test-agent",
			MaxDepth:       1,
			Limit:          10,
		}

		hits, err := store.SearchGraph(ctx, query)
		if err != nil {
			t.Fatalf("SearchGraph by name: %v", err)
		}

		if len(hits) != 1 {
			t.Fatalf("expected 1 hit (Bob), got %d", len(hits))
		}

		if hits[0].Entity.Name != "Bob" {
			t.Errorf("expected hit 'Bob', got %q", hits[0].Entity.Name)
		}
		if hits[0].Distance != 1 {
			t.Errorf("expected distance 1, got %d", hits[0].Distance)
		}
	})

	t.Run("search by name with non-existent entity returns empty", func(t *testing.T) {
		query := &memory.KGQuery{
			SeedEntityName: "NonExistent",
			Agent:          "test-agent",
			MaxDepth:       1,
			Limit:          10,
		}

		hits, err := store.SearchGraph(ctx, query)
		if err != nil {
			t.Fatalf("SearchGraph by non-existent name: %v", err)
		}
		if len(hits) != 0 {
			t.Errorf("expected 0 hits for non-existent name, got %d", len(hits))
		}
	})

	t.Run("name resolution is case-insensitive", func(t *testing.T) {
		query := &memory.KGQuery{
			SeedEntityName: "alice", // lowercase
			Agent:          "test-agent",
			MaxDepth:       1,
			Limit:          10,
		}

		hits, err := store.SearchGraph(ctx, query)
		if err != nil {
			t.Fatalf("SearchGraph by lowercase name: %v", err)
		}
		if len(hits) != 1 {
			t.Errorf("expected 1 hit for 'alice', got %d", len(hits))
		}
	})
}
