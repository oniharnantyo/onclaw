package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/memory"
)

type sqliteMemoryStore struct {
	db *sql.DB
}

// NewMemoryStore creates a new MemoryStore backed by SQLite.
func NewMemoryStore(db *sql.DB) memory.MemoryStore {
	return &sqliteMemoryStore{db: db}
}

func vectorToBlob(vec []float32) []byte {
	buf := make([]byte, len(vec)*4)
	for i, f := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

func blobToVector(blob []byte) []float32 {
	if len(blob)%4 != 0 {
		return nil
	}
	vec := make([]float32, len(blob)/4)
	for i := 0; i < len(vec); i++ {
		vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(blob[i*4:]))
	}
	return vec
}

func sanitizeFts(q string) string {
	q = strings.ReplaceAll(q, `"`, "")
	q = strings.ReplaceAll(q, `'`, "")
	q = strings.ReplaceAll(q, `*`, "")
	q = strings.ReplaceAll(q, `:`, "")
	words := strings.Fields(q)
	if len(words) == 0 {
		return ""
	}
	var escaped []string
	for _, w := range words {
		escaped = append(escaped, `"`+w+`*"`)
	}
	return strings.Join(escaped, " AND ")
}

func (s *sqliteMemoryStore) IndexDocument(ctx context.Context, doc *memory.MemoryDocument, vector []float32) (int64, error) {
	t := now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`INSERT INTO memory_documents (agent, scope, kind, content, source, embedding_model, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		doc.Agent, doc.Scope, doc.Kind, doc.Content, doc.Source, doc.EmbeddingModel, t,
	)
	if err != nil {
		return 0, fmt.Errorf("insert document: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("last insert id: %w", err)
	}

	if len(vector) > 0 {
		blob := vectorToBlob(vector)
		_, err = tx.ExecContext(ctx,
			`INSERT INTO memory_embeddings (document_id, vector) VALUES (?, ?)`,
			id, blob,
		)
		if err != nil {
			return 0, fmt.Errorf("insert embedding: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}

	return id, nil
}

func (s *sqliteMemoryStore) GetDocument(ctx context.Context, id int64) (*memory.MemoryDocument, error) {
	var doc memory.MemoryDocument
	err := s.db.QueryRowContext(ctx,
		`SELECT id, agent, scope, kind, content, source, embedding_model, created_at
		 FROM memory_documents WHERE id = ?`,
		id,
	).Scan(&doc.ID, &doc.Agent, &doc.Scope, &doc.Kind, &doc.Content, &doc.Source, &doc.EmbeddingModel, &doc.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get document: %w", err)
	}
	return &doc, nil
}

func (s *sqliteMemoryStore) UpdateDocument(ctx context.Context, id int64, content string, vector []float32) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`UPDATE memory_documents SET content = ? WHERE id = ?`,
		content, id,
	)
	if err != nil {
		return fmt.Errorf("update document: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("document not found: %w", sql.ErrNoRows)
	}

	if len(vector) > 0 {
		blob := vectorToBlob(vector)
		_, err = tx.ExecContext(ctx,
			`INSERT OR REPLACE INTO memory_embeddings (document_id, vector) VALUES (?, ?)`,
			id, blob,
		)
		if err != nil {
			return fmt.Errorf("update embedding: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

func (s *sqliteMemoryStore) DeleteDocument(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM memory_documents WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete document: %w", err)
	}
	return nil
}

func (s *sqliteMemoryStore) SearchArchive(ctx context.Context, query *memory.ArchiveQuery) ([]*memory.MemoryHit, error) {
	var rows *sql.Rows
	var err error

	if query.Query != "" {
		sanitized := sanitizeFts(query.Query)
		if sanitized == "" {
			q := `
				SELECT d.id, d.agent, d.scope, d.kind, d.content, d.source, d.embedding_model, d.created_at, e.vector, 0.0 as rank
				FROM memory_documents d
				LEFT JOIN memory_embeddings e ON d.id = e.document_id
				WHERE d.agent = ? AND (d.scope = ? OR d.scope = 'global') AND d.embedding_model = ?
			`
			rows, err = s.db.QueryContext(ctx, q, query.Agent, query.Scope, query.EmbeddingModel)
		} else {
			q := `
				SELECT d.id, d.agent, d.scope, d.kind, d.content, d.source, d.embedding_model, d.created_at, e.vector, fts.rank
				FROM memory_documents d
				JOIN memory_documents_fts fts ON d.id = fts.rowid
				LEFT JOIN memory_embeddings e ON d.id = e.document_id
				WHERE d.agent = ? AND (d.scope = ? OR d.scope = 'global') AND fts.content MATCH ? AND d.embedding_model = ?
			`
			rows, err = s.db.QueryContext(ctx, q, query.Agent, query.Scope, sanitized, query.EmbeddingModel)
		}
	} else {
		q := `
			SELECT d.id, d.agent, d.scope, d.kind, d.content, d.source, d.embedding_model, d.created_at, e.vector, 0.0 as rank
			FROM memory_documents d
			LEFT JOIN memory_embeddings e ON d.id = e.document_id
			WHERE d.agent = ? AND (d.scope = ? OR d.scope = 'global') AND d.embedding_model = ?
		`
		rows, err = s.db.QueryContext(ctx, q, query.Agent, query.Scope, query.EmbeddingModel)
	}

	if err != nil {
		return nil, fmt.Errorf("query candidates: %w", err)
	}
	defer rows.Close()

	var candidates []*memory.Candidate
	for rows.Next() {
		var doc memory.MemoryDocument
		var vecBlob []byte
		var rank float64
		err := rows.Scan(
			&doc.ID, &doc.Agent, &doc.Scope, &doc.Kind,
			&doc.Content, &doc.Source, &doc.EmbeddingModel, &doc.CreatedAt,
			&vecBlob, &rank,
		)
		if err != nil {
			return nil, fmt.Errorf("scan candidate: %w", err)
		}
		var vec []float32
		if len(vecBlob) > 0 {
			vec = blobToVector(vecBlob)
		}
		candidates = append(candidates, &memory.Candidate{
			Document:   &doc,
			Vector:     vec,
			FTSRank:    rank,
			MatchedFTS: true,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("candidates rows error: %w", err)
	}

	// Vector recall: union top-K vector-similar docs with the FTS candidates so a
	// semantically-relevant doc that FTS's conjunctive gate excluded still reaches
	// RankCandidates. Dedup by id preserves the FTS rank for docs matched both ways.
	if len(query.Vector) > 0 {
		vecCandidates, verr := s.vectorScanCandidates(ctx, query, vectorRecallTopK(query.Limit))
		if verr != nil {
			return nil, fmt.Errorf("vector recall: %w", verr)
		}
		candidates = unionCandidates(candidates, vecCandidates)
	}

	return memory.RankCandidates(candidates, query)
}

// vectorRecallTopK picks how many vector-similar docs to union into the candidate
// set. Scaled with the requested limit so RankCandidates has room to re-rank.
func vectorRecallTopK(limit int) int {
	k := 3 * limit
	if k < 20 {
		k = 20
	}
	if k > 100 {
		k = 100
	}
	return k
}

// vectorScanCandidates returns up to limit candidates (Document + Vector,
// MatchedFTS=false) most cosine-similar to query.Vector, filtered by the query's
// agent/scope/embedding_model. Linear scan (no ANN index) per the pure-Go driver
// constraint; acceptable for on-device corpus sizes.
func (s *sqliteMemoryStore) vectorScanCandidates(ctx context.Context, query *memory.ArchiveQuery, limit int) ([]*memory.Candidate, error) {
	if len(query.Vector) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}
	q := `
		SELECT d.id, d.agent, d.scope, d.kind, d.content, d.source, d.embedding_model, d.created_at, e.vector
		FROM memory_documents d
		JOIN memory_embeddings e ON d.id = e.document_id
		WHERE d.agent = ? AND (d.scope = ? OR d.scope = 'global') AND d.embedding_model = ?
	`
	rows, err := s.db.QueryContext(ctx, q, query.Agent, query.Scope, query.EmbeddingModel)
	if err != nil {
		return nil, fmt.Errorf("vector scan query: %w", err)
	}
	defer rows.Close()

	type scored struct {
		candidate *memory.Candidate
		cosine    float32
	}
	var all []scored
	for rows.Next() {
		var doc memory.MemoryDocument
		var blob []byte
		if err := rows.Scan(&doc.ID, &doc.Agent, &doc.Scope, &doc.Kind, &doc.Content, &doc.Source, &doc.EmbeddingModel, &doc.CreatedAt, &blob); err != nil {
			return nil, fmt.Errorf("vector scan scan: %w", err)
		}
		vec := blobToVector(blob)
		if len(vec) == 0 {
			continue
		}
		all = append(all, scored{
			candidate: &memory.Candidate{Document: &doc, Vector: vec},
			cosine:    memory.CosineSimilarity(query.Vector, vec),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vector scan rows: %w", err)
	}

	sort.Slice(all, func(i, j int) bool { return all[i].cosine > all[j].cosine })
	if len(all) > limit {
		all = all[:limit]
	}
	out := make([]*memory.Candidate, len(all))
	for i, a := range all {
		out[i] = a.candidate
	}
	return out, nil
}

// unionCandidates merges FTS-matched candidates with vector-recall candidates,
// deduplicating by document id. A doc present in both keeps its FTS rank
// (MatchedFTS=true) and gains a vector if it lacked one; a vector-only doc is
// added with MatchedFTS=false.
func unionCandidates(fts, vec []*memory.Candidate) []*memory.Candidate {
	byID := make(map[int64]*memory.Candidate, len(fts)+len(vec))
	for _, c := range fts {
		if c.Document == nil {
			continue
		}
		byID[c.Document.ID] = c
	}
	for _, c := range vec {
		if c.Document == nil {
			continue
		}
		if existing, ok := byID[c.Document.ID]; ok {
			if len(existing.Vector) == 0 {
				existing.Vector = c.Vector
			}
			continue
		}
		byID[c.Document.ID] = c
	}
	out := make([]*memory.Candidate, 0, len(byID))
	for _, c := range byID {
		out = append(out, c)
	}
	return out
}

func (s *sqliteMemoryStore) GetCachedEmbedding(ctx context.Context, embeddingModel string, contentHash string) ([]float32, error) {
	var blob []byte
	err := s.db.QueryRowContext(ctx,
		"SELECT vector FROM embedding_cache WHERE embedding_model = ? AND content_hash = ?",
		embeddingModel, contentHash,
	).Scan(&blob)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get cached embedding: %w", err)
	}
	return blobToVector(blob), nil
}

func (s *sqliteMemoryStore) PutCachedEmbedding(ctx context.Context, embeddingModel string, contentHash string, vector []float32) error {
	blob := vectorToBlob(vector)
	t := now()
	_, err := s.db.ExecContext(ctx,
		"INSERT OR REPLACE INTO embedding_cache (embedding_model, content_hash, vector, created_at) VALUES (?, ?, ?, ?)",
		embeddingModel, contentHash, blob, t,
	)
	if err != nil {
		return fmt.Errorf("put cached embedding: %w", err)
	}
	return nil
}
