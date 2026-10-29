package botapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testToken = "123456:TEST-TOKEN"

// recordedRequest captures everything the fake Bot API saw about one request.
type recordedRequest struct {
	method      string
	path        string
	contentType string
	body        []byte
	hasDeadline bool
	deadline    time.Time

	// multipart (sendDocument) capture:
	form        map[string]string
	fileName    string
	fileContent []byte
}

// serveBody records the incoming request and answers with the given raw body.
func serveBody(t *testing.T, rec *recordedRequest, status int, contentType string, body []byte) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		rec.method = r.Method
		rec.path = r.URL.Path
		rec.contentType = r.Header.Get("Content-Type")

		// Multipart bodies must be parsed before anything reads the raw
		// stream; JSON bodies are captured verbatim for JSONEq asserts.
		if strings.HasPrefix(rec.contentType, "multipart/form-data") {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("parse multipart form: %v", err)
				return
			}
			rec.form = make(map[string]string, len(r.Form))
			for k := range r.Form {
				rec.form[k] = r.FormValue(k)
			}
			file, header, err := r.FormFile("document")
			if err != nil {
				t.Errorf("open document part: %v", err)
				return
			}
			defer file.Close()
			rec.fileName = header.Filename
			content, err := io.ReadAll(file)
			if err != nil {
				t.Errorf("read document part: %v", err)
				return
			}
			rec.fileContent = content
		} else {
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read request body: %v", err)
				return
			}
			rec.body = raw
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if _, err := w.Write(body); err != nil {
			t.Errorf("write response: %v", err)
		}
	}
}

// serveEnvelope records the incoming request and answers with a Bot API
// envelope marshalled from env.
func serveEnvelope(t *testing.T, rec *recordedRequest, status int, env response) http.HandlerFunc {
	t.Helper()
	raw, err := json.Marshal(env)
	require.NoError(t, err)
	return serveBody(t, rec, status, "application/json", raw)
}

// okResult builds an ok envelope carrying the marshalled result.
func okResult(t *testing.T, result any) response {
	t.Helper()
	raw, err := json.Marshal(result)
	require.NoError(t, err)
	return response{OK: true, Result: raw}
}

// newTestClient points a client at a fresh fake Bot API server.
func newTestClient(t *testing.T, h http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(testToken, WithBaseURL(srv.URL))
}

func TestGetUpdatesLongPoll(t *testing.T) {
	var rec recordedRequest
	c := newTestClient(t, serveEnvelope(t, &rec, http.StatusOK, okResult(t, []Update{
		{
			ID: 11,
			Message: &Message{
				MessageID: 101,
				From:      &User{ID: 42, FirstName: "Oni", Username: "oni"},
				Chat:      &Chat{ID: 42, Type: "private"},
				Text:      "hello there",
				Photo:     []PhotoSize{{FileID: "photo-small", Width: 90, Height: 90}, {FileID: "photo-big", FileSize: 200000, Width: 1280, Height: 1280}},
			},
		},
		{
			ID: 12,
			Message: &Message{
				MessageID:      102,
				Chat:           &Chat{ID: 7, Type: "group", Title: "Buddies"},
				Document:       &Document{FileID: "doc1", FileName: "report.pdf", FileSize: 2048},
				ReplyToMessage: &Message{MessageID: 90, From: &User{ID: 99, Username: "thebot"}},
				ViaBot:         &User{ID: 555},
			},
		},
		{
			ID:            13,
			CallbackQuery: &CallbackQuery{ID: "cbq1", From: &User{ID: 42}, Data: "appr:s1:i1:approve", Message: &Message{MessageID: 103}},
		},
	})))

	updates, err := c.GetUpdates(context.Background(), GetUpdatesParams{
		Offset:         13,
		TimeoutSeconds: DefaultLongPollTimeout,
		AllowedUpdates: []string{"message", "callback_query"},
	})
	require.NoError(t, err)
	require.Len(t, updates, 3)

	// Message update: sender, chat, text and the photo ladder.
	m := updates[0].Message
	require.NotNil(t, m)
	assert.Equal(t, int64(11), updates[0].ID)
	assert.Equal(t, int64(101), m.MessageID)
	require.NotNil(t, m.From)
	assert.Equal(t, int64(42), m.From.ID)
	assert.Equal(t, "Oni", m.From.FirstName)
	assert.Equal(t, "oni", m.From.Username)
	require.NotNil(t, m.Chat)
	assert.Equal(t, int64(42), m.Chat.ID)
	assert.Equal(t, "private", m.Chat.Type)
	assert.Equal(t, "hello there", m.Text)
	require.Len(t, m.Photo, 2)
	assert.Equal(t, "photo-big", m.Photo[1].FileID)
	assert.Equal(t, int64(200000), m.Photo[1].FileSize)

	// Group message: document, reply-to and via_bot.
	g := updates[1].Message
	require.NotNil(t, g)
	require.NotNil(t, g.Chat)
	assert.Equal(t, "group", g.Chat.Type)
	assert.Equal(t, "Buddies", g.Chat.Title)
	require.NotNil(t, g.Document)
	assert.Equal(t, "doc1", g.Document.FileID)
	assert.Equal(t, "report.pdf", g.Document.FileName)
	require.NotNil(t, g.ReplyToMessage)
	assert.Equal(t, int64(90), g.ReplyToMessage.MessageID)
	assert.Equal(t, "thebot", g.ReplyToMessage.From.Username)
	require.NotNil(t, g.ViaBot)
	assert.Equal(t, int64(555), g.ViaBot.ID)

	// Callback query tap.
	cb := updates[2].CallbackQuery
	require.NotNil(t, cb)
	assert.Equal(t, "cbq1", cb.ID)
	assert.Equal(t, int64(42), cb.From.ID)
	assert.Equal(t, "appr:s1:i1:approve", cb.Data)
	require.NotNil(t, cb.Message)
	assert.Equal(t, int64(103), cb.Message.MessageID)

	// The wire shape: POST to /bot<token>/getUpdates with offset, timeout
	// and allowed_updates in the JSON body.
	assert.Equal(t, http.MethodPost, rec.method)
	assert.Equal(t, "/bot"+testToken+"/getUpdates", rec.path)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.body, &body))
	assert.Equal(t, float64(13), body["offset"])
	assert.Equal(t, float64(DefaultLongPollTimeout), body["timeout"])
	assert.Equal(t, []any{"message", "callback_query"}, body["allowed_updates"])
}

// recordingRoundTripper captures the outgoing request and answers with a
// canned ok envelope — used to observe the client-side request deadline,
// which never crosses to the server's handler context.
type recordingRoundTripper struct {
	deadline time.Time
	captured bool
}

func (rt *recordingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.deadline, rt.captured = req.Context().Deadline()
	body, _ := json.Marshal(okResultTBD())
	return &http.Response{
		StatusCode:    http.StatusOK,
		Body:          io.NopCloser(bytes.NewReader(body)),
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		ContentLength: int64(len(body)),
	}, nil
}

// okResultTBD is an empty-update envelope for the RoundTripper stub.
func okResultTBD() response {
	return response{OK: true, Result: json.RawMessage("[]")}
}

func TestGetUpdatesCarriesLongPollDeadline(t *testing.T) {
	tests := []struct {
		name            string
		timeoutSeconds  int
		wantMinDeadline time.Duration
		wantMaxDeadline time.Duration
	}{
		{name: "long poll: timeout param plus grace", timeoutSeconds: DefaultLongPollTimeout, wantMinDeadline: 64 * time.Second, wantMaxDeadline: longPollGrace + time.Duration(DefaultLongPollTimeout)*time.Second},
		{name: "short poll: regular call timeout", timeoutSeconds: 0, wantMinDeadline: callTimeout - time.Second, wantMaxDeadline: callTimeout},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := &recordingRoundTripper{}
			c := &Client{token: testToken, baseURL: defaultBaseURL, http: &http.Client{Transport: rt}}

			_, err := c.GetUpdates(context.Background(), GetUpdatesParams{TimeoutSeconds: tt.timeoutSeconds})
			require.NoError(t, err)
			require.True(t, rt.captured, "request context must carry a deadline")

			delta := time.Until(rt.deadline)
			assert.Greater(t, delta, tt.wantMinDeadline)
			assert.LessOrEqual(t, delta, tt.wantMaxDeadline)
		})
	}
}

func TestGetUpdatesOmitsZeroParams(t *testing.T) {
	var rec recordedRequest
	c := newTestClient(t, serveEnvelope(t, &rec, http.StatusOK, okResult(t, []Update{})))

	updates, err := c.GetUpdates(context.Background(), GetUpdatesParams{})
	require.NoError(t, err)
	assert.Empty(t, updates)
	assert.JSONEq(t, `{}`, string(rec.body), "zero params must be omitted (Telegram default behaviour)")
}

func TestSendMessageReturnsMessageForEditChaining(t *testing.T) {
	var rec recordedRequest
	c := newTestClient(t, serveEnvelope(t, &rec, http.StatusOK, okResult(t, Message{
		MessageID: 77,
		Chat:      &Chat{ID: 42, Type: "private"},
	})))

	msg, err := c.SendMessage(context.Background(), SendMessageParams{
		ChatID:      42,
		Text:        "working on it…",
		ParseMode:   "MarkdownV2",
		ReplyMarkup: json.RawMessage(`{"inline_keyboard":[[{"text":"Approve","callback_data":"appr:s1:i1:approve"}]]}`),
	})
	require.NoError(t, err)
	require.NotNil(t, msg)
	assert.Equal(t, int64(77), msg.MessageID)

	assert.Equal(t, "/bot"+testToken+"/sendMessage", rec.path)
	assert.JSONEq(t, `{
		"chat_id": 42,
		"text": "working on it…",
		"parse_mode": "MarkdownV2",
		"reply_markup": {"inline_keyboard": [[{"text": "Approve", "callback_data": "appr:s1:i1:approve"}]]}
	}`, string(rec.body))
}

func TestSendMessageOmitsOptionalFields(t *testing.T) {
	var rec recordedRequest
	c := newTestClient(t, serveEnvelope(t, &rec, http.StatusOK, okResult(t, Message{MessageID: 1})))

	_, err := c.SendMessage(context.Background(), SendMessageParams{ChatID: 42, Text: "plain"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"chat_id": 42, "text": "plain"}`, string(rec.body))
}

func TestEditMessageText(t *testing.T) {
	var rec recordedRequest
	c := newTestClient(t, serveEnvelope(t, &rec, http.StatusOK, okResult(t, Message{MessageID: 77})))

	msg, err := c.EditMessageText(context.Background(), EditMessageTextParams{
		ChatID:    42,
		MessageID: 77,
		Text:      "*done*",
		ParseMode: "MarkdownV2",
	})
	require.NoError(t, err)
	require.NotNil(t, msg)
	assert.Equal(t, int64(77), msg.MessageID)

	assert.Equal(t, "/bot"+testToken+"/editMessageText", rec.path)
	assert.JSONEq(t, `{"chat_id": 42, "message_id": 77, "text": "*done*", "parse_mode": "MarkdownV2"}`, string(rec.body))
}

func TestSendChatAction(t *testing.T) {
	var rec recordedRequest
	c := newTestClient(t, serveEnvelope(t, &rec, http.StatusOK, response{OK: true, Result: json.RawMessage("true")}))

	err := c.SendChatAction(context.Background(), 42, ActionTyping)
	require.NoError(t, err)

	assert.Equal(t, "/bot"+testToken+"/sendChatAction", rec.path)
	assert.JSONEq(t, `{"chat_id": 42, "action": "typing"}`, string(rec.body))
}

func TestSendDocumentMultipart(t *testing.T) {
	var rec recordedRequest
	c := newTestClient(t, serveEnvelope(t, &rec, http.StatusOK, okResult(t, Message{MessageID: 5})))

	msg, err := c.SendDocument(context.Background(), SendDocumentParams{
		ChatID:   42,
		Document: strings.NewReader("ARTIFACT-BYTES"),
		FileName: "build.log",
		Caption:  "the build log",
	})
	require.NoError(t, err)
	require.NotNil(t, msg)
	assert.Equal(t, int64(5), msg.MessageID)

	assert.Equal(t, "/bot"+testToken+"/sendDocument", rec.path)
	assert.True(t, strings.HasPrefix(rec.contentType, "multipart/form-data; boundary="),
		"sendDocument must POST multipart/form-data, got %q", rec.contentType)
	assert.Equal(t, "42", rec.form["chat_id"])
	assert.Equal(t, "the build log", rec.form["caption"])
	assert.Equal(t, "build.log", rec.fileName)
	assert.Equal(t, "ARTIFACT-BYTES", string(rec.fileContent))
}

// TestSimpleMethods covers the request/response shape of the remaining
// JSON-only methods in one table.
func TestSimpleMethods(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		call     func(c *Client) error
		result   string // raw JSON in the envelope's result
		wantBody string // expected JSON request body
	}{
		{
			name:     "getFile",
			path:     "getFile",
			result:   `{"file_id": "doc1", "file_path": "documents/file_1.pdf"}`,
			wantBody: `{"file_id": "doc1"}`,
			call: func(c *Client) error {
				f, err := c.GetFile(context.Background(), "doc1")
				require.NoError(t, err)
				assert.Equal(t, "doc1", f.FileID)
				assert.Equal(t, "documents/file_1.pdf", f.FilePath)
				return nil
			},
		},
		{
			name:     "answerCallbackQuery",
			path:     "answerCallbackQuery",
			result:   `true`,
			wantBody: `{"callback_query_id": "cbq1"}`,
			call: func(c *Client) error {
				return c.AnswerCallbackQuery(context.Background(), "cbq1")
			},
		},
		{
			name:     "getMe",
			path:     "getMe",
			result:   `{"id": 123456, "first_name": "Buddies", "username": "buddies_bot"}`,
			wantBody: `{}`,
			call: func(c *Client) error {
				me, err := c.GetMe(context.Background())
				require.NoError(t, err)
				assert.Equal(t, int64(123456), me.ID)
				assert.Equal(t, "buddies_bot", me.Username)
				return nil
			},
		},
		{
			name:     "getWebhookInfo",
			path:     "getWebhookInfo",
			result:   `{"pending_update_count": 4}`,
			wantBody: `{}`,
			call: func(c *Client) error {
				info, err := c.GetWebhookInfo(context.Background())
				require.NoError(t, err)
				assert.Equal(t, 4, info.PendingUpdateCount)
				return nil
			},
		},
		{
			name:     "setMyCommands",
			path:     "setMyCommands",
			result:   `true`,
			wantBody: `{"commands": [{"command": "new", "description": "Start a fresh session"}, {"command": "stop", "description": "Stop the running turn"}]}`,
			call: func(c *Client) error {
				return c.SetMyCommands(context.Background(), []BotCommand{
					{Command: "new", Description: "Start a fresh session"},
					{Command: "stop", Description: "Stop the running turn"},
				})
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var rec recordedRequest
			c := newTestClient(t, serveEnvelope(t, &rec, http.StatusOK, response{OK: true, Result: json.RawMessage(tt.result)}))

			require.NoError(t, tt.call(c))

			assert.Equal(t, "/bot"+testToken+"/"+tt.path, rec.path)
			assert.JSONEq(t, tt.wantBody, string(rec.body))
		})
	}
}

// TestCallClassifiesFailures drives a real client call against each failure
// shape and checks the resulting typed error.
func TestCallClassifiesFailures(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string // raw response body; empty → marshalled envelope
		env       response
		wantKind  ErrorKind
		wantRetry time.Duration
	}{
		{
			name:     "401 unauthorized (bad token)",
			status:   http.StatusUnauthorized,
			env:      response{OK: false, ErrorCode: 401, Description: "Unauthorized"},
			wantKind: KindAuthInvalid,
		},
		{
			name:     "403 forbidden (bot blocked)",
			status:   http.StatusForbidden,
			env:      response{OK: false, ErrorCode: 403, Description: "Forbidden: bot was blocked by the user"},
			wantKind: KindForbidden,
		},
		{
			name:     "404 not found (malformed token)",
			status:   http.StatusNotFound,
			env:      response{OK: false, ErrorCode: 404, Description: "Not Found"},
			wantKind: KindNotFound,
		},
		{
			name:     "409 conflict (another getUpdates consumer)",
			status:   http.StatusConflict,
			env:      response{OK: false, ErrorCode: 409, Description: "Conflict: terminated by other getUpdates request"},
			wantKind: KindConflict,
		},
		{
			name:      "429 with retry_after",
			status:    http.StatusTooManyRequests,
			env:       response{OK: false, ErrorCode: 429, Description: "Too Many Requests: retry after 17", Parameters: responseParameters{RetryAfter: 17}},
			wantKind:  KindRateLimited,
			wantRetry: 17 * time.Second,
		},
		{
			name:     "429 without retry_after",
			status:   http.StatusTooManyRequests,
			env:      response{OK: false, ErrorCode: 429, Description: "Too Many Requests"},
			wantKind: KindRateLimited,
		},
		{
			name:     "400 parse failure (MarkdownV2 fallback signal)",
			status:   http.StatusBadRequest,
			env:      response{OK: false, ErrorCode: 400, Description: "Bad Request: can't parse entities: Unsupported start tag ..."},
			wantKind: KindParse,
		},
		{
			name:     "400 other bad request",
			status:   http.StatusBadRequest,
			env:      response{OK: false, ErrorCode: 400, Description: "Bad Request: chat not found"},
			wantKind: KindTransient,
		},
		{
			name:     "500 server error",
			status:   http.StatusInternalServerError,
			env:      response{OK: false, ErrorCode: 500, Description: "Internal Server Error"},
			wantKind: KindTransient,
		},
		{
			name:      "envelope error_code wins over http status",
			status:    http.StatusOK,
			env:       response{OK: false, ErrorCode: 429, Description: "Too Many Requests: retry after 7", Parameters: responseParameters{RetryAfter: 7}},
			wantKind:  KindRateLimited,
			wantRetry: 7 * time.Second,
		},
		{
			name:     "http status fallback when envelope lacks error_code",
			status:   http.StatusUnauthorized,
			env:      response{OK: false, Description: "Unauthorized"},
			wantKind: KindAuthInvalid,
		},
		{
			name:     "non-JSON body (intermediary error page)",
			status:   http.StatusBadGateway,
			body:     "<html>Bad Gateway</html>",
			wantKind: KindTransient,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var rec recordedRequest
			handler := serveEnvelope(t, &rec, tt.status, tt.env)
			if tt.body != "" {
				handler = serveBody(t, &rec, tt.status, "text/html", []byte(tt.body))
			}
			c := newTestClient(t, handler)

			_, err := c.GetMe(context.Background())
			require.Error(t, err)

			var apiErr *Error
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, tt.wantKind, apiErr.Kind)
			assert.Equal(t, tt.wantRetry, apiErr.RetryAfter)
			assert.NotEmpty(t, apiErr.Description)
		})
	}
}

func TestNetworkErrorIsTransient(t *testing.T) {
	// A server that no longer accepts connections: every call fails at the
	// transport layer.
	srv := httptest.NewServer(http.NotFoundHandler())
	baseURL := srv.URL
	srv.Close()

	c := New(testToken, WithBaseURL(baseURL))

	_, err := c.GetMe(context.Background())
	require.Error(t, err)

	var apiErr *Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, KindTransient, apiErr.Kind)
	assert.NotEmpty(t, apiErr.Description)
}
