package agent_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/agent"
	"github.com/oniharnantyo/onclaw/internal/agent/middlewares"
	"github.com/oniharnantyo/onclaw/internal/llm/adapter"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// streamingRecorder wraps a model.AgenticModel and records whether the run
// used the Generate (buffered) or Stream (token-level) path.
type streamingRecorder struct {
	model.AgenticModel
	generated int
	streamed  int
}

func (r *streamingRecorder) Generate(ctx context.Context, in []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	r.generated++
	return r.AgenticModel.Generate(ctx, in, opts...)
}

func (r *streamingRecorder) Stream(ctx context.Context, in []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	r.streamed++
	return r.AgenticModel.Stream(ctx, in, opts...)
}

func newStreamingAgent(t *testing.T, rec *streamingRecorder) *agent.Agent {
	t.Helper()
	tmpDir := t.TempDir()
	workspace := filepath.Join(tmpDir, "workspace")
	_ = os.MkdirAll(workspace, 0755)
	userConfigDir := filepath.Join(tmpDir, "config")
	_ = os.MkdirAll(userConfigDir, 0755)

	agentConf := &store.Agent{Name: "test-streaming-agent"}

	ag, err := agent.AssembleAgent(
		context.Background(), agentConf, rec, rec,
		workspace, userConfigDir, "deny", nil, nil, 64000,
		dummyConvStore{}, 1, nil, nil, nil, "test",
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, 0, nil, 3,
	)
	if err != nil {
		t.Fatalf("assemble agent: %v", err)
	}
	return ag
}

func drainAgentRun(t *testing.T, it agent.EventIterator) {
	t.Helper()
	for {
		if _, ok := it.Next(); !ok {
			break
		}
	}
	if err := it.Err(); err != nil {
		t.Fatalf("agent run error: %v", err)
	}
}

func buildStubModel(t *testing.T) model.AgenticModel {
	t.Helper()
	m, err := adapter.NewStubAdapter().Build(
		context.Background(),
		&store.Profile{Name: "stub", ProviderType: "stub", Enabled: 1},
		"model", "",
	)
	if err != nil {
		t.Fatalf("build stub model: %v", err)
	}
	return m
}

func TestAgentRun_StreamingEnabledUsesStreamPath(t *testing.T) {
	rec := &streamingRecorder{AgenticModel: buildStubModel(t)}
	ag := newStreamingAgent(t, rec)

	ctx := middlewares.WithStreaming(context.Background(), true)
	drainAgentRun(t, ag.Run(ctx, "Hello"))

	if rec.streamed == 0 {
		t.Error("expected model Stream to be called when streaming is enabled")
	}
	if rec.generated != 0 {
		t.Errorf("expected Generate not to be called when streaming is enabled, got %d", rec.generated)
	}
}

func TestAgentRun_StreamingDisabledUsesGeneratePath(t *testing.T) {
	rec := &streamingRecorder{AgenticModel: buildStubModel(t)}
	ag := newStreamingAgent(t, rec)

	// No WithStreaming call -> defaults to disabled.
	drainAgentRun(t, ag.Run(context.Background(), "Hello"))

	if rec.generated == 0 {
		t.Error("expected model Generate to be called when streaming is disabled")
	}
	if rec.streamed != 0 {
		t.Errorf("expected Stream not to be called when streaming is disabled, got %d", rec.streamed)
	}
}
