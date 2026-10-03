package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
)

// The control server is the bench's deterministic hook target: a local HTTP
// server bound to 127.0.0.1 on an ephemeral port serving /block and /allow.
// Suite hook steps point {{control_url}} at it, so a pre_tool_use hook can
// block a named tool call mid-run — the harness's http hook handler POSTs the
// event to the URL and honors a 2xx body that parses as a decision object
// ({"decision": "allow"|"block", "reason"}), the same strict whole-object
// rule every handler kind shares. /block returns exactly that block shape;
// /allow the allow shape. Any other 2xx body would mean allow, so the
// responses here are the canonical decision objects, byte-exact.
const (
	controlBlockReason = "blocked by onclaw-bench control server"
	controlAllowReason = "allowed by onclaw-bench control server"
)

// hookDecision is the wire shape the harness's hook handlers parse
// (ParseDecisionJSON: the whole trimmed body must be one JSON object with
// decision exactly "allow" or "block"; reason optional).
type hookDecision struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

// controlServer is one started bench control server.
type controlServer struct {
	server *http.Server
	base   string
}

// startControlServer binds 127.0.0.1 on an ephemeral port and serves /block
// and /allow until Close. The port is assigned by the kernel, so concurrent
// bench processes never collide.
func startControlServer() (*controlServer, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("control server: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/block", serveDecision(hookDecision{Decision: "block", Reason: controlBlockReason}))
	mux.HandleFunc("/allow", serveDecision(hookDecision{Decision: "allow", Reason: controlAllowReason}))

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	return &controlServer{server: srv, base: "http://" + ln.Addr().String()}, nil
}

// serveDecision answers every request with the fixed decision body — the
// harness POSTs events to it, and the tests GET it, so the method is not
// constrained.
func serveDecision(d hookDecision) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		body, err := json.Marshal(d)
		if err != nil {
			http.Error(w, "encode decision", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}
}

// BaseURL is the value substituted for {{control_url}} in hook handler URLs.
func (c *controlServer) BaseURL() string { return c.base }

// Close shuts the server down (best-effort; called once at run end).
func (c *controlServer) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.server.Shutdown(ctx)
}
