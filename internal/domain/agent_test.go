package domain_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"path/filepath"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

func TestValidateAgentAutonomy(t *testing.T) {
	valid := []domain.AgentAutonomy{
		domain.AutonomyApproval,
		domain.AutonomySuggest,
		domain.AutonomyFull,
	}

	for _, a := range valid {
		if err := domain.ValidateAgentAutonomy(a); err != nil {
			t.Errorf("ValidateAgentAutonomy(%q) expected nil error, got %v", a, err)
		}
	}

	invalid := []domain.AgentAutonomy{
		"",
		"none",
		"manual",
		"auto",
		"APPROVAL",
	}

	for _, a := range invalid {
		err := domain.ValidateAgentAutonomy(a)
		if err == nil {
			t.Errorf("ValidateAgentAutonomy(%q) expected error, got nil", a)
		}
		if !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("ValidateAgentAutonomy(%q) error = %v, want %v", a, err, domain.ErrInvalid)
		}
	}
}

func TestValidateAgentTemperature(t *testing.T) {
	valid := []float64{0.0, 0.5, 1.0, 1.5, 2.0, 0.7}
	for _, temp := range valid {
		if err := domain.ValidateAgentTemperature(temp); err != nil {
			t.Errorf("ValidateAgentTemperature(%v) expected nil error, got %v", temp, err)
		}
	}

	invalid := []float64{-0.1, -1.0, 2.01, 2.1, 10.0}
	for _, temp := range invalid {
		err := domain.ValidateAgentTemperature(temp)
		if err == nil {
			t.Errorf("ValidateAgentTemperature(%v) expected error, got nil", temp)
		}
		if !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("ValidateAgentTemperature(%v) error = %v, want %v", temp, err, domain.ErrInvalid)
		}
	}
}

func TestValidateAgentAvatar(t *testing.T) {
	tests := []struct {
		name    string
		avatar  json.RawMessage
		wantErr bool
	}{
		{
			name:    "nil avatar",
			avatar:  nil,
			wantErr: false,
		},
		{
			name:    "empty avatar",
			avatar:  json.RawMessage(""),
			wantErr: false,
		},
		{
			name:    "empty json object",
			avatar:  json.RawMessage("{}"),
			wantErr: false,
		},
		{
			name:    "valid props object",
			avatar:  json.RawMessage(`{"sex":"man","faceColor":"#F9C9B6","earSize":"small","eyeStyle":"circle"}`),
			wantErr: false,
		},
		{
			name:    "string is rejected",
			avatar:  json.RawMessage(`"https://example.com/avatar.png"`),
			wantErr: true,
		},
		{
			name:    "array is rejected",
			avatar:  json.RawMessage(`["faceColor", "#F9C9B6"]`),
			wantErr: true,
		},
		{
			name:    "number is rejected",
			avatar:  json.RawMessage(`12345`),
			wantErr: true,
		},
		{
			name:    "boolean is rejected",
			avatar:  json.RawMessage(`true`),
			wantErr: true,
		},
		{
			name:    "invalid json is rejected",
			avatar:  json.RawMessage(`{not a valid json`),
			wantErr: true,
		},
		{
			name:    "avatar exceeding 2KB is rejected",
			avatar:  json.RawMessage(`{"data":"` + strings.Repeat("x", 2048) + `"}`),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := domain.ValidateAgentAvatar(tt.avatar)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateAgentAvatar() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(err, domain.ErrInvalid) {
				t.Errorf("ValidateAgentAvatar() error = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestValidateAgentSlug(t *testing.T) {
	valid := []string{"atlas", "support-bot", "agent-1", "v2-researcher"}
	for _, slug := range valid {
		if err := domain.ValidateAgentSlug(slug); err != nil {
			t.Errorf("ValidateAgentSlug(%q) expected nil error, got %v", slug, err)
		}
	}

	invalid := []string{
		"",
		"Atlas",
		"-agent",
		"agent-",
		"agent@bot",
		"master",
		"api",
		"auth",
		"new",
		"settings",
		"workspaces",
		"login",
		"logout",
		strings.Repeat("a", 64),
	}

	for _, slug := range invalid {
		err := domain.ValidateAgentSlug(slug)
		if err == nil {
			t.Errorf("ValidateAgentSlug(%q) expected error, got nil", slug)
		}
		if !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("ValidateAgentSlug(%q) error = %v, want %v", slug, err, domain.ErrInvalid)
		}
	}
}

func TestValidateAgentContextWindow(t *testing.T) {
	valid := []*int{nil}
	for _, val := range []int{1, 100, 200000, 1000000} {
		v := val
		valid = append(valid, &v)
	}
	for _, cw := range valid {
		if err := domain.ValidateAgentContextWindow(cw); err != nil {
			t.Errorf("ValidateAgentContextWindow(%v) expected nil error, got %v", cw, err)
		}
	}

	invalid := []int{0, -1, -100, -200000}
	for _, val := range invalid {
		v := val
		err := domain.ValidateAgentContextWindow(&v)
		if err == nil {
			t.Errorf("ValidateAgentContextWindow(%d) expected error, got nil", v)
		}
		if !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("ValidateAgentContextWindow(%d) error = %v, want %v", v, err, domain.ErrInvalid)
		}
	}
}

func TestResolveContextWindow(t *testing.T) {
	val50k := 50000
	val128k := 128000
	zero := 0
	neg := -100

	tests := []struct {
		name      string
		agentCW   *int
		catalogCW *int
		want      int
	}{
		{
			name:      "agent override wins over catalog limit",
			agentCW:   &val50k,
			catalogCW: &val128k,
			want:      50000,
		},
		{
			name:      "agent value used when catalog is nil",
			agentCW:   &val50k,
			catalogCW: nil,
			want:      50000,
		},
		{
			name:      "catalog limit used when agent is nil",
			agentCW:   nil,
			catalogCW: &val128k,
			want:      128000,
		},
		{
			name:      "default fallback 200000 when both are nil",
			agentCW:   nil,
			catalogCW: nil,
			want:      200000,
		},
		{
			name:      "default fallback when agent value is non-positive and catalog is nil",
			agentCW:   &zero,
			catalogCW: nil,
			want:      200000,
		},
		{
			name:      "catalog limit used when agent value is negative",
			agentCW:   &neg,
			catalogCW: &val128k,
			want:      128000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := domain.ResolveContextWindow(tt.agentCW, tt.catalogCW)
			if got != tt.want {
				t.Errorf("ResolveContextWindow(%v, %v) = %d, want %d", tt.agentCW, tt.catalogCW, got, tt.want)
			}
		})
	}
}

func TestPathHelpers(t *testing.T) {
	base := "/var/lib/onclaw"

	if got, want := domain.WorkspaceRoot(base), filepath.Join(base, "workspaces"); got != want {
		t.Errorf("WorkspaceRoot() = %q, want %q", got, want)
	}

	if got, want := domain.SystemSkillsDir(base), filepath.Join(base, "skills"); got != want {
		t.Errorf("SystemSkillsDir() = %q, want %q", got, want)
	}

	if got, want := domain.WorkspaceSkillsDir(base, "acme-corp"), filepath.Join(base, "workspaces", "acme-corp", "skills"); got != want {
		t.Errorf("WorkspaceSkillsDir() = %q, want %q", got, want)
	}

	if got, want := domain.AgentSkillsDir(base, "acme-corp", "radar"), filepath.Join(base, "workspaces", "acme-corp", "agents", "radar", "skills"); got != want {
		t.Errorf("AgentSkillsDir() = %q, want %q", got, want)
	}

	wsRoot := domain.WorkspaceRoot(base)
	if got, want := domain.AgentWorkspaceDir(wsRoot, "acme-corp", "radar"), filepath.Join(base, "workspaces", "acme-corp", "agents", "radar"); got != want {
		t.Errorf("AgentWorkspaceDir() = %q, want %q", got, want)
	}
}

func TestDefaultOnClawDir(t *testing.T) {
	got := domain.DefaultOnClawDir()
	if got == "" {
		t.Error("DefaultOnClawDir() returned empty string")
	}
	if !strings.HasSuffix(got, ".onclaw") {
		t.Errorf("DefaultOnClawDir() = %q, want path ending in .onclaw", got)
	}
}

func TestAgentWorkspaceDir(t *testing.T) {
	root := "/var/lib/onclaw/.onclaw/workspaces"
	got := domain.AgentWorkspaceDir(root, "acme-corp", "radar")
	want := filepath.Join(root, "acme-corp", "agents", "radar")
	if got != want {
		t.Errorf("AgentWorkspaceDir() = %q, want %q", got, want)
	}
}
