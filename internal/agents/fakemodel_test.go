package agents

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
	"github.com/stretchr/testify/require"
)

// fakeReply is one scripted model response.
type fakeReply struct {
	text             string // streamed as one chunk per word
	reasoning        string // streamed as a single reasoning chunk when set
	toolCalls        []schema.ToolCall
	promptTokens     int
	completionTokens int
	err              error // fails the stream up front when set
}

// fakeModel is a deterministic eino chat model for tests: it records every
// call's input messages and replies with a scripted response. Safe for
// concurrent use (turn goroutine plus test assertions).
type fakeModel struct {
	mu     sync.Mutex
	inputs [][]*schema.Message
	// toolsPerCall records the tool names bound via model options on each
	// call, aligned with inputs.
	toolsPerCall [][]string

	// reply builds the response for one call from its input messages.
	reply func(input []*schema.Message) fakeReply
	// midStream, when set, blocks each Stream call after its first chunk is
	// sent — a deterministic mid-turn pause for disconnect tests. The
	// hook's own synchronization is the caller's responsibility.
	midStream func()
}

func (f *fakeModel) Generate(_ context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	f.record(input, toolNamesOf(opts))
	r := f.reply(input)
	if r.err != nil {
		return nil, r.err
	}
	msg := schema.AssistantMessage(r.text, r.toolCalls)
	msg.ResponseMeta = &schema.ResponseMeta{Usage: tokenUsage(r)}
	return msg, nil
}

func (f *fakeModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	f.record(input, toolNamesOf(opts))
	r := f.reply(input)
	if r.err != nil {
		return nil, r.err
	}

	chunks := make([]*schema.Message, 0, 8)
	if r.reasoning != "" {
		reasoningChunk := schema.AssistantMessage("", nil)
		reasoningChunk.ReasoningContent = r.reasoning
		chunks = append(chunks, reasoningChunk)
	}
	for _, call := range r.toolCalls {
		// Tool calls ride their own chunk, the provider convention.
		chunks = append(chunks, schema.AssistantMessage("", []schema.ToolCall{call}))
	}
	words := strings.Fields(r.text)
	for i, word := range words {
		// Whitespace between words rides each chunk; the final word carries
		// none, so the concatenated text equals the scripted text.
		suffix := " "
		if i == len(words)-1 {
			suffix = ""
		}
		chunks = append(chunks, schema.AssistantMessage(word+suffix, nil))
	}
	if len(chunks) == 0 {
		chunks = append(chunks, schema.AssistantMessage("", nil))
	}
	// Providers report usage on the final frame of a stream.
	chunks[len(chunks)-1].ResponseMeta = &schema.ResponseMeta{Usage: tokenUsage(r)}

	sr, sw := schema.Pipe[*schema.Message](len(chunks))
	go func() {
		defer sw.Close()
		for i, chunk := range chunks {
			if sw.Send(chunk, nil) {
				return
			}
			if i == 0 && f.midStream != nil {
				// The pause is interruptible like a real provider stream:
				// a cancelled request context aborts the stream with the
				// cancellation error.
				paused := make(chan struct{})
				go func() { defer close(paused); f.midStream() }()
				select {
				case <-paused:
				case <-ctx.Done():
					sw.Send(nil, ctx.Err())
					return
				}
			}
		}
	}()
	return sr, nil
}

func tokenUsage(r fakeReply) *schema.TokenUsage {
	return &schema.TokenUsage{
		PromptTokens:     r.promptTokens,
		CompletionTokens: r.completionTokens,
		TotalTokens:      r.promptTokens + r.completionTokens,
	}
}

func (f *fakeModel) record(input []*schema.Message, toolNames []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inputs = append(f.inputs, input)
	f.toolsPerCall = append(f.toolsPerCall, toolNames)
}

// toolNamesOf extracts the tool names bound through the call options.
func toolNamesOf(opts []model.Option) []string {
	o := model.GetCommonOptions(&model.Options{}, opts...)
	if len(o.Tools) == 0 {
		return nil
	}
	names := make([]string, 0, len(o.Tools))
	for _, t := range o.Tools {
		names = append(names, t.Name)
	}
	return names
}

// calls returns a copy of every recorded call's input.
func (f *fakeModel) calls() [][]*schema.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]*schema.Message, len(f.inputs))
	copy(out, f.inputs)
	return out
}

// toolsOf returns a copy of the tool names offered on the i-th call.
func (f *fakeModel) toolsOf(i int) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i >= len(f.toolsPerCall) {
		return nil
	}
	out := make([]string, len(f.toolsPerCall[i]))
	copy(out, f.toolsPerCall[i])
	return out
}

// fakeRegistry returns the fake model for every Build, recording the agents
// and overrides it was asked to build for.
type fakeRegistry struct {
	model *fakeModel

	mu        sync.Mutex
	agents    []store.Agent
	overrides []ModelOverride
}

func (r *fakeRegistry) Build(_ context.Context, agent store.Agent, override ModelOverride) (model.BaseChatModel, error) {
	r.mu.Lock()
	r.agents = append(r.agents, agent)
	r.overrides = append(r.overrides, override)
	r.mu.Unlock()
	return r.model, nil
}

// lastOverride returns the override of the i-th Build call.
func (r *fakeRegistry) overrideAt(i int) ModelOverride {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i >= len(r.overrides) {
		return ModelOverride{}
	}
	return r.overrides[i]
}

func (r *fakeRegistry) Models(providerType string) []string { return CatalogModels(providerType) }

// fakePromptDocs is a deterministic PromptDocs source: it returns canned
// documents and counts every read, so tests can pin the fresh-read-per-turn
// contract. The zero value supplies no documents.
type fakePromptDocs struct {
	mu        sync.Mutex
	reads     int
	identity  string
	soul      string
	bootstrap string
	err       error // fails every read when set
}

func (f *fakePromptDocs) Documents(_ context.Context, _ string) (string, string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	if f.err != nil {
		return "", "", "", f.err
	}
	return f.identity, f.soul, f.bootstrap, nil
}

func (f *fakePromptDocs) readCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads
}

// isSummarizeCall reports whether an input is a compaction summarizer call:
// the summarize system prompt followed by the transcript.
func isSummarizeCall(input []*schema.Message) bool {
	return len(input) == 2 &&
		input[0].Role == schema.System &&
		input[0].Content == summarizeSystemPrompt
}

// fixture wires a manager over the in-memory store with the fake registry,
// and seeds one agent ("tester", claude-sonnet-4-5 on an enabled provider).
type fixture struct {
	store    *fake.Store
	sessions sessions.Service
	model    *fakeModel
	registry *fakeRegistry
	mgr      *manager
	agent    store.Agent
}

func newFixture(t *testing.T, reply func(input []*schema.Message) fakeReply, opts ...Option) *fixture {
	t.Helper()

	fs := fake.New()
	svc := sessions.NewService(fs)
	fm := &fakeModel{reply: reply}
	reg := &fakeRegistry{model: fm}
	// The browser sits in the catalog, so an open-allowlist turn constructs
	// it; every fixture gets a throwaway browser home (production wiring
	// always supplies the layout's browser directory).
	m := newManager(fs, svc, reg, append(opts, WithBrowserDir(t.TempDir()))...)

	provider := &store.Provider{Name: "anthropic", Type: ProviderAnthropic, APIKey: "key-test", Enabled: true}
	require.NoError(t, fs.Providers().Create(t.Context(), provider))
	agent := &store.Agent{
		Name:       "tester",
		ProviderID: provider.ID,
		Model:      "claude-sonnet-4-5",
	}
	require.NoError(t, fs.Agents().Create(t.Context(), agent))

	return &fixture{store: fs, sessions: svc, model: fm, registry: reg, mgr: m, agent: *agent}
}

// newSession creates a session for the fixture's agent.
func (f *fixture) newSession(t *testing.T) sessions.Session {
	t.Helper()
	sess, err := f.mgr.CreateSession(t.Context(), f.agent.ID)
	require.NoError(t, err)
	return sess
}

// subCollector gathers a subscription's events until Close; Wait drains.
// Text deltas are also forwarded on a sideband channel so tests can observe
// streaming progress mid-turn.
type subCollector struct {
	ch     <-chan sessions.Event
	done   chan []sessions.Event
	deltas chan sessions.Event
	cancel func()
}

// collect subscribes to the session and gathers events in the background.
func collect(t *testing.T, m *manager, sessionID string) *subCollector {
	t.Helper()
	sub, err := m.Subscribe(t.Context(), sessionID)
	require.NoError(t, err)

	c := &subCollector{
		ch:     sub.Events(),
		done:   make(chan []sessions.Event, 1),
		deltas: make(chan sessions.Event, 64),
	}
	go func() {
		events := make([]sessions.Event, 0)
		for ev := range c.ch {
			events = append(events, ev)
			if ev.Kind == sessions.KindTextDelta && len(c.deltas) < cap(c.deltas) {
				c.deltas <- ev
			}
		}
		c.done <- events
	}()
	c.cancel = sub.Close
	return c
}

// Wait stops collecting and returns everything received so far.
func (c *subCollector) Wait() []sessions.Event {
	c.cancel()
	return <-c.done
}

// waitDelta blocks until n text deltas have been observed mid-turn.
func (c *subCollector) waitDelta(t *testing.T, n int) {
	t.Helper()
	for range n {
		<-c.deltas
	}
}

// kindsOf maps events to their kind sequence, for readable assertions.
func kindsOf(events []sessions.Event) []string {
	out := make([]string, 0, len(events))
	for _, ev := range events {
		out = append(out, string(ev.Kind))
	}
	return out
}

// textOf concatenates a kind's text payloads.
func textOf(events []sessions.Event, kind sessions.Kind) string {
	var sb strings.Builder
	for _, ev := range events {
		if ev.Kind == kind {
			sb.WriteString(ev.Text)
		}
	}
	return sb.String()
}

// eventsOfKind filters a kind's events.
func eventsOfKind(events []sessions.Event, kind sessions.Kind) []sessions.Event {
	out := make([]sessions.Event, 0)
	for _, ev := range events {
		if ev.Kind == kind {
			out = append(out, ev)
		}
	}
	return out
}
