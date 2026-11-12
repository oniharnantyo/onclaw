package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/webhooks"
)

// WebhookRuntime is the narrow seam between the webhook service and the
// receiver's live machinery (add-webhook-triggers design D9): the listener's
// readable state, the no-restart address rebind, and the self-test action.
// *webhooks.Service satisfies it structurally; tests fake it.
type WebhookRuntime interface {
	// State snapshots the listener's serving state.
	State() webhooks.ListenerState
	// Reconfigure applies a changed listen address without a restart.
	Reconfigure(addr string)
	// Test exercises the whole delivery path for one endpoint through the
	// real listener.
	Test(ctx context.Context, endpointID string) error
}

// WebhookVerifierView is one verifier row's read shape, as bound to the
// frontend. It deliberately carries no secret material (design D6, the
// channel-token precedent): callers see only whether a secret is stored and
// its non-secret last-four-style hint, never the value.
type WebhookVerifierView struct {
	ID       string `json:"id"`
	Position int    `json:"position"`
	// Kind is one of the store.WebhookVerifier* engine kinds.
	Kind string `json:"kind"`
	// Preset names the gallery card the row was configured from; empty for
	// manually built rows. It doubles as the delivery log's via label.
	Preset          string `json:"preset"`
	HeaderName      string `json:"headerName"`
	TimestampHeader string `json:"timestampHeader"`
	Algo            string `json:"algo"`
	Encoding        string `json:"encoding"`
	Prefix          string `json:"prefix"`
	Scheme          string `json:"scheme"`
	// SecretSet and SecretHint are the secret-free views: only whether a
	// secret is stored and its last four characters, empty when unset.
	SecretSet  bool   `json:"secretSet"`
	SecretHint string `json:"secretHint"`
}

// WebhookEndpointView is one webhook endpoint's read shape, as bound to the
// frontend: the secret-free projection (verifier rows carry presence and
// hints only), the generated delivery URL, and the last delivery's outcome
// for the list row.
type WebhookEndpointView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// AgentID and GroupID locate the target; exactly one is set. TargetName
	// is the resolved display name — empty when the target no longer exists
	// (the dead-target state the delivery log then explains).
	AgentID    string                `json:"agentId"`
	GroupID    string                `json:"groupId"`
	TargetName string                `json:"targetName"`
	Prompt     string                `json:"prompt"`
	Enabled    bool                  `json:"enabled"`
	Verifiers  []WebhookVerifierView `json:"verifiers"`
	// URL is the delivery URL on the configured listener address, including
	// the path-token segment when the endpoint verifies one. Copy it into
	// the sender's webhook configuration.
	URL string `json:"url"`
	// LastOutcome, LastStatus, and LastAttemptedAt summarize the endpoint's
	// newest delivery log row; zero-valued when nothing was delivered yet.
	LastOutcome     string    `json:"lastOutcome"`
	LastStatus      int       `json:"lastStatus"`
	LastAttemptedAt time.Time `json:"lastAttemptedAt"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// WebhookListenerView is the receiver's live serving state, as bound to the
// frontend: a bind failure reads as Serving=false with BindError set (the
// browser-use lockfile precedent — never a silent dead port).
type WebhookListenerView struct {
	Serving   bool   `json:"serving"`
	BoundAddr string `json:"boundAddr"`
	BindError string `json:"bindError"`
}

// WebhookDeliveryView is one row of an endpoint's delivery log, newest
// first. Every field is non-secret: Note carries the human-readable refusal
// reason, Via the preset label that authenticated the delivery.
type WebhookDeliveryView struct {
	ID          string    `json:"id"`
	AttemptedAt time.Time `json:"attemptedAt"`
	Status      int       `json:"status"`
	Outcome     string    `json:"outcome"`
	// SessionID locates the agent-target fire session and MessageID the
	// group transcript message; exactly the produced one is set when the
	// delivery dispatched (the UI's link target).
	SessionID string `json:"sessionId"`
	MessageID string `json:"messageId"`
	Via       string `json:"via"`
	// SenderDeliveryID is the sender's own delivery id (the retry-dedupe
	// marker) when present.
	SenderDeliveryID string    `json:"senderDeliveryId"`
	Note             string    `json:"note"`
	CreatedAt        time.Time `json:"createdAt"`
}

// WebhookVerifierInput is one verifier row's write shape: the engine kind,
// its non-secret configuration, and the secret. The secret is write-only —
// a blank Secret on an update keeps the stored one (per row position,
// mirroring SaveChannel's blank-keeps), a non-blank one replaces it.
type WebhookVerifierInput struct {
	Kind            string `json:"kind"`
	Preset          string `json:"preset"`
	HeaderName      string `json:"headerName"`
	TimestampHeader string `json:"timestampHeader"`
	Algo            string `json:"algo"`
	Encoding        string `json:"encoding"`
	Prefix          string `json:"prefix"`
	Scheme          string `json:"scheme"`
	Secret          string `json:"secret"`
}

// WebhookEndpointInput is one endpoint's write shape. Exactly one of
// AgentID or GroupID must name an existing target at save time; verifiers
// require at least one row (the ≥1-credential rule).
type WebhookEndpointInput struct {
	Name      string                 `json:"name"`
	AgentID   string                 `json:"agentId"`
	GroupID   string                 `json:"groupId"`
	Prompt    string                 `json:"prompt"`
	Enabled   bool                   `json:"enabled"`
	Verifiers []WebhookVerifierInput `json:"verifiers"`
}

// WebhookPresetView is one sender preset's gallery card: the stable name the
// save payload references, the display title, the where-to-find-it hint, and
// the verifier rows the preset seeds (non-secret configuration only — the
// wizard renders one secret field over them).
type WebhookPresetView struct {
	Name      string                `json:"name"`
	Title     string                `json:"title"`
	Hint      string                `json:"hint"`
	Verifiers []WebhookVerifierView `json:"verifiers"`
}

// WebhookService is the bound service for the Channels pane's Webhooks
// section (add-webhook-triggers tasks 7.1): endpoint CRUD with write-only
// verifier secrets, the deliveries read, the listener address applying
// without a restart, the self-test action, and the listener state read.
// Every mutation is announced through the emitter as EventWebhooksChanged so
// the section refetches.
type WebhookService struct {
	store   store.Store
	emitter Emitter
	runtime WebhookRuntime
	// OnAddrChange, when set, fires after a saved listener-address change so
	// the receiver rebinds without a restart (the channel runtime's
	// OnChange kick pattern). Async, like the emitter.
	OnAddrChange func(addr string)
}

// NewWebhookService returns the webhook service over st, announcing every
// mutation through emitter and driving the receiver's live machinery
// through runtime.
func NewWebhookService(st store.Store, emitter Emitter, runtime WebhookRuntime) *WebhookService {
	return &WebhookService{store: st, emitter: emitter, runtime: runtime}
}

// ListEndpoints returns every endpoint's secret-free view in the store's
// (case-insensitive name) order. The result is empty (not nil) when no
// endpoints exist.
func (s *WebhookService) ListEndpoints(ctx context.Context) ([]WebhookEndpointView, error) {
	eps, err := s.store.Webhooks().ListEndpoints(ctx)
	if err != nil {
		return nil, fmt.Errorf("list webhook endpoints: %w", err)
	}
	out := make([]WebhookEndpointView, 0, len(eps))
	addr := s.configuredAddr(ctx)
	for _, ep := range eps {
		view, err := s.newEndpointView(ctx, ep, addr)
		if err != nil {
			return nil, err
		}
		out = append(out, view)
	}
	return out, nil
}

// GetEndpoint returns one endpoint's view; store.ErrNotFound when absent.
func (s *WebhookService) GetEndpoint(ctx context.Context, id string) (WebhookEndpointView, error) {
	ep, err := s.store.Webhooks().GetEndpoint(ctx, id)
	if err != nil {
		return WebhookEndpointView{}, fmt.Errorf("get webhook endpoint %s: %w", id, err)
	}
	return s.newEndpointView(ctx, *ep, s.configuredAddr(ctx))
}

// CreateEndpoint validates and stores a new endpoint. The store assigns the
// ID; a name already in use (case-insensitively) returns an error wrapping
// store.ErrDuplicate, and a target that does not resolve names it.
func (s *WebhookService) CreateEndpoint(ctx context.Context, in WebhookEndpointInput) (WebhookEndpointView, error) {
	return s.save(ctx, "", in)
}

// UpdateEndpoint rewrites one endpoint's definition and verifier list. A
// blank secret on a verifier input keeps the stored secret for that row
// position; a non-blank one rotates it. store.ErrNotFound when absent.
func (s *WebhookService) UpdateEndpoint(ctx context.Context, id string, in WebhookEndpointInput) (WebhookEndpointView, error) {
	return s.save(ctx, id, in)
}

// save applies one create or update: target resolution, verifier
// validation, the endpoint row, and the verifier swap — the last two commit
// together through WithTx. An empty id inserts.
func (s *WebhookService) save(ctx context.Context, id string, in WebhookEndpointInput) (WebhookEndpointView, error) {
	if err := s.resolveTarget(ctx, in); err != nil {
		return WebhookEndpointView{}, err
	}
	if err := webhooks.ValidateVerifierSet(verifierConfigs(in.Verifiers)); err != nil {
		return WebhookEndpointView{}, fmt.Errorf("save webhook endpoint: %w", err)
	}

	// The blank-keeps baseline: on an update, a row's blank secret inherits
	// the stored secret at its position (the edit form resubmits only what
	// changed, design D6); on a create there is nothing to inherit.
	var stored *store.WebhookEndpoint
	if id != "" {
		var err error
		stored, err = s.store.Webhooks().GetEndpoint(ctx, id)
		if err != nil {
			return WebhookEndpointView{}, fmt.Errorf("get webhook endpoint %s: %w", id, err)
		}
	}

	ep := &store.WebhookEndpoint{
		ID:      id,
		Name:    in.Name,
		AgentID: in.AgentID,
		GroupID: in.GroupID,
		Prompt:  in.Prompt,
		Enabled: in.Enabled,
	}
	rows := verifierRows(in.Verifiers, stored)

	err := s.store.WithTx(ctx, func(tx store.Store) error {
		if err := tx.Webhooks().SaveEndpoint(ctx, ep); err != nil {
			return err
		}
		return tx.Webhooks().SaveVerifiers(ctx, ep.ID, rows)
	})
	if err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			return WebhookEndpointView{}, fmt.Errorf("save webhook endpoint: the name %q is already in use: %w", in.Name, store.ErrDuplicate)
		}
		return WebhookEndpointView{}, fmt.Errorf("save webhook endpoint %s: %w", id, err)
	}

	s.changed(ep.ID)
	saved, err := s.store.Webhooks().GetEndpoint(ctx, ep.ID)
	if err != nil {
		return WebhookEndpointView{}, fmt.Errorf("get webhook endpoint %s: %w", ep.ID, err)
	}
	return s.newEndpointView(ctx, *saved, s.configuredAddr(ctx))
}

// SetEnabled toggles the endpoint's enabled flag without touching its
// definition or verifier secrets. store.ErrNotFound when absent.
func (s *WebhookService) SetEnabled(ctx context.Context, id string, enabled bool) (WebhookEndpointView, error) {
	stored, err := s.store.Webhooks().GetEndpoint(ctx, id)
	if err != nil {
		return WebhookEndpointView{}, fmt.Errorf("get webhook endpoint %s: %w", id, err)
	}
	stored.Enabled = enabled
	if err := s.store.Webhooks().SaveEndpoint(ctx, stored); err != nil {
		return WebhookEndpointView{}, fmt.Errorf("set webhook endpoint %s enabled: %w", id, err)
	}
	s.changed(id)
	return s.newEndpointView(ctx, *stored, s.configuredAddr(ctx))
}

// DeleteEndpoint removes the endpoint; its verifiers and delivery log go
// with it through the store's cascade. store.ErrNotFound when absent.
func (s *WebhookService) DeleteEndpoint(ctx context.Context, id string) error {
	if err := s.store.Webhooks().DeleteEndpoint(ctx, id); err != nil {
		return fmt.Errorf("delete webhook endpoint %s: %w", id, err)
	}
	s.changed(id)
	return nil
}

// Deliveries returns one endpoint's delivery log, newest first. The result
// is empty (not nil) when nothing was delivered yet; store.ErrNotFound when
// the endpoint is absent.
func (s *WebhookService) Deliveries(ctx context.Context, endpointID string) ([]WebhookDeliveryView, error) {
	rows, err := s.store.Webhooks().ListDeliveries(ctx, endpointID)
	if err != nil {
		return nil, fmt.Errorf("list webhook deliveries %s: %w", endpointID, err)
	}
	out := make([]WebhookDeliveryView, 0, len(rows))
	for _, row := range rows {
		out = append(out, WebhookDeliveryView{
			ID:               row.ID,
			AttemptedAt:      row.AttemptedAt,
			Status:           row.Status,
			Outcome:          row.Outcome,
			SessionID:        row.SessionID,
			MessageID:        row.MessageID,
			Via:              row.Via,
			SenderDeliveryID: row.SenderDeliveryID,
			Note:             row.Note,
			CreatedAt:        row.CreatedAt,
		})
	}
	return out, nil
}

// ListenerState reports the receiver's live serving state.
func (s *WebhookService) ListenerState() WebhookListenerView {
	state := s.runtime.State()
	return WebhookListenerView{
		Serving:   state.Serving,
		BoundAddr: state.BoundAddr,
		BindError: state.BindError,
	}
}

// SetListenerAddress stores the receiver's listen address on the gateway
// settings row (empty means the loopback default) and kicks the runtime's
// reconfigure, so the change applies without an app restart
// (add-webhook-triggers spec "Receiver lifecycle").
func (s *WebhookService) SetListenerAddress(ctx context.Context, addr string) error {
	settings, err := s.store.GatewaySettings().Get(ctx)
	if err != nil {
		return fmt.Errorf("set webhook listener address: %w", err)
	}
	if settings == nil {
		settings = &store.GatewaySettings{}
	}
	settings.WebhookAddr = addr
	if err := s.store.GatewaySettings().Save(ctx, settings); err != nil {
		return fmt.Errorf("set webhook listener address: %w", err)
	}
	s.reconfigure(addr)
	return nil
}

// Test exercises the whole delivery path for one endpoint — a self-signed
// delivery through the real listener (design D8). A failed test returns the
// surfaced error; the attempt still appears in the delivery log when it
// reached the pipeline.
func (s *WebhookService) Test(ctx context.Context, endpointID string) error {
	if err := s.runtime.Test(ctx, endpointID); err != nil {
		return fmt.Errorf("webhook test %s: %w", endpointID, err)
	}
	s.changed(endpointID)
	return nil
}

// Presets returns the sender preset catalog in gallery order: one card per
// sender with its where-to-find-it hint and the verifier rows it seeds.
func (s *WebhookService) Presets() []WebhookPresetView {
	presets := webhooks.Presets()
	out := make([]WebhookPresetView, 0, len(presets))
	for _, p := range presets {
		out = append(out, WebhookPresetView{
			Name:      p.Name,
			Title:     p.Title,
			Hint:      p.Hint,
			Verifiers: newVerifierViews(presetRows(p)),
		})
	}
	return out
}

// resolveTarget verifies the input names exactly one existing target — the
// save-time resolution the spec requires (a delivery to a later-deleted
// target is the dispatch path's dead-target record, not a save error).
func (s *WebhookService) resolveTarget(ctx context.Context, in WebhookEndpointInput) error {
	if (in.AgentID == "") == (in.GroupID == "") {
		return fmt.Errorf("save webhook endpoint: exactly one of an agent or a group target is required")
	}
	if in.AgentID != "" {
		if _, err := s.store.Agents().ByID(ctx, in.AgentID); err != nil {
			return fmt.Errorf("save webhook endpoint: target agent: %w", err)
		}
		return nil
	}
	if _, err := s.store.Groups().GroupByID(ctx, in.GroupID); err != nil {
		return fmt.Errorf("save webhook endpoint: target group: %w", err)
	}
	return nil
}

// configuredAddr reads the persisted listener address, applying the loopback
// default when the settings row is absent or the column empty.
func (s *WebhookService) configuredAddr(ctx context.Context) string {
	settings, err := s.store.GatewaySettings().Get(ctx)
	if err != nil || settings == nil || settings.WebhookAddr == "" {
		return webhooks.DefaultAddr
	}
	return settings.WebhookAddr
}

// reconfigure kicks the runtime rebind after a saved address change, on its
// own goroutine like changed (the rebind's teardown join must not sit inside
// a binding call).
func (s *WebhookService) reconfigure(addr string) {
	// A cleared address means the loopback default, not "leave the listener
	// where it is": the runtime's no-op-on-empty rule is bypassed by passing
	// the resolved default explicitly.
	applied := addr
	if applied == "" {
		applied = webhooks.DefaultAddr
	}
	if s.OnAddrChange != nil {
		go s.OnAddrChange(applied)
	}
}

// changed announces a webhook mutation to the pane. The emitter contract
// forbids blocking, so the emit runs on its own goroutine (the channels
// service pattern).
func (s *WebhookService) changed(endpointID string) {
	go s.emitter.Emit(EventWebhooksChanged, WebhooksChangedEvent{EndpointID: endpointID})
}

// newEndpointView projects a stored endpoint onto the read shape: secret-free
// verifiers, the generated URL on the given listener address, and the last
// delivery's summary. The raw secrets are consumed here and must not leak
// into the result.
func (s *WebhookService) newEndpointView(ctx context.Context, ep store.WebhookEndpoint, addr string) (WebhookEndpointView, error) {
	view := WebhookEndpointView{
		ID:        ep.ID,
		Name:      ep.Name,
		AgentID:   ep.AgentID,
		GroupID:   ep.GroupID,
		Prompt:    ep.Prompt,
		Enabled:   ep.Enabled,
		Verifiers: newVerifierViews(ep.Verifiers),
		URL:       webhooks.URL(addr, ep),
		CreatedAt: ep.CreatedAt,
		UpdatedAt: ep.UpdatedAt,
	}
	view.TargetName = s.targetName(ctx, ep)

	rows, err := s.store.Webhooks().ListDeliveries(ctx, ep.ID)
	if err != nil {
		return WebhookEndpointView{}, fmt.Errorf("list webhook deliveries %s: %w", ep.ID, err)
	}
	if len(rows) > 0 {
		view.LastOutcome = rows[0].Outcome
		view.LastStatus = rows[0].Status
		view.LastAttemptedAt = rows[0].AttemptedAt
	}
	return view, nil
}

// targetName resolves the endpoint's target display name; a deleted target
// (or a failed read) yields the empty string — the view's dead-target state.
func (s *WebhookService) targetName(ctx context.Context, ep store.WebhookEndpoint) string {
	if ep.AgentID != "" {
		agent, err := s.store.Agents().ByID(ctx, ep.AgentID)
		if err != nil {
			return ""
		}
		return agent.Name
	}
	group, err := s.store.Groups().GroupByID(ctx, ep.GroupID)
	if err != nil {
		return ""
	}
	return group.Name
}

// verifierConfigs maps input rows onto the engine configs the verifier
// validation runs, preserving the submitted order.
func verifierConfigs(in []WebhookVerifierInput) []webhooks.VerifierConfig {
	cfgs := make([]webhooks.VerifierConfig, len(in))
	for i, row := range in {
		cfgs[i] = webhooks.VerifierConfig{
			Kind:            webhooks.VerifierKind(row.Kind),
			Preset:          row.Preset,
			HeaderName:      row.HeaderName,
			TimestampHeader: row.TimestampHeader,
			Algo:            webhooks.HMACAlgo(row.Algo),
			Encoding:        webhooks.HMACEncoding(row.Encoding),
			Prefix:          row.Prefix,
			Scheme:          webhooks.TsHMACScheme(row.Scheme),
		}
	}
	return cfgs
}

// verifierRows maps input rows onto the store rows to persist, merging the
// blank-keep secrets: a blank secret on an update inherits the stored row at
// the same position; a non-blank one replaces it. On a create (stored nil)
// the given secret is stored as-is.
func verifierRows(in []WebhookVerifierInput, stored *store.WebhookEndpoint) []store.WebhookVerifier {
	rows := make([]store.WebhookVerifier, len(in))
	for i, row := range in {
		secret := row.Secret
		if secret == "" && stored != nil && i < len(stored.Verifiers) {
			secret = stored.Verifiers[i].Secret
		}
		rows[i] = store.WebhookVerifier{
			Kind:            row.Kind,
			Preset:          row.Preset,
			HeaderName:      row.HeaderName,
			TimestampHeader: row.TimestampHeader,
			Algo:            row.Algo,
			Encoding:        row.Encoding,
			Prefix:          row.Prefix,
			Scheme:          row.Scheme,
			Secret:          secret,
		}
	}
	return rows
}

// presetRows renders a preset's seeded verifier configs as store-shaped rows
// (no secrets — the user fills them in the wizard).
func presetRows(p webhooks.Preset) []store.WebhookVerifier {
	rows := make([]store.WebhookVerifier, len(p.Verifiers))
	for i, cfg := range p.Verifiers {
		rows[i] = store.WebhookVerifier{
			Position:        i,
			Kind:            string(cfg.Kind),
			Preset:          cfg.Preset,
			HeaderName:      cfg.HeaderName,
			TimestampHeader: cfg.TimestampHeader,
			Algo:            string(cfg.Algo),
			Encoding:        string(cfg.Encoding),
			Prefix:          cfg.Prefix,
			Scheme:          string(cfg.Scheme),
		}
	}
	return rows
}

// newVerifierViews projects stored verifier rows onto the read shape. The
// raw secrets are consumed here: presence and the last-four hint only.
func newVerifierViews(rows []store.WebhookVerifier) []WebhookVerifierView {
	out := make([]WebhookVerifierView, 0, len(rows))
	for _, row := range rows {
		out = append(out, WebhookVerifierView{
			ID:              row.ID,
			Position:        row.Position,
			Kind:            row.Kind,
			Preset:          row.Preset,
			HeaderName:      row.HeaderName,
			TimestampHeader: row.TimestampHeader,
			Algo:            row.Algo,
			Encoding:        row.Encoding,
			Prefix:          row.Prefix,
			Scheme:          row.Scheme,
			SecretSet:       row.Secret != "",
			SecretHint:      keyHint(row.Secret),
		})
	}
	return out
}
