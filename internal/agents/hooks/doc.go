// Package hooks implements the agent lifecycle-hook pipeline (OpenSpec
// change port-agent-runtime, design D4/D9; spec agent-hooks): the five turn
// events, the matcher grammar, the command and script handler types with
// their shared allow/block decision contract, and the per-run dispatcher
// that resolves the two assignment tiers — the agent's assigned hooks by
// position, then the unassigned global tier — and delivers events with an
// audit row per evaluation.
//
// # Decision contract
//
// Every handler shares one contract: a successful execution that returns no
// decision object is informational and never blocks; a decision of block
// (with an optional reason) stops the prompt or tool call. A handler that
// fails — spawn error, timeout, sandbox exception — follows the hook's
// on_failure policy ("allow" default, "block" fails closed) and is recorded
// in the audit trail; a failing hook never crashes the turn.
//
// # Package boundary
//
// Like the backend/tools/mcp subpackages, this package speaks no store
// types: resolution reads through the narrow Store seam in store.go,
// satisfied by an adapter over store.Store living in internal/agents.
package hooks
