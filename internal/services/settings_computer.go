package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/agents/mcp"
	"github.com/oniharnantyo/buddies/internal/computeruse"
)

// The Computer Use methods on SettingsService: the settings pane's config
// read/save plus the permission warm-up gate (add-computer-use design D6,
// tasks 3.1; spec settings "Computer Use pane", spec computer-use
// "Permission warm-up gate" and "Configuration and spawn boundaries").
//
// The service owns no driver or daemon machinery: the config persists as the
// tool_settings "computer" document via computeruse (the single decode/encode
// site), and everything live — driver download state, permission probes, the
// daemon restart, the adapter's supervision status — arrives through the
// injected ComputerHooks the composition root wires (design D6's
// WithSystemComputerUse seam). Changes take effect at spawn boundaries, never
// mid-action: save persists immediately and then kicks the enable or disable
// hook once; a grant change found by Check again restarts the driver exactly
// once (spec computer-use "Driver daemon lifecycle": permission answers are
// cached per process).

// ErrComputerUnavailable is the server build's answer to the Computer Use
// calls that would manage the desktop lane: server mode has no GUI session,
// so there is nothing to probe, prompt, enable, or supervise (add-computer-use
// design D9; spec computer-use "Desktop-only availability" — the pane presents
// the capability as unavailable rather than offering toggles that cannot
// work). Mirrors ErrClipboardUnavailable. Never returned by desktop builds.
var ErrComputerUnavailable = errors.New("computer use: unavailable in server mode")

// ComputerConfigView is the Computer Use configuration as bound to the
// frontend for the pane's enable toggle. It is the pane-editable view of the
// stored document (computeruse.Config): one field today, more only when the
// adapter grows knobs (design D1's "any adapter knobs" clause).
type ComputerConfigView struct {
	// Enabled gates the whole lane: when false the daemon and adapter do not
	// run and no agent is offered computer-use tools. The default is false —
	// Computer Use is opt-in end to end (design D2).
	Enabled bool `json:"enabled"`
}

// ComputerDriverState is the vendored driver's acquisition state as the pane
// renders it (spec settings "Computer Use pane": a one-time download state
// with its verification outcome). The composition adapts the computeruse
// package's pin status into it — the service only passes it through.
type ComputerDriverState struct {
	// State is one of the ComputerDriver* vocabulary values below.
	State string `json:"state"`
	// Detail carries the verification failure reason when State is failed;
	// empty otherwise.
	Detail string `json:"detail,omitempty"`
}

// The driver acquisition vocabulary the ComputerHooks.PinStatus hook reports.
const (
	// ComputerDriverPending: the driver has not been downloaded yet.
	ComputerDriverPending = "pending"
	// ComputerDriverDownloading: a download or verification is in flight.
	ComputerDriverDownloading = "downloading"
	// ComputerDriverReady: the pinned driver is downloaded and verified.
	ComputerDriverReady = "ready"
	// ComputerDriverFailed: download or digest verification failed; Detail
	// names the reason and the driver refuses to run (spec computer-use
	// "Driver acquisition and integrity").
	ComputerDriverFailed = "failed"
)

// ComputerPermission is one OS permission the desktop driver needs, as the
// pane's permission card renders it: live granted state plus the Manage
// action's deep link into the matching system settings pane (spec settings
// "Computer Use pane").
type ComputerPermission struct {
	// Name is the display name ("Accessibility", "Screen Recording").
	Name string `json:"name"`
	// Granted is the live preflight answer. Probing never raises a prompt
	// (spec computer-use "Permission warm-up gate").
	Granted bool `json:"granted"`
	// ManageURL opens the matching system settings pane
	// (x-apple.systempreferences:... on macOS; empty when the platform has
	// no settings pane to open).
	ManageURL string `json:"manage_url"`
}

// The pane's status-line vocabulary (spec settings "Computer Use pane": fed
// by the live supervision state — ready, awaiting permissions, downloading,
// failed with reason, or unavailable in server mode).
const (
	// ComputerStatusOff: the toggle is disabled; nothing runs.
	ComputerStatusOff = "off"
	// ComputerStatusStarting: enabled, permissions ready, the driver is
	// downloaded — the adapter has not completed its first spawn yet.
	ComputerStatusStarting = "starting"
	// ComputerStatusReady: enabled, permissions granted, driver verified,
	// adapter connected.
	ComputerStatusReady = "ready"
	// ComputerStatusAwaitingPermissions: enabled but at least one OS
	// permission is ungranted — zero tools and no daemon until granted.
	ComputerStatusAwaitingPermissions = "awaiting-permissions"
	// ComputerStatusDownloading: enabled but the vendored driver is not
	// acquired yet (download in flight or pending the ensure path).
	ComputerStatusDownloading = "downloading"
	// ComputerStatusFailed: the driver failed verification or the adapter
	// supervision errored; StatusDetail carries the reason.
	ComputerStatusFailed = "failed"
	// ComputerStatusUnavailable: server mode — the whole lane is absent
	// (design D9).
	ComputerStatusUnavailable = "unavailable"
)

// ComputerStatus is the pane's status aggregate: everything the permission
// card, download state, and status line render in one bound read.
type ComputerStatus struct {
	// Available reports whether this build offers the lane at all — false in
	// server mode, where the pane presents the capability as unavailable
	// (design D9) and the remaining fields stay at their zero values.
	Available bool `json:"available"`
	// Enabled is the persisted toggle.
	Enabled bool `json:"enabled"`
	// Status is the status line, one of the ComputerStatus* vocabulary
	// values.
	Status string `json:"status"`
	// StatusDetail carries the failure reason when Status is failed; empty
	// otherwise.
	StatusDetail string `json:"status_detail,omitempty"`
	// Permissions lists each required OS permission with its live granted
	// state and Manage deep link. Empty on platforms with no permission
	// ceremony (design D9); never nil on desktop builds.
	Permissions []ComputerPermission `json:"permissions"`
	// Driver is the vendored driver's acquisition state, passed through from
	// the PinStatus hook.
	Driver ComputerDriverState `json:"driver"`
	// Supervision is the live adapter supervision status string
	// (mcp.ServerState.Status verbatim: connected / error / unknown).
	Supervision string `json:"supervision"`
}

// ComputerHooks carries the Computer Use lane's injected runtime seams
// (add-computer-use design D6): the service aggregates, persists, and
// sequences; the composition owns the driver/daemon/adapter machinery and
// adapts the platform probes (settings_computer_darwin.go /
// settings_computer_stub.go) and the computeruse package's status into these
// functions. One struct, wired whole via WithComputerHooks — a missing hook
// is a wiring bug that surfaces as a readable error, never a panic.
type ComputerHooks struct {
	// PinStatus reports the vendored driver's acquisition state (download
	// state and verification outcome).
	PinStatus func() ComputerDriverState
	// ProbePermissions prefights the platform's automation permissions
	// WITHOUT raising prompts (spec computer-use "Permission warm-up gate"):
	// the app owns prompting, the driver never prompts.
	ProbePermissions func() ([]ComputerPermission, error)
	// PromptPermissions raises the OS permission prompts — wired only into
	// the pane's enable action (PromptComputerPermissions), never the probe
	// path.
	PromptPermissions func() error
	// EnsureDriver kicks the enable path: acquire the pinned driver if
	// needed and let supervision warm the daemon up when permissions allow
	// (the daemon must not run before every permission is granted).
	EnsureDriver func(ctx context.Context) error
	// RestartDriver restarts the driver daemon after a permission grant
	// changed (spec computer-use "Driver daemon lifecycle": permission
	// answers are cached per process).
	RestartDriver func(ctx context.Context) error
	// StopDriver stops the daemon and adapter so no tools are offered from
	// the next turn on (spec computer-use "Configuration and spawn
	// boundaries").
	StopDriver func(ctx context.Context) error
	// SupervisionState reads the live adapter supervision status.
	SupervisionState func() mcp.ServerState
	// DaemonRunning reports whether the driver daemon child is actually up
	// (a dead generation counts as down). Optional: unwired (nil), the
	// status trusts supervision alone — the desktop lane always wires it.
	DaemonRunning func() bool
}

// WithComputerHooks supplies the Computer Use lane's runtime hooks
// (add-computer-use design D6). The composition passes the struct whole; the
// Computer methods refuse with a readable error until every hook is present
// (nil deps are wiring bugs — mirrors the vault/templates refusal pattern).
// In server mode the seam answers before the hooks are ever consulted, so
// server compositions never wire them.
func WithComputerHooks(hooks ComputerHooks) SettingsOption {
	return func(s *SettingsService) {
		s.computerHooks = hooks
	}
}

// ComputerConfig returns the persisted Computer Use configuration. A missing
// row — or stored keys left out — falls back to the default (disabled,
// computeruse.DefaultConfig): Computer Use is opt-in end to end (design D2).
func (s *SettingsService) ComputerConfig(ctx context.Context) (ComputerConfigView, error) {
	raw, ok, err := s.store.ToolSettings().Get(ctx, computeruse.ComputerSettingsKey)
	if err != nil {
		return ComputerConfigView{}, fmt.Errorf("get computer config: %w", err)
	}
	if !ok || raw == "" {
		def := computeruse.DefaultConfig()
		return ComputerConfigView{Enabled: def.Enabled}, nil
	}
	cfg, err := computeruse.DecodeStoredConfig(raw)
	if err != nil {
		return ComputerConfigView{}, fmt.Errorf("get computer config: %w", err)
	}
	return ComputerConfigView{Enabled: cfg.Enabled}, nil
}

// SaveComputerConfig create-or-replaces the Computer Use configuration and
// fires the enabled-change hook once when the toggle actually moved: disable
// stops the daemon and adapter (no tools from the next turn on), enable kicks
// the ensure path (spec computer-use "Configuration and spawn boundaries":
// changes take effect at spawn boundaries, never mid-action — the hook is the
// boundary, not a mid-action restart). The document persists first and
// immediately even when the hook then fails: the stored choice is the truth
// supervision converges on at the next boundary.
//
// The server build refuses the save outright (ErrComputerUnavailable): there
// is no lane to configure (design D9).
func (s *SettingsService) SaveComputerConfig(ctx context.Context, view ComputerConfigView) error {
	if !computerLaneAvailable() {
		return ErrComputerUnavailable
	}
	hooks, err := s.computerHooksReady()
	if err != nil {
		return fmt.Errorf("save computer config: %w", err)
	}
	previous, err := s.ComputerConfig(ctx)
	if err != nil {
		return fmt.Errorf("save computer config: %w", err)
	}
	raw, err := computeruse.EncodeStoredConfig(computeruse.Config{Enabled: view.Enabled})
	if err != nil {
		return fmt.Errorf("save computer config: %w", err)
	}
	if err := s.store.ToolSettings().Set(ctx, computeruse.ComputerSettingsKey, raw); err != nil {
		return fmt.Errorf("save computer config: %w", err)
	}
	if previous.Enabled == view.Enabled {
		return nil
	}
	if view.Enabled {
		if err := hooks.EnsureDriver(ctx); err != nil {
			return fmt.Errorf("enable computer use: %w", err)
		}
		return nil
	}
	if err := hooks.StopDriver(ctx); err != nil {
		return fmt.Errorf("disable computer use: %w", err)
	}
	return nil
}

// ComputerStatus aggregates the pane's status read: enabled, the
// per-permission list (live granted state + Manage deep links), the driver's
// download state, and the adapter's live supervision status, resolved into
// the status line. Probing never raises a prompt. The first read also seeds
// the permission baseline CheckComputerPermissions compares against — the
// pane cannot press Check again without having rendered a status first, so
// the baseline is always older than the user's grant (later reads do not
// re-seed: a poll must not absorb a grant the user is about to Check).
func (s *SettingsService) ComputerStatus(ctx context.Context) (ComputerStatus, error) {
	if !computerLaneAvailable() {
		return ComputerStatus{Available: false, Status: ComputerStatusUnavailable}, nil
	}
	hooks, err := s.computerHooksReady()
	if err != nil {
		return ComputerStatus{}, err
	}
	perms, err := hooks.ProbePermissions()
	if err != nil {
		return ComputerStatus{}, fmt.Errorf("probe computer permissions: %w", err)
	}
	s.computerMu.Lock()
	if s.computerGrants == nil {
		s.computerGrants = computerGrantSnapshot(perms)
	}
	s.computerMu.Unlock()
	return s.computerStatus(ctx, hooks, perms)
}

// CheckComputerPermissions re-probes and applies a grant change (spec
// settings "Computer Use pane": the Check again action "re-probes and applies
// a grant change (restarting the driver when one occurred)"). The fresh probe
// is compared against the baseline the pane's earlier status read seeded: a
// change in any permission's granted state invokes the restart hook exactly
// once (permission answers are cached per daemon process — spec computer-use
// "Driver daemon lifecycle"), updates the baseline, and returns the fresh
// status. No change, no restart. Returns the server-unavailable status in
// server mode.
func (s *SettingsService) CheckComputerPermissions(ctx context.Context) (ComputerStatus, error) {
	if !computerLaneAvailable() {
		return ComputerStatus{Available: false, Status: ComputerStatusUnavailable}, nil
	}
	hooks, err := s.computerHooksReady()
	if err != nil {
		return ComputerStatus{}, err
	}
	perms, err := hooks.ProbePermissions()
	if err != nil {
		return ComputerStatus{}, fmt.Errorf("probe computer permissions: %w", err)
	}
	s.computerMu.Lock()
	baseline := s.computerGrants
	changed := baseline != nil && !computerGrantsEqual(baseline, perms)
	s.computerGrants = computerGrantSnapshot(perms)
	s.computerMu.Unlock()
	if changed {
		if err := hooks.RestartDriver(ctx); err != nil {
			return ComputerStatus{}, fmt.Errorf("restart computer driver after grant change: %w", err)
		}
	}
	return s.computerStatus(ctx, hooks, perms)
}

// PromptComputerPermissions raises the OS permission prompts — the pane's
// enable action calls it host-side; the driver never prompts (add-computer-use
// design D6; embedded mode enforces this on the driver side). Probing stays
// prompt-free; this is the only path that raises a prompt. On platforms with
// no permission ceremony there is nothing to raise and the wired hook is a
// no-op.
func (s *SettingsService) PromptComputerPermissions(ctx context.Context) error {
	if !computerLaneAvailable() {
		return ErrComputerUnavailable
	}
	hooks, err := s.computerHooksReady()
	if err != nil {
		return fmt.Errorf("prompt computer permissions: %w", err)
	}
	if err := hooks.PromptPermissions(); err != nil {
		return fmt.Errorf("prompt computer permissions: %w", err)
	}
	return nil
}

// OpenComputerManageURL opens one of the Computer Use pane's Manage deep
// links (add-computer-use design D6/D8) into the matching system settings
// pane. The URL must be one the service itself issued in a permission
// probe — a custom scheme (x-apple.systempreferences:…) the webview cannot
// open itself, so it rides the host's URL opener. Anything else is refused:
// the bridge must not become a generic URL opener.
func (s *SettingsService) OpenComputerManageURL(_ context.Context, url string) error {
	if !computerLaneAvailable() {
		return ErrComputerUnavailable
	}
	allowed := false
	for _, perm := range computerPermissions() {
		if perm.ManageURL != "" && perm.ManageURL == url {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("open computer manage url: url %q is not a known manage link", url)
	}
	if err := s.urlOpener.Open(url); err != nil {
		return fmt.Errorf("open computer manage url: %w", err)
	}
	return nil
}

// computerStatus resolves the status line from an already-probed permission
// list (the caller owns probing — status and re-check share the aggregate
// without double-probing).
func (s *SettingsService) computerStatus(ctx context.Context, hooks ComputerHooks, perms []ComputerPermission) (ComputerStatus, error) {
	cfg, err := s.ComputerConfig(ctx)
	if err != nil {
		return ComputerStatus{}, fmt.Errorf("aggregate computer status: %w", err)
	}
	driver := hooks.PinStatus()
	supervision := hooks.SupervisionState()
	status := ComputerStatus{
		Available:   true,
		Enabled:     cfg.Enabled,
		Permissions: perms,
		Driver:      driver,
		Supervision: supervision.Status,
	}
	switch {
	case !cfg.Enabled:
		status.Status = ComputerStatusOff
	case driver.State == ComputerDriverFailed:
		status.Status = ComputerStatusFailed
		status.StatusDetail = driver.Detail
	case !computerAllGranted(perms):
		// The permission gate is the consent boundary (design D6): until it
		// passes, the pane's ask is the grant — not the driver, which the
		// gate holds unacquired. A pending or in-flight download under an
		// ungranted lane still reads awaiting-permissions.
		status.Status = ComputerStatusAwaitingPermissions
	case driver.State == ComputerDriverDownloading:
		status.Status = ComputerStatusDownloading
	case driver.State != ComputerDriverReady:
		// Enabled, granted, driver not acquired yet: the supervision tick
		// kicks the acquisition, so the lane is on its way up — starting,
		// never a false "downloading" for a fetch that has not begun.
		status.Status = ComputerStatusStarting
	case supervision.Status == mcp.StatusConnected && hooks.DaemonRunning != nil && !hooks.DaemonRunning():
		// The adapter is supervised but the daemon child is down — a boot
		// probe that raced the launch, or a crash the supervisor cannot see.
		// The lane's reconciliation brings the daemon back within a tick:
		// starting, never a lying "ready" over a dead daemon.
		status.Status = ComputerStatusStarting
	case supervision.Status == mcp.StatusConnected:
		status.Status = ComputerStatusReady
	case supervision.Status == mcp.StatusError:
		status.Status = ComputerStatusFailed
		status.StatusDetail = supervision.StatusError
	default:
		status.Status = ComputerStatusStarting
	}
	return status, nil
}

// computerHooksReady returns the wired hooks or a readable wiring-bug error.
func (s *SettingsService) computerHooksReady() (ComputerHooks, error) {
	h := s.computerHooks
	if h.PinStatus == nil || h.ProbePermissions == nil || h.PromptPermissions == nil ||
		h.EnsureDriver == nil || h.RestartDriver == nil || h.StopDriver == nil ||
		h.SupervisionState == nil {
		return ComputerHooks{}, errors.New("computer use: runtime hooks not wired")
	}
	return h, nil
}

// computerAllGranted reports whether every required permission is granted. An
// empty list — a platform with no permission ceremony (design D9) — is ready
// by definition.
func computerAllGranted(perms []ComputerPermission) bool {
	for _, p := range perms {
		if !p.Granted {
			return false
		}
	}
	return true
}

// computerGrantsEqual compares two probes by permission identity and granted
// state (the Manage URL is presentation, not state).
func computerGrantsEqual(a, b []ComputerPermission) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Granted != b[i].Granted {
			return false
		}
	}
	return true
}

// computerGrantSnapshot copies a probe result so the baseline cannot be
// mutated by a later call returning a shared slice.
func computerGrantSnapshot(perms []ComputerPermission) []ComputerPermission {
	if len(perms) == 0 {
		return nil
	}
	out := make([]ComputerPermission, len(perms))
	copy(out, perms)
	return out
}
