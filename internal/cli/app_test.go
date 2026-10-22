package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/gateway"
	"github.com/oniharnantyo/buddies/internal/scheduler"
	"github.com/oniharnantyo/buddies/internal/services"
	"github.com/oniharnantyo/buddies/internal/store"
)

// recordingEmitter is a hand-written services.Emitter fake recording event
// names.
type recordingEmitter struct {
	mu    sync.Mutex
	names []string
}

func (e *recordingEmitter) Emit(name string, _ ...any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.names = append(e.names, name)
}

func TestAssemble_BuildsFullServiceSet(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	emitter := &recordingEmitter{}

	app, err := Assemble(ctx, emitter, services.NoBrowser{})
	require.NoError(t, err)
	defer func() { require.NoError(t, app.Close()) }()

	// The data layout exists on disk.
	require.DirExists(t, app.Layout.Root)
	require.DirExists(t, app.Layout.Skills)
	require.DirExists(t, app.Layout.Logs)
	require.FileExists(t, app.Layout.DBPath)

	// AppInfo: static metadata.
	meta, err := app.Services.AppInfo.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, services.AppName, meta.Name)

	// Agents: a row seeded through the store lists through the bound
	// service. Service-level create generates prompts synchronously against
	// the agent's real provider, so it is exercised in the services tests,
	// not here.
	seeded := store.Agent{Name: "Scout", Slug: "scout", Brief: "Find things quickly."}
	require.NoError(t, app.Store.Agents().Create(ctx, &seeded))
	agents, err := app.Services.Agents.List(ctx)
	require.NoError(t, err)
	require.Len(t, agents, 1)
	require.Equal(t, seeded.ID, agents[0].ID)

	// Chat: session lifecycle and transcript over the created agent.
	session, err := app.Services.Chat.CreateSession(ctx, seeded.ID)
	require.NoError(t, err)
	sessions, err := app.Services.Chat.Sessions(ctx, seeded.ID)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	transcript, err := app.Services.Chat.Transcript(ctx, session.ID)
	require.NoError(t, err)
	require.Empty(t, transcript)

	// Settings: every registry answers.
	provider, err := app.Services.Settings.CreateProvider(ctx, services.ProviderInput{Name: "anthropic", Type: "anthropic"})
	require.NoError(t, err)
	require.NotEmpty(t, provider.ID)
	skills, err := app.Services.Settings.Skills(ctx)
	require.NoError(t, err)
	require.Empty(t, skills)
	hooks, err := app.Services.Settings.Hooks(ctx)
	require.NoError(t, err)
	require.Empty(t, hooks)
	mcpServers, err := app.Services.Settings.MCPServers(ctx)
	require.NoError(t, err)
	require.Empty(t, mcpServers)

	// MCP: a definition round trip exercises the invalidate-on-write wiring
	// into the runtime's connection cache (design D7); Close below reaps the
	// manager itself.
	mcpServer, err := app.Services.Settings.CreateMCPServer(ctx, store.MCPServer{
		Name:      "fs",
		Transport: "stdio",
		Command:   "/bin/true",
		Enabled:   true,
	})
	require.NoError(t, err)
	mcpServer.Enabled = false
	_, err = app.Services.Settings.UpdateMCPServer(ctx, mcpServer)
	require.NoError(t, err)
	require.NoError(t, app.Services.Settings.DeleteMCPServer(ctx, mcpServer.ID))

	settings, err := app.Services.Settings.Gateway(ctx)
	require.NoError(t, err)
	require.True(t, settings.Enabled)

	// Gateway: lifecycle transitions through the service.
	state, err := app.Services.Gateway.State(ctx)
	require.NoError(t, err)
	require.Equal(t, gateway.StateStopped.String(), state)
	require.NoError(t, app.Services.Gateway.Start(ctx))
	require.NoError(t, app.Services.Gateway.Pause(ctx))
	require.NoError(t, app.Services.Gateway.Resume(ctx))
	state, err = app.Services.Gateway.State(ctx)
	require.NoError(t, err)
	require.Equal(t, gateway.StateRunning.String(), state)

	// Schedule: a schedule round-trips through the bound service over the
	// same store. The ticker is attached (it started on the Running
	// transition above) and Close below stops it before the gateway.
	require.NotNil(t, app.Services.Schedule)
	schedule, err := app.Services.Schedule.Create(ctx, scheduler.ScheduleInput{
		Name:    "Digest",
		AgentID: seeded.ID,
		Prompt:  "say hi",
		Cron:    "30 9 * * *",
		Enabled: true,
	})
	require.NoError(t, err)
	schedules, err := app.Services.Schedule.List(ctx)
	require.NoError(t, err)
	require.Len(t, schedules, 1)
	require.Equal(t, schedule.ID, schedules[0].ID)
	require.NotNil(t, schedules[0].NextFireAt)
}

func TestAssemble_Close_StopsGatewayAndIsIdempotent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	app, err := Assemble(ctx, &recordingEmitter{}, services.NoBrowser{})
	require.NoError(t, err)

	require.NoError(t, app.Services.Gateway.Start(ctx))
	require.Equal(t, gateway.StateRunning, app.Gateway.State())

	require.NoError(t, app.Close())
	require.Equal(t, gateway.StateStopped, app.Gateway.State())
	require.NoError(t, app.Close()) // idempotent
}

// TestAssemble_FailsStartupOnBrokenDatabase pins the migration gate: a
// corrupted database file must abort assembly instead of yielding services
// over a stale schema.
func TestAssemble_FailsStartupOnBrokenDatabase(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	require.NoError(t, os.MkdirAll(filepath.Join(home, ".buddies"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".buddies", "buddies.db"), []byte("not a database"), 0o600))

	_, err := Assemble(context.Background(), &recordingEmitter{}, services.NoBrowser{})
	require.Error(t, err)
}

func guiShouldNotRun(_ context.Context, _ *App) error {
	return errors.New("the GUI runner must not be invoked by this command")
}

func TestRootCommand_Default_RunsGUIWithAssembledApp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var ran *App
	cmd := NewRootCommand(&recordingEmitter{}, services.NoBrowser{}, func(_ context.Context, app *App) error {
		ran = app
		return nil
	})

	require.NoError(t, cmd.Run(context.Background(), []string{services.AppName}))
	require.NotNil(t, ran)
	require.NotNil(t, ran.Services.AppInfo)
	require.NotNil(t, ran.Services.Agents)
	require.NotNil(t, ran.Services.Chat)
	require.NotNil(t, ran.Services.Settings)
	require.NotNil(t, ran.Services.Gateway)
	require.NotNil(t, ran.Services.Schedule)
	require.NoError(t, ran.Close()) // already closed by the action; idempotent
}

func TestRootCommand_Version_PrintsNameAndVersion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cmd := NewRootCommand(&recordingEmitter{}, services.NoBrowser{}, guiShouldNotRun)
	buf := &bytes.Buffer{}
	cmd.Writer = buf

	require.NoError(t, cmd.Run(context.Background(), []string{services.AppName, "version"}))
	require.Contains(t, buf.String(), services.AppName)
	require.Contains(t, buf.String(), services.AppVersion)
}
