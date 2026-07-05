package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/oniharnantyo/onclaw/internal/memory"
)

// sqliteKGStore implements memory.KGStore backed by SQLite.
type sqliteKGStore struct {
	db *sql.DB
}

// NewKGStore creates a new KGStore backed by SQLite.
func NewKGStore(db *sql.DB) memory.KGStore {
	return &sqliteKGStore{db: db}
}

// normalizeName lowercases and trims whitespace for entity name comparison.
func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// IngestExtraction ingests entities and relations from an episodic summary.
// Sets valid_from = now. Relations carry entity names (FromName/ToName) from the
// LLM extractor; this method resolves them to entity IDs, creating new entities
// for any names not yet in the graph. If a contradictory relation exists, it
// supersedes the old one (sets valid_until = now) and inserts a new row.
func (s *sqliteKGStore) IngestExtraction(ctx context.Context, ext *memory.Extraction) error {
	now := time.Now().Format(time.RFC3339)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 1. Insert entities, collecting name → ID mapping
	nameToID := make(map[string]int64)
	for _, ent := range ext.Entities {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO kg_entities (type, name, agent, valid_from)
			 VALUES (?, ?, ?, ?)`,
			ent.Type, ent.Name, ext.Agent, now,
		)
		if err != nil {
			return fmt.Errorf("insert entity: %w", err)
		}
		id, _ := res.LastInsertId()
		// key by (type, name) to distinguish same name different types
		nameToID[nameKey(ent.Type, ent.Name)] = id
	}

	// 2. Resolve relation endpoints and ensure every referenced entity exists
	type resolvedRel struct {
		FromEntity int64
		ToEntity   int64
		Predicate  string
	}
	resolved := make([]resolvedRel, 0, len(ext.Relations))
	for _, rel := range ext.Relations {
		fromID, toID := rel.FromEntity, rel.ToEntity

		// If caller already set integer IDs, use those directly.
		// Otherwise resolve from names.
		if fromID == 0 {
			fromName := normalizeName(rel.FromName)
			if fromName == "" {
				continue
			}
			fromID = s.resolveEntityID(ctx, tx, ext.Agent, fromName, now)
		}
		if toID == 0 {
			toName := normalizeName(rel.ToName)
			if toName == "" {
				continue
			}
			toID = s.resolveEntityID(ctx, tx, ext.Agent, toName, now)
		}
		if fromID == 0 || toID == 0 {
			continue
		}

		resolved = append(resolved, resolvedRel{
			FromEntity: fromID,
			ToEntity:   toID,
			Predicate:  rel.Predicate,
		})
	}

	// 3. Insert resolved relations with supersession
	for _, rel := range resolved {
		// Supersede any existing current relations between same endpoints
		_, err := tx.ExecContext(ctx,
			`UPDATE kg_relations
			 SET valid_until = ?
			 WHERE from_entity = ? AND to_entity = ? AND agent = ? AND valid_until IS NULL`,
			now, rel.FromEntity, rel.ToEntity, ext.Agent,
		)
		if err != nil {
			return fmt.Errorf("supersede old relation: %w", err)
		}

		// Insert the new relation
		_, err = tx.ExecContext(ctx,
			`INSERT INTO kg_relations (from_entity, to_entity, predicate, agent, valid_from)
			 VALUES (?, ?, ?, ?, ?)`,
			rel.FromEntity, rel.ToEntity, rel.Predicate, ext.Agent, now,
		)
		if err != nil {
			return fmt.Errorf("insert relation: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

// resolveEntityID finds or creates an entity by normalized name for the given agent.
// It first checks the in-transaction name map, then queries the DB, and finally
// creates a new entity if none is found.
func (s *sqliteKGStore) resolveEntityID(ctx context.Context, tx *sql.Tx, agent, name, now string) int64 {
	// Check existing entities (case-insensitive match)
	var id int64
	err := tx.QueryRowContext(ctx,
		`SELECT id FROM kg_entities
		 WHERE agent = ? AND LOWER(TRIM(name)) = LOWER(TRIM(?)) AND valid_until IS NULL
		 ORDER BY id ASC LIMIT 1`,
		agent, name,
	).Scan(&id)
	if err == nil {
		return id
	}

	// Create a new entity for this name with type "Unknown"
	res, err := tx.ExecContext(ctx,
		`INSERT INTO kg_entities (type, name, agent, valid_from)
		 VALUES ('Unknown', ?, ?, ?)`,
		name, agent, now,
	)
	if err != nil {
		return 0
	}
	id, _ = res.LastInsertId()
	return id
}

// nameKey builds a composite key for entity lookup.
func nameKey(typ, name string) string {
	return typ + "::" + strings.ToLower(strings.TrimSpace(name))
}

// resolveName resolves an entity name to an ID using the name map or DB.
// Not currently used directly but available for future resolution.
func (s *sqliteKGStore) resolveName(ctx context.Context, tx *sql.Tx, agent, name string) int64 {
	return s.resolveEntityID(ctx, tx, agent, name, time.Now().Format(time.RFC3339))
}

// DedupAfterExtraction merges semantically-equivalent entities
// (same type + normalized name) and re-points their relations.
// Entities with the same name but different types are left unmerged — they may
// be ambiguous (e.g. "Apple" as Fruit vs Company) and need a human review
// surface to resolve, which is a future enhancement.
func (s *sqliteKGStore) DedupAfterExtraction(ctx context.Context, agentID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().Format(time.RFC3339)

	// Find groups of duplicate entities by (agent, type, normalized_name)
	// We want to keep the oldest entity as the canonical one and merge others into it.
	query := `
		SELECT type, LOWER(TRIM(name)) as norm_name, MIN(id) as canonical_id, COUNT(*) as cnt
		FROM kg_entities
		WHERE agent = ? AND valid_until IS NULL
		GROUP BY type, norm_name
		HAVING cnt > 1
	`
	rows, err := tx.QueryContext(ctx, query, agentID)
	if err != nil {
		return fmt.Errorf("query duplicates: %w", err)
	}
	defer rows.Close()

	var mergeGroups []struct {
		Type        string
		NormName    string
		CanonicalID int64
		Count       int
	}

	for rows.Next() {
		var g struct {
			Type        string
			NormName    string
			CanonicalID int64
			Count       int
		}
		if err := rows.Scan(&g.Type, &g.NormName, &g.CanonicalID, &g.Count); err != nil {
			return fmt.Errorf("scan duplicate group: %w", err)
		}
		mergeGroups = append(mergeGroups, g)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate duplicate groups: %w", err)
	}

	// For each group of duplicates, re-point relations and invalidate duplicates
	for _, group := range mergeGroups {
		// Find all duplicate entity IDs for this group
		dupQuery := `
			SELECT id FROM kg_entities
			WHERE agent = ? AND type = ? AND LOWER(TRIM(name)) = ? AND valid_until IS NULL AND id != ?
		`
		dupRows, err := tx.QueryContext(ctx, dupQuery, agentID, group.Type, group.NormName, group.CanonicalID)
		if err != nil {
			return fmt.Errorf("query duplicate ids: %w", err)
		}

		var dupIDs []int64
		for dupRows.Next() {
			var id int64
			if err := dupRows.Scan(&id); err != nil {
				dupRows.Close()
				return fmt.Errorf("scan duplicate id: %w", err)
			}
			dupIDs = append(dupIDs, id)
		}
		dupRows.Close()
		if err := dupRows.Err(); err != nil {
			return fmt.Errorf("iterate duplicate ids: %w", err)
		}

		// Re-point relations from duplicate entities to canonical entity
		// We need to invalidate old relations and create new ones, consolidating duplicates
		for _, dupID := range dupIDs {
			// Find all relations from this duplicate entity
			relQuery := `
				SELECT id, to_entity, predicate
				FROM kg_relations
				WHERE from_entity = ? AND valid_until IS NULL
			`
			relRows, err := tx.QueryContext(ctx, relQuery, dupID)
			if err != nil {
				return fmt.Errorf("query relations from duplicate: %w", err)
			}

			for relRows.Next() {
				var relID, toEntity int64
				var predicate string
				if err := relRows.Scan(&relID, &toEntity, &predicate); err != nil {
					relRows.Close()
					return fmt.Errorf("scan relation from duplicate: %w", err)
				}

				// Invalidate old relation
				_, err = tx.ExecContext(ctx,
					`UPDATE kg_relations SET valid_until = ? WHERE id = ?`,
					now, relID,
				)
				if err != nil {
					relRows.Close()
					return fmt.Errorf("invalidate old relation: %w", err)
				}

				// Check if a relation from canonical to target with same predicate already exists
				var existingRelID int64
				checkErr := tx.QueryRowContext(ctx,
					`SELECT id FROM kg_relations
					 WHERE from_entity = ? AND to_entity = ? AND predicate = ? AND agent = ? AND valid_until IS NULL`,
					group.CanonicalID, toEntity, predicate, agentID,
				).Scan(&existingRelID)

				if checkErr == sql.ErrNoRows {
					// No existing relation, create a new one
					_, err = tx.ExecContext(ctx,
						`INSERT INTO kg_relations (from_entity, to_entity, predicate, agent, valid_from)
						 VALUES (?, ?, ?, ?, ?)`,
						group.CanonicalID, toEntity, predicate, agentID, now,
					)
					if err != nil {
						relRows.Close()
						return fmt.Errorf("create new relation: %w", err)
					}
				} else if checkErr != nil {
					relRows.Close()
					return fmt.Errorf("check existing relation: %w", checkErr)
				}
				// If relation already exists, we just skip creating a duplicate
			}
			relRows.Close()

			// Find all relations to this duplicate entity
			toRelQuery := `
				SELECT id, from_entity, predicate
				FROM kg_relations
				WHERE to_entity = ? AND valid_until IS NULL
			`
			toRelRows, err := tx.QueryContext(ctx, toRelQuery, dupID)
			if err != nil {
				return fmt.Errorf("query relations to duplicate: %w", err)
			}

			for toRelRows.Next() {
				var relID, fromEntity int64
				var predicate string
				if err := toRelRows.Scan(&relID, &fromEntity, &predicate); err != nil {
					toRelRows.Close()
					return fmt.Errorf("scan relation to duplicate: %w", err)
				}

				// Invalidate old relation
				_, err = tx.ExecContext(ctx,
					`UPDATE kg_relations SET valid_until = ? WHERE id = ?`,
					now, relID,
				)
				if err != nil {
					toRelRows.Close()
					return fmt.Errorf("invalidate old to-relation: %w", err)
				}

				// Check if a relation from source to canonical with same predicate already exists
				var existingRelID int64
				checkErr := tx.QueryRowContext(ctx,
					`SELECT id FROM kg_relations
					 WHERE from_entity = ? AND to_entity = ? AND predicate = ? AND agent = ? AND valid_until IS NULL`,
					fromEntity, group.CanonicalID, predicate, agentID,
				).Scan(&existingRelID)

				if checkErr == sql.ErrNoRows {
					// No existing relation, create a new one
					_, err = tx.ExecContext(ctx,
						`INSERT INTO kg_relations (from_entity, to_entity, predicate, agent, valid_from)
						 VALUES (?, ?, ?, ?, ?)`,
						fromEntity, group.CanonicalID, predicate, agentID, now,
					)
					if err != nil {
						toRelRows.Close()
						return fmt.Errorf("create new to-relation: %w", err)
					}
				} else if checkErr != nil {
					toRelRows.Close()
					return fmt.Errorf("check existing to-relation: %w", checkErr)
				}
				// If relation already exists, we just skip creating a duplicate
			}
			toRelRows.Close()

			// Invalidate the duplicate entity
			_, err = tx.ExecContext(ctx,
				`UPDATE kg_entities SET valid_until = ? WHERE id = ?`,
				now, dupID,
			)
			if err != nil {
				return fmt.Errorf("invalidate duplicate entity: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

// SearchGraph traverses the knowledge graph via recursive CTE.
// Returns entities connected to the seed within max_depth hops.
// Scoped to the agent. Returns paths of relations.
// If SeedEntity is 0 and SeedEntityName is set, resolves the name to an ID first.
func (s *sqliteKGStore) SearchGraph(ctx context.Context, query *memory.KGQuery) ([]memory.KGHit, error) {
	// Resolve seed entity by name if no direct ID given
	seedID := query.SeedEntity
	if seedID == 0 && query.SeedEntityName != "" {
		normName := normalizeName(query.SeedEntityName)
		err := s.db.QueryRowContext(ctx,
			`SELECT id FROM kg_entities
			 WHERE agent = ? AND LOWER(TRIM(name)) = ? AND valid_until IS NULL
			 ORDER BY id ASC LIMIT 1`,
			query.Agent, normName,
		).Scan(&seedID)
		if err != nil {
			// Entity not found — empty result, not an error
			return nil, nil
		}
	}

	// Use a recursive CTE to traverse the graph up to max_depth
	// The CTE returns (entity_id, relation_id, from_entity, to_entity, predicate, depth, path)
	// where path is a comma-separated list of relation IDs for reconstruction.

	q := `
	WITH RECURSIVE cte_graph AS (
		-- Base case: direct relations from seed
		SELECT
			r.to_entity as entity_id,
			r.id as relation_id,
			r.from_entity,
			r.to_entity,
			r.predicate,
			1 as depth,
			CAST(r.id AS TEXT) as path_ids
		FROM kg_relations r
		WHERE r.from_entity = ? AND r.agent = ? AND r.valid_until IS NULL

		UNION ALL

		-- Recursive case: follow relations from current depth
		SELECT
			next_r.to_entity as entity_id,
			next_r.id as relation_id,
			next_r.from_entity,
			next_r.to_entity,
			next_r.predicate,
			cte_graph.depth + 1 as depth,
			cte_graph.path_ids || ',' || CAST(next_r.id AS TEXT) as path_ids
		FROM cte_graph
		JOIN kg_relations next_r ON cte_graph.entity_id = next_r.from_entity
		WHERE next_r.agent = ? AND next_r.valid_until IS NULL AND cte_graph.depth < ?
	)
	SELECT DISTINCT entity_id, relation_id, from_entity, to_entity, predicate, depth, path_ids
	FROM cte_graph
	`

	var rows *sql.Rows
	var err error
	if query.Limit > 0 {
		rows, err = s.db.QueryContext(ctx, q, seedID, query.Agent, query.Agent, query.MaxDepth, query.Limit)
	} else {
		rows, err = s.db.QueryContext(ctx, q, seedID, query.Agent, query.Agent, query.MaxDepth)
	}
	if err != nil {
		return nil, fmt.Errorf("query graph traversal: %w", err)
	}
	defer rows.Close()

	// Collect results
	type graphResult struct {
		EntityID   int64
		RelationID int64
		FromEntity int64
		ToEntity   int64
		Predicate  string
		Depth      int
		PathIDs    string
	}

	var results []graphResult
	for rows.Next() {
		var gr graphResult
		if err := rows.Scan(&gr.EntityID, &gr.RelationID, &gr.FromEntity, &gr.ToEntity, &gr.Predicate, &gr.Depth, &gr.PathIDs); err != nil {
			return nil, fmt.Errorf("scan graph result: %w", err)
		}
		results = append(results, gr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate graph results: %w", err)
	}

	// Convert results to KGHit by fetching entity details and reconstructing paths
	var hits []memory.KGHit
	for _, gr := range results {
		// Fetch the target entity
		var ent memory.Entity
		err := s.db.QueryRowContext(ctx,
			`SELECT id, type, name, agent, valid_from, valid_until
			 FROM kg_entities
			 WHERE id = ?`,
			gr.EntityID,
		).Scan(&ent.ID, &ent.Type, &ent.Name, &ent.Agent, &ent.ValidFrom, &ent.ValidUntil)
		if err != nil {
			return nil, fmt.Errorf("fetch entity %d: %w", gr.EntityID, err)
		}

		// Reconstruct path from comma-separated relation IDs
		pathIDs := strings.Split(gr.PathIDs, ",")
		var path []memory.Relation
		for _, pathIDStr := range pathIDs {
			var pathID int64
			if _, err := fmt.Sscanf(strings.TrimSpace(pathIDStr), "%d", &pathID); err != nil {
				continue // skip invalid IDs
			}

			var rel memory.Relation
			err := s.db.QueryRowContext(ctx,
				`SELECT id, from_entity, to_entity, predicate, agent, valid_from, valid_until
				 FROM kg_relations
				 WHERE id = ?`,
				pathID,
			).Scan(&rel.ID, &rel.FromEntity, &rel.ToEntity, &rel.Predicate, &rel.Agent, &rel.ValidFrom, &rel.ValidUntil)
			if err != nil {
				return nil, fmt.Errorf("fetch relation %d in path: %w", pathID, err)
			}
			path = append(path, rel)
		}

		hits = append(hits, memory.KGHit{
			Entity:   &ent,
			Path:     path,
			Distance: gr.Depth,
		})
	}

	return hits, nil
}
