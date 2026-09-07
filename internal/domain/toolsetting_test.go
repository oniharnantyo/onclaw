package domain_test

import (
	"errors"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

func TestWorkspaceToolSetting_Validate(t *testing.T) {
	tests := []struct {
		name        string
		setting     *domain.WorkspaceToolSetting
		expectError bool
	}{
		{
			name: "valid setting with empty config",
			setting: &domain.WorkspaceToolSetting{
				WorkspaceID: "ws-1",
				ToolKey:     "browser",
				Enabled:     true,
				Config:      map[string]any{},
			},
			expectError: false,
		},
		{
			name: "valid setting with positive numeric config",
			setting: &domain.WorkspaceToolSetting{
				WorkspaceID: "ws-1",
				ToolKey:     "browser",
				Enabled:     false,
				Config: map[string]any{
					"max_pages":              float64(5),
					"idle_timeout_seconds":   30,
					"action_timeout_seconds": int64(120),
				},
			},
			expectError: false,
		},
		{
			name:        "nil setting",
			setting:     nil,
			expectError: true,
		},
		{
			name: "missing workspace id",
			setting: &domain.WorkspaceToolSetting{
				ToolKey: "browser",
			},
			expectError: true,
		},
		{
			name: "missing tool key",
			setting: &domain.WorkspaceToolSetting{
				WorkspaceID: "ws-1",
			},
			expectError: true,
		},
		{
			name: "negative max_pages",
			setting: &domain.WorkspaceToolSetting{
				WorkspaceID: "ws-1",
				ToolKey:     "browser",
				Config:      map[string]any{"max_pages": float64(-1)},
			},
			expectError: true,
		},
		{
			name: "zero idle_timeout_seconds",
			setting: &domain.WorkspaceToolSetting{
				WorkspaceID: "ws-1",
				ToolKey:     "browser",
				Config:      map[string]any{"idle_timeout_seconds": 0},
			},
			expectError: true,
		},
		{
			name: "non-numeric action_timeout_seconds",
			setting: &domain.WorkspaceToolSetting{
				WorkspaceID: "ws-1",
				ToolKey:     "browser",
				Config:      map[string]any{"action_timeout_seconds": "30"},
			},
			expectError: true,
		},
		{
			name: "unrelated non-numeric field passes structural validation",
			setting: &domain.WorkspaceToolSetting{
				WorkspaceID: "ws-1",
				ToolKey:     "browser",
				Config:      map[string]any{"headless": true, "remote_cdp_url": "ws://localhost:9222"},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.setting.Validate()
			if tt.expectError && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.expectError && !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("expected ErrInvalid, got %v", err)
			}
		})
	}
}

func TestValidateToolConfigValues(t *testing.T) {
	// Shared structural keys are validated regardless of tool key.
	err := domain.ValidateToolConfigValues(map[string]any{"max_pages": float64(3)})
	if err != nil {
		t.Fatalf("unexpected error for positive max_pages: %v", err)
	}

	err = domain.ValidateToolConfigValues(map[string]any{"max_pages": float64(0)})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for zero max_pages, got %v", err)
	}

	// Unknown keys are left to the per-tool schema (later tasks).
	err = domain.ValidateToolConfigValues(map[string]any{"anything": "goes"})
	if err != nil {
		t.Fatalf("unexpected error for unknown key: %v", err)
	}

	// Nil/empty config is structurally valid.
	if err := domain.ValidateToolConfigValues(nil); err != nil {
		t.Fatalf("unexpected error for nil config: %v", err)
	}
}
