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

func TestValidateSkillName(t *testing.T) {
	valid := []string{"incident-runbook", "deploy-helper", "research"}
	for _, name := range valid {
		if err := domain.ValidateSkillName(name); err != nil {
			t.Errorf("ValidateSkillName(%q) expected nil error, got %v", name, err)
		}
	}

	invalid := []string{"", "   ", "\t\n"}
	for _, name := range invalid {
		err := domain.ValidateSkillName(name)
		if err == nil {
			t.Errorf("ValidateSkillName(%q) expected error, got nil", name)
		}
		if !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("ValidateSkillName(%q) error = %v, want %v", name, err, domain.ErrInvalid)
		}
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
