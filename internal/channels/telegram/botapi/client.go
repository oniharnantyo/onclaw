package botapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// callTimeout bounds every regular (non-long-poll) API call. It rides
	// the per-call context, not the *http.Client, so getUpdates can carry
	// its own longer deadline.
	callTimeout = 15 * time.Second

	// longPollGrace is added on top of the getUpdates timeout parameter so
	// the HTTP request comfortably outlives the server-side long poll.
	longPollGrace = 15 * time.Second

	// DefaultLongPollTimeout is the getUpdates long-poll duration in
	// seconds the channel service uses (design D2: ~50s).
	DefaultLongPollTimeout = 50

	// ActionTyping is the sendChatAction value for the "typing…" indicator.
	ActionTyping = "typing"

	// defaultBaseURL is the public Bot API root.
	defaultBaseURL = "https://api.telegram.org/"
)

// Client is a thin Telegram Bot API client over raw net/http (design D2):
// every call is a POST to <base>/bot<token>/<method> with a JSON body
// (sendDocument uses multipart/form-data). Timeouts ride the per-call
// context — regular calls get callTimeout, getUpdates its long-poll
// deadline — so the default *http.Client carries no global timeout.
type Client struct {
	token   string
	baseURL string
	http    *http.Client
}

// Option customises a Client at construction time.
type Option func(*Client)

// WithBaseURL points the client at a custom Bot API root instead of
// https://api.telegram.org/ — tests and smoke checks point it at a fake
// server; a self-hosted Bot API server would set its address.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		if !strings.HasSuffix(baseURL, "/") {
			baseURL += "/"
		}
		c.baseURL = baseURL
	}
}

// WithProxy routes traffic through an HTTP(S) or SOCKS5 proxy URL
// (design D2: Telegram reachability varies by network). A malformed URL
// surfaces as a dial-time Transient error on the first call.
func WithProxy(proxyURL string) Option {
	return func(c *Client) {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		u, err := url.Parse(proxyURL)
		if err != nil {
			transport.Proxy = func(*http.Request) (*url.URL, error) {
				return nil, fmt.Errorf("botapi: invalid proxy URL: %w", err)
			}
		} else {
			transport.Proxy = http.ProxyURL(u)
		}
		c.http = &http.Client{Transport: transport}
	}
}

// New builds a client for the given bot token.
func New(token string, opts ...Option) *Client {
	c := &Client{token: token, baseURL: defaultBaseURL, http: &http.Client{}}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// GetUpdates long-polls for updates newer than p.Offset. With
// TimeoutSeconds > 0 the server holds the request open for that many
// seconds and the HTTP deadline is that plus longPollGrace; every other
// method is bounded by callTimeout.
func (c *Client) GetUpdates(ctx context.Context, p GetUpdatesParams) ([]Update, error) {
	timeout := callTimeout
	if p.TimeoutSeconds > 0 {
		timeout = time.Duration(p.TimeoutSeconds)*time.Second + longPollGrace
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var updates []Update
	if err := c.postJSON(ctx, "getUpdates", p, &updates); err != nil {
		return nil, err
	}
	return updates, nil
}

// SendMessage sends text to a chat and returns the sent message (its
// MessageID anchors the streaming-edit pipeline).
func (c *Client) SendMessage(ctx context.Context, p SendMessageParams) (*Message, error) {
	var msg Message
	if err := c.call(ctx, "sendMessage", p, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// EditMessageText rewrites the text of a previously sent message and returns
// the edited message.
func (c *Client) EditMessageText(ctx context.Context, p EditMessageTextParams) (*Message, error) {
	var msg Message
	if err := c.call(ctx, "editMessageText", p, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// SendChatAction shows a chat status indicator (ActionTyping); the bubble
// lasts ~5s server-side, so the delivery pump refreshes it on a loop.
func (c *Client) SendChatAction(ctx context.Context, chatID int64, action string) error {
	return c.call(ctx, "sendChatAction", struct {
		ChatID int64  `json:"chat_id"`
		Action string `json:"action"`
	}{chatID, action}, nil)
}

// SendDocument uploads a document from p.Document as a multipart/form-data
// request and returns the sent message.
func (c *Client) SendDocument(ctx context.Context, p SendDocumentParams) (*Message, error) {
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	if err := w.WriteField("chat_id", strconv.FormatInt(p.ChatID, 10)); err != nil {
		return nil, &Error{Kind: KindTransient, Description: fmt.Sprintf("encode chat_id field: %v", err)}
	}
	if p.Caption != "" {
		if err := w.WriteField("caption", p.Caption); err != nil {
			return nil, &Error{Kind: KindTransient, Description: fmt.Sprintf("encode caption field: %v", err)}
		}
	}
	part, err := w.CreateFormFile("document", p.FileName)
	if err != nil {
		return nil, &Error{Kind: KindTransient, Description: fmt.Sprintf("create document part: %v", err)}
	}
	if _, err := io.Copy(part, p.Document); err != nil {
		return nil, &Error{Kind: KindTransient, Description: fmt.Sprintf("copy document body: %v", err)}
	}
	if err := w.Close(); err != nil {
		return nil, &Error{Kind: KindTransient, Description: fmt.Sprintf("close multipart writer: %v", err)}
	}

	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	var msg Message
	if err := c.postBody(ctx, "sendDocument", w.FormDataContentType(), body.Bytes(), &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// GetFile resolves a file_id into a File whose FilePath names the download
// (<base>/file/bot<token>/<file_path>).
func (c *Client) GetFile(ctx context.Context, fileID string) (*File, error) {
	var f File
	err := c.call(ctx, "getFile", struct {
		FileID string `json:"file_id"`
	}{fileID}, &f)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// DownloadFile fetches one bot-uploaded file's bytes by its server-relative
// path (the FilePath a getFile result names): GET <base>/file/bot<token>/<path>
// (design D11's media-inbound download step).
func (c *Client) DownloadFile(ctx context.Context, filePath string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.fileURL(filePath), nil)
	if err != nil {
		return nil, &Error{Kind: KindTransient, Description: fmt.Sprintf("build file request: %v", err)}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &Error{Kind: KindTransient, Description: fmt.Sprintf("download file: %v", err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &Error{Kind: KindTransient, Description: fmt.Sprintf("download file: HTTP %d", resp.StatusCode)}
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &Error{Kind: KindTransient, Description: fmt.Sprintf("download file body: %v", err)}
	}
	return data, nil
}

// AnswerCallbackQuery acknowledges an inline keyboard tap; without the
// acknowledgement Telegram keeps showing the loading spinner on the button.
func (c *Client) AnswerCallbackQuery(ctx context.Context, callbackQueryID string) error {
	return c.call(ctx, "answerCallbackQuery", struct {
		CallbackQueryID string `json:"callback_query_id"`
	}{callbackQueryID}, nil)
}

// GetMe returns the bot's own identity (the send-path liveness probe and the
// username /cmd@bot dispatch needs).
func (c *Client) GetMe(ctx context.Context) (*User, error) {
	var me User
	if err := c.call(ctx, "getMe", struct{}{}, &me); err != nil {
		return nil, err
	}
	return &me, nil
}

// GetWebhookInfo returns the webhook state; polling-only channels read its
// PendingUpdateCount as the poll-path liveness probe (design D10).
func (c *Client) GetWebhookInfo(ctx context.Context) (*WebhookInfo, error) {
	var info WebhookInfo
	if err := c.call(ctx, "getWebhookInfo", struct{}{}, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// SetMyCommands replaces the bot's command menu (design D12 housekeeping,
// run off the connect path).
func (c *Client) SetMyCommands(ctx context.Context, commands []BotCommand) error {
	return c.call(ctx, "setMyCommands", struct {
		Commands []BotCommand `json:"commands"`
	}{commands}, nil)
}

// call issues a regular API call bounded by callTimeout.
func (c *Client) call(ctx context.Context, method string, params any, dst any) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return c.postJSON(ctx, method, params, dst)
}

// postJSON marshals params as the JSON request body and posts it on the
// caller's context (getUpdates sets its own long-poll deadline).
func (c *Client) postJSON(ctx context.Context, method string, params any, dst any) error {
	body, err := json.Marshal(params)
	if err != nil {
		return &Error{Kind: KindTransient, Description: fmt.Sprintf("encode %s params: %v", method, err)}
	}
	return c.postBody(ctx, method, "application/json", body, dst)
}

// postBody posts a pre-encoded request body and decodes the Bot API
// envelope, decoding a successful result into dst when non-nil.
func (c *Client) postBody(ctx context.Context, method, contentType string, body []byte, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url(method), bytes.NewReader(body))
	if err != nil {
		return &Error{Kind: KindTransient, Description: fmt.Sprintf("build %s request: %v", method, err)}
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := c.http.Do(req)
	if err != nil {
		return &Error{Kind: KindTransient, Description: fmt.Sprintf("%s: %v", method, err)}
	}
	defer resp.Body.Close()

	var env response
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return &Error{Kind: KindTransient, Description: fmt.Sprintf("%s: HTTP %d with non-JSON body: %v", method, resp.StatusCode, err)}
	}
	if !env.OK {
		return classify(resp.StatusCode, &env)
	}
	if dst != nil && len(env.Result) > 0 {
		if err := json.Unmarshal(env.Result, dst); err != nil {
			return &Error{Kind: KindTransient, Description: fmt.Sprintf("decode %s result: %v", method, err)}
		}
	}
	return nil
}

// url builds the Bot API endpoint for one method call.
func (c *Client) url(method string) string {
	return c.baseURL + "bot" + c.token + "/" + method
}

// fileURL builds the file-download endpoint for one server-relative file path
// (<base>/file/bot<token>/<file_path>).
func (c *Client) fileURL(filePath string) string {
	return c.baseURL + "file/bot" + c.token + "/" + strings.TrimPrefix(filePath, "/")
}
