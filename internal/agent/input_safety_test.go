package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/agent"
	"github.com/oniharnantyo/onclaw/internal/agent/middlewares"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// TestEstimateFloorTokens verifies the fixed floor (system instruction + tool
// schemas) is computed as chars/4, mirroring the in-package estimateTokenCount.
func TestEstimateFloorTokens(t *testing.T) {
	instruction := "You are a helpful assistant."
	tools := []*schema.ToolInfo{
		{Name: "read_file", Desc: "Reads a file from the workspace."},
		{Name: "write_file", Desc: "Writes a file to the workspace."},
	}

	want := len(instruction) / 4
	for _, tl := range tools {
		tl_ := *tl
		tl_.Extra = nil
		b, err := json.Marshal(tl_)
		if err != nil {
			t.Fatalf("marshal tool info in test: %v", err)
		}
		want += len(b) / 4
	}

	got, err := agent.EstimateFloorTokens(context.Background(), instruction, tools)
	if err != nil {
		t.Fatalf("EstimateFloorTokens: %v", err)
	}
	if got != want {
		t.Errorf("floor = %d, want %d", got, want)
	}

	// Empty tool set: floor is just the instruction.
	onlyInstruction, err := agent.EstimateFloorTokens(context.Background(), instruction, nil)
	if err != nil {
		t.Fatalf("EstimateFloorTokens (no tools): %v", err)
	}
	if onlyInstruction != len(instruction)/4 {
		t.Errorf("floor without tools = %d, want %d", onlyInstruction, len(instruction)/4)
	}
}

// TestAssembleAgent_InputFloorExceedsSafetyLimit reproduces the 5000/7400 case
// at a small scale: a tiny context window makes the default tool floor exceed
// the safety limit, so AssembleAgent must fail fast with ErrInputFloorExceedsSafetyLimit.
func TestAssembleAgent_InputFloorExceedsSafetyLimit(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-floor-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	workspace := filepath.Join(tmpDir, "workspace")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	userConfigDir := filepath.Join(tmpDir, "config")
	if err := os.MkdirAll(userConfigDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	fm := &fakeChatModel{}
	agentConf := &store.Agent{Name: "test-floor-agent"}

	ctx := context.Background()
	// contextWindow=100 -> FloorSafetyLimit=50; the default builtin tool floor
	// is far larger, so assembly must be rejected before any model call.
	_, err = agent.AssembleAgent(ctx, agentConf, fm, fm, workspace, userConfigDir, "deny", nil, nil, 100, dummyConvStore{}, 1, nil, nil, nil, "test", nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, 0, nil, 3)
	if err == nil {
		t.Fatal("expected AssembleAgent to fail on input floor, got nil")
	}
	if !errors.Is(err, middlewares.ErrInputFloorExceedsSafetyLimit) {
		t.Errorf("expected ErrInputFloorExceedsSafetyLimit, got %v", err)
	}
}
