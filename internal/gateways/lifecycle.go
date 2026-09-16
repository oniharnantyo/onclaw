package gateways

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// TokenDecryptor resolves a gateway's encrypted bot credential (the
// AES-256-GCM secrets envelope with workspace-bound AAD, design D12) to the
// plaintext the platform adapter needs. Implemented by the composition root
// over the secrets service — the gateway core never sees key material.
type TokenDecryptor interface {
	DecryptGatewayToken(ctx context.Context, workspaceID, envelope string) (string, error)
}

// AdapterSpec is everything a platform adapter needs to be constructed.
// The AdapterFactory (composition root) turns it into a PlatformAdapter,
// dispatching on (platform, lane) — add-whatsapp-gateway design D1/D10 —
// typically adapters/telegram.NewAdapter closed over the service as the
// InboundHandler.
type AdapterSpec struct {
	GatewayID   string
	WorkspaceID string
	Platform    string
	Lane        string // domain gateway lane (design D1); empty for telegram
	BotToken    string // decrypted; never logged
	BotUsername string
	Transport   string // domain.GatewayTransportWebhook | LongPolling
	WebhookURL  string
}

// AdapterFactory constructs the platform adapter for one gateway spec.
type AdapterFactory func(spec AdapterSpec) (PlatformAdapter, error)

// supportedPlatforms is the platform registry Manager.Sync reconciles
// (add-whatsapp-gateway design D10): each enabled gateway row is started,
// stopped, and restarted per its own (platform, lane) configuration — a new
// platform is a row plus a factory registration, not a core change.
var supportedPlatforms = []string{
	domain.GatewayPlatformTelegram,
	domain.GatewayPlatformWhatsApp,
}

// WebhookReceiver is the optional capability a webhook-mode adapter exposes:
// the public ingress handler feeds raw platform payloads to the gateway
// through it, after secret-token validation.
type WebhookReceiver interface {
	// IngestWebhook parses one raw platform update and delivers it to the
	// service's InboundHandler. Malformed payloads are dropped (zero
	// uncaught parse errors), not errors.
	IngestWebhook(ctx context.Context, raw []byte) error
}

// Manager owns gateway lifecycle (design D11): one running adapter per
// enabled gateway, started with the server (StartAll), reconciled on config
// changes (Sync), and stopped at shutdown (Stop). A transport switch
// (webhook ↔ long-polling) or token change tears the adapter down and
// rebuilds it.
type Manager struct {
	gateways  store.GatewayStore
	decryptor TokenDecryptor
	factory   AdapterFactory
	service   *Service

	// base drives adapter lifetimes: detached from request contexts so a
	// webhook request ending never stops a long-polling loop.
	base    context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	running map[string]*gatewayRuntime // gatewayID → runtime
}

// gatewayRuntime is one live gateway: its adapter and the config snapshot it
// was built from (identity, token ciphertext, transport) — the reconciliation
// diff.
type gatewayRuntime struct {
	adapter PlatformAdapter
	cfg     domain.GatewayConfig
}

// ManagerOption customizes a Manager.
type ManagerOption func(*Manager)

// NewManager builds the lifecycle manager. Dependencies are required: the
// gateway store, the token decryptor, the adapter factory, and the service
// the adapters deliver to.
func NewManager(
	gateways store.GatewayStore,
	decryptor TokenDecryptor,
	factory AdapterFactory,
	service *Service,
	opts ...ManagerOption,
) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		gateways:  gateways,
		decryptor: decryptor,
		factory:   factory,
		service:   service,
		base:      ctx,
		cancel:    cancel,
		running:   make(map[string]*gatewayRuntime),
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// StartAll reconciles every workspace's gateway at server boot (design D11).
// Workspace IDs come from the composition root (WorkspaceStore.ListAll).
// A per-gateway start failure is logged and skipped — one broken bot token
// must not keep the server from booting.
func (m *Manager) StartAll(ctx context.Context, workspaceIDs []string) {
	for _, workspaceID := range workspaceIDs {
		if err := m.Sync(ctx, workspaceID); err != nil {
			slog.Error("gateway start failed for workspace", "workspace_id", workspaceID, "err", err)
		}
	}
}

// Sync reconciles one workspace's gateways to their stored configuration
// (multi-bot gateways): start when newly enabled, stop when disabled/deleted,
// restart on token/lane/transport/config change. Called by StartAll at boot
// and by the admin API wave on config changes.
func (m *Manager) Sync(ctx context.Context, workspaceID string) error {
	list, err := m.gateways.ListGateways(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("gateway manager: list gateways: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Map of desired enabled gateways by ID.
	desired := make(map[string]*domain.GatewayConfig)
	for i := range list {
		cfg := &list[i]
		if cfg.Enabled {
			desired[cfg.ID] = cfg
		}
	}

	// Stop any currently running gateways for this workspace that are no longer in desired (deleted or disabled).
	for id, rt := range m.running {
		if rt.cfg.WorkspaceID == workspaceID {
			if _, ok := desired[id]; !ok {
				m.stopLocked(id)
			}
		}
	}

	// For each desired enabled gateway:
	var errs []error
	for _, cfg := range desired {
		current := m.running[cfg.ID]
		if current != nil && gatewayUnchanged(&current.cfg, cfg) {
			continue
		}
		if current != nil {
			m.stopLocked(cfg.ID)
		}

		token := ""
		if cfg.BotTokenCiphertext != "" {
			var decErr error
			token, decErr = m.decryptor.DecryptGatewayToken(ctx, workspaceID, cfg.BotTokenCiphertext)
			if decErr != nil {
				errs = append(errs, fmt.Errorf("gateway manager: decrypt bot token for %s: %w", cfg.ID, decErr))
				continue
			}
		}

		gatewayID := cfg.ID
		adapter, bErr := m.factory(AdapterSpec{
			GatewayID:   gatewayID,
			WorkspaceID: workspaceID,
			Platform:    cfg.Platform,
			Lane:        cfg.Lane,
			BotToken:    token,
			BotUsername: cfg.BotUsername,
			Transport:   cfg.Transport,
			WebhookURL:  cfg.WebhookURL,
		})
		if bErr != nil {
			errs = append(errs, fmt.Errorf("gateway manager: build adapter for %s: %w", cfg.ID, bErr))
			continue
		}
		if sErr := adapter.Start(m.base); sErr != nil {
			errs = append(errs, fmt.Errorf("gateway manager: start adapter for %s: %w", cfg.ID, sErr))
			continue
		}

		m.running[gatewayID] = &gatewayRuntime{adapter: adapter, cfg: *cfg}
		m.service.AttachGateway(gatewayID, workspaceID, cfg.Platform, adapter)
		slog.Info("gateway started",
			"gateway_id", gatewayID, "workspace_id", workspaceID,
			"platform", cfg.Platform, "lane", cfg.Lane, "transport", cfg.Transport)
	}

	return errors.Join(errs...)
}

// Stop shuts every gateway down (server shutdown, design D11).
func (m *Manager) Stop(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id := range m.running {
		m.stopLocked(id)
	}
	m.cancel()
}

// HandleWebhook feeds one raw platform payload to the webhook receiver of
// the gateway it belongs to. The public ingress handler (composition wave)
// validates the secret token, then calls this. Unknown gateways are an
// error; malformed payloads are dropped inside the receiver.
func (m *Manager) HandleWebhook(ctx context.Context, gatewayID string, raw []byte) error {
	m.mu.Lock()
	rt := m.running[gatewayID]
	m.mu.Unlock()
	if rt == nil {
		return fmt.Errorf("%w: gateway not found or not running", domain.ErrNotFound)
	}
	receiver, ok := rt.adapter.(WebhookReceiver)
	if !ok {
		return fmt.Errorf("gateway manager: gateway %s does not accept webhooks", rt.cfg.ID)
	}
	return receiver.IngestWebhook(ctx, raw)
}

// stopLocked stops and unregisters one gateway. m.mu must be held.
func (m *Manager) stopLocked(gatewayID string) {
	rt := m.running[gatewayID]
	if rt == nil {
		return
	}
	delete(m.running, gatewayID)

	workspaceID := rt.cfg.WorkspaceID
	if err := rt.adapter.Stop(context.WithoutCancel(m.base)); err != nil {
		slog.Error("gateway stop failed", "gateway_id", gatewayID, "err", err)
	}
	m.service.DetachGateway(gatewayID)
	slog.Info("gateway stopped", "gateway_id", gatewayID, "workspace_id", workspaceID)
}

// gatewayUnchanged reports whether a running adapter's config snapshot
// matches the stored config — identity, credential, lane, transport, and bot
// username (a re-saved token must take effect; an enabled-flag flip is
// handled by the desired-state check, not here). Lane participates because
// it selects the adapter implementation (design D1/D10).
func gatewayUnchanged(a, b *domain.GatewayConfig) bool {
	return a.ID == b.ID &&
		a.AgentID == b.AgentID &&
		a.Identity == b.Identity &&
		a.BotTokenCiphertext == b.BotTokenCiphertext &&
		a.BotUsername == b.BotUsername &&
		a.Lane == b.Lane &&
		a.Transport == b.Transport &&
		a.WebhookURL == b.WebhookURL
}
