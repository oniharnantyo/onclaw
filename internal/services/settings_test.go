package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
)

func TestSettingsService_ProviderCRUD_Success(t *testing.T) {
	svc := NewSettingsService(fake.New())
	ctx := context.Background()

	created, err := svc.CreateProvider(ctx, ProviderInput{Name: "anthropic", Type: "anthropic", APIKey: "k", Enabled: true})
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)

	got, err := svc.Provider(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "anthropic", got.Type)
	require.True(t, got.KeySet)
	require.Equal(t, "k", got.KeyHint)

	list, err := svc.Providers(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)

	updated, err := svc.UpdateProvider(ctx, created.ID, ProviderInput{Type: "anthropic", Name: "anthropic", Enabled: true, APIKey: "rotated"})
	require.NoError(t, err)
	require.True(t, updated.KeySet)
	require.Equal(t, "ated", updated.KeyHint)

	require.NoError(t, svc.DeleteProvider(ctx, created.ID))
	_, err = svc.Provider(ctx, created.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestSettingsService_ProviderCRUD_NotFound(t *testing.T) {
	svc := NewSettingsService(fake.New())
	ctx := context.Background()

	_, err := svc.Provider(ctx, "missing")
	require.ErrorIs(t, err, store.ErrNotFound)

	_, err = svc.UpdateProvider(ctx, "missing", ProviderInput{})
	require.ErrorIs(t, err, store.ErrNotFound)

	err = svc.DeleteProvider(ctx, "missing")
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestSettingsService_ProviderModels_ListsCatalog(t *testing.T) {
	svc := NewSettingsService(fake.New())
	ctx := context.Background()

	created, err := svc.CreateProvider(ctx, ProviderInput{Name: "a", Type: "anthropic"})
	require.NoError(t, err)

	models, err := svc.ProviderModels(ctx, created.ID)
	require.NoError(t, err)
	// The seeded catalog row declares no efforts, so it offers only high
	// (design D3: the stored row is authoritative, no catalog fallback).
	require.Contains(t, models, ModelCapability{ID: "claude-sonnet-4-5", ContextWindow: 200000, Efforts: []string{"low", "medium", "high"}, ImageInput: true, PDFInput: true})
}

func TestSettingsService_Skills_ListAndToggle(t *testing.T) {
	st := fake.New()
	skill := &store.Skill{Name: "summarize", Description: "Summarize text"}
	require.NoError(t, st.Skills().Upsert(context.Background(), skill))

	svc := NewSettingsService(st)
	ctx := context.Background()

	skills, err := svc.Skills(ctx)
	require.NoError(t, err)
	require.Len(t, skills, 1)
	require.Equal(t, "summarize", skills[0].Name)

	require.NoError(t, svc.SetSkillEnabled(ctx, "summarize", false))

	skills, err = svc.Skills(ctx)
	require.NoError(t, err)
	require.False(t, skills[0].Enabled)
}

func TestSettingsService_SetSkillEnabled_NotFound(t *testing.T) {
	svc := NewSettingsService(fake.New())

	err := svc.SetSkillEnabled(context.Background(), "missing", true)
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestSettingsService_HookCRUD_Success(t *testing.T) {
	svc := NewSettingsService(fake.New())
	ctx := context.Background()

	created, err := svc.CreateHook(ctx, store.Hook{
		Name:        "notify",
		Event:       "run_finished",
		HandlerType: "command",
		Config:      `{"command":"true"}`,
		Enabled:     true,
	})
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)

	hooks, err := svc.Hooks(ctx)
	require.NoError(t, err)
	require.Len(t, hooks, 1)

	created.Name = "notify-v2"
	updated, err := svc.UpdateHook(ctx, created)
	require.NoError(t, err)
	require.Equal(t, "notify-v2", updated.Name)

	require.NoError(t, svc.DeleteHook(ctx, created.ID))

	hooks, err = svc.Hooks(ctx)
	require.NoError(t, err)
	require.Empty(t, hooks)
}

func TestSettingsService_HookCRUD_NotFound(t *testing.T) {
	svc := NewSettingsService(fake.New())

	_, err := svc.UpdateHook(context.Background(), store.Hook{
		ID:          "missing",
		Name:        "notify",
		Event:       "run_finished",
		HandlerType: "command",
		Config:      `{"command":"true"}`,
	})
	require.ErrorIs(t, err, store.ErrNotFound)

	err = svc.DeleteHook(context.Background(), "missing")
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestSettingsService_MCPServerCRUD_Success(t *testing.T) {
	svc := NewSettingsService(fake.New())
	ctx := context.Background()

	created, err := svc.CreateMCPServer(ctx, store.MCPServer{Name: "fs", Transport: "stdio", Command: "mcp-fs", Enabled: true})
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)

	got, err := svc.MCPServer(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "fs", got.Name)

	servers, err := svc.MCPServers(ctx)
	require.NoError(t, err)
	require.Len(t, servers, 1)

	created.Command = "mcp-fs-2"
	updated, err := svc.UpdateMCPServer(ctx, created)
	require.NoError(t, err)
	require.Equal(t, "mcp-fs-2", updated.Command)

	require.NoError(t, svc.DeleteMCPServer(ctx, created.ID))
	_, err = svc.MCPServer(ctx, created.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestSettingsService_MCPServerCRUD_NotFound(t *testing.T) {
	svc := NewSettingsService(fake.New())
	ctx := context.Background()

	_, err := svc.MCPServer(ctx, "missing")
	require.ErrorIs(t, err, store.ErrNotFound)

	_, err = svc.UpdateMCPServer(ctx, store.MCPServer{ID: "missing"})
	require.ErrorIs(t, err, store.ErrNotFound)

	err = svc.DeleteMCPServer(ctx, "missing")
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestSettingsService_GatewaySettings_DefaultsWhenUnsaved(t *testing.T) {
	svc := NewSettingsService(fake.New())

	settings, err := svc.Gateway(context.Background())
	require.NoError(t, err)
	require.Equal(t, DefaultGatewaySettings(), settings)
	require.True(t, settings.Enabled)
}

func TestSettingsService_GatewaySettings_SaveAndReload(t *testing.T) {
	svc := NewSettingsService(fake.New())
	ctx := context.Background()

	require.NoError(t, svc.SaveGateway(ctx, store.GatewaySettings{Enabled: false, DefaultAgentID: "agent1"}))

	settings, err := svc.Gateway(ctx)
	require.NoError(t, err)
	require.False(t, settings.Enabled)
	require.Equal(t, "agent1", settings.DefaultAgentID)
}
