// Package browseruse is the Browser Use system MCP server: a sole-owner
// stdio process (design D2/D3) that owns all live browser state — profile
// sessions, the idle janitor, cookie jars, the Chromium launcher, the
// private-network guard, and the single-owner lockfile on the browser
// directory.
//
// The app spawns this package through its own binary's "browser-mcp"
// subcommand (internal/cli); the server speaks MCP over stdin/stdout, keeps
// no network listener, and exits when its input stream closes so a dead
// parent leaves no orphan. The durable state is <browserDir>/profiles/ on
// disk exactly as before, so a server restart is the already-implemented
// crash-recovery path.
package browseruse
