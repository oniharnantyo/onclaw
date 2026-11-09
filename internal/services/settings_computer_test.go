//go:build !server

package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents/mcp"
	"github.com/oniharnantyo/buddies/internal/computeruse"
	"github.com/oniharnantyo/buddies/internal/store/fake"
)

// computerCalls records how many times each lifecycle hook fired in a test.
type computerCalls struct {
	ensure, restart, stop, prompt int
}

// newComputerSettings builds a settings service over a fake store with the
// given hooks wired, mirroring what the composition root does on desktop.
func newComputerSettings(t *testing.T, hooks ComputerHooks) *SettingsService {
	t.Helper()
	return NewSettingsService(fake.New(), WithComputerHooks(hooks))
}

// newComputerSettingsWithStore is newComputerSettings for the tests that
// assert on the persisted document.
func newComputerSettingsWithStore(t *testing.T, hooks ComputerHooks) (*SettingsService, *fake.Store) {
	t.Helper()
	fs := fake.New()
	return NewSettingsService(fs, WithComputerHooks(hooks)), fs
}

// computerStubHooks builds a complete hook set: a fixed probe answer, a
// connected supervision state, and per-hook call counters when a recorder is
// passed (nil skips recording).
func computerStubHooks(perms []ComputerPermission, driver ComputerDriverState, sup mcp.ServerState, calls *computerCalls) ComputerHooks {
	return ComputerHooks{
		PinStatus: func() ComputerDriverState { return driver },
		ProbePermissions: func() ([]ComputerPermission, error) {
			// A fresh copy per call: the baseline snapshots must not alias
			// the stub's slice.
			return append([]ComputerPermission(nil), perms...), nil
		},
		PromptPermissions: func() error {
			if calls != nil {
				calls.prompt++
			}
			return nil
		},
		EnsureDriver: func(_ context.Context) error {
			if calls != nil {
				calls.ensure++
			}
			return nil
		},
		RestartDriver: func(_ context.Context) error {
			if calls != nil {
				calls.restart++
			}
			return nil
		},
		StopDriver: func(_ context.Context) error {
			if calls != nil {
				calls.stop++
			}
			return nil
		},
		SupervisionState: func() mcp.ServerState { return sup },
	}
}

// computerStubPerms builds the darwin-shaped permission list with every
// permission set to granted.
func computerStubPerms(granted bool) []ComputerPermission {
	return []ComputerPermission{
		{
			Name:      "Accessibility",
			Granted:   granted,
			ManageURL: "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility",
		},
		{
			Name:      "Screen Recording",
			Granted:   granted,
			ManageURL: "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture",
		},
	}
}

// boolPtr is the table's DaemonRunning fixture (nil keeps the hook unwired).
func boolPtr(v bool) *bool { return &v }

// TestSettingsComputer_ConfigDefaultsWhenUnset proves a fresh store yields
// the default — disabled, the opt-in posture (add-computer-use design D2).
func TestSettingsComputer_ConfigDefaultsWhenUnset(t *testing.T) {
	svc := NewSettingsService(fake.New(), WithComputerHooks(computerStubHooks(nil, ComputerDriverState{State: ComputerDriverReady}, mcp.ServerState{}, nil)))

	cfg, err := svc.ComputerConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, ComputerConfigView{Enabled: computeruse.DefaultConfig().Enabled}, cfg)
}

// TestSettingsComputer_ConfigRoundTrip saves both toggle values and reads
// them back, proving the persisted JSON is the exact document
// computeruse.DecodeStoredConfig decodes at every spawn boundary — and that
// an explicit disabled is STORED, not inherited from the absent-document
// default (the browseruse pointer-field semantics, tasks 2.1).
func TestSettingsComputer_ConfigRoundTrip(t *testing.T) {
	ctx := context.Background()
	svc, fs := newComputerSettingsWithStore(t, computerStubHooks(nil, ComputerDriverState{State: ComputerDriverReady}, mcp.ServerState{}, nil))

	// Explicit enabled: stored, decodable by the runtime's single decode
	// site, and read back by the pane.
	require.NoError(t, svc.SaveComputerConfig(ctx, ComputerConfigView{Enabled: true}))
	raw, ok, err := fs.ToolSettings().Get(ctx, computeruse.ComputerSettingsKey)
	require.NoError(t, err)
	require.True(t, ok)
	var stored struct {
		Enabled *bool `json:"enabled"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &stored))
	require.NotNil(t, stored.Enabled)
	require.True(t, *stored.Enabled)
	decoded, err := computeruse.DecodeStoredConfig(raw)
	require.NoError(t, err)
	require.True(t, decoded.Enabled)
	cfg, err := svc.ComputerConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, ComputerConfigView{Enabled: true}, cfg)

	// Explicit zero: disabled persists as its own key — the stored document
	// states the choice rather than inheriting the default.
	require.NoError(t, svc.SaveComputerConfig(ctx, ComputerConfigView{Enabled: false}))
	raw, ok, err = fs.ToolSettings().Get(ctx, computeruse.ComputerSettingsKey)
	require.NoError(t, err)
	require.True(t, ok)
	stored.Enabled = nil
	require.NoError(t, json.Unmarshal([]byte(raw), &stored))
	require.NotNil(t, stored.Enabled, "an explicit disabled must be stored, not omitted")
	require.False(t, *stored.Enabled)
	cfg, err = svc.ComputerConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, ComputerConfigView{Enabled: false}, cfg)
}

// TestSettingsComputer_SaveFiresEnabledChange proves the save persists first
// and then fires the enabled-change hook exactly once per actual change:
// enable kicks the ensure path, disable stops the daemon and adapter, and
// saving the same value fires nothing (spec computer-use "Configuration and
// spawn boundaries").
func TestSettingsComputer_SaveFiresEnabledChange(t *testing.T) {
	ctx := context.Background()
	calls := &computerCalls{}
	svc, fs := newComputerSettingsWithStore(t, computerStubHooks(nil, ComputerDriverState{State: ComputerDriverReady}, mcp.ServerState{}, calls))

	require.NoError(t, svc.SaveComputerConfig(ctx, ComputerConfigView{Enabled: true}))
	require.Equal(t, 1, calls.ensure, "enabling kicks the ensure path once")
	require.Zero(t, calls.stop)

	require.NoError(t, svc.SaveComputerConfig(ctx, ComputerConfigView{Enabled: true}))
	require.Equal(t, 1, calls.ensure, "saving the same value fires nothing")

	require.NoError(t, svc.SaveComputerConfig(ctx, ComputerConfigView{Enabled: false}))
	require.Equal(t, 1, calls.stop, "disabling stops the daemon and adapter once")
	require.Equal(t, 1, calls.ensure)

	// The persisted document carries the final choice either way.
	raw, ok, err := fs.ToolSettings().Get(ctx, computeruse.ComputerSettingsKey)
	require.NoError(t, err)
	require.True(t, ok)
	decoded, err := computeruse.DecodeStoredConfig(raw)
	require.NoError(t, err)
	require.False(t, decoded.Enabled)
}

// TestSettingsComputer_SaveHookErrorStillPersists proves the stored choice is
// the truth even when the ensure path then fails: the document persists
// immediately (spec computer-use "Configuration and spawn boundaries") and
// the error surfaces to the pane, which renders the driver state anyway.
func TestSettingsComputer_SaveHookErrorStillPersists(t *testing.T) {
	ctx := context.Background()
	hooks := computerStubHooks(nil, ComputerDriverState{State: ComputerDriverReady}, mcp.ServerState{}, nil)
	hooks.EnsureDriver = func(_ context.Context) error { return errors.New("download failed") }
	svc, fs := newComputerSettingsWithStore(t, hooks)

	err := svc.SaveComputerConfig(ctx, ComputerConfigView{Enabled: true})
	require.ErrorContains(t, err, "download failed")

	raw, ok, err := fs.ToolSettings().Get(ctx, computeruse.ComputerSettingsKey)
	require.NoError(t, err)
	require.True(t, ok)
	decoded, err := computeruse.DecodeStoredConfig(raw)
	require.NoError(t, err)
	require.True(t, decoded.Enabled, "the persisted choice outlives the failed ensure kick")
}

// TestSettingsComputer_StatusAggregation proves the status line resolves in
// the pane's precedence order (driver failure, then download, then
// permissions, then supervision) and that the driver state and supervision
// string pass through verbatim from the hooks.
func TestSettingsComputer_StatusAggregation(t *testing.T) {
	connected := mcp.ServerState{Status: mcp.StatusConnected}
	errored := mcp.ServerState{Status: mcp.StatusError, StatusError: "spawn failed"}

	tests := []struct {
		name    string
		enabled bool
		perms   []ComputerPermission
		driver  ComputerDriverState
		sup     mcp.ServerState
		// daemonUp wires the DaemonRunning hook when non-nil (nil = the hook
		// is unwired and the status trusts supervision alone).
		daemonUp        *bool
		wantStatus      string
		wantDetail      string
		wantSupervision string
	}{
		{
			name:            "disabled is off",
			enabled:         false,
			perms:           computerStubPerms(false),
			driver:          ComputerDriverState{State: ComputerDriverReady},
			sup:             connected,
			wantStatus:      ComputerStatusOff,
			wantSupervision: mcp.StatusConnected,
		},
		{
			name:            "enabled with ungranted permissions awaits",
			enabled:         true,
			perms:           computerStubPerms(false),
			driver:          ComputerDriverState{State: ComputerDriverReady},
			sup:             connected,
			wantStatus:      ComputerStatusAwaitingPermissions,
			wantSupervision: mcp.StatusConnected,
		},
		{
			name:            "pending driver is starting, never a false downloading",
			enabled:         true,
			perms:           computerStubPerms(true),
			driver:          ComputerDriverState{State: ComputerDriverPending},
			sup:             connected,
			wantStatus:      ComputerStatusStarting,
			wantSupervision: mcp.StatusConnected,
		},
		{
			name:            "in-flight driver is downloading",
			enabled:         true,
			perms:           computerStubPerms(true),
			driver:          ComputerDriverState{State: ComputerDriverDownloading},
			sup:             connected,
			wantStatus:      ComputerStatusDownloading,
			wantSupervision: mcp.StatusConnected,
		},
		{
			name:            "failed driver is failed with its reason",
			enabled:         true,
			perms:           computerStubPerms(true),
			driver:          ComputerDriverState{State: ComputerDriverFailed, Detail: "digest mismatch"},
			sup:             connected,
			wantStatus:      ComputerStatusFailed,
			wantDetail:      "digest mismatch",
			wantSupervision: mcp.StatusConnected,
		},
		{
			name:            "granted, verified, connected is ready",
			enabled:         true,
			perms:           computerStubPerms(true),
			driver:          ComputerDriverState{State: ComputerDriverReady},
			sup:             connected,
			wantStatus:      ComputerStatusReady,
			wantSupervision: mcp.StatusConnected,
		},
		{
			name:            "connected supervision over a dead daemon is starting, never a lying ready",
			enabled:         true,
			perms:           computerStubPerms(true),
			driver:          ComputerDriverState{State: ComputerDriverReady},
			sup:             connected,
			daemonUp:        boolPtr(false),
			wantStatus:      ComputerStatusStarting,
			wantSupervision: mcp.StatusConnected,
		},
		{
			name:            "connected supervision with a live daemon is ready",
			enabled:         true,
			perms:           computerStubPerms(true),
			driver:          ComputerDriverState{State: ComputerDriverReady},
			sup:             connected,
			daemonUp:        boolPtr(true),
			wantStatus:      ComputerStatusReady,
			wantSupervision: mcp.StatusConnected,
		},
		{
			name:            "supervision error is failed with its reason",
			enabled:         true,
			perms:           computerStubPerms(true),
			driver:          ComputerDriverState{State: ComputerDriverReady},
			sup:             errored,
			wantStatus:      ComputerStatusFailed,
			wantDetail:      "spawn failed",
			wantSupervision: mcp.StatusError,
		},
		{
			name:            "unspawned supervision is starting",
			enabled:         true,
			perms:           computerStubPerms(true),
			driver:          ComputerDriverState{State: ComputerDriverReady},
			sup:             mcp.ServerState{Status: mcp.StatusUnknown},
			wantStatus:      ComputerStatusStarting,
			wantSupervision: mcp.StatusUnknown,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enabled := tt.enabled
			hooks := computerStubHooks(tt.perms, tt.driver, tt.sup, nil)
			if tt.daemonUp != nil {
				up := *tt.daemonUp
				hooks.DaemonRunning = func() bool { return up }
			}
			svc := NewSettingsService(fake.New(), WithComputerHooks(hooks))
			require.NoError(t, svc.SaveComputerConfig(context.Background(), ComputerConfigView{Enabled: enabled}))

			st, err := svc.ComputerStatus(context.Background())
			require.NoError(t, err)
			require.Equal(t, ComputerStatus{
				Available:    true,
				Enabled:      enabled,
				Status:       tt.wantStatus,
				StatusDetail: tt.wantDetail,
				Permissions:  tt.perms,
				Driver:       tt.driver,
				Supervision:  tt.wantSupervision,
			}, st)
		})
	}
}

// TestSettingsComputer_AwaitingListsPermissionsWithManageURLs proves the
// ungranted pane lists every missing permission with its Manage deep link
// (spec settings "Computer Use pane": the pane "lists the missing permissions
// with Manage actions").
func TestSettingsComputer_AwaitingListsPermissionsWithManageURLs(t *testing.T) {
	perms := computerStubPerms(false)
	perms[1].Granted = true // one granted, one missing
	hooks := computerStubHooks(perms, ComputerDriverState{State: ComputerDriverReady}, mcp.ServerState{Status: mcp.StatusConnected}, nil)
	svc := newComputerSettings(t, hooks)
	require.NoError(t, svc.SaveComputerConfig(context.Background(), ComputerConfigView{Enabled: true}))

	st, err := svc.ComputerStatus(context.Background())
	require.NoError(t, err)
	require.Equal(t, ComputerStatusAwaitingPermissions, st.Status)
	require.Len(t, st.Permissions, 2)
	require.False(t, st.Permissions[0].Granted)
	require.Equal(t, "Accessibility", st.Permissions[0].Name)
	require.Equal(t, "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility", st.Permissions[0].ManageURL)
	require.True(t, st.Permissions[1].Granted)
	require.Equal(t, "Screen Recording", st.Permissions[1].Name)
	require.Equal(t, "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture", st.Permissions[1].ManageURL)
}

// TestSettingsComputer_CheckAgainRestartsOnGrantChange proves the Check
// again contract end to end (spec settings "Check again applies a grant"):
// the pane's ungranted status seeds the baseline, a grant flipped between
// that read and the re-check restarts the driver exactly once, and re-checks
// without a further change restart nothing.
func TestSettingsComputer_CheckAgainRestartsOnGrantChange(t *testing.T) {
	ctx := context.Background()
	granted := false
	calls := &computerCalls{}
	hooks := computerStubHooks(nil, ComputerDriverState{State: ComputerDriverReady},
		mcp.ServerState{Status: mcp.StatusConnected}, calls)
	hooks.ProbePermissions = func() ([]ComputerPermission, error) {
		return computerStubPerms(granted), nil
	}
	svc := newComputerSettings(t, hooks)
	require.NoError(t, svc.SaveComputerConfig(ctx, ComputerConfigView{Enabled: true}))

	// The pane renders the ungranted state: the baseline is seeded, nothing
	// restarts.
	st, err := svc.ComputerStatus(ctx)
	require.NoError(t, err)
	require.Equal(t, ComputerStatusAwaitingPermissions, st.Status)
	require.Zero(t, calls.restart)

	// The user grants in System Settings and presses Check again.
	granted = true
	st, err = svc.CheckComputerPermissions(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, calls.restart, "a grant change restarts the driver exactly once")
	require.Equal(t, ComputerStatusReady, st.Status, "the fresh status advances toward ready")
	require.True(t, st.Permissions[0].Granted)
	require.True(t, st.Permissions[1].Granted)

	// Re-checking without a further change restarts nothing.
	st, err = svc.CheckComputerPermissions(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, calls.restart)
	require.Equal(t, ComputerStatusReady, st.Status)
}

// TestSettingsComputer_CheckAgainWithoutChangeNeverRestarts proves both the
// no-change path and the baseline-seeding path stay restart-free: a fresh
// service's first re-check only records the baseline (the pane cannot press
// Check again without having rendered a status first), and stable grants
// never restart the driver.
func TestSettingsComputer_CheckAgainWithoutChangeNeverRestarts(t *testing.T) {
	ctx := context.Background()
	calls := &computerCalls{}
	hooks := computerStubHooks(computerStubPerms(false), ComputerDriverState{State: ComputerDriverReady},
		mcp.ServerState{Status: mcp.StatusConnected}, calls)
	svc := newComputerSettings(t, hooks)
	require.NoError(t, svc.SaveComputerConfig(ctx, ComputerConfigView{Enabled: true}))

	for i := 0; i < 3; i++ {
		_, err := svc.CheckComputerPermissions(ctx)
		require.NoError(t, err)
		require.Zero(t, calls.restart, "stable ungranted state never restarts the driver")
	}
}

// TestSettingsComputer_CheckAgainAppliesRevocation proves a grant moving the
// other way is a change too: the daemon's cached answers are stale after any
// permission change (spec computer-use "Driver daemon lifecycle").
func TestSettingsComputer_CheckAgainAppliesRevocation(t *testing.T) {
	ctx := context.Background()
	granted := true
	calls := &computerCalls{}
	hooks := computerStubHooks(nil, ComputerDriverState{State: ComputerDriverReady},
		mcp.ServerState{Status: mcp.StatusConnected}, calls)
	hooks.ProbePermissions = func() ([]ComputerPermission, error) {
		perms := computerStubPerms(true)
		perms[1].Granted = granted
		return perms, nil
	}
	svc := newComputerSettings(t, hooks)
	require.NoError(t, svc.SaveComputerConfig(ctx, ComputerConfigView{Enabled: true}))

	st, err := svc.ComputerStatus(ctx)
	require.NoError(t, err)
	require.Equal(t, ComputerStatusReady, st.Status)

	granted = false
	st, err = svc.CheckComputerPermissions(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, calls.restart, "a revoked permission restarts the driver once")
	require.Equal(t, ComputerStatusAwaitingPermissions, st.Status)
}

// TestSettingsComputer_CheckAgainRestartError surfaces a failed restart to
// the pane instead of reporting ready over a dead daemon.
func TestSettingsComputer_CheckAgainRestartError(t *testing.T) {
	ctx := context.Background()
	granted := false
	hooks := computerStubHooks(nil, ComputerDriverState{State: ComputerDriverReady},
		mcp.ServerState{Status: mcp.StatusConnected}, nil)
	hooks.ProbePermissions = func() ([]ComputerPermission, error) {
		return computerStubPerms(granted), nil
	}
	hooks.RestartDriver = func(_ context.Context) error { return errors.New("daemon refused") }
	svc := newComputerSettings(t, hooks)
	_, err := svc.ComputerStatus(ctx)
	require.NoError(t, err)

	granted = true
	_, err = svc.CheckComputerPermissions(ctx)
	require.ErrorContains(t, err, "daemon refused")
}

// TestSettingsComputer_PromptRaisesPromptHook proves PromptComputerPermissions
// is the only path that raises the OS prompts (add-computer-use design D6:
// the app owns prompting, the driver never prompts) and that its error
// surfaces to the pane's enable action.
func TestSettingsComputer_PromptRaisesPromptHook(t *testing.T) {
	ctx := context.Background()
	calls := &computerCalls{}
	hooks := computerStubHooks(computerStubPerms(false), ComputerDriverState{State: ComputerDriverReady}, mcp.ServerState{}, calls)
	svc := newComputerSettings(t, hooks)

	require.NoError(t, svc.PromptComputerPermissions(ctx))
	require.Equal(t, 1, calls.prompt)

	hooks.PromptPermissions = func() error { return errors.New("prompt refused") }
	svc = newComputerSettings(t, hooks)
	require.ErrorContains(t, svc.PromptComputerPermissions(ctx), "prompt refused")
}

// TestSettingsComputer_UnwiredHooksRefuse proves the wiring-bug contract: a
// settings service without WithComputerHooks refuses every Computer method
// with a readable error instead of panicking (the composition root always
// wires the hooks on desktop; server mode never reaches them).
func TestSettingsComputer_UnwiredHooksRefuse(t *testing.T) {
	ctx := context.Background()
	svc := NewSettingsService(fake.New())

	_, err := svc.ComputerStatus(ctx)
	require.ErrorContains(t, err, "not wired")
	_, err = svc.CheckComputerPermissions(ctx)
	require.ErrorContains(t, err, "not wired")
	require.ErrorContains(t, svc.SaveComputerConfig(ctx, ComputerConfigView{Enabled: true}), "not wired")
	require.ErrorContains(t, svc.PromptComputerPermissions(ctx), "not wired")

	// The config read stays store-backed without hooks: it touches no driver
	// machinery.
	cfg, err := svc.ComputerConfig(ctx)
	require.NoError(t, err)
	require.False(t, cfg.Enabled)
}
