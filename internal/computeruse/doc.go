// Package computeruse is the Computer Use system MCP server: a supervised
// stdio adapter (design D1) that curates a vendored OS-automation daemon
// (cua-driver, trycua/cua, MIT) into a fixed mcp__computer-use__* tool
// surface for desktop control — windows, keyboard, pointer, clipboard —
// sibling of Browser Use (internal/browseruse), with the two pipelines kept
// disjoint.
//
// The process tree (spec computer-use "Driver daemon lifecycle"): the app
// spawns the driver daemon (serve --embedded --socket) as a direct child of
// itself so macOS TCC attributes grants to buddies, and spawns this
// package's adapter through its own binary's "computer-mcp" subcommand
// (internal/cli); the adapter reaches the daemon through the driver's stable
// MCP frontend (cua-driver mcp --socket) over stdio, keeps no network
// listener of its own, and exits when its input stream closes. Every
// desktop action passes one process-wide dispatch mutex (design D3): a
// second concurrent caller receives the fixed busy refusal, never a queue.
//
// The daemon's telemetry and update checks are disabled by environment at
// spawn (design D1); the vendored binary's digest is pinned per release
// (pin.go) and verified before first use; the vendor's code signature is
// never touched.
package computeruse
