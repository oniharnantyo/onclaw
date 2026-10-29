// The add-telegram-channel headless smoke (tasks.md 10.2): the full channel
// loop — pairing → allow → message → queued message → streamed reply →
// approval card → artifact turn → receipts/dedupe — driven through the real
// composition root against two in-process fakes. Env-gated, skip by default
// (the internal/cli smoke precedent); run with:
//
//	BUDDIES_TELEGRAM_SMOKE=1 go test ./internal/channels/telegram -run TestTelegramSmoke -v -count=1

package telegram_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oniharnantyo/buddies/internal/agents/hooks"
	"github.com/oniharnantyo/buddies/internal/channels/telegram"
	"github.com/oniharnantyo/buddies/internal/cli"
	"github.com/oniharnantyo/buddies/internal/services"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

// TestTelegramSmoke is the env-gated headless smoke for the add-telegram-channel
// change (tasks.md 10.2): the full channel loop — pairing → allow → message →
// queued message → streamed reply → approval card → artifact turn — driven
// through the real composition root (cli.Assemble, the same construction path
// the server-mode host uses) against two in-process fakes:
//
//   - a scripted fake Bot API the REAL botapi client dials through the
//     BUDDIES_TELEGRAM_API_BASE override (service.go's EnvAPIBase), which
//     records every call and serves scripted getUpdates batches;
//   - a scripted OpenAI chat-completions backend the REAL model connector
//     dials through a seeded provider row's BaseURL, which streams text
//     deltas, tool calls, and post-tool replies per leg.
//
// Skipped unless BUDDIES_TELEGRAM_SMOKE=1 (the promptgen/memory smoke
// convention).
//
// The cli smoke precedents (internal/cli/memory_smoke_test.go) run in-package,
// but this test must live in the external package: internal/cli imports this
// package, so an in-package test cannot import cli (import cycle). The
// service's test-shortenable tunables therefore keep their pinned values and
// every wait is a bounded poll instead of a tuned sleep.
func TestTelegramSmoke(t *testing.T) {
	if os.Getenv("BUDDIES_TELEGRAM_SMOKE") == "" {
		t.Skip("set BUDDIES_TELEGRAM_SMOKE=1 to run the headless Telegram channel smoke")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// The layout resolves from HOME (config.Resolve), so a temp home isolates
	// the app: fresh sqlite store, fresh workspaces, no user config.
	home := t.TempDir()
	t.Setenv("HOME", home)

	// The scripted model backend must exist before Assemble: the provider row
	// carries its URL, and the first turn builds the connector from it.
	model := newFakeModelBackend()

	// The scripted Bot API must exist before the channel's first connect too:
	// the base-URL override is read when the channel builds its client.
	bot := newFakeBotAPI()
	t.Setenv(telegram.EnvAPIBase, bot.server.URL)

	// A run_started command hook turns the run's origin observable: the
	// dispatcher feeds the event payload (whose origin field is stamped from
	// the Submit's Origin) to the hook's stdin, which this appends to a file.
	hookLog := filepath.Join(t.TempDir(), "hook-run-started.jsonl")

	app, err := cli.Assemble(ctx, services.Noop{}, services.NoBrowser{})
	require.NoError(t, err)
	defer func() { require.NoError(t, app.Close()) }()

	// Seed the store through the real stores: a provider whose connector
	// dials the fake model, a stored model row (the registry serves only
	// enabled stored rows), an agent on it, the gateway default, an enabled
	// telegram channel row, and the origin-observing hook.
	prov := &store.Provider{Name: "Smoke Fake", Type: "openai-compatible",
		BaseURL: model.server.URL, APIKey: "test-key", Enabled: true}
	require.NoError(t, app.Store.Providers().Create(ctx, prov))
	require.NoError(t, app.Store.ProviderModels().Create(ctx, &store.ProviderModel{
		ProviderID: prov.ID, ModelID: "fake-model", ContextWindow: 8192, Enabled: true,
	}))
	agent := &store.Agent{Name: "Smoke", Slug: "smoke", Brief: "headless smoke agent",
		ProviderID: prov.ID, Model: "fake-model"}
	require.NoError(t, app.Store.Agents().Create(ctx, agent))
	require.NoError(t, app.Store.GatewaySettings().Save(ctx, &store.GatewaySettings{
		Enabled: true, DefaultAgentID: agent.ID,
	}))
	require.NoError(t, app.Store.Channels().SaveChannel(ctx, &store.Channel{
		ID: telegram.DefaultChannelID, Enabled: true, BotToken: "test-token",
	}))
	require.NoError(t, app.Store.Hooks().Create(ctx, &store.Hook{
		Name: "smoke-origin-observer", Event: hooks.EventRunStarted,
		HandlerType: hooks.HandlerCommand, Config: fmt.Sprintf(`{"command":"cat >> %s"}`, hookLog),
		TimeoutMS: 5000, Enabled: true,
	}))

	// Starting the gateway starts the channel runtime (Attach in Assemble) —
	// nothing polled before this line.
	require.NoError(t, app.Gateway.Start(ctx))

	// The cold-boot sync (design D9) ack-and-drops whatever getUpdates holds
	// at startup: the queue is empty through it, and updates are pushed only
	// after the boot poll is observed.
	select {
	case <-bot.coldBootDone:
	case <-time.After(10 * time.Second):
		t.Fatalf("channel never ran its cold-boot poll\n%s", bot.dump())
	}

	const (
		userID = int64(42)
		chatID = int64(42)
	)
	wsDir := filepath.Join(app.Layout.Workspaces, agent.Slug)

	t.Run("pairing reply to an unknown sender", func(t *testing.T) {
		bot.pushUpdate(dmUpdate(101, chatID, userID, "/start"))

		// The pairing reply names the sender's numeric id (design D4) — the
		// value the operator enters in the Channels pane.
		waitFor(t, bot, "the pairing reply naming the sender's numeric id", 20*time.Second, func() bool {
			return bot.hasCall("sendMessage", func(c botCall) bool {
				return c.chatID == chatID && strings.Contains(c.text, fmt.Sprintf("%d", userID))
			})
		})
		for _, c := range bot.callsOf("sendMessage") {
			if strings.Contains(c.text, fmt.Sprintf("%d", userID)) {
				require.Empty(t, c.parseMode, "the pairing notice is a plain notice, not markdown")
				require.Contains(t, c.text, "Your Telegram user id is 42")
			}
		}
		// One pending pairing row for the sender.
		waitFor(t, bot, "the pending pairing row", 10*time.Second, func() bool {
			pairings, err := app.Store.Channels().ListPendingPairings(ctx)
			return err == nil && len(pairings) == 1 && pairings[0].TelegramUserID == userID
		})
	})

	t.Run("operator approval allowlists the sender", func(t *testing.T) {
		require.NoError(t, app.Services.Channels.ApprovePairing(ctx, userID))
		users, err := app.Store.Channels().ListChannelUsers(ctx)
		require.NoError(t, err)
		require.Len(t, users, 1, "exactly the approved user is allowlisted")
		require.Equal(t, userID, users[0].TelegramUserID)
		pairings, err := app.Store.Channels().ListPendingPairings(ctx)
		require.NoError(t, err)
		require.Empty(t, pairings, "the pairing request must be consumed")
	})

	var sessionID string
	t.Run("DM turn streams, delivers, and a busy turn queues the next DM", func(t *testing.T) {
		// The scripted "hello" reply streams one delta immediately, a second
		// after 2.4s and a third after 4.8s — past the pinned 2s EditInterval,
		// so the pump must send a streaming preview and then edit it.
		bot.pushUpdate(dmUpdate(102, chatID, userID, "hello"))
		// The second DM lands while the first turn is still streaming (the
		// scripted stream holds it busy for ~5s): the per-chat FIFO queue
		// (design D5) must hold it and dispatch it after, in order.
		time.Sleep(1500 * time.Millisecond)
		bot.pushUpdate(dmUpdate(103, chatID, userID, "queued please"))

		// The first DM creates the chat row and pins a persistent session
		// (restart-safe binding, design D5).
		waitFor(t, bot, "the chat row's pinned session", 20*time.Second, func() bool {
			row, err := app.Store.Channels().GetChannelChat(ctx, chatID)
			return err == nil && row != nil && row.SessionID != ""
		})
		row, err := app.Store.Channels().GetChannelChat(ctx, chatID)
		require.NoError(t, err)
		require.Equal(t, store.ChannelChatKindDM, row.Kind)
		sessionID = row.SessionID

		// In-order dispatch: the queued message's turn starts only after the
		// first turn's model call finished.
		var firstCall, secondCall *modelCall
		waitFor(t, bot, "both DM turns dispatched in order", 30*time.Second, func() bool {
			firstCall, secondCall = model.callFor("hello", false), model.callFor("queued", false)
			return firstCall != nil && secondCall != nil &&
				secondCall.started.After(firstCall.finished)
		})

		// The first turn's final answer is delivered — the streaming preview
		// was established (a sendMessage), so the final rides an edit carrying
		// the full converted text. (The wire text is MarkdownV2-escaped, so
		// the match avoids escapable characters: "model." ships as "model\.".)
		waitFor(t, bot, "the first turn's streaming preview and final edit", 20*time.Second, func() bool {
			return bot.hasCall("sendMessage", func(c botCall) bool {
				return c.chatID == chatID && c.parseMode == "MarkdownV2" && strings.HasPrefix(c.text, "Hello from")
			}) && bot.hasCall("editMessageText", func(c botCall) bool {
				return c.chatID == chatID && strings.Contains(c.text, "fake model")
			})
		})
		// The queued turn's reply arrives after the first turn's final edit.
		var finalEditSeq, secondReplySeq int
		waitFor(t, bot, "the queued turn's reply after the first turn's final", 20*time.Second, func() bool {
			for _, c := range bot.callsOf("editMessageText") {
				if strings.Contains(c.text, "fake model") && c.seq > finalEditSeq {
					finalEditSeq = c.seq
				}
			}
			for _, c := range bot.callsOf("sendMessage") {
				if strings.Contains(c.text, "Second reply") && c.seq > secondReplySeq {
					secondReplySeq = c.seq
				}
			}
			return finalEditSeq > 0 && secondReplySeq > finalEditSeq
		})

		// The transcript carries the turns: two user messages and two final
		// assistant messages (the first assembled from the streamed deltas).
		events, err := app.Services.Chat.Transcript(ctx, sessionID)
		require.NoError(t, err)
		var userTexts, assistantTexts []string
		for _, ev := range events {
			switch ev.Kind {
			case sessions.KindUserMessage:
				userTexts = append(userTexts, ev.Text)
			case sessions.KindAssistantMessage:
				assistantTexts = append(assistantTexts, ev.Text)
			}
		}
		require.Contains(t, userTexts, "hello")
		require.Contains(t, userTexts, "queued please")
		require.Len(t, assistantTexts, 2, "one final assistant message per DM turn; got %v", assistantTexts)
		require.Equal(t, "Hello from the fake model.", assistantTexts[0])
		require.Equal(t, "Second reply.", assistantTexts[1])

		// The submitted turn's origin is observable through the run_started
		// hook payload: chats.go submits channel turns with hooks.OriginTelegram.
		hookRaw, err := os.ReadFile(hookLog)
		require.NoError(t, err)
		require.Contains(t, string(hookRaw), `"origin":"telegram"`)
	})

	t.Run("dangerous command interrupts, card tap resolves", func(t *testing.T) {
		// The command the model will ask to run — a forced recursive delete the
		// gate must interrupt on — targets a scratch file inside the jailed
		// workspace, so the post-approval execution is observable and harmless.
		require.NoError(t, os.MkdirAll(wsDir, 0o755))
		stale := filepath.Join(wsDir, "stale.txt")
		require.NoError(t, os.WriteFile(stale, []byte("delete me"), 0o644))

		bot.pushUpdate(dmUpdate(104, chatID, userID, "run the cleanup"))

		// The approval card: a sendMessage carrying the inline keyboard whose
		// callback_data is appr:<sessionID>:<interruptID>:<decision>.
		var cardMarkup string
		waitFor(t, bot, "the approval card with its inline keyboard", 30*time.Second, func() bool {
			for _, c := range bot.callsOf("sendMessage") {
				if c.replyMarkup != "" && strings.Contains(c.replyMarkup, "appr:") {
					cardMarkup = c.replyMarkup
					return true
				}
			}
			return false
		})
		var kb struct {
			InlineKeyboard [][]struct {
				Text         string `json:"text"`
				CallbackData string `json:"callback_data"`
			} `json:"inline_keyboard"`
		}
		require.NoError(t, json.Unmarshal([]byte(cardMarkup), &kb))
		require.Len(t, kb.InlineKeyboard, 1)
		require.Len(t, kb.InlineKeyboard[0], 2, "approve and deny buttons")
		approve, deny := kb.InlineKeyboard[0][0].CallbackData, kb.InlineKeyboard[0][1].CallbackData
		require.Equal(t, "Approve", kb.InlineKeyboard[0][0].Text)
		parts := strings.Split(approve, ":")
		require.Len(t, parts, 4, "callback_data must be appr:<sessionID>:<interruptID>:<decision>")
		require.Equal(t, "appr", parts[0])
		require.Equal(t, sessionID, parts[1], "the card names the pinned session")
		require.NotEmpty(t, parts[2], "the interrupt id must be present")
		require.True(t, strings.HasSuffix(approve, ":approve"))
		require.True(t, strings.HasSuffix(deny, ":deny"))

		// The interrupt is on the transcript: the pending dangerous command.
		waitFor(t, bot, "the approval_required transcript event", 10*time.Second, func() bool {
			events, err := app.Services.Chat.Transcript(ctx, sessionID)
			if err != nil {
				return false
			}
			for _, ev := range events {
				if ev.Kind == sessions.KindApprovalRequired && ev.Approval != nil &&
					strings.Contains(ev.Approval.Command, "rm -rf") {
					return true
				}
			}
			return false
		})

		// The allowlisted operator taps Approve; the channel re-verifies the
		// allowlist, resolves through the runtime, and answers the callback.
		bot.pushUpdate(map[string]any{
			"update_id": int64(105),
			"callback_query": map[string]any{
				"id": "cbq-1", "from": map[string]any{"id": userID},
				"data": approve,
				"message": map[string]any{
					"message_id": int64(7), "date": 1,
					"chat": map[string]any{"id": chatID, "type": "private"},
				},
			},
		})
		waitFor(t, bot, "the callback answered", 30*time.Second, func() bool {
			return bot.hasCall("answerCallbackQuery", func(c botCall) bool { return c.callbackQueryID == "cbq-1" })
		})

		// The approved command really executed inside the jail, the resumed
		// turn delivered its post-approval answer, and the turn closed.
		waitFor(t, bot, "the resumed turn's completion", 30*time.Second, func() bool {
			if _, err := os.Stat(stale); !os.IsNotExist(err) {
				return false
			}
			events, err := app.Services.Chat.Transcript(ctx, sessionID)
			if err != nil {
				return false
			}
			for _, ev := range events {
				if ev.Kind == sessions.KindAssistantMessage && ev.Text == "Cleaned up." {
					return true
				}
			}
			return false
		})
		require.NoFileExists(t, stale, "the approved rm must have executed in the workspace")
		interrupt := model.callFor("cleanup", false)
		resume := model.callFor("cleanup", true)
		require.NotNil(t, resume, "the resumed turn never re-called the model")
		require.True(t, resume.started.After(interrupt.finished),
			"the post-approval model call runs after the interrupting one")
	})

	t.Run("turn artifacts ride the terminal event", func(t *testing.T) {
		bot.pushUpdate(dmUpdate(106, chatID, userID, "make a report"))
		artPath := filepath.Join(wsDir, "artifacts", "report.txt")

		// The model writes into the workspace's artifacts/ directory through
		// the jailed shell; the turn_completed event must carry the delta.
		waitFor(t, bot, "the artifact turn's completion", 30*time.Second, func() bool {
			if _, err := os.Stat(artPath); err != nil {
				return false
			}
			events, err := app.Services.Chat.Transcript(ctx, sessionID)
			if err != nil {
				return false
			}
			for _, ev := range events {
				if ev.Kind == sessions.KindTurnCompleted {
					for _, art := range ev.Artifacts {
						if art.Path == "artifacts/report.txt" {
							return true
						}
					}
				}
			}
			return false
		})
		raw, err := os.ReadFile(artPath)
		require.NoError(t, err)
		require.Equal(t, "smoke-report", string(raw))

		// The wired artifact reader (cli.Assemble's WithArtifacts) delivers
		// the file through sendDocument: the artifact's base name is the
		// document name and the bytes are the file's (design D12).
		waitFor(t, bot, "the artifact delivered via sendDocument", 10*time.Second, func() bool {
			return bot.hasCall("sendDocument", func(c botCall) bool {
				return c.chatID == chatID && c.documentName == "report.txt"
			})
		})
		for _, c := range bot.callsOf("sendDocument") {
			if c.documentName == "report.txt" {
				require.Equal(t, chatID, c.chatID)
				require.Equal(t, "smoke-report", c.documentBody, "the document must carry the artifact's bytes")
			}
		}
	})

	t.Run("receipts exist and a redelivered update is not dispatched twice", func(t *testing.T) {
		// Every admitted update left a durable receipt (design D9) keyed by
		// the bot username.
		for _, id := range []int64{101, 102, 103, 104, 105, 106} {
			done, err := app.Store.Channels().HasUpdateReceipt(ctx, botUser, id)
			require.NoError(t, err)
			require.True(t, done, "no receipt for update %d", id)
		}

		// Redelivery of an already-processed update id — the Bot API's
		// unacked-redelivery behavior the receipts exist for — must not
		// dispatch the turn twice.
		pollsBefore := bot.getPollCount()
		bot.pushUpdate(dmUpdate(102, chatID, userID, "hello"))
		waitFor(t, bot, "several polls after the redelivery", 15*time.Second, func() bool {
			return bot.getPollCount() >= pollsBefore+3
		})
		require.Len(t, model.callsOf("hello", false), 1, "the redelivered DM dispatched a second turn")
		events, err := app.Services.Chat.Transcript(ctx, sessionID)
		require.NoError(t, err)
		hellos := 0
		for _, ev := range events {
			if ev.Kind == sessions.KindUserMessage && ev.Text == "hello" {
				hellos++
			}
		}
		require.Equal(t, 1, hellos, "the redelivered update persisted a duplicate user message")
	})
}

// dmUpdate builds a private-chat text message update from the allowlisted user.
func dmUpdate(id, chatID, userID int64, text string) map[string]any {
	return map[string]any{
		"update_id": id,
		"message": map[string]any{
			"message_id": id - 100, "date": 1,
			"from": map[string]any{"id": userID, "is_bot": false, "first_name": "Smoke", "username": "smokehuman"},
			"chat": map[string]any{"id": chatID, "type": "private"},
			"text": text,
		},
	}
}

// waitFor polls cond until it holds or the timeout lapses, then fails with
// the fake Bot API's call log (the primary forensic surface on mismatch).
func waitFor(t *testing.T, bot *fakeBotAPI, what string, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("smoke condition not met within %s: %s\n%s", timeout, what, bot.dump())
}

// ————— fake OpenAI-compatible model backend —————

// modelCall is one chat-completions request the backend served.
type modelCall struct {
	seq      int
	lastUser string // the last user message's text
	postTool bool   // a tool result follows the last user message (the resume phase)
	started  time.Time
	finished time.Time
}

// fakeModelBackend is a scripted OpenAI chat-completions server: it records
// every request and streams the reply the leg's script names. The reply is
// chosen from the request's last user message and whether a tool result
// follows it (the post-approval resume re-prompts with the tool output).
type fakeModelBackend struct {
	server *httptest.Server

	mu    sync.Mutex
	seq   int
	calls []modelCall
}

func newFakeModelBackend() *fakeModelBackend {
	f := &fakeModelBackend{}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	return f
}

func (f *fakeModelBackend) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/chat/completions" {
		http.Error(w, `{"error":"unknown path"}`, http.StatusNotFound)
		return
	}
	var body struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"bad body"}`, http.StatusBadRequest)
		return
	}
	lastUser, postTool := "", false
	for _, m := range body.Messages {
		switch {
		case m.Role == "user":
			lastUser = decodeContent(m.Content)
			postTool = false
		case m.Role == "tool":
			postTool = true
		}
	}

	f.mu.Lock()
	f.seq++
	call := modelCall{seq: f.seq, lastUser: lastUser, postTool: postTool, started: time.Now()}
	f.calls = append(f.calls, call)
	f.mu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream")
	flusher := w.(http.Flusher)
	chunk := func(delta map[string]any, finish string) {
		choice := map[string]any{"index": 0, "delta": delta}
		if finish != "" {
			choice["finish_reason"] = finish
		}
		raw, _ := json.Marshal(map[string]any{
			"id": fmt.Sprintf("chatcmpl-%d", call.seq), "object": "chat.completion.chunk",
			"created": 1, "model": "fake-model", "choices": []any{choice},
		})
		fmt.Fprintf(w, "data: %s\n\n", raw)
		flusher.Flush()
	}

	switch {
	case !postTool && strings.Contains(lastUser, "hello"):
		// Slow streaming: deltas spaced past the pinned EditInterval so the
		// delivery pump must establish a preview and edit it.
		for i, piece := range []string{"Hello from", " the fake", " model."} {
			if i > 0 {
				time.Sleep(2400 * time.Millisecond)
			}
			chunk(map[string]any{"role": "assistant", "content": piece}, "")
		}
		chunk(map[string]any{}, "stop")
	case !postTool && strings.Contains(lastUser, "queued"):
		chunk(map[string]any{"role": "assistant", "content": "Second reply."}, "")
		chunk(map[string]any{}, "stop")
	case !postTool && strings.Contains(lastUser, "cleanup"):
		chunk(map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{
			"index": 0, "id": "call-cleanup", "type": "function",
			"function": map[string]any{"name": "execute", "arguments": `{"command":"rm -rf /workspace/stale.txt"}`},
		}}}, "")
		chunk(map[string]any{}, "tool_calls")
	case !postTool && strings.Contains(lastUser, "report"):
		chunk(map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{
			"index": 0, "id": "call-report", "type": "function",
			"function": map[string]any{"name": "execute",
				"arguments": `{"command":"mkdir -p artifacts && printf smoke-report > artifacts/report.txt"}`},
		}}}, "")
		chunk(map[string]any{}, "tool_calls")
	case postTool && strings.Contains(lastUser, "cleanup"):
		chunk(map[string]any{"role": "assistant", "content": "Cleaned up."}, "")
		chunk(map[string]any{}, "stop")
	case postTool && strings.Contains(lastUser, "report"):
		chunk(map[string]any{"role": "assistant", "content": "Report ready."}, "")
		chunk(map[string]any{}, "stop")
	default:
		chunk(map[string]any{"role": "assistant", "content": "unexpected prompt: " + lastUser}, "")
		chunk(map[string]any{}, "stop")
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()

	f.mu.Lock()
	f.calls[len(f.calls)-1].finished = time.Now()
	f.mu.Unlock()
}

// decodeContent extracts a message's text from a string-or-parts content.
func decodeContent(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err == nil {
		var b strings.Builder
		for _, p := range parts {
			b.WriteString(p.Text)
		}
		return b.String()
	}
	return ""
}

func (f *fakeModelBackend) callFor(userText string, postTool bool) *modelCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.calls {
		if f.calls[i].postTool == postTool && strings.Contains(f.calls[i].lastUser, userText) {
			c := f.calls[i]
			return &c
		}
	}
	return nil
}

func (f *fakeModelBackend) callsOf(userText string, postTool bool) []modelCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []modelCall
	for _, c := range f.calls {
		if c.postTool == postTool && strings.Contains(c.lastUser, userText) {
			out = append(out, c)
		}
	}
	return out
}

// ————— fake Telegram Bot API —————

// botUser is the fake bot's @username (the receipt key's other half).
const botUser = "smokebot"

// botCall is one Bot API method call the fake recorded.
type botCall struct {
	seq             int
	method          string
	chatID          int64
	text            string
	parseMode       string
	replyMarkup     string
	callbackQueryID string
	documentName    string
	documentBody    string
}

// fakeBotAPI is a scripted Bot API server behind the BUDDIES_TELEGRAM_API_BASE
// override: it records every method call, serves a pushed update queue from
// getUpdates (responding promptly — empty results after a short hold — so the
// long-poll semantics stay bounded), and answers every method with the
// {ok:true,result:...} envelope.
type fakeBotAPI struct {
	server       *httptest.Server
	coldBootDone chan struct{}

	mu        sync.Mutex
	seq       int
	calls     []botCall
	updates   []map[string]any
	msgID     atomic.Int64
	pollCount int
	coldBoots int
}

func newFakeBotAPI() *fakeBotAPI {
	f := &fakeBotAPI{coldBootDone: make(chan struct{})}
	f.msgID.Store(100)
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	return f
}

func (f *fakeBotAPI) serve(w http.ResponseWriter, r *http.Request) {
	// The client posts to <base>/bot<token>/<method>; token "test-token".
	const prefix = "/bottest-token/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error_code": 404,
			"description": "unknown path " + r.URL.Path})
		return
	}
	method := strings.TrimPrefix(r.URL.Path, prefix)

	w.Header().Set("Content-Type", "application/json")
	ok := func(result any) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
	}

	switch method {
	case "getUpdates":
		f.mu.Lock()
		f.pollCount++
		f.coldBoots++
		coldBoot := f.coldBoots == 1
		batch := f.updates
		f.updates = nil
		f.mu.Unlock()
		if coldBoot {
			close(f.coldBootDone) // the ack-and-drop boot poll is served
		}
		if len(batch) == 0 {
			// Long-poll semantics, bounded: hold briefly so the poll loop
			// does not spin, then answer empty (nothing pending).
			time.Sleep(100 * time.Millisecond)
		}
		if batch == nil {
			batch = []map[string]any{}
		}
		ok(batch)
	case "getMe":
		ok(map[string]any{"id": 9001, "is_bot": true, "first_name": "SmokeBot", "username": botUser})
	case "getWebhookInfo":
		ok(map[string]any{"pending_update_count": 0})
	case "setMyCommands":
		ok(true)
	case "sendMessage", "editMessageText":
		var p struct {
			ChatID      int64           `json:"chat_id"`
			MessageID   int64           `json:"message_id"`
			Text        string          `json:"text"`
			ParseMode   string          `json:"parse_mode"`
			ReplyMarkup json.RawMessage `json:"reply_markup"`
		}
		_ = json.NewDecoder(r.Body).Decode(&p)
		f.record(botCall{method: method, chatID: p.ChatID, text: p.Text,
			parseMode: p.ParseMode, replyMarkup: string(p.ReplyMarkup)})
		ok(map[string]any{"message_id": f.msgID.Add(1),
			"chat": map[string]any{"id": p.ChatID, "type": "private"}})
	case "sendChatAction":
		var p struct {
			ChatID int64  `json:"chat_id"`
			Action string `json:"action"`
		}
		_ = json.NewDecoder(r.Body).Decode(&p)
		f.record(botCall{method: method, chatID: p.ChatID})
		ok(true)
	case "answerCallbackQuery":
		var p struct {
			CallbackQueryID string `json:"callback_query_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&p)
		f.record(botCall{method: method, callbackQueryID: p.CallbackQueryID})
		ok(true)
	case "sendDocument":
		if err := r.ParseMultipartForm(10 << 20); err == nil {
			var chatID int64
			fmt.Sscanf(r.FormValue("chat_id"), "%d", &chatID)
			name, body := "", ""
			if file := r.MultipartForm.File["document"]; len(file) > 0 {
				name = file[0].Filename
				if f, ferr := file[0].Open(); ferr == nil {
					raw, _ := io.ReadAll(f)
					_ = f.Close()
					body = string(raw)
				}
			}
			f.record(botCall{method: method, chatID: chatID, documentName: name, documentBody: body})
		}
		ok(map[string]any{"message_id": f.msgID.Add(1),
			"chat": map[string]any{"id": 0, "type": "private"}})
	default:
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error_code": 404,
			"description": "unscripted method " + method})
	}
}

func (f *fakeBotAPI) record(c botCall) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	c.seq = f.seq
	f.calls = append(f.calls, c)
}

func (f *fakeBotAPI) pushUpdate(u map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, u)
}

func (f *fakeBotAPI) getPollCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pollCount
}

func (f *fakeBotAPI) callsOf(method string) []botCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []botCall
	for _, c := range f.calls {
		if c.method == method {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeBotAPI) hasCall(method string, match func(botCall) bool) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c.method == method && match(c) {
			return true
		}
	}
	return false
}

// dump renders the call log for failure output.
func (f *fakeBotAPI) dump() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var b strings.Builder
	b.WriteString("Bot API call log:\n")
	for _, c := range f.calls {
		fmt.Fprintf(&b, "  #%d %s chat=%d", c.seq, c.method, c.chatID)
		if c.text != "" {
			fmt.Fprintf(&b, " text=%q parse_mode=%q", c.text, c.parseMode)
		}
		if c.replyMarkup != "" {
			fmt.Fprintf(&b, " reply_markup=%s", c.replyMarkup)
		}
		if c.callbackQueryID != "" {
			fmt.Fprintf(&b, " callback_query_id=%q", c.callbackQueryID)
		}
		if c.documentName != "" {
			fmt.Fprintf(&b, " document=%q body=%q", c.documentName, c.documentBody)
		}
		b.WriteString("\n")
	}
	return b.String()
}
