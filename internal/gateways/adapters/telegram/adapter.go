package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/gateways"
)

// Transport modes (design D12). Long polling runs its own loop; webhook mode
// receives updates through the public ingress handler (IngestWebhook).
const (
	TransportModeLongPolling = "long_polling"
	TransportModeWebhook     = "webhook"
)

// Adapter implements gateways.PlatformAdapter for the Telegram Bot API.
// One adapter per enabled gateway: constructed by the composition root's
// adapter factory (design D11), started/stopped by the gateway lifecycle
// manager, and delivering normalized traffic to the service through the
// InboundHandler.
type Adapter struct {
	gatewayID string
	handler   gateways.InboundHandler
	transport Transport
	mode      string

	// defaultTransport is the adapter's own HTTP transport — WithAPIBase and
	// WithHTTPClient retarget it; WithTransport replaces a.transport
	// wholesale (tests inject the fake).
	defaultTransport *httpTransport

	// pollTimeout is the getUpdates long-poll hang; the HTTP client timeout
	// must comfortably exceed it.
	pollTimeout time.Duration

	// botID / botUsername resolve at Start via getMe: the token is validated,
	// self-echo is dropped, and group targeting knows the bot's handle.
	// Guarded by mu — a webhook POST can race the Start handshake.
	botID       int64
	botUsername string

	mu      sync.Mutex
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	started bool
	stopped bool

	// Webhook dedup ring: webhook retries can redeliver updates.
	seenMu    sync.Mutex
	seen      map[int64]struct{}
	seenOrder []int64
	seenMax   int
}

// Option customizes an Adapter.
type Option func(*Adapter)

// WithAPIBase overrides the Bot API base URL (self-hosted Bot API servers,
// tests).
func WithAPIBase(base string) Option {
	return func(a *Adapter) {
		if a.defaultTransport != nil {
			a.defaultTransport.apiBase = base
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

// WithTransportMode sets the ingestion mode (TransportModeLongPolling, the
// default, or TransportModeWebhook).
func WithTransportMode(mode string) Option {
	return func(a *Adapter) {
		if mode == TransportModeWebhook {
			a.mode = TransportModeWebhook
		}
	}
}

// WithPollTimeout overrides the getUpdates long-poll duration.
func WithPollTimeout(d time.Duration) Option {
	return func(a *Adapter) {
		if d > 0 {
			a.pollTimeout = d
		}
	}
}

// NewAdapter builds the Telegram adapter for one gateway. The handler is
// required — the adapter delivers all normalized traffic through it.
func NewAdapter(gatewayID, botToken string, handler gateways.InboundHandler, opts ...Option) (*Adapter, error) {
	if gatewayID == "" || botToken == "" {
		return nil, fmt.Errorf("%w: telegram adapter needs gateway id and bot token", ErrInvalid)
	}
	if handler == nil {
		return nil, fmt.Errorf("%w: telegram adapter needs an inbound handler", ErrInvalid)
	}
	httpTransport := newHTTPTransport(botToken, DefaultAPIBase, nil)
	a := &Adapter{
		gatewayID:      gatewayID,
		handler:        handler,
		transport:      httpTransport,
		defaultTransport: httpTransport,
		mode:           TransportModeLongPolling,
		pollTimeout:    30 * time.Second,
		seen:           make(map[int64]struct{}),
		seenMax:        512,
	}
	for _, opt := range opts {
		opt(a)
	}
	return a, nil
}

// ErrInvalid mirrors domain.ErrInvalid without importing domain — the
// adapter package stays a leaf against the gateway core.
var ErrInvalid = errors.New("invalid request")

// Start implements PlatformAdapter: validates the token via getMe, then
// starts long polling unless the adapter runs in webhook mode (the
// composition wave serves webhook ingress and calls IngestWebhook;
// RegisterWebhook points Telegram at the public URL). Idempotent per
// instance.
func (a *Adapter) Start(ctx context.Context) error {
	a.mu.Lock()
	if a.stopped {
		a.mu.Unlock()
		return errors.New("telegram adapter: cannot start after stop")
	}
	if a.started {
		a.mu.Unlock()
		return nil
	}
	a.mu.Unlock()

	// getMe validates the token and resolves the bot identity.
	me, err := a.getMe(ctx)
	if err != nil {
		return fmt.Errorf("telegram adapter: getMe: %w", err)
	}

	a.mu.Lock()
	if a.started || a.stopped {
		a.mu.Unlock()
		return nil
	}
	a.started = true
	a.botID = me.ID
	a.botUsername = me.Username
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	a.cancel = cancel
	mode := a.mode
	a.mu.Unlock()

	// Long polling runs only for its transport mode; webhook-mode adapters
	// receive updates through the public ingress handler instead.
	if mode == TransportModeWebhook {
		return nil
	}
	a.wg.Add(1)
	go a.pollLoop(runCtx)
	return nil
}

// Stop implements PlatformAdapter: cancel the poll loop and wait for it.
func (a *Adapter) Stop(ctx context.Context) error {
	a.mu.Lock()
	if a.stopped {
		a.mu.Unlock()
		return nil
	}
	a.stopped = true
	cancel := a.cancel
	a.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

// getMe resolves the bot identity (token validation).
func (a *Adapter) getMe(ctx context.Context) (*apiUser, error) {
	raw, err := a.transport.Call(ctx, "getMe", url.Values{})
	if err != nil {
		return nil, err
	}
	var me apiUser
	if err := json.Unmarshal(raw, &me); err != nil {
		return nil, fmt.Errorf("telegram getMe: decode: %w", err)
	}
	return &me, nil
}

// identity returns the resolved bot identity (guarded — webhook ingestion
// can race the Start handshake).
func (a *Adapter) identity() (int64, string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.botID, a.botUsername
}

// SendMessage implements PlatformAdapter: post an HTML message, falling back
// once to plain text when Telegram rejects the entities (design D5).
func (a *Adapter) SendMessage(ctx context.Context, chatID, htmlText string, opts gateways.SendOptions) (string, error) {
	params := url.Values{}
	params.Set("chat_id", chatID)
	params.Set("text", htmlText)
	params.Set("parse_mode", "HTML")
	if opts.DisablePreview {
		params.Set("link_preview_options", `{"is_disabled":true}`)
	}

	messageID, err := a.sendText(ctx, "sendMessage", params)
	if err == nil || !IsParseError(err) {
		return messageID, err
	}
	// Single plain-text retry (spec: parse failure falls back rather than
	// dropping the message).
	params.Del("parse_mode")
	params.Set("text", stripHTML(htmlText))
	return a.sendText(ctx, "sendMessage", params)
}

// EditMessage implements PlatformAdapter: rewrite a sent message in place,
// with the same parse-failure fallback.
func (a *Adapter) EditMessage(ctx context.Context, chatID, messageID, htmlText string) error {
	params := url.Values{}
	params.Set("chat_id", chatID)
	params.Set("message_id", messageID)
	params.Set("text", htmlText)
	params.Set("parse_mode", "HTML")

	err := a.editText(ctx, params)
	if err == nil || !IsParseError(err) {
		return err
	}
	params.Del("parse_mode")
	params.Set("text", stripHTML(htmlText))
	return a.editText(ctx, params)
}

// SendTyping implements PlatformAdapter: flash the typing indicator
// (best-effort — failures are logged, never fatal).
func (a *Adapter) SendTyping(ctx context.Context, chatID string) error {
	params := url.Values{}
	params.Set("chat_id", chatID)
	params.Set("action", "typing")
	if _, err := a.transport.Call(ctx, "sendChatAction", params); err != nil {
		slog.Warn("telegram typing indicator failed", "chat_id", chatID, "err", err)
	}
	return nil
}

// SendApprovalCard implements PlatformAdapter: post the approve/deny inline
// keyboard for one pending interrupt (design D7). callback_data uses the
// approval bridge's platform-neutral encoding (EncodeApprovalCallback) —
// the bridge is the validator, so the buttons must match it verbatim.
func (a *Adapter) SendApprovalCard(ctx context.Context, chatID string, interrupt agents.ApprovalPayload) (string, error) {
	type button struct {
		Text         string `json:"text"`
		CallbackData string `json:"callback_data"`
	}
	markup, err := json.Marshal(map[string]any{
		"inline_keyboard": [][]button{
			{
				{Text: "Approve", CallbackData: gateways.EncodeApprovalCallback(interrupt.InterruptID, true)},
				{Text: "Deny", CallbackData: gateways.EncodeApprovalCallback(interrupt.InterruptID, false)},
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("telegram approval card: markup: %w", err)
	}

	body := "Approve shell command?"
	if interrupt.Command != "" {
		body = "Approve shell command?\n<code>" + html.EscapeString(interrupt.Command) + "</code>"
	}

	params := url.Values{}
	params.Set("chat_id", chatID)
	params.Set("text", body)
	params.Set("parse_mode", "HTML")
	params.Set("reply_markup", string(markup))

	messageID, err := a.sendText(ctx, "sendMessage", params)
	if err == nil || !IsParseError(err) {
		return messageID, err
	}
	params.Del("parse_mode")
	params.Set("text", stripHTML(body))
	return a.sendText(ctx, "sendMessage", params)
}

// DownloadFile implements PlatformAdapter: getFile resolves the path, the
// transport fetches the body. Bot API caps downloads at 20 MB — the error
// propagates and the ingress stage refuses the attachment fail-soft.
func (a *Adapter) DownloadFile(ctx context.Context, fileID string) ([]byte, error) {
	params := url.Values{}
	params.Set("file_id", fileID)
	raw, err := a.transport.Call(ctx, "getFile", params)
	if err != nil {
		return nil, fmt.Errorf("telegram getFile: %w", err)
	}
	var file struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("telegram getFile: decode: %w", err)
	}
	if file.FilePath == "" {
		return nil, errors.New("telegram getFile: empty file_path")
	}
	return a.transport.Download(ctx, file.FilePath)
}

// RegisterWebhook points Telegram at the public webhook URL with the
// secret_token the ingress handler validates (task 6.2). The composition
// wave calls it when a gateway switches to webhook transport; the secret
// lives with the ingress route, not the adapter's credentials.
func (a *Adapter) RegisterWebhook(ctx context.Context, publicURL, secret string) error {
	params := url.Values{}
	params.Set("url", publicURL)
	if secret != "" {
		params.Set("secret_token", secret)
	}
	_, err := a.transport.Call(ctx, "setWebhook", params)
	return err
}

// sendText executes a text send and extracts the resulting message id.
func (a *Adapter) sendText(ctx context.Context, method string, params url.Values) (string, error) {
	raw, err := a.transport.Call(ctx, method, params)
	if err != nil {
		return "", err
	}
	var result struct {
		MessageID int64 `json:"message_id"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("telegram %s: decode: %w", method, err)
	}
	return strconv.FormatInt(result.MessageID, 10), nil
}

// editText executes an in-place edit.
func (a *Adapter) editText(ctx context.Context, params url.Values) error {
	_, err := a.transport.Call(ctx, "editMessageText", params)
	return err
}

// rememberUpdate reports whether the update id is new (webhook dedup ring).
func (a *Adapter) rememberUpdate(id int64) bool {
	a.seenMu.Lock()
	defer a.seenMu.Unlock()
	if _, dup := a.seen[id]; dup {
		return false
	}
	a.seen[id] = struct{}{}
	a.seenOrder = append(a.seenOrder, id)
	if len(a.seenOrder) > a.seenMax {
		oldest := a.seenOrder[0]
		a.seenOrder = a.seenOrder[1:]
		delete(a.seen, oldest)
	}
	return true
}

// stripHTML converts rendered Telegram HTML back to presentable plain text
// (the parse-failure fallback body): tags dropped, entities unescaped.
func stripHTML(s string) string {
	var b strings.Builder
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteByte(s[i])
			}
		}
	}
	return html.UnescapeString(b.String())
}
