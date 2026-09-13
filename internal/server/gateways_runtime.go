package server

// Gateway runtime (integrate-telegram-gateway D11, tasks 6.2): the
// composition-owned bundle that assembles the platform-neutral gateway core
// — pairing, router, approval bridge, service, adapter factory, and the
// lifecycle manager — around the runner. Built by the CLI composition root
// and shared with the HTTP layer through RouterOptions (the ChannelRuntime
// pattern); the router's nil fallback builds a fresh one for test assembly.
//
// Two seams live here because they belong to composition, not to the core:
//   - adapter resolution for the callers that hold no gateway id: attachment
//     ingress resolves by (workspace, platform) — exact for every gateway —
//     and the approval bridge's card/refusal delegate resolves the sole
//     attached adapter, failing loudly instead of cross-workspace sending
//     when several gateways run;
//   - the webhook secret: derived per (workspace, platform) from the instance
//     encryption key, used both to register the webhook with Telegram and to
//     validate the X-Telegram-Bot-Api-Secret-Token header on the public route.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/gateways"
	"github.com/oniharnantyo/onclaw/internal/gateways/adapters/telegram"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// webhookRegisterTimeout bounds the setWebhook call inside the adapter
// factory; adapter construction must not hang the manager's reconciliation.
const webhookRegisterTimeout = 15 * time.Second

// gatewayTokenDecryptor resolves a gateway's encrypted bot credential for the
// lifecycle manager: the AES-256-GCM secrets envelope, AAD-bound to the
// workspace (the provider-credential pattern).
type gatewayTokenDecryptor struct {
	encKey []byte
}

// DecryptGatewayToken implements gateways.TokenDecryptor.
func (d gatewayTokenDecryptor) DecryptGatewayToken(_ context.Context, workspaceID, envelope string) (string, error) {
	plaintext, err := secrets.Decrypt(d.encKey, []byte(workspaceID), envelope)
	if err != nil {
		return "", fmt.Errorf("gateway token decrypt: %w", err)
	}
	return string(plaintext), nil
}

// botTokenVerifier probes the Telegram Bot API getMe with a candidate bot
// token: the PUT/test paths resolve the bot username (and validity) without
// persisting anything. A narrow type here keeps the handlers package free of
// platform details.
type botTokenVerifier struct {
	apiBase string
	client  *http.Client
}

// VerifyBotToken implements the handlers.GatewayBotVerifier contract: it
// returns the bot username on success and an error carrying the platform's
// description otherwise.
func (v botTokenVerifier) VerifyBotToken(ctx context.Context, token string) (string, error) {
	apiBase := v.apiBase
	if apiBase == "" {
		apiBase = telegram.DefaultAPIBase
	}
	client := v.client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/bot"+token+"/getMe", nil)
	if err != nil {
		return "", fmt.Errorf("telegram getMe: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("telegram getMe: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("telegram getMe: read: %w", err)
	}

	var envelope struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		Result      struct {
			ID       int64  `json:"id"`
			Username string `json:"username"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return "", fmt.Errorf("telegram getMe: decode (http %d)", resp.StatusCode)
	}
	if !envelope.OK {
		return "", errors.New("telegram rejected the bot token: " + envelope.Description)
	}
	return envelope.Result.Username, nil
}

// workspaceGatewayIngress adapts the core attachment/voice ingress stage to
// per-workspace adapter resolution (task 6.2): Telegram file ids are
// bot-scoped, so a download must run on the ingesting workspace's own bot.
// The core Ingress is constructed per call — it is stateless configuration.
type workspaceGatewayIngress struct {
	resolve     func(workspaceID, platform string) (gateways.PlatformAdapter, bool)
	attachments store.AttachmentStore
	blobs       gateways.StorageResolver
}

// Enrich implements gateways.IngressStage.
func (g *workspaceGatewayIngress) Enrich(ctx context.Context, workspaceID string, msg gateways.InboundMessage) (string, []agents.AttachmentRef, string, error) {
	adapter, ok := g.resolve(workspaceID, msg.Platform)
	if !ok {
		// Fail-soft refusal (design D8 shape): the gateway's adapter is not
		// running (disabled between message arrival and media fetch).
		return "", nil, "The gateway is not accepting files right now. Please try again shortly.", nil
	}
	return gateways.NewIngress(adapter, g.attachments, g.blobs).Enrich(ctx, workspaceID, msg)
}

// soleGatewayAdapter implements PlatformAdapter by forwarding every call to
// the service's sole attached adapter. The approval bridge sends cards and
// refusals with chat coordinates only — no gateway identity — so with zero
// or multiple attached gateways it fails loudly rather than delivering
// through the wrong workspace's bot (flagged as follow-up design work:
// thread gateway-aware resolution through the bridge).
type soleGatewayAdapter struct {
	resolve func() (gateways.PlatformAdapter, error)
}

func (a *soleGatewayAdapter) Start(context.Context) error {
	return errors.New("gateway adapter delegate: lifecycle is owned by the gateway manager")
}

func (a *soleGatewayAdapter) Stop(context.Context) error {
	return errors.New("gateway adapter delegate: lifecycle is owned by the gateway manager")
}

func (a *soleGatewayAdapter) SendMessage(ctx context.Context, chatID, htmlText string, opts gateways.SendOptions) (string, error) {
	adapter, err := a.resolve()
	if err != nil {
		return "", err
	}
	return adapter.SendMessage(ctx, chatID, htmlText, opts)
}

func (a *soleGatewayAdapter) EditMessage(ctx context.Context, chatID, messageID, htmlText string) error {
	adapter, err := a.resolve()
	if err != nil {
		return err
	}
	return adapter.EditMessage(ctx, chatID, messageID, htmlText)
}

func (a *soleGatewayAdapter) SendTyping(ctx context.Context, chatID string) error {
	adapter, err := a.resolve()
	if err != nil {
		return err
	}
	return adapter.SendTyping(ctx, chatID)
}

func (a *soleGatewayAdapter) SendApprovalCard(ctx context.Context, chatID string, interrupt agents.ApprovalPayload) (string, error) {
	adapter, err := a.resolve()
	if err != nil {
		return "", err
	}
	return adapter.SendApprovalCard(ctx, chatID, interrupt)
}

func (a *soleGatewayAdapter) DownloadFile(ctx context.Context, fileID string) ([]byte, error) {
	adapter, err := a.resolve()
	if err != nil {
		return nil, err
	}
	return adapter.DownloadFile(ctx, fileID)
}

// gatewayRunSubmitter adapts *agents.Runner to gateways.RunSubmitter: the
// runner's Resume takes the approval payload by pointer while the gateway
// core passes it by value (the bridge always holds a decoded payload).
type gatewayRunSubmitter struct {
	runner *agents.Runner
}

// GatewayRunSubmitter adapts the runner to the gateway core's RunSubmitter
// seam (the composition-level pointer adaptation).
func GatewayRunSubmitter(runner *agents.Runner) gateways.RunSubmitter {
	return gatewayRunSubmitter{runner: runner}
}

// Run implements gateways.RunSubmitter.
func (s gatewayRunSubmitter) Run(ctx context.Context, req agents.ExecRequest) (*agents.EventStream, error) {
	return s.runner.Run(ctx, req)
}

// Resume implements gateways.RunSubmitter.
func (s gatewayRunSubmitter) Resume(ctx context.Context, req agents.ExecRequest, approval agents.ApprovalPayload, approved bool) (*agents.EventStream, error) {
	return s.runner.Resume(ctx, req, &approval, approved)
}

// GatewayRuntime is the composition-owned gateway assembly shared by the
// runner, the HTTP handlers, and the server lifecycle (design D11).
type GatewayRuntime struct {
	// Pairing backs the pairing-token endpoints (handlers) and the router.
	Pairing *gateways.PairingService
	// Service is the inbound orchestrator (the adapters' InboundHandler).
	Service *gateways.Service
	// Manager owns adapter lifecycle: StartAll at boot, Sync on config
	// changes (the handlers call it after every mutation), Stop at shutdown.
	Manager *gateways.Manager
	// Outbox is the durable delivery loop (design D9): the composition root
	// runs Start on its lifecycle context — its first cycle is the startup
	// redelivery sweep, cancellation stops it at shutdown.
	Outbox *gateways.Outbox
	// Verifier probes bot tokens for the PUT/test endpoints.
	Verifier handlers.GatewayBotVerifier
}

// NewGatewayRuntime assembles the gateway stack around the runner. Every
// dependency is required and resolved by the caller (injected dependencies
// are never nil): the granular gateway/outbox/session-event/agent/member/user
// stores, the runner as the run submitter, the attachment store and
// workspace blob resolver for media ingress, and the instance encryption key.
func NewGatewayRuntime(
	gatewayStore store.GatewayStore,
	bindings store.GatewayBindings,
	links store.GatewayLinks,
	outboxStore store.GatewayOutbox,
	sessionEvents store.SessionEventStore,
	members store.MemberStore,
	agentsStore store.AgentStore,
	users store.UserStore,
	submitter gateways.RunSubmitter,
	attachments store.AttachmentStore,
	blobs gateways.StorageResolver,
	encryptionKey []byte,
) *GatewayRuntime {
	pairing := gateways.NewPairingService(links)
	usage := gateways.NewSessionUsageReader(sessionEvents)

	// The ingress stage and the bridge delegate resolve adapters through the
	// service; svc is assigned below, before the manager can start any
	// adapter (no inbound traffic exists before StartAll/Sync).
	var svc *gateways.Service

	gwRouter := gateways.NewRouter(
		gatewayStore, bindings, links, agentsStore, members, users, pairing, usage,
		gateways.WithIngress(&workspaceGatewayIngress{
			resolve: func(workspaceID, platform string) (gateways.PlatformAdapter, bool) {
				return svc.AdapterForWorkspace(workspaceID, platform)
			},
			attachments: attachments,
			blobs:       blobs,
		}),
	)
	bridge := gateways.NewApprovalBridge(submitter, &soleGatewayAdapter{
		resolve: func() (gateways.PlatformAdapter, error) { return svc.SoleAttachedAdapter() },
	}, links)

	// Delivery outbox (design D9): the streamer commits every final reply
	// write-before-send; this service redelivers committed-but-unsent rows on
	// its background loop. Entries resolve their send seam by the generating
	// gateway's id — one adapter per workspace gateway on multi-workspace
	// instances — through the service's registry (read after adapters attach;
	// no inbound traffic exists before StartAll/Sync).
	outboxSvc := gateways.NewOutbox(outboxStore, func(gatewayID string) (gateways.MessageSender, bool) {
		return svc.Adapter(gatewayID)
	})
	svc = gateways.NewService(gwRouter, submitter, bridge, gatewayStore, outboxSvc)

	factory := func(spec gateways.AdapterSpec) (gateways.PlatformAdapter, error) {
		var opts []telegram.Option
		if spec.Transport == domain.GatewayTransportWebhook {
			opts = append(opts, telegram.WithTransportMode(telegram.TransportModeWebhook))
		}
		adapter, err := telegram.NewAdapter(spec.GatewayID, spec.BotToken, svc, opts...)
		if err != nil {
			return nil, err
		}
		// Webhook transport (task 6.2): point Telegram at the public URL with
		// the derived secret the ingress route validates. Long polling needs
		// no registration.
		if spec.Transport == domain.GatewayTransportWebhook && spec.WebhookURL != "" {
			regCtx, cancel := context.WithTimeout(context.Background(), webhookRegisterTimeout)
			defer cancel()
			secret := handlers.GatewayWebhookSecret(encryptionKey, spec.WorkspaceID, spec.Platform)
			if err := adapter.RegisterWebhook(regCtx, spec.WebhookURL, secret); err != nil {
				return nil, fmt.Errorf("register webhook: %w", err)
			}
		}
		return adapter, nil
	}

	manager := gateways.NewManager(gatewayStore, gatewayTokenDecryptor{encKey: encryptionKey}, factory, svc)

	return &GatewayRuntime{
		Pairing:  pairing,
		Service:  svc,
		Manager:  manager,
		Outbox:   outboxSvc,
		Verifier: botTokenVerifier{},
	}
}
