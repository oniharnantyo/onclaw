package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// hookTestServer records the received request and replies with a canned body.
type hookTestServer struct {
	srv      *httptest.Server
	gotBody  map[string]any
	gotReq   *http.Request
	gotAuth  string
	gotEvent string
	gotDeliv string
}

func newHookTestServer(t *testing.T, status int, body string) *hookTestServer {
	t.Helper()
	hs := &hookTestServer{}
	hs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(raw, &payload)
		hs.gotBody = payload
		hs.gotReq = r
		hs.gotAuth = r.Header.Get("Authorization")
		hs.gotEvent = r.Header.Get("X-Onclaw-Event")
		hs.gotDeliv = r.Header.Get("X-Onclaw-Delivery")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(hs.srv.Close)
	return hs
}

func testEvent() Event {
	return Event{
		Event:      "pre_tool_use",
		DeliveryID: "delivery-42",
		Origin:     "user",
		Workspace:  EventRef{ID: "ws-1", Name: "Acme"},
		Agent:      EventRef{ID: "ag-1", Name: "Atlas"},
		SessionID:  "sess-1",
		Tool:       &EventTool{Name: "shell.run", CallID: "call-1", Args: `{"cmd":"rm -rf /"}`},
	}
}

func testHook() HookRef {
	return HookRef{Name: "guard", Level: domain.HookLevelWorkspace}
}

func registryForTestServer(srv *httptest.Server) *Registry {
	return NewRegistry(
		WithHTTPClient(srv.Client()),
		WithHTTPAllowPrivate(true),
	)
}

func httpCfg(url string) json.RawMessage {
	return json.RawMessage(`{"url":` + mustJSONString(url) + `,"headers":[{"name":"Authorization","value":"Bearer tok-123"}]}`)
}

func mustJSONString(s string) string {
	raw, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func TestHTTPHandler_BlockHonored(t *testing.T) {
	hs := newHookTestServer(t, http.StatusOK, `{"decision":"block","reason":"deployments are frozen"}`)
	reg := registryForTestServer(hs.srv)

	res, err := reg.Execute(context.Background(), domain.HookHandlerHTTP, httpCfg(hs.srv.URL), testEvent(), testHook(), 5*time.Second)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Decision != "block" || res.Reason != "deployments are frozen" {
		t.Errorf("result = %+v, want block with reason", res)
	}
	if res.HTTPStatus == nil || *res.HTTPStatus != 200 {
		t.Errorf("HTTPStatus = %v, want 200", res.HTTPStatus)
	}
	if hs.gotEvent != "pre_tool_use" {
		t.Errorf("X-Onclaw-Event = %q, want pre_tool_use", hs.gotEvent)
	}
	if hs.gotDeliv != "delivery-42" {
		t.Errorf("X-Onclaw-Delivery = %q, want delivery-42", hs.gotDeliv)
	}
	if hs.gotAuth != "Bearer tok-123" {
		t.Errorf("Authorization = %q, want configured header applied", hs.gotAuth)
	}
	if hs.gotReq.Method != http.MethodPost {
		t.Errorf("method = %q, want POST", hs.gotReq.Method)
	}
	if ct := hs.gotReq.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if hs.gotBody["delivery_id"] != "delivery-42" {
		t.Errorf("event body not the JSON payload: %v", hs.gotBody)
	}
}

func TestHTTPHandler_NonJSON2xxMeansAllow(t *testing.T) {
	for _, body := range []string{"", "ok", "<html>accepted</html>", `{"status":"received"}`} {
		hs := newHookTestServer(t, http.StatusOK, body)
		reg := registryForTestServer(hs.srv)
		res, err := reg.Execute(context.Background(), domain.HookHandlerHTTP, httpCfg(hs.srv.URL), testEvent(), testHook(), 5*time.Second)
		if err != nil {
			t.Fatalf("Execute with body %q: %v", body, err)
		}
		if res.Decision != "allow" {
			t.Errorf("body %q: decision = %q, want allow", body, res.Decision)
		}
	}
}

func TestHTTPHandler_AllowDecisionCarriesReason(t *testing.T) {
	hs := newHookTestServer(t, http.StatusAccepted, `{"decision":"allow","reason":"within quota"}`)
	reg := registryForTestServer(hs.srv)

	res, err := reg.Execute(context.Background(), domain.HookHandlerHTTP, httpCfg(hs.srv.URL), testEvent(), testHook(), 5*time.Second)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Decision != "allow" || res.Reason != "within quota" {
		t.Errorf("result = %+v, want allow with reason", res)
	}
}

func TestHTTPHandler_Non2xxIsFailure(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusNotFound, http.StatusForbidden} {
		hs := newHookTestServer(t, status, `{"decision":"allow"}`)
		reg := registryForTestServer(hs.srv)
		res, err := reg.Execute(context.Background(), domain.HookHandlerHTTP, httpCfg(hs.srv.URL), testEvent(), testHook(), 5*time.Second)
		if err == nil {
			t.Errorf("status %d: want failure error", status)
			continue
		}
		// A non-2xx is a delivery failure even if the body claims allow.
		if res.HTTPStatus == nil || *res.HTTPStatus != status {
			t.Errorf("status %d: HTTPStatus = %v, want %d", status, res.HTTPStatus, status)
		}
	}
}

func TestHTTPHandler_TransportErrorIsFailure(t *testing.T) {
	hs := newHookTestServer(t, http.StatusOK, "ok")
	client := hs.srv.Client()
	url := hs.srv.URL
	hs.srv.Close() // nothing listens anymore

	reg := NewRegistry(WithHTTPClient(client), WithHTTPAllowPrivate(true))
	if _, err := reg.Execute(context.Background(), domain.HookHandlerHTTP, httpCfg(url), testEvent(), testHook(), 5*time.Second); err == nil {
		t.Error("delivery to a dead endpoint must fail")
	}
}

func TestHTTPHandler_BudgetExceededIsFailure(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(func() { close(release); srv.Close() })

	reg := registryForTestServer(srv)
	_, err := reg.Execute(context.Background(), domain.HookHandlerHTTP, httpCfg(srv.URL), testEvent(), testHook(), 50*time.Millisecond)
	if err == nil {
		t.Fatal("execution over budget must fail")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "deadline") && !strings.Contains(err.Error(), "Client.Timeout") {
		t.Errorf("budget error should indicate timeout, got: %v", err)
	}
}

// TestHTTPHandler_SSFGuardBlocksLoopbackOnDefaultPath exercises the shared
// outbound-fetch guard through the production default client path: a loopback
// URL must error without a delivery ever being attempted.
func TestHTTPHandler_SSFGuardBlocksLoopbackOnDefaultPath(t *testing.T) {
	reg := NewRegistry() // production defaults: guarded client, no private allowance
	for _, target := range []string{
		"http://127.0.0.1:8080/hook",
		"http://[::1]/hook",
		"http://10.0.0.5/hook",
		"http://192.168.1.10/hook",
		"http://169.254.169.254/latest/meta-data",
		"file:///etc/passwd",
	} {
		_, err := reg.Execute(context.Background(), domain.HookHandlerHTTP, httpCfg(target), testEvent(), testHook(), 5*time.Second)
		if err == nil {
			t.Errorf("target %s must be blocked by the outbound guard", target)
			continue
		}
		if !strings.Contains(err.Error(), "non-public") && !strings.Contains(err.Error(), "only http and https") {
			t.Errorf("target %s: unexpected guard error: %v", target, err)
		}
	}
}

// TestHTTPHandler_DefaultClientIsGuarded pins that the registry's production
// default client is the shared guarded one (per-hop IP validation + redirect
// re-validation), not a bare http.Client.
func TestHTTPHandler_DefaultClientIsGuarded(t *testing.T) {
	reg := NewRegistry()
	if reg.httpClient == http.DefaultClient {
		t.Fatal("default client must be the SSRF-guarded client")
	}
	if reg.httpClient.CheckRedirect == nil {
		t.Fatal("guarded client must re-validate redirect hops")
	}
}

// TestRegistry_OpensSealedConfigForHandlers pins the contract that handlers
// always receive decrypted config: a sealed Authorization header reaches the
// webhook as plaintext after the registry opens it with the workspace AAD.
func TestRegistry_OpensSealedConfigForHandlers(t *testing.T) {
	hs := newHookTestServer(t, http.StatusOK, "ok")
	key := testKey(t)
	reg := NewRegistry(
		WithHTTPClient(hs.srv.Client()),
		WithHTTPAllowPrivate(true),
		WithEncryptionKey(key),
	)

	plain := json.RawMessage(`{"url":` + mustJSONString(hs.srv.URL) + `,"headers":[{"name":"Authorization","value":"Bearer sealed-secret"}]}`)
	sealed, err := SealHookConfig(key, "ws-1", plain)
	if err != nil {
		t.Fatalf("SealHookConfig: %v", err)
	}
	if strings.Contains(string(sealed), "sealed-secret") {
		t.Fatal("config was not actually sealed")
	}

	ev := testEvent()
	ev.Workspace.ID = "ws-1"
	if _, err := reg.Execute(context.Background(), domain.HookHandlerHTTP, sealed, ev, testHook(), 5*time.Second); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if hs.gotAuth != "Bearer sealed-secret" {
		t.Errorf("handler received %q, want decrypted header", hs.gotAuth)
	}
}
