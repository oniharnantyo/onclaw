package server

// Gateway runtime (integrate-telegram-gateway D11, tasks 6.2): the
// composition-owned bundle that assembles the platform-neutral gateway core
// — pairing, router, approval bridge, service, adapter factory, and the
// lifecycle manager — around the runner. Built by the CLI composition root
// and shared with the HTTP layer through RouterOptions (the ChannelRuntime
// pattern); the router's nil fallback builds a fresh one for test assembly.
//
// Seams that live here because they belong to composition, not to the core:
//   - the adapter factory, dispatching on (platform, lane)
//     (add-whatsapp-gateway design D1/D10): telegram keeps the historical
//     construction path byte-identical; whatsapp+cloud_api builds the
//     Cloud API adapter over the decrypted credential envelope (webhook
//     registration with Meta is manual — no setWebhook equivalent is
//     called); whatsapp+multi_device builds the whatsmeow adapter over a
//     pgx-stdlib database/sql pool scoped to a per-gateway Postgres database
//     (design D6 as amended — see mdDevicePool);
//   - adapter resolution for the callers that hold no gateway id: attachment
//     ingress resolves by (workspace, platform) — exact for every gateway —
//     and the approval bridge's card/refusal delegate resolves the sole
//     attached adapter, failing loudly instead of cross-workspace sending
//     when several gateways run;
//   - the telegram webhook secret: derived per (workspace, platform) from the
//     instance encryption key, used both to register the webhook with Telegram
//     and to validate the X-Telegram-Bot-Api-Secret-Token header on the
//     public route;
//   - the WhatsApp seams the HTTP layer drives (add-whatsapp-gateway D5/D8):
//     cloud-envelope webhook authentication (X-Hub-Signature-256 and the GET
//     hub.challenge handshake), the credential probe behind the config PUT,
//     cloud health, and the multi-device pairing state machine — implemented
//     here over the adapter packages so the handlers package stays free of
//     platform details.

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/gateways"
	"github.com/oniharnantyo/onclaw/internal/gateways/adapters/telegram"
	"github.com/oniharnantyo/onclaw/internal/gateways/adapters/whatsappcloud"
	"github.com/oniharnantyo/onclaw/internal/gateways/adapters/whatsappmd"
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

// Capabilities implements PlatformAdapter by forwarding to the resolved
// adapter (add-whatsapp-gateway design D2).
func (a *soleGatewayAdapter) Capabilities() gateways.AdapterCapabilities {
	adapter, err := a.resolve()
	if err != nil {
		return gateways.AdapterCapabilities{}
	}
	return adapter.Capabilities()
}

func (a *soleGatewayAdapter) SendMessage(ctx context.Context, chatID, body, flavor string, opts gateways.SendOptions) (string, error) {
	adapter, err := a.resolve()
	if err != nil {
		return "", err
	}
	return adapter.SendMessage(ctx, chatID, body, flavor, opts)
}

func (a *soleGatewayAdapter) EditMessage(ctx context.Context, chatID, messageID, body, flavor string) error {
	adapter, err := a.resolve()
	if err != nil {
		return err
	}
	return adapter.EditMessage(ctx, chatID, messageID, body, flavor)
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

	// gatewayStore, encKey, databaseURL, and waCloudAPIBase back the
	// WhatsApp composition seams below (envelope decryption, the md device
	// pools, the cloud API base override). Resolved by the composition root
	// before construction; never mutated after NewGatewayRuntime returns.
	gatewayStore   store.GatewayStore
	encKey         []byte
	databaseURL    string
	waCloudAPIBase string
	mdMu           sync.Mutex
	mdDBs          map[string]*sql.DB // gateway id → its database-scoped whatsmeow pool (design D6 as amended, see mdDevicePool)
}

// NewGatewayRuntime assembles the gateway stack around the runner. Every
// dependency is required and resolved by the caller (injected dependencies
// are never nil): the granular gateway/outbox/session-event/agent/member/user
// stores, the runner as the run submitter, the attachment store and
// workspace blob resolver for media ingress, and the instance encryption key.
// databaseURL is the same PostgreSQL DSN the store uses — the multi-device
// lane bridges whatsmeow's sqlstore over it (design D6); whatsappCloudAPIBase
// overrides the Cloud API endpoint for the cloud lane (empty keeps
// whatsappcloud.DefaultAPIBase; tests and proxies).
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
	databaseURL string,
	whatsappCloudAPIBase string,
) *GatewayRuntime {
	rt := &GatewayRuntime{
		gatewayStore:   gatewayStore,
		encKey:         encryptionKey,
		databaseURL:    databaseURL,
		waCloudAPIBase: whatsappCloudAPIBase,
		mdDBs:          make(map[string]*sql.DB),
	}

	pairing := gateways.NewPairingService(links)
	usage := gateways.NewSessionUsageReader(sessionEvents)

	// The ingress stage and the bridge delegate resolve adapters through the
	// service; svc is assigned below, before the manager can start any
	// adapter (no inbound traffic exists before StartAll/Sync).
	var svc *gateways.Service

	// The approval bridge is constructed before the router so the text-reply
	// interception can consult its pending state (add-whatsapp-gateway
	// design D3): on CanButton=false platforms (whatsapp multi_device) the
	// router turns a DM'd APPROVE/DENY into the decision.
	bridge := gateways.NewApprovalBridge(submitter, &soleGatewayAdapter{
		resolve: func() (gateways.PlatformAdapter, error) { return svc.SoleAttachedAdapter() },
	}, links)

	gwRouter := gateways.NewRouter(
		gatewayStore, bindings, links, agentsStore, members, users, pairing, usage,
		gateways.WithIngress(&workspaceGatewayIngress{
			resolve: func(workspaceID, platform string) (gateways.PlatformAdapter, bool) {
				return svc.AdapterForWorkspace(workspaceID, platform)
			},
			attachments: attachments,
			blobs:       blobs,
		}),
		gateways.WithApprovalIntercept(bridge),
	)

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

	// Adapter factory (add-whatsapp-gateway design D10): dispatch on
	// (platform, lane). Telegram keeps the historical construction path —
	// including the setWebhook registration — byte-identical; the WhatsApp
	// lanes never call a registration endpoint (cloud ingestion is
	// webhook-only with manual Meta registration, design D8; the multi-device
	// lane ingests over its own socket).
	factory := func(spec gateways.AdapterSpec) (gateways.PlatformAdapter, error) {
		switch {
		case spec.Platform == domain.GatewayPlatformWhatsApp && spec.Lane == domain.GatewayLaneCloudAPI:
			// Cloud lane (design D5): the decrypted "bot token" IS the JSON
			// credential envelope; the adapter parses and validates it.
			var opts []whatsappcloud.Option
			if rt.waCloudAPIBase != "" {
				opts = append(opts, whatsappcloud.WithAPIBase(rt.waCloudAPIBase))
			}
			return whatsappcloud.NewAdapter(spec.GatewayID, spec.BotToken, svc, opts...)
		case spec.Platform == domain.GatewayPlatformWhatsApp && spec.Lane == domain.GatewayLaneMultiDevice:
			// Multi-device lane (design D6 as amended, see mdDevicePool): the
			// adapter needs a database/sql pool scoped to its own whatsmeow
			// device database — one linked device per gateway even on shared
			// instances.
			db, err := rt.mdDevicePool(spec.GatewayID)
			if err != nil {
				return nil, err
			}
			return whatsappmd.NewAdapter(spec.GatewayID, db, svc)
		default:
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
					secret := handlers.GatewayWebhookSecret(encryptionKey, spec.GatewayID)
					if err := adapter.RegisterWebhook(regCtx, spec.WebhookURL, secret); err != nil {
						return nil, fmt.Errorf("register webhook: %w", err)
					}
				}
			return adapter, nil
		}
	}

	manager := gateways.NewManager(gatewayStore, gatewayTokenDecryptor{encKey: encryptionKey}, factory, svc)

	rt.Pairing = pairing
	rt.Service = svc
	rt.Manager = manager
	rt.Outbox = outboxSvc
	rt.Verifier = botTokenVerifier{}
	return rt
}

// -------------------------------------------------------------------------
// WhatsApp composition seams (add-whatsapp-gateway D5/D8/D10)
// -------------------------------------------------------------------------

// nopInboundHandler absorbs adapter deliveries for throwaway probe adapters:
// the config-PUT credential probe constructs an adapter solely to call its
// phone-number probe; it is never started and never attached to the service.
type nopInboundHandler struct{}

func (nopInboundHandler) HandleMessage(context.Context, string, gateways.InboundMessage) {}
func (nopInboundHandler) HandleCallback(context.Context, string, gateways.Callback)      {}

// decryptCloudCredentials resolves the decrypted cloud-lane
// credential envelope by gateway ID (design D5): the AES-256-GCM secrets envelope,
// AAD-bound to the workspace, parsed by the adapter's validator.
func (r *GatewayRuntime) decryptCloudCredentials(ctx context.Context, gatewayID string) (whatsappcloud.Credentials, error) {
	if a, ok := r.Service.Adapter(gatewayID); ok {
		if cloud, isCloud := a.(*whatsappcloud.Adapter); isCloud {
			return cloud.Credentials(), nil
		}
	}
	return whatsappcloud.Credentials{}, fmt.Errorf("%w: whatsapp cloud credentials not found or gateway not running", domain.ErrNotFound)
}

// VerifyWebhookSignature implements the handlers.WhatsAppWebhookAuth
// contract (add-whatsapp-gateway design D8): the X-Hub-Signature-256 header
// is validated against the gateway's app secret from the decrypted
// envelope. Every failure — no envelope, undecryptable, bad signature — is
// fail-closed false.
func (r *GatewayRuntime) VerifyWebhookSignature(ctx context.Context, gatewayID string, raw []byte, signatureHeader string) bool {
	a, ok := r.Service.Adapter(gatewayID)
	if !ok {
		return false
	}
	cloud, isCloud := a.(*whatsappcloud.Adapter)
	if !isCloud {
		return false
	}
	return whatsappcloud.VerifySignature(cloud.Credentials().AppSecret, raw, signatureHeader)
}

// VerifyChallengeToken implements the handlers.WhatsAppWebhookAuth contract
// (design D8): the Meta verification handshake's hub.verify_token is matched
// against the gateway's stored verify token. Constant-time compare.
func (r *GatewayRuntime) VerifyChallengeToken(ctx context.Context, gatewayID, verifyToken string) bool {
	a, ok := r.Service.Adapter(gatewayID)
	if !ok {
		return false
	}
	cloud, isCloud := a.(*whatsappcloud.Adapter)
	if !isCloud {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(cloud.Credentials().VerifyToken), []byte(verifyToken)) == 1
}

// CloudHealth implements the handlers.WhatsAppRuntime contract (design D10):
// the cloud lane's health check is the phone-number metadata probe against
// the RUNNING gateway's adapter. An error carries the human-readable detail.
func (r *GatewayRuntime) CloudHealth(ctx context.Context, gatewayID string) (string, error) {
	a, ok := r.Service.Adapter(gatewayID)
	if !ok {
		return "", errors.New("the WhatsApp gateway is not running")
	}
	cloud, ok := a.(*whatsappcloud.Adapter)
	if !ok {
		return "", errors.New("the WhatsApp gateway is not on the cloud lane")
	}
	profile, err := cloud.Probe(ctx)
	if err != nil {
		return "", err
	}
	detail := profile.VerifiedName
	if profile.DisplayPhoneNumber != "" {
		if detail != "" {
			detail += " · "
		}
		detail += profile.DisplayPhoneNumber
	}
	return detail, nil
}

// ProbeCredentials implements the handlers.WhatsAppRuntime contract: a
// candidate credential envelope is validated live (parse + phone-number
// probe) BEFORE save — the botTokenVerifier analogue for the cloud lane
// (design D10). Returns the verified display identity.
func (r *GatewayRuntime) ProbeCredentials(ctx context.Context, credentialJSON string) (string, error) {
	// The probe adapter honors the same API base override as the factory so
	// stubbed deployments (and tests) probe the reachable endpoint.
	var opts []whatsappcloud.Option
	if r.waCloudAPIBase != "" {
		opts = append(opts, whatsappcloud.WithAPIBase(r.waCloudAPIBase))
	}
	probe, err := whatsappcloud.NewAdapter("credential-probe", credentialJSON, nopInboundHandler{}, opts...)
	if err != nil {
		return "", err
	}
	profile, err := probe.Probe(ctx)
	if err != nil {
		return "", err
	}
	if profile.VerifiedName != "" {
		return profile.VerifiedName, nil
	}
	return profile.DisplayPhoneNumber, nil
}

// PairingState implements the handlers.WhatsAppRuntime contract: the
// multi-device pairing/link snapshot of the gateway's RUNNING md adapter
// (design D10). found is false when no md adapter is running (gateway
// disabled, never enabled, or another lane) — the caller renders not_started.
func (r *GatewayRuntime) PairingState(ctx context.Context, gatewayID string) (handlers.WhatsAppPairingSnapshot, bool) {
	a, ok := r.Service.Adapter(gatewayID)
	if !ok {
		return handlers.WhatsAppPairingSnapshot{}, false
	}
	md, ok := a.(*whatsappmd.Adapter)
	if !ok {
		return handlers.WhatsAppPairingSnapshot{}, false
	}
	st := md.PairingStatus()
	return handlers.WhatsAppPairingSnapshot{
		State:         string(st.State),
		Connection:    string(st.Connection),
		QRDataURL:     st.QRDataURL,
		QR:            st.QRContent,
		PairCode:      st.PairCode,
		LinkedNumber:  st.LinkedNumber,
		PairExpiresAt: st.PairExpiresAt,
		Error:         st.Error,
	}, true
}

// StartPairing implements the handlers.WhatsAppRuntime contract: run the md
// pairing flow on the gateway's running adapter (QR, or pair code when
// phone carries account digits).
func (r *GatewayRuntime) StartPairing(ctx context.Context, gatewayID, phone string) error {
	md, ok := r.mdAdapter(gatewayID)
	if !ok {
		return errors.New("the WhatsApp gateway is not running — enable the multi-device lane first")
	}
	return md.StartPairing(ctx, phone)
}

// RegeneratePairing implements the handlers.WhatsAppRuntime contract.
func (r *GatewayRuntime) RegeneratePairing(ctx context.Context, gatewayID string) error {
	md, ok := r.mdAdapter(gatewayID)
	if !ok {
		return errors.New("the WhatsApp gateway is not running — enable the multi-device lane first")
	}
	return md.Regenerate(ctx)
}

// LogoutDevice implements the handlers.WhatsAppRuntime contract: tear the
// linked device down (session rows deleted from the gateway's schema).
func (r *GatewayRuntime) LogoutDevice(ctx context.Context, gatewayID string) error {
	md, ok := r.mdAdapter(gatewayID)
	if !ok {
		return errors.New("the WhatsApp gateway is not running — enable the multi-device lane first")
	}
	return md.Logout(ctx)
}

// mdAdapter resolves the running multi-device adapter for a gateway ID.
func (r *GatewayRuntime) mdAdapter(gatewayID string) (*whatsappmd.Adapter, bool) {
	a, ok := r.Service.Adapter(gatewayID)
	if !ok {
		return nil, false
	}
	md, ok := a.(*whatsappmd.Adapter)
	return md, ok
}

// mdDevicePool opens (once per gateway) the database/sql pool the gateway's
// whatsmeow sqlstore runs over (design D6, storage scope amended — see the
// deviation note on mdDatabaseName): the pgx stdlib adapter on the same
// Postgres instance as the store, connected to a per-gateway device DATABASE
// so this whatsmeow release serves exactly one linked device per gateway.
// The database is created on first use and never dropped or recreated on
// boot — device sessions survive restarts. The pool lives for the process
// lifetime (the adapter never closes it — the composition root owns it).
//
// Why a database, not a schema: dbutil (go.mau.fi/util), which runs
// whatsmeow's store self-upgrade, probes table/column existence in
// information_schema UNQUALIFIED — database-wide — while resolving
// whatsmeow_version through search_path — per-schema. With any other schema
// holding whatsmeow_* tables, a fresh per-gateway schema's existence checks
// answer true from the foreign tables, CREATE is skipped, and the first
// query fails 42P01 (relation "whatsmeow_version" does not exist) — the
// second md gateway could never start. information_schema is per-database,
// so isolating gateways by database scopes those checks correctly without
// vendoring dbutil's DDL (forbidden). The trade-off is the CREATEDB
// requirement, probed below with an actionable error.
func (r *GatewayRuntime) mdDevicePool(gatewayID string) (*sql.DB, error) {
	r.mdMu.Lock()
	defer r.mdMu.Unlock()
	if db, ok := r.mdDBs[gatewayID]; ok {
		return db, nil
	}
	if r.databaseURL == "" {
		return nil, errors.New("whatsapp multi-device lane: no database URL configured for the device store")
	}
	deviceName := mdDatabaseName(gatewayID)
	connCfg, err := pgx.ParseConfig(r.databaseURL)
	if err != nil {
		return nil, fmt.Errorf("whatsappmd device store: parse dsn: %w", err)
	}
	// The device connections must not inherit the app DSN's startup
	// parameters: integration DSNs pin a search_path, which would land
	// whatsmeow's tables in the wrong schema of the new database.
	delete(connCfg.RuntimeParams, "options")
	delete(connCfg.RuntimeParams, "search_path")

	// Provision the per-gateway device database over an admin connection to
	// the app database (CREATE DATABASE cannot run inside a transaction and
	// is a no-op when the database already exists — the boot path hits the
	// existence check and never recreates).
	admin := stdlib.OpenDB(*connCfg.Copy())
	defer admin.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := ensureMdDeviceDatabase(ctx, admin, deviceName); err != nil {
		return nil, err
	}

	deviceCfg := connCfg.Copy()
	deviceCfg.Database = deviceName
	db := stdlib.OpenDB(*deviceCfg)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("whatsappmd device store: connect to %s: %w", deviceName, err)
	}
	r.mdDBs[gatewayID] = db
	return db, nil
}

// ensureMdDeviceDatabase creates the named device database when missing. The
// CREATEDB privilege is probed first so a role that can never provision the
// database fails LOUDLY with the remedy, not with a cryptic permission error
// mid-upgrade. A concurrent provisioner winning the race (SQLSTATE 42P04,
// duplicate_database) is success.
func ensureMdDeviceDatabase(ctx context.Context, admin *sql.DB, deviceName string) error {
	var role string
	var createdb bool
	if err := admin.QueryRowContext(ctx,
		`SELECT current_user, rolcreatedb FROM pg_roles WHERE rolname = current_user`,
	).Scan(&role, &createdb); err != nil {
		return fmt.Errorf("whatsappmd device store: probe role privileges: %w", err)
	}
	if !createdb {
		return fmt.Errorf(
			"whatsapp multi-device lane: the database role %q lacks the CREATEDB privilege required to provision the per-gateway device database %s — grant it once (ALTER ROLE %q CREATEDB) or pre-create the database",
			role, deviceName, role)
	}
	var exists bool
	if err := admin.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, deviceName,
	).Scan(&exists); err != nil {
		return fmt.Errorf("whatsappmd device store: probe %s: %w", deviceName, err)
	}
	if exists {
		return nil
	}
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+deviceName); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.DuplicateDatabase {
			return nil
		}
		return fmt.Errorf("whatsappmd device store: create database %s: %w", deviceName, err)
	}
	return nil
}

// mdDatabaseName derives the per-gateway whatsmeow device database
// (add-whatsapp-gateway design D6 as amended by the fix wave): the gateway
// id's hexits are identifier-safe once the hyphens are gone, so the name
// needs no quoting anywhere — neither in CREATE DATABASE nor in the pool's
// connection config. 9 + 32 = 41 bytes, well under Postgres's 63-byte limit.
//
// DEVIATION from D6's letter: D6 says "the same DATABASE_URL ... one
// container table per workspace gateway". The same DSN would confine the
// isolation to a per-gateway schema, which dbutil's database-wide existence
// probes cannot support (see mdDevicePool). The amendment keeps D6's
// substance — the same Postgres instance and credentials, one dedicated
// container scope per gateway, no main-migration involvement — while moving
// the boundary from schema to database.
func mdDatabaseName(gatewayID string) string {
	return "whatsmeow_" + strings.ReplaceAll(gatewayID, "-", "")
}
