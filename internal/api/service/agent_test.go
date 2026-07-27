package service_test

import (
	"context"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/api/service"
	"github.com/oniharnantyo/onclaw/internal/store"
)

func TestService_CreateAgent_IsDefault(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	_, err := f.svc.CreateAgent(ctx, service.AgentInput{
		Name:      "default-agent",
		Provider:  "openai",
		IsDefault: true,
	})
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}

	list, err := f.svc.ListAgents(ctx)
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	for _, a := range list {
		if a.Name == "default-agent" && !a.IsDefault {
			t.Error("expected IsDefault=true for the default agent")
		}
	}
}

func TestService_UpdateAgent_IsDefault(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.svc.CreateAgent(ctx, service.AgentInput{Name: "upd-default", Provider: "openai"})
	_, err := f.svc.UpdateAgent(ctx, "upd-default", service.AgentInput{
		Provider:  "anthropic",
		IsDefault: true,
	})
	if err != nil {
		t.Fatalf("UpdateAgent: %v", err)
	}

	list, _ := f.svc.ListAgents(ctx)
	for _, a := range list {
		if a.Name == "upd-default" && !a.IsDefault {
			t.Error("expected IsDefault=true after update")
		}
	}
}

func TestService_DeleteAgent_WasDefault(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.svc.CreateAgent(ctx, service.AgentInput{Name: "was-default", Provider: "openai", IsDefault: true})
	if err := f.svc.DeleteAgent(ctx, "was-default"); err != nil {
		t.Fatalf("DeleteAgent: %v", err)
	}

	// kv entry for default_agent should be cleared
	_, err := f.kvStore.Get(ctx, "default_agent")
	if err == nil {
		t.Error("expected default_agent kv entry to be cleared after delete")
	}
}

func TestService_CreateAgent_And_ListAgents(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	_, err := f.svc.CreateAgent(ctx, service.AgentInput{
		Name:     "agent-one",
		Provider: "openai",
		Model:    "gpt-4",
	})
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}

	list, err := f.svc.ListAgents(ctx)
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(list))
	}
	if list[0].Name != "agent-one" {
		t.Errorf("unexpected agent name: %q", list[0].Name)
	}
}

func TestService_GetAgent_NotFound(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.GetAgent(context.Background(), "ghost")
	if err == nil {
		t.Error("expected error for missing agent")
	}
}

func TestService_SetAgentTools_Denylist(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// Seed the tool registry with builtin tools.
	registry := []string{"read_file", "write_file", "ls", "execute"}
	for _, name := range registry {
		f.toolStore.UpsertTool(ctx, &store.ToolRegistry{Name: name, Enabled: 1})
	}

	// Agent created carries an empty denylist = all tools enabled.
	f.svc.CreateAgent(ctx, service.AgentInput{Name: "test-agent", Provider: "openai"})
	if got := mustAgentDisabledTools(t, f, "test-agent"); got != "" {
		t.Fatalf("expected empty denylist initially, got %q", got)
	}

	// Disabling one tool adds it to disabled_tools.
	if err := f.svc.SetAgentTools(ctx, "test-agent", "read_file", false); err != nil {
		t.Fatalf("SetAgentTools: %v", err)
	}
	got := splitSet(mustAgentDisabledTools(t, f, "test-agent"))
	want := splitSet("read_file")
	if !equalSet(got, want) {
		t.Errorf("after disabling read_file, expected %v, got %v", want, got)
	}

	// Disabling a second tool adds it to disabled_tools as well.
	if err := f.svc.SetAgentTools(ctx, "test-agent", "execute", false); err != nil {
		t.Fatalf("SetAgentTools: %v", err)
	}
	got = splitSet(mustAgentDisabledTools(t, f, "test-agent"))
	want = splitSet("read_file,execute")
	if !equalSet(got, want) {
		t.Errorf("after disabling execute, expected %v, got %v", want, got)
	}

	// Enabling a tool removes it from disabled_tools.
	if err := f.svc.SetAgentTools(ctx, "test-agent", "read_file", true); err != nil {
		t.Fatalf("SetAgentTools: %v", err)
	}
	got = splitSet(mustAgentDisabledTools(t, f, "test-agent"))
	want = splitSet("execute")
	if !equalSet(got, want) {
		t.Errorf("after enabling read_file, expected %v, got %v", want, got)
	}
}

func mustAgentDisabledTools(t *testing.T, f *fixture, name string) string {
	t.Helper()
	a, err := f.svc.GetAgent(context.Background(), name)
	if err != nil {
		t.Fatalf("GetAgent %q: %v", name, err)
	}
	return a.DisabledTools
}

func splitSet(s string) map[string]bool {
	out := make(map[string]bool)
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out[p] = true
		}
	}
	return out
}

func equalSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func TestService_UpdateAgent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.svc.CreateAgent(ctx, service.AgentInput{Name: "updatable", Provider: "openai", Model: "gpt-3", MaxContextTokens: 2000})
	_, err := f.svc.UpdateAgent(ctx, "updatable", service.AgentInput{
		Provider:         "anthropic",
		Model:            "claude-3",
		MaxContextTokens: 4000,
	})
	if err != nil {
		t.Fatalf("UpdateAgent: %v", err)
	}

	v, _ := f.svc.GetAgent(ctx, "updatable")
	if v.Provider != "anthropic" {
		t.Errorf("expected Provider=anthropic, got %q", v.Provider)
	}
	if v.MaxContextTokens != 4000 {
		t.Errorf("expected MaxContextTokens=4000, got %d", v.MaxContextTokens)
	}
}

func TestService_DeleteAgent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.svc.CreateAgent(ctx, service.AgentInput{Name: "del-agent", Provider: "openai"})
	if err := f.svc.DeleteAgent(ctx, "del-agent"); err != nil {
		t.Fatalf("DeleteAgent: %v", err)
	}

	_, err := f.svc.GetAgent(ctx, "del-agent")
	if err == nil {
		t.Error("expected error after deletion")
	}
}
