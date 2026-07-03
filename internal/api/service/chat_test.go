package service_test

import (
	"context"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/api/service"
)

func TestService_Chat_WithResolveError(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// Replace fixture's svc with one that has a resolve func that errors
	svc := service.New(
		f.llmSvc,
		f.kvStore,
		&fakeConversationStore{},
		func(_ context.Context, agentName, _, _, _, _ string, _ int64) (service.AssembledAgent, string, error) {
			return nil, "", service.ErrNotFound
		},
		nil, // installer
		nil, // log
		f.hookStore,
		f.execStore,
		f.mcpStore,
		func() {},
		nil,
		f.toolStore,
		f.cfgStore,
	)

	_, _, err := svc.Chat(ctx, service.ChatInput{
		Prompt: "hello",
	})
	if err == nil {
		t.Error("expected error from Chat when resolve fails")
	}
}

func TestService_Chat_NoAgent_UsesDefault(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// Set a default agent in kv
	f.kvStore.Set(ctx, "default_agent", "my-default")

	resolved := ""
	svc := service.New(
		f.llmSvc,
		f.kvStore,
		&fakeConversationStore{},
		func(_ context.Context, agentName, _, _, _, _ string, convID int64) (service.AssembledAgent, string, error) {
			resolved = agentName
			return nil, "", service.ErrNotFound // abort after resolving
		},
		nil, nil,
		f.hookStore, f.execStore, f.mcpStore,
		func() {}, nil,
		f.toolStore, f.cfgStore,
	)

	svc.Chat(ctx, service.ChatInput{Prompt: "hi", ConversationID: 1})
	if resolved != "my-default" {
		t.Errorf("expected default agent 'my-default', got %q", resolved)
	}
}
