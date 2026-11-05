// Package mcp provides the MCP (Model Context Protocol) runtime for the
// agent runtime: a transport factory for mcp-go clients (design D7: stdio,
// streamable HTTP, SSE via the eino-ext MCP tool component), the idle-reaped
// connection manager with negative-cache retry, provider-safe tool naming
// (mcp__<server>__<tool>), the on-demand probe, and OAuth 2.1 for HTTP/SSE
// servers — unauthenticated discovery of a server's OAuth advertisement plus
// the out-of-band browser authorization flow (add-mcp-oauth D1/D2). The
// manager also synthesizes and supervises the app-provided browser-use
// system server (rebrand-browser-use-system-mcp D6/D7/D8): a stdio child
// spawned from buddies' own binary at init, respawned with backoff, and
// restarted on config change only at idle boundaries. The cache is keyed by
// server id alone — buddies is single-tenant.
package mcp
