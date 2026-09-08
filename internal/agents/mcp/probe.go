package mcp

import (
	"context"
)

// Probe dials ref with a fresh connection — bypassing the manager's cache, a
// probe is never a cache hit or fill — within the caller's context (the probe
// endpoint bounds it at ~10s), lists the server's tools, and reports the
// exposed-tool count. A failed dial, handshake, or listing returns the error
// with a zero count. The connection is abandoned for asynchronous teardown
// after the listing so a wedged server cannot hold the probe response past
// the dial bound (the close watchdog reaps it).
func Probe(ctx context.Context, ref Ref) (int, error) {
	conn, err := connect(ctx, ref.Conn)
	if err != nil {
		return 0, err
	}
	k := key{ref.WorkspaceID, ref.ServerID}
	entry := &cacheEntry{conn: conn}
	go closeClient(k, entry)
	return len(conn.tools), nil
}
