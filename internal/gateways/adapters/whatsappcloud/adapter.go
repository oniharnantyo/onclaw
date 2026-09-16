package whatsappcloud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/gateways"
)

// Adapter knobs pinned by the change spec.
const (
	// TypingHeartbeat is the mark-read + typing cadence composition
	// configures for the streamer's typing loop on cloud-lane gateways
	// (WithTypingInterval). Cloud API typing is tied to marking the user's
	// message read, so the cadence is deliberately slower than Telegram's —
	// openspec add-whatsapp-gateway task 8.1 (live spike pending) owns the
	// final number; until probed, 25 s is pinned here.
	TypingHeartbeat = 25 * time.Second

	// bodyHardLimit is the WhatsApp text cap (add-whatsapp-gateway design
	// D9, same 4,096 as Telegram). The core renders/splits to a 4,000-char
	// budget before sending; this backstop refuses an oversized body with a
	// clear error instead of a wire failure.
	bodyHardLimit = 4096

	// buttonTitleLimit is the observed quick-reply title cap (~20 chars,
	// design Context). "Approve"/"Deny" sit well under it.
	buttonTitleLimit = 20
)

// ErrInvalid mirrors domain.ErrInvalid without importing domain — the
// adapter package stays a leaf against the gateway core.
var ErrInvalid = errors.New("invalid request")

// ErrEditingNotSupported is the honest edit contract (add-whatsapp-gateway
// design D2): the Cloud API has no edit-sent-message endpoint,
// Capabilities().CanEdit is false, and the capability-aware core (streamer,
// approval bridge) never calls EditMessage on this adapter — but a call that
// slips through must fail loudly, never silently no-op.
var ErrEditingNotSupported = errors.New("whatsapp cloud api cannot edit sent messages")

// ErrBodyTooLong backs the 4,096-character text cap (design D9): the core's
// split machinery budgets under it, so tripping this means a caller bypassed
// the splitter.
var ErrBodyTooLong = errors.New("message body exceeds the whatsapp text limit")

// Credentials is the decrypted cloud-lane credential envelope
// (add-whatsapp-gateway design D5): the decryptor stays string-shaped and
// this package owns the JSON shape {"access_token","phone_number_id",
// "app_secret","verify_token"}. The token and secret are never logged or
// serialized by the adapter.
type Credentials struct {
	AccessToken   string `json:"access_token"`
	PhoneNumberID string `json:"phone_number_id"`
	AppSecret     string `json:"app_secret"`
	VerifyToken   string `json:"verify_token"`
}

// ParseCredentials validates the decrypted envelope (tasks.md 3.3): all four
// fields are required — the app secret and verify token are consumed by the
// webhook ingress (signature validation, GET handshake), so an envelope
// missing them would break ingestion half-configured.
func ParseCredentials(credentialJSON string) (Credentials, error) {
	var c Credentials
	if err := json.Unmarshal([]byte(credentialJSON), &c); err != nil {
		return Credentials{}, fmt.Errorf("%w: credential envelope is not valid JSON: %v", ErrInvalid, err)
	}
	for field, value := range map[string]string{
		"access_token":    c.AccessToken,
		"phone_number_id": c.PhoneNumberID,
		"app_secret":      c.AppSecret,
		"verify_token":    c.VerifyToken,
	} {
		if value == "" {
			return Credentials{}, fmt.Errorf("%w: credential envelope is missing %q", ErrInvalid, field)
		}
	}
	return c, nil
}

// PhoneNumberProfile is the phone-number metadata the health probe returns
// (GET /{phone_number_id}) — composition surfaces it as the cloud lane's
// health check (add-whatsapp-gateway design D10).
type PhoneNumberProfile struct {
	ID                 string `json:"id"`
	VerifiedName       string `json:"verified_name"`
	DisplayPhoneNumber string `json:"display_phone_number"`
	QualityRating      string `json:"quality_rating"`
}

// Adapter implements gateways.PlatformAdapter for the WhatsApp Cloud API
// (cloud lane). One adapter per enabled gateway: constructed by the
// composition root's adapter factory on (platform, lane) — design D1/D10 —
// started/stopped by the gateway lifecycle manager, and delivering
// normalized webhook traffic to the service through the InboundHandler.
// Ingestion is webhook-only: Start validates the credential and returns —
// there is no poll loop (the Cloud API has no polling surface); raw payloads
// arrive via IngestWebhook through gateways.Manager.HandleWebhook.
type Adapter struct {
	gatewayID string
	handler   gateways.InboundHandler
	transport Transport
	creds     Credentials

	// defaultTransport is the adapter's own HTTP transport — WithAPIBase,
	// WithAPIVersion, and WithHTTPClient retarget it; WithTransport replaces
	// a.transport wholesale (tests inject the fake).
	defaultTransport *httpTransport

	mu           sync.Mutex
	started      bool
	stopped      bool
	verifiedName string

	// lastInbound tracks each chat's most recent inbound message id so
	// SendTyping can mark it read + flash the typing indicator (Cloud API
	// typing is tied to mark-as-read). Guarded by mu.
	lastInbound map[string]string

	// Inbound dedup ring: Meta redelivers webhook payloads on retry.
	seenMu    sync.Mutex
	seen      map[string]struct{}
	seenOrder []string
	seenMax   int
}

// Option customizes an Adapter.
type Option func(*Adapter)

// WithAPIBase overrides the Graph API base URL (proxies, tests).
func WithAPIBase(base string) Option {
	return func(a *Adapter) {
		if a.defaultTransport != nil {
			a.defaultTransport.apiBase = base
		}
	}
}

// WithAPIVersion overrides the pinned Graph API version path.
func WithAPIVersion(version string) Option {
	return func(a *Adapter) {
		if a.defaultTransport != nil {
			a.defaultTransport.apiVersion = version
		}
	}
}

// WithHTTPClient overrides the HTTP client (tests, proxies).
func WithHTTPClient(client *http.Client) Option {
	return func(a *Adapter) {
		if a.defaultTransport != nil {
			a.defaultTransport.client = client
		}
	}
}

// WithTransport replaces the transport wholesale (tests inject the fake).
func WithTransport(t Transport) Option {
	return func(a *Adapter) { a.transport = t }
}

// NewAdapter builds the Cloud API adapter for one gateway. credentialJSON is
// the DECRYPTED envelope plaintext (tasks.md 3.3, design D5): the
// composition's decryptor stays string-shaped, and this constructor parses
// and validates the JSON shape internally. The handler is required — the
// adapter delivers all normalized traffic through it.
func NewAdapter(gatewayID, credentialJSON string, handler gateways.InboundHandler, opts ...Option) (*Adapter, error) {
	if gatewayID == "" {
		return nil, fmt.Errorf("%w: whatsapp cloud adapter needs a gateway id", ErrInvalid)
	}
	creds, err := ParseCredentials(credentialJSON)
	if err != nil {
		return nil, fmt.Errorf("whatsapp cloud adapter: %w", err)
	}
	if handler == nil {
		return nil, fmt.Errorf("%w: whatsapp cloud adapter needs an inbound handler", ErrInvalid)
	}
	httpTransport := newHTTPTransport(creds, DefaultAPIBase, APIVersion, nil)
	a := &Adapter{
		gatewayID:        gatewayID,
		handler:          handler,
		transport:        httpTransport,
		creds:            creds,
		defaultTransport: httpTransport,
		lastInbound:      make(map[string]string),
		seen:             make(map[string]struct{}),
		seenMax:          512,
	}
	for _, opt := range opts {
		opt(a)
	}
	return a, nil
}

// Credentials returns the parsed envelope — composition reads AppSecret and
// VerifyToken for the webhook ingress (signature validation and the GET
// handshake) without re-parsing the plaintext (design D5/D8).
func (a *Adapter) Credentials() Credentials {
	return a.creds
}

// Start implements PlatformAdapter: validate the credential live via the
// phone-number metadata probe (the telegram getMe analogue), then return —
// cloud ingestion is webhook-only and arrives through IngestWebhook; webhook
// registration with Meta is manual (dashboard paste, design D8). Idempotent
// per instance.
func (a *Adapter) Start(ctx context.Context) error {
	a.mu.Lock()
	if a.stopped {
		a.mu.Unlock()
		return errors.New("whatsapp cloud adapter: cannot start after stop")
	}
	if a.started {
		a.mu.Unlock()
		return nil
	}
	a.mu.Unlock()

	// The probe validates the access token and phone number id before the
	// gateway goes live — a bad credential must fail Sync, not first
	// delivery.
	profile, err := a.Probe(ctx)
	if err != nil {
		return fmt.Errorf("whatsapp cloud adapter: probe: %w", err)
	}

	a.mu.Lock()
	if a.started || a.stopped {
		a.mu.Unlock()
		return nil
	}
	a.started = true
	a.verifiedName = profile.VerifiedName
	a.mu.Unlock()

	slog.Info("whatsapp cloud adapter started",
		"gateway_id", a.gatewayID, "phone_number_id", a.creds.PhoneNumberID,
		"verified_name", profile.VerifiedName)
	return nil
}

// Stop implements PlatformAdapter: there is no ingestion loop to tear down
// (webhook-only), so this only closes the adapter to restarts. Idempotent.
func (a *Adapter) Stop(_ context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stopped = true
	return nil
}

// Probe fetches the phone-number metadata — the cloud lane's health check
// (add-whatsapp-gateway design D10; the telegram analogue is getMe).
func (a *Adapter) Probe(ctx context.Context) (PhoneNumberProfile, error) {
	raw, err := a.transport.PhoneNumber(ctx)
	if err != nil {
		return PhoneNumberProfile{}, err
	}
	var profile PhoneNumberProfile
	if err := json.Unmarshal(raw, &profile); err != nil {
		return PhoneNumberProfile{}, fmt.Errorf("whatsapp cloud probe: decode: %w", err)
	}
	return profile, nil
}

// Capabilities implements PlatformAdapter (add-whatsapp-gateway design
// D2/D3): the Cloud API has no edit endpoint (CanEdit false) but working
// quick-reply buttons (CanButton true), so approval cards render interactive
// and resolve with a receipt follow-up instead of an edit.
func (a *Adapter) Capabilities() gateways.AdapterCapabilities {
	return gateways.AdapterCapabilities{CanEdit: false, CanButton: true}
}

// flavorMatches reports whether body is rendered in the format this adapter
// speaks (add-whatsapp-gateway design D9). An empty flavor means the core's
// format-agnostic copy paths and is accepted as this adapter's own WhatsApp
// markdown wire format; a body rendered in another platform's flavor is
// refused, never misparsed.
func flavorMatches(flavor string) bool {
	return flavor == "" || flavor == gateways.FlavorWhatsAppMD
}

// SendMessage implements PlatformAdapter: post a text message. There is no
// plain-text retry — the WhatsApp markdown subset cannot be rejected by the
// wire (unknown markup renders literally, design D9; the flavor's
// PlainFallback is the identity), so a send failure is terminal here and the
// outbox's reschedule owns the retry. DisablePreview is ignored: WhatsApp
// has no preview toggle on this surface (design D9).
func (a *Adapter) SendMessage(ctx context.Context, chatID, body, flavor string, opts gateways.SendOptions) (string, error) {
	if !flavorMatches(flavor) {
		return "", fmt.Errorf("whatsapp cloud adapter: cannot deliver %q body", flavor)
	}
	if n := utf8.RuneCountInString(body); n > bodyHardLimit {
		return "", fmt.Errorf("%w: body is %d runes, cap is %d", ErrBodyTooLong, n, bodyHardLimit)
	}
	payload, err := json.Marshal(map[string]any{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                chatID,
		"type":              "text",
		"text":              map[string]any{"body": body},
	})
	if err != nil {
		return "", fmt.Errorf("whatsapp cloud send message: encode payload: %w", err)
	}

	raw, err := a.transport.SendMessage(ctx, payload)
	if err != nil {
		return "", fmt.Errorf("whatsapp cloud send message: %w", err)
	}
	return decodeMessageID(raw)
}

// EditMessage implements PlatformAdapter: refused — the Cloud API cannot
// edit sent messages (ErrEditingNotSupported, design D2). Capability
// negotiation means the core never calls this; the error is the honest
// contract, not a silent no-op.
func (a *Adapter) EditMessage(_ context.Context, chatID, messageID, _, _ string) error {
	return fmt.Errorf("%w: message %s in chat %s", ErrEditingNotSupported, messageID, chatID)
}

// SendTyping implements PlatformAdapter: mark the chat's last inbound
// message read with the typing indicator (the Cloud API couples typing to
// mark-as-read; blue-tick side effect accepted per add-whatsapp-gateway
// design D11). No-op-safe when nothing inbound is tracked yet — there is no
// message id to attach the status to. Best-effort: failures are logged,
// never fatal.
func (a *Adapter) SendTyping(ctx context.Context, chatID string) error {
	a.mu.Lock()
	messageID := a.lastInbound[chatID]
	a.mu.Unlock()
	if messageID == "" {
		return nil
	}

	payload, err := json.Marshal(map[string]any{
		"messaging_product": "whatsapp",
		"status":            "read",
		"message_id":        messageID,
		"typing_indicator":  map[string]any{"type": "text"},
	})
	if err != nil {
		return fmt.Errorf("whatsapp cloud mark read: encode payload: %w", err)
	}
	if err := a.transport.MarkRead(ctx, payload); err != nil {
		slog.Warn("whatsapp cloud typing indicator failed", "chat_id", chatID, "err", err)
	}
	return nil
}

// SendApprovalCard implements PlatformAdapter: post the interactive
// quick-reply card (add-whatsapp-gateway design D3, CanButton=true). Button
// ids are the approval bridge's platform-neutral encoding
// (EncodeApprovalCallback) verbatim — the bridge is the validator, so the
// ids must round-trip untouched. No send-failure fallback to a plain card:
// capabilities advertise buttons, so a text-reply card would be unanswerable
// (the router only intercepts APPROVE/DENY text on CanButton=false
// platforms) — a failed interactive send propagates.
func (a *Adapter) SendApprovalCard(ctx context.Context, chatID string, interrupt agents.ApprovalPayload) (string, error) {
	body := "Approve shell command?"
	if interrupt.Command != "" {
		body = "Approve shell command?\n`" + interrupt.Command + "`"
	}

	payload, err := json.Marshal(interactivePayload{
		MessagingProduct: "whatsapp",
		To:               chatID,
		Type:             "interactive",
		Interactive: interactiveBody{
			Type: "button",
			Body: struct {
				Text string `json:"text"`
			}{Text: body},
			Action: interactiveAction{
				Buttons: []interactiveButton{
					{
						Type: "reply",
						Reply: interactiveReply{
							ID:    gateways.EncodeApprovalCallback(interrupt.InterruptID, true),
							Title: "Approve",
						},
					},
					{
						Type: "reply",
						Reply: interactiveReply{
							ID:    gateways.EncodeApprovalCallback(interrupt.InterruptID, false),
							Title: "Deny",
						},
					},
				},
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("whatsapp cloud approval card: encode payload: %w", err)
	}

	raw, err := a.transport.SendMessage(ctx, payload)
	if err != nil {
		return "", fmt.Errorf("whatsapp cloud approval card: %w", err)
	}
	return decodeMessageID(raw)
}

// DownloadFile implements PlatformAdapter: the media id resolves to a
// download URL, the transport fetches the bytes. Oversize failures
// propagate — the ingress stage refuses the attachment fail-soft.
func (a *Adapter) DownloadFile(ctx context.Context, fileID string) ([]byte, error) {
	raw, err := a.transport.Media(ctx, fileID)
	if err != nil {
		return nil, fmt.Errorf("whatsapp cloud media resolve: %w", err)
	}
	var media struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &media); err != nil {
		return nil, fmt.Errorf("whatsapp cloud media resolve: decode: %w", err)
	}
	if media.URL == "" {
		return nil, errors.New("whatsapp cloud media resolve: empty url")
	}
	data, err := a.transport.Download(ctx, media.URL)
	if err != nil {
		return nil, fmt.Errorf("whatsapp cloud media download: %w", err)
	}
	return data, nil
}

// interactivePayload is the Cloud API interactive (quick-reply button)
// message wire shape — sends only; inbound interactive replies parse in
// update.go.
type interactivePayload struct {
	MessagingProduct string          `json:"messaging_product"`
	To               string          `json:"to"`
	Type             string          `json:"type"`
	Interactive      interactiveBody `json:"interactive"`
}

type interactiveBody struct {
	Type string `json:"type"` // "button"
	Body struct {
		Text string `json:"text"`
	} `json:"body"`
	Action interactiveAction `json:"action"`
}

type interactiveAction struct {
	Buttons []interactiveButton `json:"buttons"` // max 3 (design Context)
}

type interactiveButton struct {
	Type  string           `json:"type"` // "reply"
	Reply interactiveReply `json:"reply"`
}

type interactiveReply struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// decodeMessageID extracts the platform message id from a send response
// ({"messages":[{"id": "wamid..."}]}); the outbox and approval bridge key on
// it.
func decodeMessageID(raw json.RawMessage) (string, error) {
	var resp struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", fmt.Errorf("whatsapp cloud send: decode response: %w", err)
	}
	if len(resp.Messages) == 0 || resp.Messages[0].ID == "" {
		return "", errors.New("whatsapp cloud send: response carries no message id")
	}
	return resp.Messages[0].ID, nil
}
