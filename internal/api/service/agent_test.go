package service_test

import (
	"context"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/api/service"
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

func TestService_UpdateAgent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.svc.CreateAgent(ctx, service.AgentInput{Name: "updatable", Provider: "openai", Model: "gpt-3"})
	_, err := f.svc.UpdateAgent(ctx, "updatable", service.AgentInput{
		Provider: "anthropic",
		Model:    "claude-3",
	})
	if err != nil {
		t.Fatalf("UpdateAgent: %v", err)
	}

	v, _ := f.svc.GetAgent(ctx, "updatable")
	if v.Provider != "anthropic" {
		t.Errorf("expected Provider=anthropic, got %q", v.Provider)
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
