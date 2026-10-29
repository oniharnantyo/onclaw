package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/buddies/internal/services"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

// TestPromptGenSmoke is the env-gated headless live smoke for the prompt
// generation free-form retry: it regenerates one real agent's prompts on its
// real provider and requires the row to land ready. Skipped unless
// BUDDIES_PROMPTGEN_SMOKE=1; BUDDIES_PROMPTGEN_SMOKE_AGENT names the agent
// (default Jono):
//
//	BUDDIES_PROMPTGEN_SMOKE=1 go test ./internal/cli -run TestPromptGenSmoke -v -count=1 -timeout 360s
func TestPromptGenSmoke(t *testing.T) {
	if os.Getenv("BUDDIES_PROMPTGEN_SMOKE") == "" {
		t.Skip("set BUDDIES_PROMPTGEN_SMOKE=1 to run the live prompt generation smoke against the real ~/.buddies")
	}
	wantAgent := "Jono"
	if name := os.Getenv("BUDDIES_PROMPTGEN_SMOKE_AGENT"); name != "" {
		wantAgent = name
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	app, err := Assemble(ctx, services.Noop{}, services.NoBrowser{})
	require.NoError(t, err)
	defer func() { require.NoError(t, app.Close()) }()

	agents, err := app.Store.Agents().List(ctx)
	require.NoError(t, err)
	var target *store.Agent
	for i := range agents {
		if agents[i].Name == wantAgent {
			target = &agents[i]
		}
	}
	require.NotNil(t, target, "agent %q not found", wantAgent)
	provider, err := app.Store.Providers().ByID(ctx, target.ProviderID)
	require.NoError(t, err)
	t.Logf("agent: %s (provider %s %s, model %s, status %s)",
		target.Name, provider.Name, provider.Type, target.Model, target.PromptsStatus)

	err = app.Services.Agents.RegeneratePrompts(ctx, target.ID, "")
	require.NoError(t, err, "regeneration failed — the free-form retry did not rescue a structured-output refusal")

	updated, err := app.Store.Agents().ByID(ctx, target.ID)
	require.NoError(t, err)
	require.Equal(t, store.PromptsStatusReady, updated.PromptsStatus)
	require.Empty(t, updated.PromptsError)

	ws := app.Layout.Workspaces
	for _, doc := range []string{"IDENTITY.md", "SOUL.md", "BOOTSTRAP.md"} {
		raw, err := os.ReadFile(filepath.Join(ws, target.Slug, doc))
		require.NoError(t, err, "%s missing after a ready generation", doc)
		require.Greater(t, len(strings.TrimSpace(string(raw))), 0, "%s is empty", doc)
		t.Logf("%s: %d bytes", doc, len(raw))
	}
}
