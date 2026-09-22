package webhooks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// DefaultPruneInterval is the delivery-window cleanup cadence (ingress
// contract §6): the dedupe table (design.md D3) prunes rows older than
// domain.ConnectionWebhookDeliveryWindow hourly — far below the window,
// far above per-delivery work.
const DefaultPruneInterval = time.Hour

// ingestURLPath is the public ingest path the pinned URL shape derives from
// (domain recipe setup URLPathShape): /api/ingest/webhooks/{workspace_slug}/
// {connection_id}. The host is the instance's public base URL.
const ingestURLPath = "/api/ingest/webhooks/"

// Service is the webhook management surface (contract §5): enable (with
// secret generation and the one-time reveal), disable, rotate, target and
// event updates. Every write goes through the store's full-state
// UpdateState — the only webhook-column write path — and every recipe-
// scoped check rides domain.ConnectionWebhook.ValidateForRecipe. Mutations
// return the updated stored state; the HTTP layer derives the served view
// with BuildView (the ingest URL needs the request-scoped workspace slug
// and the instance's public base URL — deliberately not service state).
type Service struct {
	connections store.Connections
	webhooks    store.ConnectionWebhookStore
	agents      store.AgentStore
	channels    store.ChannelStore
	secrets     SecretCipher
	now         func() time.Time
}

// ServiceOption customizes a Service.
type ServiceOption func(*Service)

// WithServiceClock pins the prune loop's clock (tests).
func WithServiceClock(now func() time.Time) ServiceOption {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// NewService builds the management service. Dependencies are granular and
// positional (AGENTS.md): the connections port (recipe identity), the
// webhook state port (the only state path), the agent and channel stores
// (target binding validation), and the secret cipher — every one required,
// resolved by the composition root, never nil.
func NewService(
	connections store.Connections,
	webhooks store.ConnectionWebhookStore,
	agents store.AgentStore,
	channels store.ChannelStore,
	secrets SecretCipher,
	opts ...ServiceOption,
) *Service {
	s := &Service{
		connections: connections,
		webhooks:    webhooks,
		agents:      agents,
		channels:    channels,
		secrets:     secrets,
		now:         time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// EnableRequest is the enable payload (contract §5: "body: target + events"):
// the bound agent, the bound thread or channel, and the selected event
// subset. An empty event selection starts from the recipe's read-flavored
// default set (spec: service-authority attribution).
type EnableRequest struct {
	AgentID    string   `json:"agent_id"`
	TargetKind string   `json:"target_kind"`
	TargetID   string   `json:"target_id"`
	Events     []string `json:"events"`
}

// TargetRequest is the PUT target payload (contract §5: "agent + kind + id").
type TargetRequest struct {
	AgentID    string `json:"agent_id"`
	TargetKind string `json:"target_kind"`
	TargetID   string `json:"target_id"`
}

// EventsRequest is the PUT events payload (contract §5: "event id list").
type EventsRequest struct {
	Events []string `json:"events"`
}

// TargetView is the target binding half of the webhook view (contract §4).
type TargetView struct {
	AgentID    string `json:"agent_id"`
	TargetKind string `json:"target_kind"`
	TargetID   string `json:"target_id"`
}

// LastErrorView is the rendered last render-failure residue (add-connection-
// webhooks task 2.4, design.md risks): the dropped delivery's event id, the
// render error, and the drop timestamp. The manage surface shows it so an
// operator can see why deliveries stopped producing turns; the next
// successful render clears it back to null.
type LastErrorView struct {
	Event string `json:"event"`
	Error string `json:"error"`
	At    string `json:"at"`
}

// View is the served webhook state (contract §4): the toggle, the
// display-once residue, the derived ingest URL, the target binding, the
// selected events, and the last render-failure residue (null when clear).
// The plaintext secret never appears here.
type View struct {
	Enabled    bool           `json:"enabled"`
	SecretHint string         `json:"secret_hint"`
	IngestURL  string         `json:"ingest_url"`
	Target     *TargetView    `json:"target,omitempty"`
	Events     []string       `json:"events"`
	LastError  *LastErrorView `json:"last_error"`
}

// BuildView derives the served view from the stored state (contract §4):
// the ingest URL is computed — workspace public base + slug + connection
// id — never stored. The state's connection id names the URL; every store
// read stamps it (the inert default included). A corrupt last-error residue
// degrades to null — the view never fails on diagnostic state.
func BuildView(state *domain.ConnectionWebhook, workspaceSlug, publicBaseURL string) View {
	if state == nil {
		state = &domain.ConnectionWebhook{}
	}
	view := View{
		Enabled:    state.Enabled,
		SecretHint: state.SecretHint,
		IngestURL: strings.TrimSuffix(publicBaseURL, "/") + ingestURLPath +
			workspaceSlug + "/" + state.ConnectionID,
		Events: append([]string(nil), state.Events...),
	}
	if view.Events == nil {
		view.Events = []string{}
	}
	if state.TargetKind != "" || state.TargetID != "" || state.TargetAgentID != "" {
		view.Target = &TargetView{
			AgentID:    state.TargetAgentID,
			TargetKind: state.TargetKind,
			TargetID:   state.TargetID,
		}
	}
	if env, ok := parseLastError(state.LastError); ok {
		view.LastError = &LastErrorView{
			Event: env.Event,
			Error: env.Error,
			At:    env.At,
		}
	}
	return view
}

// Enable turns webhooks on for a connection (tasks 2.6, spec "Webhook
// enablement"): validates the target binding against the workspace's agents
// and channels, validates the event selection against the recipe catalog,
// generates the HMAC secret (crypto/rand, 32 bytes, base64url), stores the
// workspace-AAD envelope + last-4 hint, and returns the plaintext exactly
// once. Enabling with a prior secret replaces it — providers accept one
// secret per webhook, so the fresh value is the only valid one.
func (s *Service) Enable(ctx context.Context, workspaceID, connectionID string, req EnableRequest) (*domain.ConnectionWebhook, string, error) {
	_, recipe, err := s.resolveWebhookConnection(ctx, workspaceID, connectionID)
	if err != nil {
		return nil, "", err
	}
	if err := s.validateBinding(ctx, workspaceID, req.AgentID, req.TargetKind, req.TargetID); err != nil {
		return nil, "", err
	}

	events := req.Events
	if len(events) == 0 {
		events = recipe.Webhooks.DefaultEvents
	}

	state, err := s.webhooks.GetState(ctx, workspaceID, connectionID)
	if err != nil {
		return nil, "", fmt.Errorf("webhook service: read state: %w", err)
	}

	secret, envelope, hint, err := s.mintSecret(workspaceID)
	if err != nil {
		return nil, "", err
	}
	state.Enabled = true
	state.SecretCiphertext = envelope
	state.SecretHint = hint
	state.TargetAgentID = req.AgentID
	state.TargetKind = req.TargetKind
	state.TargetID = req.TargetID
	state.Events = append([]string(nil), events...)

	if err := state.ValidateForRecipe(recipe); err != nil {
		return nil, "", err
	}
	if err := s.webhooks.UpdateState(ctx, state); err != nil {
		return nil, "", fmt.Errorf("webhook service: enable: %w", err)
	}
	return state, secret, nil
}

// Disable turns ingestion off, leaving the binding, event selection, and
// secret envelope intact (spec: "Disabling SHALL stop ingestion and leave
// the connection otherwise intact").
func (s *Service) Disable(ctx context.Context, workspaceID, connectionID string) (*domain.ConnectionWebhook, error) {
	_, _, state, err := s.loadForWrite(ctx, workspaceID, connectionID)
	if err != nil {
		return nil, err
	}
	state.Enabled = false
	if err := s.webhooks.UpdateState(ctx, state); err != nil {
		return nil, fmt.Errorf("webhook service: disable: %w", err)
	}
	return state, nil
}

// Rotate mints a fresh secret, invalidating the previous one immediately
// (design.md D2: providers accept one secret per webhook — the single
// envelope write is the invalidation). Requires a live enablement; the
// plaintext rides the response exactly once.
func (s *Service) Rotate(ctx context.Context, workspaceID, connectionID string) (*domain.ConnectionWebhook, string, error) {
	_, recipe, state, err := s.loadForWrite(ctx, workspaceID, connectionID)
	if err != nil {
		return nil, "", err
	}
	if !state.Enabled {
		return nil, "", fmt.Errorf("%w: webhooks are not enabled on this connection", domain.ErrInvalid)
	}

	secret, envelope, hint, err := s.mintSecret(workspaceID)
	if err != nil {
		return nil, "", err
	}
	state.SecretCiphertext = envelope
	state.SecretHint = hint
	if err := state.ValidateForRecipe(recipe); err != nil {
		return nil, "", err
	}
	if err := s.webhooks.UpdateState(ctx, state); err != nil {
		return nil, "", fmt.Errorf("webhook service: rotate: %w", err)
	}
	return state, secret, nil
}

// UpdateTarget rebinds the delivery target. Enablement state is preserved:
// an enabled connection must stay completely bound, which
// ValidateForRecipe enforces at the write.
func (s *Service) UpdateTarget(ctx context.Context, workspaceID, connectionID string, req TargetRequest) (*domain.ConnectionWebhook, error) {
	_, recipe, state, err := s.loadForWrite(ctx, workspaceID, connectionID)
	if err != nil {
		return nil, err
	}
	if err := s.validateBinding(ctx, workspaceID, req.AgentID, req.TargetKind, req.TargetID); err != nil {
		return nil, err
	}
	state.TargetAgentID = req.AgentID
	state.TargetKind = req.TargetKind
	state.TargetID = req.TargetID
	if err := state.ValidateForRecipe(recipe); err != nil {
		return nil, err
	}
	if err := s.webhooks.UpdateState(ctx, state); err != nil {
		return nil, fmt.Errorf("webhook service: update target: %w", err)
	}
	return state, nil
}

// UpdateEvents replaces the selected event subset. An empty selection is
// only legal while disabled — ValidateForRecipe refuses it on a live
// connection (spec: enabling requires at least one event).
func (s *Service) UpdateEvents(ctx context.Context, workspaceID, connectionID string, req EventsRequest) (*domain.ConnectionWebhook, error) {
	_, recipe, state, err := s.loadForWrite(ctx, workspaceID, connectionID)
	if err != nil {
		return nil, err
	}
	state.Events = append([]string(nil), req.Events...)
	if err := state.ValidateForRecipe(recipe); err != nil {
		return nil, err
	}
	if err := s.webhooks.UpdateState(ctx, state); err != nil {
		return nil, fmt.Errorf("webhook service: update events: %w", err)
	}
	return state, nil
}

// Get reads the stored webhook state (task 3.1's GET state route).
func (s *Service) Get(ctx context.Context, workspaceID, connectionID string) (*domain.ConnectionWebhook, error) {
	if _, _, err := s.resolveWebhookConnection(ctx, workspaceID, connectionID); err != nil {
		return nil, err
	}
	state, err := s.webhooks.GetState(ctx, workspaceID, connectionID)
	if err != nil {
		return nil, fmt.Errorf("webhook service: read state: %w", err)
	}
	return state, nil
}

// StateForView is the connection-view enrichment seam (contract §4): the
// stored webhook state for a connection whose recipe declares webhooks,
// alongside the connection's service id — the handler derives the View.
// declares is false (omit the key) when the recipe declares no webhooks;
// unknown connections pass the store's ErrNotFound through.
func (s *Service) StateForView(ctx context.Context, workspaceID, connectionID string) (state *domain.ConnectionWebhook, service string, declares bool, err error) {
	conn, err := s.connections.Get(ctx, workspaceID, connectionID)
	if err != nil {
		return nil, "", false, err
	}
	recipe := domain.RecipeByID(conn.Service)
	if recipe == nil || recipe.Webhooks == nil {
		return nil, "", false, nil
	}
	state, err = s.webhooks.GetState(ctx, workspaceID, connectionID)
	if err != nil {
		return nil, "", false, fmt.Errorf("webhook service: read state: %w", err)
	}
	return state, conn.Service, true, nil
}

// PruneOnce removes delivery rows recorded before now minus the dedupe
// window (ingress contract §6) and reports the removal count.
func (s *Service) PruneOnce(ctx context.Context, now time.Time) (int64, error) {
	return s.webhooks.PruneDeliveries(ctx, now.Add(-domain.ConnectionWebhookDeliveryWindow))
}

// PruneLoop runs the window cleanup on DefaultPruneInterval until the
// context is cancelled (the composition root's lifecycle context).
func (s *Service) PruneLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultPruneInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pruned, err := s.PruneOnce(ctx, s.now())
			if err != nil {
				slog.DebugContext(ctx, "webhook delivery prune failed", "error", err)
				continue
			}
			if pruned > 0 {
				slog.DebugContext(ctx, "webhook delivery window pruned", "removed", pruned)
			}
		}
	}
}

// resolveWebhookConnection loads the connection and asserts its registered
// recipe declares webhook support — the service-side precondition every
// management operation shares.
func (s *Service) resolveWebhookConnection(ctx context.Context, workspaceID, connectionID string) (*domain.Connection, *domain.Recipe, error) {
	conn, err := s.connections.Get(ctx, workspaceID, connectionID)
	if err != nil {
		return nil, nil, err
	}
	recipe := domain.RecipeByID(conn.Service)
	if recipe == nil || recipe.Webhooks == nil {
		return nil, nil, fmt.Errorf("%w: recipe %q does not declare webhook support", domain.ErrInvalid, conn.Service)
	}
	return conn, recipe, nil
}

// loadForWrite is resolveWebhookConnection plus the current state — the
// shape every mutating operation reads before its full-state write.
func (s *Service) loadForWrite(ctx context.Context, workspaceID, connectionID string) (*domain.Connection, *domain.Recipe, *domain.ConnectionWebhook, error) {
	conn, recipe, err := s.resolveWebhookConnection(ctx, workspaceID, connectionID)
	if err != nil {
		return nil, nil, nil, err
	}
	state, err := s.webhooks.GetState(ctx, workspaceID, connectionID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("webhook service: read state: %w", err)
	}
	return conn, recipe, state, nil
}

// validateBinding checks a target request against the workspace: the agent
// must exist in the workspace; a channel target must name an existing
// channel. Thread targets are session ids the runner resolves lazily, so
// only shape (non-empty) is checkable here — domain validation covers the
// rest at the store boundary.
func (s *Service) validateBinding(ctx context.Context, workspaceID, agentID, targetKind, targetID string) error {
	if _, err := s.agents.ByID(ctx, workspaceID, agentID); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("%w: target agent %q not found in workspace", domain.ErrInvalid, agentID)
		}
		return fmt.Errorf("webhook service: resolve target agent: %w", err)
	}

	switch targetKind {
	case domain.ConnectionWebhookTargetThread:
		// Session ids resolve lazily at run time; shape is enforced at the
		// store boundary.
	case domain.ConnectionWebhookTargetChannel:
		if _, err := s.channels.ChannelByID(ctx, workspaceID, targetID); err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return fmt.Errorf("%w: target channel %q not found in workspace", domain.ErrInvalid, targetID)
			}
			return fmt.Errorf("webhook service: resolve target channel: %w", err)
		}
	default:
		return fmt.Errorf("%w: target kind %q must be %s or %s", domain.ErrInvalid, targetKind, domain.ConnectionWebhookTargetThread, domain.ConnectionWebhookTargetChannel)
	}
	return nil
}

// mintSecret generates the secret and its stored halves: the workspace-AAD
// envelope and the display-once hint.
func (s *Service) mintSecret(workspaceID string) (plaintext, envelope, hint string, err error) {
	plaintext, err = GenerateSecret()
	if err != nil {
		return "", "", "", fmt.Errorf("webhook service: generate secret: %w", err)
	}
	envelope, err = s.secrets.EncryptSecret(workspaceID, []byte(plaintext))
	if err != nil {
		return "", "", "", fmt.Errorf("webhook service: seal secret: %w", err)
	}
	return plaintext, envelope, SecretHint(plaintext), nil
}
