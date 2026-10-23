package agents

import (
	"context"
	"fmt"
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
	inputs [][]*schema.AgenticMessage
	// toolsPerCall records the tool names bound via model options on each
	// call, aligned with inputs.
	toolsPerCall [][]string

	// reply builds the response for one call from its input messages.
	// Can be func([]*schema.AgenticMessage) fakeReply or func([]*schema.Message) fakeReply.
	reply any
	// midStream, when set, blocks each Stream call after its first chunk is
	// sent — a deterministic mid-turn pause for disconnect tests. The
	// hook's own synchronization is the caller's responsibility.
	midStream func()
}

func (f *fakeModel) Generate(_ context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	f.record(input, toolNamesOf(opts))
	r := adaptReply(f.reply)(input)
	if r.err != nil {
		return nil, r.err
	}
	blocks := make([]*schema.ContentBlock, 0, len(r.toolCalls)+2)
	if r.reasoning != "" {
		blocks = append(blocks, schema.NewContentBlock(&schema.Reasoning{Text: r.reasoning}))
	}
	if r.text != "" {
		blocks = append(blocks, schema.NewContentBlock(&schema.AssistantGenText{Text: r.text}))
	}
	for _, call := range r.toolCalls {
		blocks = append(blocks, schema.NewContentBlock(&schema.FunctionToolCall{
			CallID:    call.ID,
			Name:      call.Function.Name,
			Arguments: call.Function.Arguments,
		}))
	}
	msg := &schema.AgenticMessage{
		Role:          schema.AgenticRoleTypeAssistant,
		ContentBlocks: blocks,
		ResponseMeta:  &schema.AgenticResponseMeta{TokenUsage: tokenUsage(r)},
	}
	return msg, nil
}

func (f *fakeModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	f.record(input, toolNamesOf(opts))
	r := adaptReply(f.reply)(input)
	if r.err != nil {
		return nil, r.err
	}

	// Streamed blocks carry StreamingMeta the way real providers emit them:
	// eino concatenates a stream by block index, so every text chunk shares
	// one index and merges back into a single text block, while reasoning and
	// each tool call keep their own.
	chunks := make([]*schema.AgenticMessage, 0, 8)
	nextIndex := 0
	streamed := func(block *schema.ContentBlock, index int) *schema.AgenticMessage {
		block.StreamingMeta = &schema.StreamingMeta{Index: index}
		return &schema.AgenticMessage{
			Role:          schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{block},
		}
	}
	if r.reasoning != "" {
		chunks = append(chunks, streamed(schema.NewContentBlock(&schema.Reasoning{Text: r.reasoning}), nextIndex))
		nextIndex++
	}
	for _, call := range r.toolCalls {
		chunks = append(chunks, streamed(schema.NewContentBlock(&schema.FunctionToolCall{
			CallID:    call.ID,
			Name:      call.Function.Name,
			Arguments: call.Function.Arguments,
		}), nextIndex))
		nextIndex++
	}
	words := strings.Fields(r.text)
	textIndex := nextIndex
	for i, word := range words {
		suffix := " "
		if i == len(words)-1 {
			suffix = ""
		}
		chunks = append(chunks, streamed(schema.NewContentBlock(&schema.AssistantGenText{Text: word + suffix}), textIndex))
	}
	if len(chunks) == 0 {
		chunks = append(chunks, &schema.AgenticMessage{
			Role: schema.AgenticRoleTypeAssistant,
		})
	}
	chunks[len(chunks)-1].ResponseMeta = &schema.AgenticResponseMeta{TokenUsage: tokenUsage(r)}

	sr, sw := schema.Pipe[*schema.AgenticMessage](len(chunks))
	go func() {
		defer sw.Close()
		for i, chunk := range chunks {
			if sw.Send(chunk, nil) {
				return
			}
			if i == 0 && f.midStream != nil {
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

func (f *fakeModel) record(input []*schema.AgenticMessage, toolNames []string) {
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

// calls returns a copy of every recorded call's input as legacy schema.Message.
func (f *fakeModel) calls() [][]*schema.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]*schema.Message, len(f.inputs))
	for i, inp := range f.inputs {
		out[i] = agenticSliceToLegacy(inp)
	}
	return out
}

// agenticCalls returns a copy of every recorded call's input as schema.AgenticMessage.
func (f *fakeModel) agenticCalls() [][]*schema.AgenticMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]*schema.AgenticMessage, len(f.inputs))
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

// toolsOfCallWhere returns the tool names offered on the first recorded call
// whose input satisfies pred; nil when no call matches. Background children
// run concurrently with the parent, so call order is not a stable handle.
func (f *fakeModel) toolsOfCallWhere(pred func([]*schema.Message) bool) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, inp := range f.inputs {
		if pred(agenticSliceToLegacy(inp)) {
			out := make([]string, len(f.toolsPerCall[i]))
			copy(out, f.toolsPerCall[i])
			return out
		}
	}
	return nil
}

// fakeRegistry returns the fake model for every Build, recording the agents
// and overrides it was asked to build for.
type fakeRegistry struct {
	model *fakeModel

	mu        sync.Mutex
	agents    []store.Agent
	overrides []ModelOverride
}

func (r *fakeRegistry) Build(_ context.Context, agent store.Agent, override ModelOverride) (model.BaseModel[*schema.AgenticMessage], error) {
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

func agenticToLegacy(am *schema.AgenticMessage) *schema.Message {
	if am == nil {
		return nil
	}
	var role schema.RoleType
	switch am.Role {
	case schema.AgenticRoleTypeUser:
		role = schema.User
	case schema.AgenticRoleTypeAssistant:
		role = schema.Assistant
	case schema.AgenticRoleTypeSystem:
		role = schema.System
	default:
		role = schema.RoleType(am.Role)
	}
	var content string
	var multiContent []schema.MessageInputPart
	var toolCalls []schema.ToolCall
	var toolCallID string

	for _, block := range am.ContentBlocks {
		if block == nil {
			continue
		}
		switch block.Type {
		case schema.ContentBlockTypeUserInputText:
			if block.UserInputText != nil {
				content += block.UserInputText.Text
				multiContent = append(multiContent, schema.MessageInputPart{
					Type: schema.ChatMessagePartTypeText,
					Text: block.UserInputText.Text,
				})
			}
		case schema.ContentBlockTypeUserInputImage:
			if block.UserInputImage != nil {
				b64 := block.UserInputImage.Base64Data
				multiContent = append(multiContent, schema.MessageInputPart{
					Type: schema.ChatMessagePartTypeImageURL,
					Image: &schema.MessageInputImage{
						MessagePartCommon: schema.MessagePartCommon{
							Base64Data: &b64,
							MIMEType:   block.UserInputImage.MIMEType,
						},
					},
				})
			}
		case schema.ContentBlockTypeUserInputFile:
			if block.UserInputFile != nil {
				b64 := block.UserInputFile.Base64Data
				multiContent = append(multiContent, schema.MessageInputPart{
					Type: schema.ChatMessagePartTypeFileURL,
					File: &schema.MessageInputFile{
						MessagePartCommon: schema.MessagePartCommon{
							Base64Data: &b64,
							MIMEType:   block.UserInputFile.MIMEType,
						},
						Name: block.UserInputFile.Name,
					},
				})
			}
		case schema.ContentBlockTypeUserInputVideo:
			if block.UserInputVideo != nil {
				b64 := block.UserInputVideo.Base64Data
				multiContent = append(multiContent, schema.MessageInputPart{
					Type: schema.ChatMessagePartTypeVideoURL,
					Video: &schema.MessageInputVideo{
						MessagePartCommon: schema.MessagePartCommon{
							Base64Data: &b64,
							MIMEType:   block.UserInputVideo.MIMEType,
						},
					},
				})
			}
		case schema.ContentBlockTypeAssistantGenText:
			if block.AssistantGenText != nil {
				content += block.AssistantGenText.Text
			}
		case schema.ContentBlockTypeFunctionToolCall:
			if block.FunctionToolCall != nil {
				toolCalls = append(toolCalls, schema.ToolCall{
					ID: block.FunctionToolCall.CallID,
					Function: schema.FunctionCall{
						Name:      block.FunctionToolCall.Name,
						Arguments: block.FunctionToolCall.Arguments,
					},
				})
			}
		case schema.ContentBlockTypeFunctionToolResult:
			if block.FunctionToolResult != nil {
				role = schema.Tool
				toolCallID = block.FunctionToolResult.CallID
				for _, c := range block.FunctionToolResult.Content {
					if c != nil && c.Type == schema.FunctionToolResultContentBlockTypeText && c.Text != nil {
						content += c.Text.Text
					}
				}
			}
		}
	}
	return &schema.Message{
		Role:                  role,
		Content:               content,
		UserInputMultiContent: multiContent,
		ToolCalls:             toolCalls,
		ToolCallID:            toolCallID,
	}
}

func agenticSliceToLegacy(in []*schema.AgenticMessage) []*schema.Message {
	if in == nil {
		return nil
	}
	out := make([]*schema.Message, len(in))
	for i, m := range in {
		out[i] = agenticToLegacy(m)
	}
	return out
}

func adaptReply(r any) func([]*schema.AgenticMessage) fakeReply {
	if r == nil {
		return func(_ []*schema.AgenticMessage) fakeReply { return fakeReply{} }
	}
	switch fn := r.(type) {
	case func([]*schema.AgenticMessage) fakeReply:
		return fn
	case func([]*schema.Message) fakeReply:
		return func(input []*schema.AgenticMessage) fakeReply {
			return fn(agenticSliceToLegacy(input))
		}
	default:
		panic(fmt.Sprintf("unsupported reply function type: %T", r))
	}
}

func newFixture(t *testing.T, reply any, opts ...Option) *fixture {
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
