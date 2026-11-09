//go:build server

package cli

import (
	"context"

	"github.com/oniharnantyo/buddies/internal/agents/mcp"
	"github.com/oniharnantyo/buddies/internal/services"
)

// The server build's Computer Use composition seam (add-computer-use design
// D9; spec computer-use "Desktop-only availability"): the whole lane is
// absent — no supervised spec, no daemon, no hooks, no warm-up. The settings
// service's server seam (settings_computer_server.go) answers unavailable
// before any hook is consulted, so the composition never wires them — the
// clipboard service's server-gate pattern applied to the lane.

// managerOption registers nothing: server mode supervises no computer-use
// system server.
func (l *computerLane) managerOption() mcp.ManagerOption {
	return func(*mcp.MCPManager) {}
}

// settingsHooks wires nothing: the lane's driver machinery does not exist in
// server builds, and the service methods refuse before the hooks are
// consulted.
func (l *computerLane) settingsHooks(_ *mcp.MCPManager) services.SettingsOption {
	return func(*services.SettingsService) {}
}

// warmUp is a no-op: no daemon runs in server mode.
func (l *computerLane) warmUp(_ context.Context) {}

// stopAtClose is a no-op: nothing was started.
func (l *computerLane) stopAtClose() {}
