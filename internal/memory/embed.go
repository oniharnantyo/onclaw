package memory

// The embedding write stage (wave3-memory-vectors-and-graph D3/D4/D5): the
// pipeline's vector channel over an openai-compatible provider's native
// embeddings endpoint. The Embedder port resolves the workspace's embedding
// config (provider + model + dimension) from the memory settings record per
// call — credentials stay tenant-scoped and re-resolve, never cached, the
// same seam the side-call lane and the settings pane's connection probe use.
// One embeddings call per text batch (D4: the endpoint accepts arrays); the
// dimension is whatever the batch actually returned (the configured one when
// set — a drift is a fail-soft error, never a silently mismatched row, D5).
// Every failure is an error to the caller; the worker owns the fail-soft
// counters. A workspace with no embedding model configured is
// ErrEmbeddingNotConfigured — both stages no-op and the world stays
// lexical-only, byte-identical to the pre-wave-3 pipeline.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// ErrEmbeddingNotConfigured marks the no-model state (D4): the workspace's
// memory settings record pins no embedding provider (or no model), so both
// embedding stages no-op. The worker treats it as a quiet no-op — never a
// failure, never a counter bump — because lexical-only is a normal world.
var ErrEmbeddingNotConfigured = errors.New("memory embed: no embedding model configured")

// embedTimeout bounds one embeddings request (the settings pane's probe
// bound); ingestion is off the hot path but must not hang a worker slot.
const embedTimeout = 10 * time.Second

// Embedder produces one vector per input text at the workspace's configured
// embedding dimension. Vectors come back in input order and share one
// length — the caller stamps that length as the rows' dimension (D5).
type Embedder interface {
	// Embed returns one vector per text, in input order.
	// ErrEmbeddingNotConfigured when the workspace pins no embedding model.
	Embed(ctx context.Context, workspaceID string, texts []string) ([][]float32, error)
}

// ProviderEmbedder is the production Embedder: the workspace's configured
// embedding provider (base URL + decrypted credential from the provider
// catalog) hit on its openai-compatible /embeddings route, one POST per
// batch. The provider registry resolves the canonical origin when the
// provider record carries no base URL — the settings pane's probe precedent.
type ProviderEmbedder struct {
	providerStore store.ProviderStore
	settings      store.ToolSettingsStore
	encKey        []byte
	registry      *providers.Registry
	client        *http.Client
}

// NewProviderEmbedder constructs the embeddings lane from its granular
// dependencies: the workspace provider catalog (endpoint + sealed
// credential), the memory settings record (which provider, which model,
// which dimension), the instance encryption key, and the provider registry
// for canonical-origin fallback.
func NewProviderEmbedder(providerStore store.ProviderStore, settings store.ToolSettingsStore, encryptionKey []byte, registry *providers.Registry) *ProviderEmbedder {
	return &ProviderEmbedder{
		providerStore: providerStore,
		settings:      settings,
		encKey:        encryptionKey,
		registry:      registry,
		client:        &http.Client{Timeout: embedTimeout},
	}
}

// embeddingConfig is the memory settings record's embedding pin.
type embeddingConfig struct {
	providerID string
	model      string
	dimension  int
}

// resolveEmbeddingConfig reads the embedding pin off the workspace's memory
// settings record (flat keys, the handler's storage convention). ok=false is
// the no-model world: either key missing or half-set — half never guesses.
func resolveEmbeddingConfig(ctx context.Context, settings store.ToolSettingsStore, workspaceID string) (embeddingConfig, bool) {
	if settings == nil {
		return embeddingConfig{}, false
	}
	row, err := settings.Get(ctx, workspaceID, "memory")
	if err != nil || row == nil || row.Config == nil {
		return embeddingConfig{}, false
	}
	cfg := embeddingConfig{}
	cfg.providerID, _ = row.Config["embedding_provider_id"].(string)
	cfg.model, _ = row.Config["embedding_model"].(string)
	switch d := row.Config["embedding_dimension"].(type) {
	case float64:
		cfg.dimension = int(d)
	case int:
		cfg.dimension = d
	case int64:
		cfg.dimension = int(d)
	}
	if cfg.providerID == "" || cfg.model == "" {
		return embeddingConfig{}, false
	}
	return cfg, true
}

// Embed resolves the workspace's embedding config and posts the whole batch
// in one openai-compatible embeddings request.
func (e *ProviderEmbedder) Embed(ctx context.Context, workspaceID string, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	cfg, ok := resolveEmbeddingConfig(ctx, e.settings, workspaceID)
	if !ok {
		return nil, ErrEmbeddingNotConfigured
	}

	provider, err := e.providerStore.ByID(ctx, workspaceID, cfg.providerID)
	if err != nil {
		return nil, fmt.Errorf("memory embed: provider: %w", err)
	}
	var apiKey string
	if provider.KeyCiphertext != "" {
		plaintext, err := secrets.Decrypt(e.encKey, []byte(workspaceID), provider.KeyCiphertext)
		if err != nil {
			return nil, errors.New("memory embed: decrypt provider credentials")
		}
		apiKey = string(plaintext)
	}
	endpoint := provider.BaseURL
	if endpoint == "" {
		impl, err := e.registry.Get(provider.Type)
		if err != nil {
			return nil, fmt.Errorf("memory embed: resolve provider endpoint: %w", err)
		}
		endpoint = impl.CanonicalOrigin()
	}

	return e.post(ctx, endpoint, apiKey, cfg.model, cfg.dimension, texts)
}

// embeddingsRequest is the openai-compatible embeddings body.
type embeddingsRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// embeddingsResponse is the openai-compatible embeddings answer; data rows
// carry their input position in index.
type embeddingsResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// post performs the one embeddings HTTP call for the batch and validates the
// answer: one vector per text, all vectors sharing one length, and — when
// the workspace pins a dimension — that length (D5: a drifted model is a
// failed stage, never a silently mismatched row).
func (e *ProviderEmbedder) post(ctx context.Context, endpoint, apiKey, model string, dimension int, texts []string) ([][]float32, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("memory embed: endpoint %q is not a valid http(s) URL", endpoint)
	}
	base := strings.TrimRight(endpoint, "/")
	if strings.HasSuffix(base, "/embeddings") {
		// The operator may already point at the full embeddings route.
		base = strings.TrimSuffix(base, "/embeddings")
	}

	body, err := json.Marshal(embeddingsRequest{Model: model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("memory embed: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("memory embed: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("memory embed: endpoint unreachable: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("memory embed: endpoint read failed: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("memory embed: endpoint returned HTTP %d: %s", resp.StatusCode, truncateEmbedReason(string(raw)))
	}

	var out embeddingsResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, errors.New("memory embed: endpoint response is not openai-compatible JSON")
	}
	if len(out.Data) != len(texts) {
		return nil, fmt.Errorf("memory embed: endpoint returned %d vectors for %d texts", len(out.Data), len(texts))
	}

	vectors := make([][]float32, len(texts))
	ordered := true
	for i := range out.Data {
		if out.Data[i].Index < 0 || out.Data[i].Index >= len(vectors) || vectors[out.Data[i].Index] != nil {
			ordered = false
			break
		}
		vectors[out.Data[i].Index] = out.Data[i].Embedding
	}
	if !ordered {
		// Indexes missing or repeated: trust the arrival order instead (the
		// openai contract returns data in input order anyway).
		for i := range out.Data {
			vectors[i] = out.Data[i].Embedding
		}
	}

	length := 0
	for i, v := range vectors {
		if len(v) == 0 {
			return nil, fmt.Errorf("memory embed: endpoint returned an empty vector for text %d", i)
		}
		if length == 0 {
			length = len(v)
		}
		if len(v) != length {
			return nil, fmt.Errorf("memory embed: batch vectors disagree in length (%d vs %d)", len(v), length)
		}
	}
	if dimension > 0 && length != dimension {
		return nil, fmt.Errorf("memory embed: endpoint returned dimension %d, configured %d", length, dimension)
	}
	return vectors, nil
}

// truncateEmbedReason bounds an endpoint error body to a log-safe one-liner
// (the probe's verbatim-reason convention, size-capped; never carries the
// request's credentials).
func truncateEmbedReason(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) > 300 {
		return s[:300]
	}
	return s
}

// ---------------------------------------------------------------------------
// The worker's embedding stages (wave3 D3/D4): fail-soft, individually
// skippable, and silent no-ops without a configured embedding model. The
// worker owns the counters; these methods own the row assembly.
// ---------------------------------------------------------------------------

// embedRow is one committed row's embedding write: the text to embed and the
// snapshot the index row carries. Note/event rows take their snapshot from
// the committed row itself; reads still take scope from the live linked row.
type embedRow struct {
	targetType    domain.MemoryTargetType
	targetID      string
	text          string
	visibility    domain.MemoryVisibility
	userID        *string
	agentID       *string
	sourceEventID string
}

// embedRawTurn is the raw-evidence stage (D3 index-before-extract): the
// turn's rendered text is embedded and upserted as one raw-index row before
// the extraction stages run, stamped with the participant-rule visibility
// snapshot and the turn's last event id as its permanent citation pointer.
// Every failure logs, bumps the embed-failure counter, and returns — the
// extraction stages still run, and a skipped row is lexical-only at worst.
func (w *Worker) embedRawTurn(ctx context.Context, job IngestJob, win gistWindow) {
	if w.rawEnabled != nil && !w.rawEnabled(ctx, job.WorkspaceID) {
		return
	}
	turn := turnEvents(win.events, job.TurnID)
	text := renderMaterial(turn)
	if strings.TrimSpace(text) == "" {
		return
	}
	sourceID := turn[len(turn)-1].EventID

	visibility, owner := participantVisibility(job)
	w.insertEmbeddings(ctx, job, []domain.MemoryEmbedding{{
		WorkspaceID:   job.WorkspaceID,
		TargetType:    domain.MemoryTargetRaw,
		TargetID:      sourceID,
		Embedding:     nil, // filled below, after the vectors come back
		Visibility:    visibility,
		UserID:        owner,
		AgentID:       ownerAgentID(visibility, job.AgentID),
		SourceEventID: sourceID,
		LearnedAt:     time.Now().UTC(),
	}}, []string{text})
}

// embedCommittedRows is the row-embedding stage (D4): the job's committed
// gist and the gate's committed notes are batch-embedded in ONE embeddings
// call and upserted with their own visibility snapshot and provenance. A
// no-embedder-configured workspace no-ops (lexical-only, byte-identical to
// the pre-wave-3 pipeline); any other failure logs and counts.
func (w *Worker) embedCommittedRows(ctx context.Context, job IngestJob, gist *domain.MemoryEvent, result GateResult) {
	if gist == nil && len(result.Notes) == 0 {
		return
	}
	rows := make([]embedRow, 0, 1+len(result.Notes))
	if gist != nil {
		text := strings.TrimSpace(gist.Description)
		if gist.Outcome != "" {
			text = strings.TrimSpace(text + "\n" + gist.Outcome)
		}
		if text != "" {
			agentID := gist.AgentID
			rows = append(rows, embedRow{
				targetType:    domain.MemoryTargetEvent,
				targetID:      gist.ID,
				text:          text,
				visibility:    gist.Visibility,
				userID:        gist.UserID,
				agentID:       ownerPtr(gist.Visibility, &agentID),
				sourceEventID: gist.SourceEventID,
			})
		}
	}
	for _, note := range result.Notes {
		if strings.TrimSpace(note.Content) == "" {
			continue
		}
		rows = append(rows, embedRow{
			targetType:    domain.MemoryTargetNote,
			targetID:      note.ID,
			text:          note.Content,
			visibility:    note.Visibility,
			userID:        note.UserID,
			agentID:       note.AgentID,
			sourceEventID: note.SourceEventID,
		})
	}
	if len(rows) == 0 {
		return
	}

	texts := make([]string, len(rows))
	indexed := make([]domain.MemoryEmbedding, len(rows))
	for i := range rows {
		texts[i] = rows[i].text
		indexed[i] = domain.MemoryEmbedding{
			WorkspaceID:   job.WorkspaceID,
			TargetType:    rows[i].targetType,
			TargetID:      rows[i].targetID,
			Visibility:    rows[i].visibility,
			UserID:        rows[i].userID,
			AgentID:       rows[i].agentID,
			SourceEventID: rows[i].sourceEventID,
			LearnedAt:     time.Now().UTC(),
		}
	}
	w.insertEmbeddings(ctx, job, indexed, texts)
}

// insertEmbeddings performs the one embeddings call for a stage's batch and
// upserts the rows at the returned per-batch dimension (D5). Shared by both
// stages: ErrEmbeddingNotConfigured is a quiet no-op (the lexical-only
// world), every other failure logs and bumps the embed-failure counter.
func (w *Worker) insertEmbeddings(ctx context.Context, job IngestJob, rows []domain.MemoryEmbedding, texts []string) {
	vectors, err := w.embedder.Embed(ctx, job.WorkspaceID, texts)
	if err != nil {
		if errors.Is(err, ErrEmbeddingNotConfigured) {
			return
		}
		w.log.Warn("memory: embedding stage failed", "workspace_id", job.WorkspaceID, "session_id", job.SessionID, "error", err)
		w.embedFailures.Add(1)
		return
	}
	if len(vectors) != len(rows) {
		// A conforming Embedder cannot do this; guarded for the port's sake.
		w.log.Warn("memory: embedding batch length mismatch", "vectors", len(vectors), "rows", len(rows))
		w.embedFailures.Add(1)
		return
	}
	for i := range rows {
		rows[i].Dimension = len(vectors[i])
		rows[i].Embedding = vectors[i]
	}
	if err := w.embeddings.InsertEmbeddings(ctx, rows); err != nil {
		w.log.Warn("memory: embedding rows rejected", "workspace_id", job.WorkspaceID, "session_id", job.SessionID, "error", err)
		w.embedFailures.Add(1)
	}
}

// ownerPtr returns p exactly when the tier is agent — the memory_events
// owner-column convention (agent_id set iff visibility=agent). For plain
// string fields like MemoryEvent.AgentID the caller passes &value.
func ownerPtr(visibility domain.MemoryVisibility, p *string) *string {
	if visibility == domain.MemoryVisibilityAgent {
		return p
	}
	return nil
}

// ownerAgentID maps the agent tier to its owner column the domain validator
// pins (set iff visibility=agent) for the raw stage's job-carried agent id.
func ownerAgentID(visibility domain.MemoryVisibility, agentID string) *string {
	if visibility == domain.MemoryVisibilityAgent {
		return &agentID
	}
	return nil
}
