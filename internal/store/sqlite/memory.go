package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
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
		`INSERT INTO memory_documents (agent, scope, kind, content, source, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		doc.Agent, doc.Scope, doc.Kind, doc.Content, doc.Source, t,
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
		`SELECT id, agent, scope, kind, content, source, created_at
		 FROM memory_documents WHERE id = ?`,
		id,
	).Scan(&doc.ID, &doc.Agent, &doc.Scope, &doc.Kind, &doc.Content, &doc.Source, &doc.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get document: %w", err)
	}
	return &doc, nil
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
				SELECT d.id, d.agent, d.scope, d.kind, d.content, d.source, d.created_at, e.vector, 0.0 as rank
				FROM memory_documents d
				LEFT JOIN memory_embeddings e ON d.id = e.document_id
				WHERE d.agent = ? AND (d.scope = ? OR d.scope = 'global')
			`
			rows, err = s.db.QueryContext(ctx, q, query.Agent, query.Scope)
		} else {
			q := `
				SELECT d.id, d.agent, d.scope, d.kind, d.content, d.source, d.created_at, e.vector, fts.rank
				FROM memory_documents d
				JOIN memory_documents_fts fts ON d.id = fts.rowid
				LEFT JOIN memory_embeddings e ON d.id = e.document_id
				WHERE d.agent = ? AND (d.scope = ? OR d.scope = 'global') AND fts.content MATCH ?
			`
			rows, err = s.db.QueryContext(ctx, q, query.Agent, query.Scope, sanitized)
		}
	} else {
		q := `
			SELECT d.id, d.agent, d.scope, d.kind, d.content, d.source, d.created_at, e.vector, 0.0 as rank
			FROM memory_documents d
			LEFT JOIN memory_embeddings e ON d.id = e.document_id
			WHERE d.agent = ? AND (d.scope = ? OR d.scope = 'global')
		`
		rows, err = s.db.QueryContext(ctx, q, query.Agent, query.Scope)
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
			&doc.Content, &doc.Source, &doc.CreatedAt,
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
			Document: &doc,
			Vector:   vec,
			FTSRank:  rank,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("candidates rows error: %w", err)
	}

	return memory.RankCandidates(candidates, query)
}

func (s *sqliteMemoryStore) GetCachedEmbedding(ctx context.Context, contentHash string) ([]float32, error) {
	var blob []byte
	err := s.db.QueryRowContext(ctx,
		"SELECT vector FROM embedding_cache WHERE content_hash = ?",
		contentHash,
	).Scan(&blob)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get cached embedding: %w", err)
	}
	return blobToVector(blob), nil
}

func (s *sqliteMemoryStore) PutCachedEmbedding(ctx context.Context, contentHash string, vector []float32) error {
	blob := vectorToBlob(vector)
	t := now()
	_, err := s.db.ExecContext(ctx,
		"INSERT OR REPLACE INTO embedding_cache (content_hash, vector, created_at) VALUES (?, ?, ?)",
		contentHash, blob, t,
	)
	if err != nil {
		return fmt.Errorf("put cached embedding: %w", err)
	}
	return nil
}
