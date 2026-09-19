package memory

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

func TestGateBudgetFromConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		config map[string]any
		want   time.Duration
		ok     bool
	}{
		{"absent", map[string]any{}, 0, false},
		{"nil config", nil, 0, false},
		{"stored json number", map[string]any{"gate_budget_ms": float64(6000)}, 6000 * time.Millisecond, true},
		{"in-process int", map[string]any{"gate_budget_ms": 6000}, 6000 * time.Millisecond, true},
		{"lower bound", map[string]any{"gate_budget_ms": float64(500)}, 500 * time.Millisecond, true},
		{"upper bound", map[string]any{"gate_budget_ms": float64(20000)}, 20000 * time.Millisecond, true},
		{"below bounds", map[string]any{"gate_budget_ms": float64(499)}, 0, false},
		{"above bounds", map[string]any{"gate_budget_ms": float64(20001)}, 0, false},
		{"non-integer", map[string]any{"gate_budget_ms": 1500.5}, 0, false},
		{"wrong type", map[string]any{"gate_budget_ms": "4000"}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := GateBudgetFromConfig(tt.config)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("GateBudgetFromConfig(%v) = (%v, %v), want (%v, %v)", tt.config, got, ok, tt.want, tt.ok)
			}
		})
	}
}

type stubSettingsStore struct {
	store.ToolSettingsStore
	row *domain.WorkspaceToolSetting
	err error
}

func (s stubSettingsStore) Get(ctx context.Context, workspaceID, toolKey string) (*domain.WorkspaceToolSetting, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.row, nil
}

func TestSettingsGateBudget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	resolver := SettingsGateBudget(stubSettingsStore{}, log)
	if got := resolver(ctx, "ws1"); got != time.Duration(DefaultGateBudgetMS)*time.Millisecond {
		t.Fatalf("absent row: got %v, want default %v", got, DefaultGateBudgetMS)
	}

	resolver = SettingsGateBudget(stubSettingsStore{row: &domain.WorkspaceToolSetting{
		Config: map[string]any{"gate_budget_ms": float64(8000)},
	}}, log)
	if got := resolver(ctx, "ws1"); got != 8000*time.Millisecond {
		t.Fatalf("stored budget: got %v, want 8s", got)
	}

	resolver = SettingsGateBudget(stubSettingsStore{row: &domain.WorkspaceToolSetting{
		Config: map[string]any{"gate_budget_ms": float64(10)},
	}}, log)
	if got := resolver(ctx, "ws1"); got != time.Duration(DefaultGateBudgetMS)*time.Millisecond {
		t.Fatalf("out-of-contract stored budget: got %v, want default", got)
	}

	resolver = SettingsGateBudget(stubSettingsStore{err: errors.New("db down")}, log)
	if got := resolver(ctx, "ws1"); got != time.Duration(DefaultGateBudgetMS)*time.Millisecond {
		t.Fatalf("store error: got %v, want default", got)
	}
}
