package webhooks

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/webhookfilter"
)

// StandalonePruneInterval is the standalone sweep's cadence: the dedupe
// window and the bounded delivery log prune hourly, the connection lane's
// DefaultPruneInterval posture (design.md D8).
const StandalonePruneInterval = DefaultPruneInterval

// standaloneIngestURLPath is the public ingest path the pinned URL shape
// derives from (design.md D5): /api/ingest/hooks/{workspace_slug}/
// {webhook_id}. The host is the instance's public base URL.
const standaloneIngestURLPath = "/api/ingest/hooks/"

// List row source discriminators (design.md D2/D9 — the split-brain v1):
// the pane's list merges the workspace's standalone endpoints with the
// connection-bound webhooks surfaced read-in.
const (
	StandaloneWebhookSourceStandalone = "standalone"
	StandaloneWebhookSourceConnection = "connection"
)

// ConnectionLister is the read-in's connection identity seam: the narrow
// slice of store.Connections the management list needs (AGENTS.md granular
// dependencies). *store adapters satisfy it structurally.
type ConnectionLister interface {
	List(ctx context.Context, workspaceID string) ([]domain.Connection, error)
}

// ConnectionWebhookViewSource is the connection-lane state seam the read-in
// consumes (design.md D2): *Service.StateForView satisfies it structurally —
// the exact enrichment shape the connections handler resolves through.
type ConnectionWebhookViewSource interface {
	StateForView(ctx context.Context, workspaceID, connectionID string) (state *domain.ConnectionWebhook, service string, declares bool, err error)
}

// CreateStandaloneWebhookInput is the create payload (spec: Webhook endpoint
// lifecycle). Secret blank generates one (revealed exactly once in the
// response); a caller-supplied secret (BYO) is stored and never re-revealed.
type CreateStandaloneWebhookInput struct {
	Name             string                             `json:"name"`
	Scheme           string                             `json:"scheme"`
	Secret           string                             `json:"secret,omitempty"`
	EventSource      domain.WorkspaceWebhookEventSource `json:"event_source"`
	DeliveryIDSource string                             `json:"delivery_id_source"`
	FilterExpression string                             `json:"filter_expression,omitempty"`
	TurnTemplate     string                             `json:"turn_template,omitempty"`
	SamplePayload    string                             `json:"sample_payload,omitempty"`
	TargetAgentID    string                             `json:"target_agent_id"`
	TargetKind       string                             `json:"target_kind"`
	TargetID         string                             `json:"target_id"`
	Enabled          bool                               `json:"enabled"`
}

// UpdateStandaloneWebhookInput is the full-state edit payload. It carries no
// secret material — the credential is immutable on this path and Rotate is
// its only replacement.
type UpdateStandaloneWebhookInput struct {
	Name             string                             `json:"name"`
	Scheme           string                             `json:"scheme"`
	EventSource      domain.WorkspaceWebhookEventSource `json:"event_source"`
	DeliveryIDSource string                             `json:"delivery_id_source"`
	FilterExpression string                             `json:"filter_expression,omitempty"`
	TurnTemplate     string                             `json:"turn_template,omitempty"`
	SamplePayload    string                             `json:"sample_payload,omitempty"`
	TargetAgentID    string                             `json:"target_agent_id"`
	TargetKind       string                             `json:"target_kind"`
	TargetID         string                             `json:"target_id"`
	Enabled          bool                               `json:"enabled"`
}

// StandaloneWebhookCreated is the create response: the stored endpoint plus
// the plaintext secret ONLY when server-generated (reveal-once; empty for a
// BYO secret — the hint on the entity is the residue).
type StandaloneWebhookCreated struct {
	Webhook *domain.WorkspaceWebhook
	// Secret is the generated plaintext, returned exactly once; "" for BYO.
	Secret string
}

// StandaloneSecretReveal is the rotate response: the updated endpoint plus
// the replacement plaintext, revealed exactly once (spec: Rotate invalidates
// immediately).
type StandaloneSecretReveal struct {
	Webhook *domain.WorkspaceWebhook
	Secret  string
}

// ConnectionWebhookReadIn is one connection-bound webhook's list row
// (design.md D9 — surfaced read-in, managed in Integrations). Service is the
// connection's recipe id: a connection carries no separate display name, so
// the service id is the row's label.
type ConnectionWebhookReadIn struct {
	ConnectionID string `json:"connection_id"`
	Service      string `json:"service"`
	// State is the connection's raw webhook state; the transport derives the
	// display view (BuildView) with the request-scoped workspace slug and
	// public base URL — the connections handler's exact enrichment posture.
	State *domain.ConnectionWebhook `json:"-"`
}

// StandaloneWebhookListRow is one row of the management list: a standalone
// endpoint or a connection-bound webhook read-in, discriminated by Source.
type StandaloneWebhookListRow struct {
	Source string `json:"source"`
	// Webhook is the endpoint when Source = "standalone"; nil otherwise.
	Webhook *domain.WorkspaceWebhook `json:"webhook,omitempty"`
	// Connection is the read-in summary when Source = "connection"; nil
	// otherwise.
	Connection *ConnectionWebhookReadIn `json:"connection,omitempty"`
}

// rowName is the deterministic sort key: the endpoint's name, the
// connection's service id.
func (r StandaloneWebhookListRow) rowName() string {
	if r.Source == StandaloneWebhookSourceConnection && r.Connection != nil {
		return r.Connection.Service
	}
	if r.Webhook != nil {
		return r.Webhook.Name
	}
	return ""
}

// StandaloneIngestURL derives an endpoint's public ingest URL (contract §4
// posture): workspace public base + slug + the pinned path shape — computed,
// never stored (the BuildView posture). Empty base keeps the path-only URL.
func StandaloneIngestURL(publicBaseURL, workspaceSlug, webhookID string) string {
	return strings.TrimSuffix(publicBaseURL, "/") + standaloneIngestURLPath + workspaceSlug + "/" + webhookID
}

// StandaloneService is the standalone webhook endpoint's management surface
// (add-workspace-webhooks tasks 3.3, design.md D1): create (with generated or
// BYO secret), rotate, enable/pause, delete, full-state update with
// save-time filter compilation and template validation, the dry-run tester,
// the per-endpoint delivery log, and the D2 read-in list. Every write runs
// through the WorkspaceWebhookStore port; compiled expressions stay out of
// the entity (compile per operation — save, dry-run, delivery).
type StandaloneService struct {
	webhooks        store.WorkspaceWebhookStore
	secrets         SecretCipher
	connections     ConnectionLister
	connectionViews ConnectionWebhookViewSource
	now             func() time.Time
}

// StandaloneServiceOption customizes a StandaloneService.
type StandaloneServiceOption func(*StandaloneService)

// WithStandaloneServiceClock pins the prune loop's clock (tests).
func WithStandaloneServiceClock(now func() time.Time) StandaloneServiceOption {
	return func(s *StandaloneService) {
		if now != nil {
			s.now = now
		}
	}
}

// NewStandaloneService builds the management service. Dependencies are
// granular and positional (AGENTS.md): the endpoint port (the only state
// path), the secret cipher, and the two narrow connection seams the D2
// read-in needs — every one required, resolved by the composition root,
// never nil.
func NewStandaloneService(
	webhooks store.WorkspaceWebhookStore,
	secrets SecretCipher,
	connections ConnectionLister,
	connectionViews ConnectionWebhookViewSource,
	opts ...StandaloneServiceOption,
) *StandaloneService {
	s := &StandaloneService{
		webhooks:        webhooks,
		secrets:         secrets,
		connections:     connections,
		connectionViews: connectionViews,
		now:             time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Create persists a new endpoint (spec: Create with generated /
// bring-your-own secret). Order matters: the save-time filter compilation
// and template validation run BEFORE any secret work and before anything
// persists — an invalid expression rejects with the parser's message and
// leaves no row (spec: Invalid expression rejected at save). The generated
// plaintext rides the response exactly once; a BYO secret is stored and
// never re-revealed.
func (s *StandaloneService) Create(ctx context.Context, workspaceID string, in CreateStandaloneWebhookInput) (StandaloneWebhookCreated, error) {
	wh := &domain.WorkspaceWebhook{
		WorkspaceID:      workspaceID,
		Name:             in.Name,
		Scheme:           in.Scheme,
		EventSource:      in.EventSource,
		DeliveryIDSource: in.DeliveryIDSource,
		FilterExpression: in.FilterExpression,
		TurnTemplate:     in.TurnTemplate,
		SamplePayload:    in.SamplePayload,
		TargetAgentID:    in.TargetAgentID,
		TargetKind:       in.TargetKind,
		TargetID:         in.TargetID,
		Enabled:          in.Enabled,
	}
	if err := compileFilterForSave(wh.FilterExpression); err != nil {
		return StandaloneWebhookCreated{}, err
	}
	if err := ValidateTemplateForSave(wh); err != nil {
		return StandaloneWebhookCreated{}, err
	}

	created := StandaloneWebhookCreated{Webhook: wh}
	if strings.TrimSpace(in.Secret) == "" {
		plaintext, envelope, hint, err := s.mintSecret(workspaceID)
		if err != nil {
			return StandaloneWebhookCreated{}, err
		}
		wh.SecretCiphertext = envelope
		wh.SecretHint = hint
		created.Secret = plaintext // reveal-once
	} else {
		envelope, err := s.secrets.EncryptSecret(workspaceID, []byte(in.Secret))
		if err != nil {
			return StandaloneWebhookCreated{}, fmt.Errorf("standalone webhook service: seal secret: %w", err)
		}
		wh.SecretCiphertext = envelope
		wh.SecretHint = SecretHint(in.Secret)
		// BYO: never re-revealed — the hint is the residue.
	}

	if err := wh.Validate(); err != nil {
		return StandaloneWebhookCreated{}, err
	}
	if err := s.webhooks.Create(ctx, wh); err != nil {
		return StandaloneWebhookCreated{}, fmt.Errorf("standalone webhook service: create: %w", err)
	}
	return created, nil
}

// Rotate mints a replacement secret — the prior one invalid the moment the
// envelope write lands (spec: Rotate invalidates immediately) — and returns
// the plaintext exactly once.
func (s *StandaloneService) Rotate(ctx context.Context, workspaceID, id string) (StandaloneSecretReveal, error) {
	wh, err := s.loadForWrite(ctx, workspaceID, id)
	if err != nil {
		return StandaloneSecretReveal{}, err
	}
	plaintext, envelope, hint, err := s.mintSecret(workspaceID)
	if err != nil {
		return StandaloneSecretReveal{}, err
	}
	wh.SecretCiphertext = envelope
	wh.SecretHint = hint
	if err := wh.Validate(); err != nil {
		return StandaloneSecretReveal{}, err
	}
	if err := s.webhooks.Update(ctx, wh); err != nil {
		return StandaloneSecretReveal{}, fmt.Errorf("standalone webhook service: rotate: %w", err)
	}
	return StandaloneSecretReveal{Webhook: wh, Secret: plaintext}, nil
}

// SetEnabled pauses or resumes ingestion (spec: Paused endpoint ignores
// deliveries). Disabling leaves every other field intact; enabling requires
// the credential envelope, which domain.Validate enforces at the write.
func (s *StandaloneService) SetEnabled(ctx context.Context, workspaceID, id string, enabled bool) (*domain.WorkspaceWebhook, error) {
	wh, err := s.loadForWrite(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	wh.Enabled = enabled
	if err := wh.Validate(); err != nil {
		return nil, err
	}
	if err := s.webhooks.Update(ctx, wh); err != nil {
		return nil, fmt.Errorf("standalone webhook service: set enabled: %w", err)
	}
	return wh, nil
}

// Delete removes the endpoint; its dedupe rows and delivery log die with it
// (store contract).
func (s *StandaloneService) Delete(ctx context.Context, workspaceID, id string) error {
	if err := s.webhooks.Delete(ctx, workspaceID, id); err != nil {
		return fmt.Errorf("standalone webhook service: delete: %w", err)
	}
	return nil
}

// Update applies the full-state edit. The credential is immutable on this
// path — the loaded envelope and hint ride the write unchanged, and Rotate
// is the only secret replacement. The save-time checks run against the
// CANDIDATE state before the write: a filter parse error returns
// domain.ErrInvalid with the parser's message and the prior expression
// stays in force (spec: Invalid expression rejected at save).
func (s *StandaloneService) Update(ctx context.Context, workspaceID, id string, in UpdateStandaloneWebhookInput) (*domain.WorkspaceWebhook, error) {
	wh, err := s.loadForWrite(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	wh.Name = in.Name
	wh.Scheme = in.Scheme
	wh.EventSource = in.EventSource
	wh.DeliveryIDSource = in.DeliveryIDSource
	wh.FilterExpression = in.FilterExpression
	wh.TurnTemplate = in.TurnTemplate
	wh.SamplePayload = in.SamplePayload
	wh.TargetAgentID = in.TargetAgentID
	wh.TargetKind = in.TargetKind
	wh.TargetID = in.TargetID
	wh.Enabled = in.Enabled

	if err := compileFilterForSave(wh.FilterExpression); err != nil {
		return nil, err
	}
	if err := ValidateTemplateForSave(wh); err != nil {
		return nil, err
	}
	if err := wh.Validate(); err != nil {
		return nil, err
	}
	if err := s.webhooks.Update(ctx, wh); err != nil {
		return nil, fmt.Errorf("standalone webhook service: update: %w", err)
	}
	return wh, nil
}

// Get reads one endpoint (no secret material — the hint only).
func (s *StandaloneService) Get(ctx context.Context, workspaceID, id string) (*domain.WorkspaceWebhook, error) {
	wh, err := s.webhooks.ByID(ctx, workspaceID, id)
	if err != nil {
		return nil, fmt.Errorf("standalone webhook service: read endpoint: %w", err)
	}
	return wh, nil
}

// List returns the management list (design.md D2/D9): the workspace's
// standalone endpoints PLUS the connection-bound webhooks surfaced read-in
// and flagged source "connection" (their management lives in Integrations).
// Sorted deterministically by name (endpoint name, connection service id).
// A read-in enrichment failure degrades to skipping that row — the read-in
// is a surface convenience and must never blank the list (the connections
// handler's enrichWithWebhook posture).
func (s *StandaloneService) List(ctx context.Context, workspaceID string) ([]StandaloneWebhookListRow, error) {
	endpoints, err := s.webhooks.List(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("standalone webhook service: list: %w", err)
	}
	rows := make([]StandaloneWebhookListRow, 0, len(endpoints))
	for _, wh := range endpoints {
		rows = append(rows, StandaloneWebhookListRow{
			Source:  StandaloneWebhookSourceStandalone,
			Webhook: wh,
		})
	}

	conns, err := s.connections.List(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("standalone webhook service: list connections: %w", err)
	}
	for i := range conns {
		conn := &conns[i]
		state, service, declares, err := s.connectionViews.StateForView(ctx, workspaceID, conn.ID)
		if err != nil {
			slog.WarnContext(ctx, "standalone webhook list: connection read-in failed (row skipped)",
				"workspace_id", workspaceID, "connection_id", conn.ID, "error", err)
			continue
		}
		if !declares {
			continue
		}
		rows = append(rows, StandaloneWebhookListRow{
			Source: StandaloneWebhookSourceConnection,
			Connection: &ConnectionWebhookReadIn{
				ConnectionID: conn.ID,
				Service:      service,
				State:        state,
			},
		})
	}

	sort.Slice(rows, func(a, b int) bool {
		nameA, nameB := rows[a].rowName(), rows[b].rowName()
		if nameA != nameB {
			return nameA < nameB
		}
		return rowID(rows[a]) < rowID(rows[b])
	})
	return rows, nil
}

// rowID is the deterministic tie-break key (endpoint id, connection id).
func rowID(r StandaloneWebhookListRow) string {
	if r.Source == StandaloneWebhookSourceConnection && r.Connection != nil {
		return r.Connection.ConnectionID
	}
	if r.Webhook != nil {
		return r.Webhook.ID
	}
	return ""
}

// DryRun evaluates a supplied sample payload against the endpoint's saved
// expression and reports match, ignored, or error — no side effects, no
// store writes, no enqueueing (spec: Dry-run reports the outcome). A blank
// expression matches everything (the delivery path never evaluates one).
// The dry-run environment's event value resolves from the endpoint's
// declared source exactly as a delivery's does — see
// standaloneDryRunEvent for the header-source sample convention.
func (s *StandaloneService) DryRun(ctx context.Context, workspaceID, id string, samplePayloadJSON []byte) (webhookfilter.Verdict, error) {
	wh, err := s.webhooks.ByID(ctx, workspaceID, id)
	if err != nil {
		return webhookfilter.Verdict{}, fmt.Errorf("standalone webhook service: read endpoint: %w", err)
	}
	if strings.TrimSpace(wh.FilterExpression) == "" {
		return webhookfilter.Verdict{Outcome: webhookfilter.VerdictMatch}, nil
	}
	compiled, err := webhookfilter.Compile(wh.FilterExpression)
	if err != nil {
		// Save-time validation makes a stored expression that fails to
		// compile unreachable short of corrupt state; surface the compile
		// failure rather than fabricating a verdict.
		return webhookfilter.Verdict{}, fmt.Errorf("standalone webhook service: compile stored expression: %w", err)
	}
	return webhookfilter.DryRun(compiled, samplePayloadJSON, standaloneDryRunEvent(wh, samplePayloadJSON)), nil
}

// standaloneDryRunEvent resolves the dry-run environment's event value the
// way the delivery path resolves it (resolveStandaloneEvent), with the
// sample payload standing in for the delivery body: the fixed value, the
// sample's field, or — for a header source, whose value cannot ride a JSON
// sample — the sample's optional top-level `event` key (the filter engine's
// own `event`-wins convention), empty otherwise.
func standaloneDryRunEvent(wh *domain.WorkspaceWebhook, sample []byte) string {
	switch wh.EventSource.Kind {
	case domain.WorkspaceWebhookEventSourceFixed:
		return wh.EventSource.Value
	case domain.WorkspaceWebhookEventSourcePayloadField:
		value, _ := payloadFieldString(sample, wh.EventSource.Path)
		return value
	default:
		value, _ := payloadFieldString(sample, "event")
		return value
	}
}

// DeliveryLog reads the endpoint's bounded delivery history (design.md D8 —
// "did the sender's event reach the agent, and if not, why not"), newest
// first.
func (s *StandaloneService) DeliveryLog(ctx context.Context, workspaceID, id string, limit int) ([]domain.WebhookDeliveryLogEntry, error) {
	entries, err := s.webhooks.ListDeliveryLog(ctx, workspaceID, id, limit)
	if err != nil {
		return nil, fmt.Errorf("standalone webhook service: delivery log: %w", err)
	}
	return entries, nil
}

// WorkspaceRuns reads the workspace-wide webhook run listing
// (add-webhook-channel-visibility 4.4 — the run-history feed), newest
// first, bounded at limit rows (limit <= 0 means the domain default).
func (s *StandaloneService) WorkspaceRuns(ctx context.Context, workspaceID string, limit int) ([]domain.WorkspaceWebhookRun, error) {
	runs, err := s.webhooks.ListRuns(ctx, workspaceID, limit)
	if err != nil {
		return nil, fmt.Errorf("standalone webhook service: workspace runs: %w", err)
	}
	return runs, nil
}

// PruneOnce runs both store prunes — the dedupe window's expired rows and
// the delivery-log cap (design.md D4/D8).
func (s *StandaloneService) PruneOnce(ctx context.Context, now time.Time) error {
	if err := s.webhooks.PruneDeliveries(ctx, now.Add(-domain.WorkspaceWebhookDeliveryWindow)); err != nil {
		return fmt.Errorf("standalone webhook service: prune deliveries: %w", err)
	}
	if err := s.webhooks.PruneDeliveryLog(ctx, now); err != nil {
		return fmt.Errorf("standalone webhook service: prune delivery log: %w", err)
	}
	return nil
}

// PruneLoop runs the window cleanup on StandalonePruneInterval until the
// context is cancelled (the composition root's lifecycle context) — the
// connection lane's PruneLoop shape.
func (s *StandaloneService) PruneLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = StandalonePruneInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.PruneOnce(ctx, s.now()); err != nil {
				slog.DebugContext(ctx, "standalone webhook prune failed", "error", err)
			}
		}
	}
}

// loadForWrite is the shape every mutating operation shares: load the
// endpoint through the store's workspace-scoped read (foreign-workspace ids
// are ErrNotFound) so the full-state writes never clobber another
// workspace's row.
func (s *StandaloneService) loadForWrite(ctx context.Context, workspaceID, id string) (*domain.WorkspaceWebhook, error) {
	wh, err := s.webhooks.ByID(ctx, workspaceID, id)
	if err != nil {
		return nil, fmt.Errorf("standalone webhook service: read endpoint: %w", err)
	}
	return wh, nil
}

// mintSecret generates the secret and its stored halves: the workspace-AAD
// envelope and the display-once hint (the connection lane's mintSecret
// shape).
func (s *StandaloneService) mintSecret(workspaceID string) (plaintext, envelope, hint string, err error) {
	plaintext, err = GenerateSecret()
	if err != nil {
		return "", "", "", fmt.Errorf("standalone webhook service: generate secret: %w", err)
	}
	envelope, err = s.secrets.EncryptSecret(workspaceID, []byte(plaintext))
	if err != nil {
		return "", "", "", fmt.Errorf("standalone webhook service: seal secret: %w", err)
	}
	return plaintext, envelope, SecretHint(plaintext), nil
}

// compileFilterForSave is the save-time filter gate (design.md D3): a blank
// expression compiles trivially; a non-blank one must parse, sandbox-clean,
// and boolean-type — a failure wraps domain.ErrInvalid with the parser's own
// message verbatim (spec: "rejected with the parser's message").
func compileFilterForSave(expression string) error {
	if strings.TrimSpace(expression) == "" {
		return nil
	}
	if _, err := webhookfilter.Compile(expression); err != nil {
		return fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	return nil
}
