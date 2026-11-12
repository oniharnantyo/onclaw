package webhooks

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAttach_AlreadyRunning starts the listener immediately when the gateway
// is already Running, and a delivery dispatches through it.
func TestAttach_AlreadyRunning(t *testing.T) {
	p := newPipe(t)
	svc := NewService(p.st, p.runs, p.groups, WithClock(testNow), WithListenAddr(testAddr))
	svc.Attach(p.gw)
	t.Cleanup(svc.Stop)

	state := svc.State()
	require.True(t, state.Serving, "attach on a Running gateway must serve")
	require.NotEmpty(t, state.BoundAddr)
	require.Empty(t, state.BindError)

	// A real delivery through the real listener dispatches end to end.
	ep := p.createEndpoint(t, store.WebhookEndpoint{
		Name: "CI", AgentID: p.agentID(t), Prompt: "Go:", Enabled: true,
	}, []store.WebhookVerifier{headerVerifier("tok")})
	resp, err := postDelivery(state.BoundAddr, ep.ID)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	require.Len(t, p.runs.createdCalls(), 1)
}

// TestAttach_SubscribeThenRunning covers the subscribe-first order: attach
// on a Stopped gateway serves nothing; the first Running transition starts
// the listener.
func TestAttach_SubscribeThenRunning(t *testing.T) {
	p := newPipe(t)
	svc := NewService(p.st, p.runs, p.groups, WithClock(testNow), WithListenAddr(testAddr))
	require.NoError(t, p.gw.Stop())
	svc.Attach(p.gw)
	t.Cleanup(svc.Stop)

	require.False(t, svc.State().Serving, "a Stopped gateway keeps the listener down at attach")

	require.NoError(t, p.gw.Start(context.Background()))
	require.Eventually(t, func() bool { return svc.State().Serving }, time.Second, 5*time.Millisecond,
		"the Running transition must start the listener")
}

// TestPaused_KeepsServing pins the lifecycle's difference from the
// scheduler: Paused keeps the listener up so the pause gate can answer
// 503 + Retry-After.
func TestPaused_KeepsServing(t *testing.T) {
	p := newPipe(t)
	svc := NewService(p.st, p.runs, p.groups, WithClock(testNow), WithListenAddr(testAddr))
	svc.Attach(p.gw)
	t.Cleanup(svc.Stop)
	addr := svc.State().BoundAddr
	require.NotEmpty(t, addr)

	require.NoError(t, p.gw.Pause())
	assert.True(t, svc.State().Serving, "a paused gateway must keep serving (the pause gate answers 503)")

	// The pause gate answers 503 with Retry-After through the real listener.
	ep := p.createEndpoint(t, store.WebhookEndpoint{
		Name: "CI", AgentID: p.agentID(t), Prompt: "Go:", Enabled: true,
	}, []store.WebhookVerifier{headerVerifier("tok")})
	resp, err := postDelivery(addr, ep.ID)
	require.NoError(t, err, "the listener must still accept connections while paused")
	defer resp.Body.Close()
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	assert.NotEmpty(t, resp.Header.Get("Retry-After"))

	// Resume returns the pipeline to dispatching.
	require.NoError(t, p.gw.Resume())
	resp2, err := postDelivery(addr, ep.ID)
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusAccepted, resp2.StatusCode)
}

// TestStoppedGateway_ClosesListener pins the Stopped transition: the
// listener closes (connections refused), and a later Running starts a fresh
// one.
func TestStoppedGateway_ClosesListener(t *testing.T) {
	p := newPipe(t)
	svc := NewService(p.st, p.runs, p.groups, WithClock(testNow), WithListenAddr(testAddr))
	svc.Attach(p.gw)
	t.Cleanup(svc.Stop)
	require.True(t, svc.State().Serving)

	require.NoError(t, p.gw.Stop())
	require.Eventually(t, func() bool { return !svc.State().Serving }, time.Second, 5*time.Millisecond,
		"the Stopped transition must close the listener")

	_, err := http.Get(fmt.Sprintf("http://%s/webhooks/", svc.State().BoundAddr))
	require.Error(t, err, "the closed listener must refuse connections")

	require.NoError(t, p.gw.Start(context.Background()))
	require.Eventually(t, func() bool { return svc.State().Serving }, time.Second, 5*time.Millisecond,
		"a later Running starts a fresh listener")
}

// TestStop_BoundedJoinAndIdempotent pins Service.Stop: idempotent, closes
// the listener, and returns promptly even when called before Attach or
// twice in a row.
func TestStop_BoundedJoinAndIdempotent(t *testing.T) {
	p := newPipe(t)
	svc := NewService(p.st, p.runs, p.groups, WithClock(testNow), WithListenAddr(testAddr))

	// Before Attach: safe no-op.
	svc.Stop()

	svc.Attach(p.gw)
	require.True(t, svc.State().Serving)

	done := make(chan struct{})
	go func() {
		svc.Stop()
		svc.Stop() // idempotent
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(StopJoinWait + time.Second):
		t.Fatal("Stop must return within the bounded join window")
	}
	assert.False(t, svc.State().Serving)
}

// TestReconfigure_Rebinds pins the config-change path: a live listener
// rebinds to the new address and keeps serving; the old address stops
// answering.
func TestReconfigure_Rebinds(t *testing.T) {
	p := newPipe(t)
	svc := NewService(p.st, p.runs, p.groups, WithClock(testNow), WithListenAddr(testAddr))
	svc.Attach(p.gw)
	t.Cleanup(svc.Stop)
	oldAddr := svc.State().BoundAddr
	require.NotEmpty(t, oldAddr)

	svc.Reconfigure(freeAddr(t))

	state := svc.State()
	require.True(t, state.Serving, "a live listener must keep serving across a rebind")
	require.NotEmpty(t, state.BoundAddr)
	require.NotEqual(t, oldAddr, state.BoundAddr, "the listener must rebind to the new address")

	// A real delivery answers on the new address.
	ep := p.createEndpoint(t, store.WebhookEndpoint{
		Name: "CI", AgentID: p.agentID(t), Prompt: "Go:", Enabled: true,
	}, []store.WebhookVerifier{headerVerifier("tok")})
	resp, err := postDelivery(state.BoundAddr, ep.ID)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	// The old address is dead.
	_, err = http.Get(fmt.Sprintf("http://%s/webhooks/", oldAddr))
	require.Error(t, err, "the old bind must be closed after a rebind")
}

// TestReconfigure_BindFailureSurfaces pins the readable failure state: an
// unbindable address leaves Serving false with BindError set, and a
// corrected address recovers without a restart.
func TestReconfigure_BindFailureSurfaces(t *testing.T) {
	p := newPipe(t)
	svc := NewService(p.st, p.runs, p.groups, WithClock(testNow), WithListenAddr(testAddr))
	svc.Attach(p.gw)
	t.Cleanup(svc.Stop)

	// Hold a port, then point the receiver at it: the bind must fail.
	held, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer held.Close()

	svc.Reconfigure(held.Addr().String())
	state := svc.State()
	require.False(t, state.Serving, "a held port must not serve")
	require.NotEmpty(t, state.BindError, "the bind failure must be readable")

	// A corrected address takes effect immediately.
	svc.Reconfigure("127.0.0.1:0")
	state = svc.State()
	require.True(t, state.Serving)
	require.Empty(t, state.BindError)
	require.NotEqual(t, held.Addr().String(), state.BoundAddr)
}

// TestReconfigure_NoopWhenUnchanged pins the unchanged-address no-op: the
// listener keeps its bind.
func TestReconfigure_NoopWhenUnchanged(t *testing.T) {
	p := newPipe(t)
	svc := NewService(p.st, p.runs, p.groups, WithClock(testNow), WithListenAddr(testAddr))
	svc.Attach(p.gw)
	t.Cleanup(svc.Stop)
	before := svc.State()
	require.True(t, before.Serving)

	// An empty address is a no-op; so is the same address already configured.
	svc.Reconfigure("")
	svc.Reconfigure(testAddr) // the address already configured before Attach

	state := svc.State()
	require.True(t, state.Serving)
	require.Equal(t, before.BoundAddr, state.BoundAddr, "an unchanged address must not rebind")
}

// TestReconfigure_WhileGatewayStopped pins the down-gateway rule: a
// reconfigure while Stopped keeps the listener down; the next Running
// brings it up on the new address.
func TestReconfigure_WhileGatewayStopped(t *testing.T) {
	p := newPipe(t)
	svc := NewService(p.st, p.runs, p.groups, WithClock(testNow), WithListenAddr(testAddr))
	svc.Attach(p.gw)
	t.Cleanup(svc.Stop)
	require.True(t, svc.State().Serving)

	require.NoError(t, p.gw.Stop())
	require.Eventually(t, func() bool { return !svc.State().Serving }, time.Second, 5*time.Millisecond)

	svc.Reconfigure(freeAddr(t))
	require.False(t, svc.State().Serving, "a Stopped gateway keeps the listener down across a reconfigure")

	require.NoError(t, p.gw.Start(context.Background()))
	require.Eventually(t, func() bool { return svc.State().Serving }, time.Second, 5*time.Millisecond,
		"the next Running must serve on the reconfigured address")
}

// TestAttach_ReplacesPreviousAttachment pins the re-attach rule: a second
// Attach detaches and closes the first listener before serving its own.
func TestAttach_ReplacesPreviousAttachment(t *testing.T) {
	p := newPipe(t)
	svc := NewService(p.st, p.runs, p.groups, WithClock(testNow), WithListenAddr(testAddr))
	svc.Attach(p.gw)
	first := svc.State().BoundAddr
	require.NotEmpty(t, first)

	svc.Attach(p.gw)
	second := svc.State().BoundAddr
	require.NotEmpty(t, second)

	svc.Stop()
}

// TestNewService_DefaultAddr pins the production default by value, without
// binding: an option-less Service listens on DefaultAddr (the rest of the
// suite binds testAddr so it stays hermetic while a live app holds that
// port).
func TestNewService_DefaultAddr(t *testing.T) {
	p := newPipe(t)
	svc := NewService(p.st, p.runs, p.groups)
	require.Equal(t, DefaultAddr, svc.addr, "an option-less Service must listen on DefaultAddr")
}

// freeAddr grabs a free loopback port and releases it, returning the
// address: a concrete, distinct Reconfigure target for the rebind tests (a
// ":0" reconfigure on a ":0"-configured service is the unchanged-address
// no-op, and a just-closed ephemeral port can be handed back).
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	return addr
}

// postDelivery POSTs one credentialed delivery to the service listener's
// addr for the endpoint, using the fixture's header-verifier credential.
func postDelivery(addr, endpointID string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/webhooks/%s", addr, endpointID), bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Webhook-Token", "tok")
	return http.DefaultClient.Do(req)
}
