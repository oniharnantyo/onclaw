package mcp

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// stubCreds records BearerForDial calls and answers with a scripted result —
// the unit seam for the dial auth step's contract (no transport involved).
type stubCreds struct {
	mu    sync.Mutex
	calls []string // "workspaceID/agentID/serverID"
	token string
	err   error
}

func (s *stubCreds) BearerForDial(_ context.Context, workspaceID, agentID, serverID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, workspaceID+"/"+agentID+"/"+serverID)
	return s.token, s.err
}

func (s *stubCreds) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

// deadAddr is a URL transport target that refuses instantly (nothing listens
// on it), so a dial that gets past the auth step fails fast without network.
func deadAddr() string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return "http://" + addr + "/mcp"
}

func oauthRef(addr string) Ref {
	return Ref{
		WorkspaceID: "ws-1",
		AgentID:     "agent-1",
		ServerID:    "srv-1",
		Name:        "remote",
		Conn: domain.MCPConnection{
			Transport: domain.MCPTransportStreamableHTTP,
			URL:       addr,
			AuthMode:  domain.MCPAuthModeOAuth,
		},
	}
}

func staticRef(addr string) Ref {
	ref := oauthRef(addr)
	ref.Conn.AuthMode = domain.MCPAuthModeNone
	ref.Conn.Headers = []domain.EnvRow{{Name: "X-Static", Value: "yes"}}
	return ref
}

func TestWithBearerJoinsHeaders(t *testing.T) {
	conn := domain.MCPConnection{
		Headers: []domain.EnvRow{
			{Name: "X-Trace", Value: "keep"},
			{Name: "Authorization", Value: "Bearer stale"},
		},
	}
	out := withBearer(conn, "fresh-token")

	if len(out.Headers) != 3 {
		t.Fatalf("headers = %v, want the two static rows plus the bearer", out.Headers)
	}
	last := out.Headers[len(out.Headers)-1]
	if last.Name != "Authorization" || last.Value != "Bearer fresh-token" {
		t.Fatalf("last row = %v, want the live bearer appended last (it overrides the static row in the header map)", last)
	}
	if out.Headers[0].Name != "X-Trace" || out.Headers[0].Value != "keep" {
		t.Fatalf("static rows must survive alongside the bearer; got %v", out.Headers[0])
	}
	// The input is copied, not mutated.
	if len(conn.Headers) != 2 || conn.Headers[1].Value != "Bearer stale" {
		t.Fatalf("withBearer mutated its input: %v", conn.Headers)
	}
}

func TestConnectAuthorizedStaticModeNeverResolves(t *testing.T) {
	creds := &stubCreds{}
	addr := deadAddr()

	_, err := connectAuthorized(context.Background(), staticRef(addr), creds)
	if err == nil {
		t.Fatal("expected the dial itself to fail against the dead address")
	}
	if creds.callCount() != 0 {
		t.Fatalf("static-mode dial resolved credentials %d times; no OAuth machinery may run for auth mode none", creds.callCount())
	}
	if strings.Contains(err.Error(), ErrAuthorizationRequired.Error()) {
		t.Fatalf("static dial failure must not be the OAuth signal: %v", err)
	}
}

func TestConnectAuthorizedOAuthModeNeedsAuthorization(t *testing.T) {
	creds := &stubCreds{err: NewAuthorizationRequiredError()}

	_, err := connectAuthorized(context.Background(), oauthRef(deadAddr()), creds)
	if !IsAuthorizationRequired(err) {
		t.Fatalf("err = %v, want the needs-authorization signal", err)
	}
	if creds.callCount() != 1 {
		t.Fatalf("credential resolution calls = %d, want 1", creds.callCount())
	}
}

func TestConnectAuthorizedOAuthModeBearerProceedsToDial(t *testing.T) {
	creds := &stubCreds{token: "at-1"}

	_, err := connectAuthorized(context.Background(), oauthRef(deadAddr()), creds)
	if err == nil {
		t.Fatal("expected the dial itself to fail against the dead address")
	}
	if IsAuthorizationRequired(err) {
		t.Fatalf("a resolved bearer must let the dial proceed past the auth step; got the signal: %v", err)
	}
	if creds.callCount() != 1 {
		t.Fatalf("credential resolution calls = %d, want 1", creds.callCount())
	}
}

// TestProbeParityWithDial pins task 5.1's parity contract: the probe path
// surfaces the needs-authorization signal exactly like the run path, and a
// static-mode probe never touches the seam.
func TestProbeParityWithDial(t *testing.T) {
	t.Run("oauth mode surfaces the signal", func(t *testing.T) {
		creds := &stubCreds{err: NewAuthorizationRequiredError()}
		_, err := Probe(context.Background(), oauthRef(deadAddr()), creds)
		if !IsAuthorizationRequired(err) {
			t.Fatalf("probe err = %v, want the needs-authorization signal", err)
		}
	})
	t.Run("static mode untouched", func(t *testing.T) {
		creds := &stubCreds{}
		_, err := Probe(context.Background(), staticRef(deadAddr()), creds)
		if err == nil || creds.callCount() != 0 {
			t.Fatalf("static probe: err=%v, credential calls=%d; want a plain dial failure and zero calls", err, creds.callCount())
		}
	})
}

// TestManagerToolsSurfacesNeedsAuthorization drives the wired manager: an
// oauth-mode ref whose resolution demands authorization produces the signal
// from Tools — which the runner reports as the row's status detail without
// failing the run — and the failure is negatively cached like any dial error.
func TestManagerToolsSurfacesNeedsAuthorization(t *testing.T) {
	creds := &stubCreds{err: NewAuthorizationRequiredError()}
	m := NewMCPManager(WithOAuthCredentials(creds), WithConnectTimeout(time.Second))
	t.Cleanup(m.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := m.Tools(ctx, oauthRef(deadAddr()))
	if !IsAuthorizationRequired(err) {
		t.Fatalf("Tools err = %v, want the needs-authorization signal", err)
	}
	if creds.callCount() != 1 {
		t.Fatalf("credential resolution calls = %d, want 1", creds.callCount())
	}
	// The errored entry carries the signal as its status detail (what the
	// runner's status write persists on the row).
	status, statusErr, _, ok := m.Status("ws-1", "srv-1")
	if !ok || status != domain.MCPStatusError || !strings.Contains(statusErr, ErrAuthorizationRequired.Error()) {
		t.Fatalf("status = (%q, %q, ok=%v); want error with the signal as detail", status, statusErr, ok)
	}
}

// TestManagerStaticModeDialUnchanged pins the byte-identical static path on
// the wired manager: static rows never reach the credential seam.
func TestManagerStaticModeDialUnchanged(t *testing.T) {
	creds := &stubCreds{}
	m := NewMCPManager(WithOAuthCredentials(creds), WithConnectTimeout(time.Second))
	t.Cleanup(m.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := m.Tools(ctx, staticRef(deadAddr()))
	if err == nil {
		t.Fatal("expected the dial itself to fail against the dead address")
	}
	if creds.callCount() != 0 {
		t.Fatalf("static dial resolved credentials %d times", creds.callCount())
	}
	if errors.Is(err, ErrAuthorizationRequired) {
		t.Fatalf("static dial failure must not be the OAuth signal: %v", err)
	}
}
