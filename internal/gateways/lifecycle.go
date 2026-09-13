package gateways

import (
	"context"
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
// The AdapterFactory (composition root) turns it into a PlatformAdapter —
// typically adapters/telegram.NewAdapter closed over the service as the
// InboundHandler.
type AdapterSpec struct {
	GatewayID   string
	WorkspaceID string
	Platform    string
	BotToken    string // decrypted; never logged
	BotUsername string
	Transport   string // domain.GatewayTransportWebhook | LongPolling
	WebhookURL  string
}

// AdapterFactory constructs the platform adapter for one gateway spec.
type AdapterFactory func(spec AdapterSpec) (PlatformAdapter, error)

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

// Sync reconciles one workspace's gateway to its stored configuration:
// start when newly enabled, stop when disabled, restart on token/transport
// change. Called by StartAll at boot and by the admin API wave on config
// changes (D11: "started/stopped with the server and on config changes").
func (m *Manager) Sync(ctx context.Context, workspaceID string) error {
	cfg, err := m.gateways.GetGateway(ctx, workspaceID, domain.GatewayPlatformTelegram)
	if err != nil {
		return fmt.Errorf("gateway manager: load config: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	current := m.running[workspaceGatewayKey(workspaceID, domain.GatewayPlatformTelegram)]

	// Desired state: running iff an enabled config exists.
	if cfg == nil || !cfg.Enabled {
		if current != nil {
			m.stopLocked(workspaceID, domain.GatewayPlatformTelegram)
		}
		return nil
	}

	// Already running the same configuration: no-op.
	if current != nil && gatewayUnchanged(&current.cfg, cfg) {
		return nil
	}
	if current != nil {
		m.stopLocked(workspaceID, domain.GatewayPlatformTelegram)
	}

	// Build and start the adapter.
	token, err := m.decryptor.DecryptGatewayToken(ctx, workspaceID, cfg.BotTokenCiphertext)
	if err != nil {
		return fmt.Errorf("gateway manager: decrypt bot token: %w", err)
	}
	gatewayID := cfg.ID
	adapter, err := m.factory(AdapterSpec{
		GatewayID:   gatewayID,
		WorkspaceID: workspaceID,
		Platform:    cfg.Platform,
		BotToken:    token,
		BotUsername: cfg.BotUsername,
		Transport:   cfg.Transport,
		WebhookURL:  cfg.WebhookURL,
	})
	if err != nil {
		return fmt.Errorf("gateway manager: build adapter: %w", err)
	}
	if err := adapter.Start(m.base); err != nil {
		return fmt.Errorf("gateway manager: start adapter: %w", err)
	}

	m.running[workspaceGatewayKey(workspaceID, cfg.Platform)] = &gatewayRuntime{adapter: adapter, cfg: *cfg}
	m.service.AttachGateway(gatewayID, workspaceID, cfg.Platform, adapter)
	slog.Info("gateway started",
		"gateway_id", gatewayID, "workspace_id", workspaceID,
		"platform", cfg.Platform, "transport", cfg.Transport)
	return nil
}

// Stop shuts every gateway down (server shutdown, design D11).
func (m *Manager) Stop(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key := range m.running {
		m.stopLocked(keyWorkspace(key), keyPlatform(key))
	}
	m.cancel()
}

// HandleWebhook feeds one raw platform payload to the webhook receiver of
// the gateway it belongs to. The public ingress handler (composition wave)
// validates the secret token, then calls this. Unknown gateways are an
// error; malformed payloads are dropped inside the receiver.
func (m *Manager) HandleWebhook(ctx context.Context, workspaceID, platform string, raw []byte) error {
	m.mu.Lock()
	rt := m.running[workspaceGatewayKey(workspaceID, platform)]
	m.mu.Unlock()
	if rt == nil {
		return fmt.Errorf("%w: no running gateway for workspace", domain.ErrNotFound)
	}
	receiver, ok := rt.adapter.(WebhookReceiver)
	if !ok {
		return fmt.Errorf("gateway manager: gateway %s does not accept webhooks", rt.cfg.ID)
	}
	return receiver.IngestWebhook(ctx, raw)
}

// stopLocked stops and unregisters one gateway. m.mu must be held.
func (m *Manager) stopLocked(workspaceID, platform string) {
	key := workspaceGatewayKey(workspaceID, platform)
	rt := m.running[key]
	if rt == nil {
		return
	}
	delete(m.running, key)

	gatewayID := rt.cfg.ID
	if err := rt.adapter.Stop(context.WithoutCancel(m.base)); err != nil {
		slog.Error("gateway stop failed", "gateway_id", gatewayID, "err", err)
	}
	m.service.DetachGateway(gatewayID)
	slog.Info("gateway stopped", "gateway_id", gatewayID, "workspace_id", workspaceID)
}

// gatewayUnchanged reports whether a running adapter's config snapshot
// matches the stored config — identity, credential, transport, and bot
// username (a re-saved token must take effect; an enabled-flag flip is
// handled by the desired-state check, not here).
func gatewayUnchanged(a, b *domain.GatewayConfig) bool {
	return a.ID == b.ID &&
		a.BotTokenCiphertext == b.BotTokenCiphertext &&
		a.BotUsername == b.BotUsername &&
		a.Transport == b.Transport &&
		a.WebhookURL == b.WebhookURL
}

func workspaceGatewayKey(workspaceID, platform string) string {
	return workspaceID + ":" + platform
}

func keyWorkspace(key string) string {
	for i := 0; i < len(key); i++ {
		if key[i] == ':' {
			return key[:i]
		}
	}
	return key
}

func keyPlatform(key string) string {
	for i := 0; i < len(key); i++ {
		if key[i] == ':' {
			return key[i+1:]
		}
	}
	return key
}
