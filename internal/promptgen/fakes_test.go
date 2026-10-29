package promptgen

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/buddies/internal/store"
)

// fakeChatModel is a scripted BaseChatModel: Generate delegates to
// generateFunc; Stream is unsupported — generation never streams.
type fakeChatModel struct {
	generateFunc func(ctx context.Context, input []*schema.Message) (*schema.Message, error)
}

func (m *fakeChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	if m.generateFunc != nil {
		return m.generateFunc(ctx, input)
	}
	return nil, errors.New("not implemented")
}

func (m *fakeChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("stream not implemented in fake")
}

// fakeFactory is a scripted ModelFactory that records what it was asked to
// build, so tests can pin the provider/model plumbing — including the
// structured flag across the retry's two builds.
type fakeFactory struct {
	model *fakeChatModel
	// models, when non-empty, hands out one model per Build in order; the
	// retry test uses it to script a rejecting model followed by a working
	// one. When exhausted (or empty) it falls back to model.
	models   []*fakeChatModel
	buildErr error

	gotProvider     store.Provider
	gotModelName    string
	builds          int
	buildStructured []bool
}

func (f *fakeFactory) Build(ctx context.Context, provider store.Provider, modelName string, structured bool) (model.BaseChatModel, error) {
	f.builds++
	f.buildStructured = append(f.buildStructured, structured)
	f.gotProvider = provider
	f.gotModelName = modelName
	if f.buildErr != nil {
		return nil, f.buildErr
	}
	if len(f.models) > 0 {
		m := f.models[0]
		f.models = f.models[1:]
		if m != nil {
			return m, nil
		}
	}
	if f.model != nil {
		return f.model, nil
	}
	return &fakeChatModel{}, nil
}

// fakeDocStore is an in-memory DocStore. commitErr is the injection point for
// both plain write failures and backup failures — behind the DocStore contract
// both abort the commit with no document changes.
type fakeDocStore struct {
	docs      map[string]Documents
	readErr   error
	commitErr error

	commitCalls int
}

func newFakeDocStore() *fakeDocStore {
	return &fakeDocStore{docs: map[string]Documents{}}
}

func (d *fakeDocStore) ReadDocuments(slug string) (Documents, bool, error) {
	if d.readErr != nil {
		return Documents{}, false, d.readErr
	}
	docs, ok := d.docs[slug]
	return docs, ok, nil
}

func (d *fakeDocStore) CommitDocuments(slug string, docs Documents) error {
	d.commitCalls++
	if d.commitErr != nil {
		return d.commitErr
	}
	d.docs[slug] = docs
	return nil
}

// promptsToolCallMessage builds a model response carrying the documents as a
// tool call — the shape some adapters surface structured output as.
func promptsToolCallMessage(identity, soul, bootstrap string) *schema.Message {
	args, _ := json.Marshal(Documents{Identity: identity, Soul: soul, Bootstrap: bootstrap})
	return &schema.Message{
		ToolCalls: []schema.ToolCall{{
			ID:       "call_prompts",
			Type:     "function",
			Function: schema.FunctionCall{Name: "submit_prompts", Arguments: string(args)},
		}},
	}
}

// testParams builds a valid Params with a provider key the sanitize tests can
// watch for.
func testParams() Params {
	return Params{
		Provider:  store.Provider{ID: "prov-1", Type: "openai", Name: "OpenAI", APIKey: "sk-secret-provider-key"},
		Model:     "gpt-4o",
		AgentName: "Scout",
		Brief:     "Scout the perimeter.",
	}
}
