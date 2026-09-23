package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// ---------------------------------------------------------------------------
// Stubs: lister, credentials, recording provider, test recipes.
// ---------------------------------------------------------------------------

// stubConnectionLister answers ToolsFor with canned refs or an error.
type stubConnectionLister struct {
	refs []HTTPConnectionRef
	err  error
}

func (l *stubConnectionLister) AttachedHTTPConnections(context.Context, string, string) ([]HTTPConnectionRef, error) {
	return l.refs, l.err
}

// stubConnectionCreds models the CredentialForConnection seam: one raw token,
// per-connection failures, and a call record so tests can assert the
// resolution-time canary.
type stubConnectionCreds struct {
	token string
	errs  map[string]error

	mu    sync.Mutex
	calls []string
}

func (c *stubConnectionCreds) CredentialForConnection(_ context.Context, _, connectionID string) (string, error) {
	c.mu.Lock()
	c.calls = append(c.calls, connectionID)
	c.mu.Unlock()
	if err := c.errs[connectionID]; err != nil {
		return "", err
	}
	return c.token, nil
}

func (c *stubConnectionCreds) consulted() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.calls...)
}

// recordedCall is one provider request the stub server saw.
type recordedCall struct {
	method   string
	path     string
	rawQuery string
	header   http.Header
}

// recordingServer records every request, then hands the live writer and
// request to the handler so handlers can redirect, set headers, or stream.
type recordingServer struct {
	*httptest.Server

	mu    sync.Mutex
	calls []recordedCall
}

func (s *recordingServer) recorded() []recordedCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]recordedCall(nil), s.calls...)
}

func (s *recordingServer) hitCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func newRecordingServer(handler func(w http.ResponseWriter, r *http.Request)) *recordingServer {
	s := &recordingServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.calls = append(s.calls, recordedCall{
			method:   r.Method,
			path:     r.URL.Path,
			rawQuery: r.URL.RawQuery,
			header:   r.Header.Clone(),
		})
		s.mu.Unlock()
		if handler != nil {
			handler(w, r)
		}
	}))
	return s
}

// connectionTestRecipeSeq mints unique recipe ids: the domain registry panics
// on duplicate registration, and recipes are global process state.
var connectionTestRecipeSeq atomic.Int64

// registerConnectionTestRecipe registers a valid http-kind test recipe pinned
// to the given base URL (recipes are data — registering one in a test is
// legitimate), returning the registered copy. buildVerbs mints verbs under
// the generated id prefix.
func registerConnectionTestRecipe(t *testing.T, baseURL string, buildVerbs func(id string) []domain.RecipeVerb, mutate func(*domain.Recipe)) *domain.Recipe {
	t.Helper()
	id := fmt.Sprintf("connhttp-test-%d", connectionTestRecipeSeq.Add(1))
	r := domain.Recipe{
		ID:           id,
		Service:      "ConnHTTP Test " + id,
		Icon:         "plug",
		AuthKind:     domain.RecipeAuthPAT,
		Availability: domain.RecipeAvailable,
		Kind:         domain.RecipeKindHTTP,
		BaseURL:      baseURL,
		TokenHeader:  "X-Test-Token",
		AccessLevels: []string{domain.ConnectionAccessReadOnly},
		Steps:        []domain.RecipeStep{{Title: "Create a token", Detail: "Paste it into the connect dialog."}},
		Probe:        domain.RecipeProbe{Method: "GET", Path: "/ping"},
		Verbs:        buildVerbs(id),
	}
	if mutate != nil {
		mutate(&r)
	}
	domain.RegisterRecipe(r) // panics on an invalid recipe — the test fails loudly
	return domain.RecipeByID(id)
}

// connectionRef builds the attached ref for a registered test recipe.
func connectionRef(recipe *domain.Recipe) HTTPConnectionRef {
	return HTTPConnectionRef{
		ConnectionID: "conn-" + recipe.ID,
		Service:      recipe.ID,
		Status:       domain.ConnectionStatusConnected,
		Recipe:       recipe,
	}
}

// sourceWith resolves through a source wired to the stubs.
func sourceWith(refs []HTTPConnectionRef, creds *stubConnectionCreds) *ConnectionToolSource {
	return NewConnectionToolSource(&stubConnectionLister{refs: refs}, creds)
}

// resolveVerbs runs ToolsFor and fails the test on error.
func resolveVerbs(t *testing.T, refs []HTTPConnectionRef, creds *stubConnectionCreds, resolved []tool.BaseTool) []tool.BaseTool {
	t.Helper()
	out, _, err := sourceWith(refs, creds).ToolsFor(context.Background(), "ws-1", "agent-1", resolved)
	if err != nil {
		t.Fatalf("ToolsFor: %v", err)
	}
	return out
}

// invoke calls a resolved tool as the engine would.
func invoke(t *testing.T, tl tool.BaseTool, args string) (string, error) {
	t.Helper()
	invokable, ok := tl.(tool.InvokableTool)
	if !ok {
		t.Fatalf("resolved tool %T is not invokable", tl)
	}
	return invokable.InvokableRun(context.Background(), args)
}

// toolInfo fetches a resolved tool's schema.
func toolInfo(t *testing.T, tl tool.BaseTool) *schema.ToolInfo {
	t.Helper()
	info, err := tl.Info(context.Background())
	if err != nil {
		t.Fatalf("tool info: %v", err)
	}
	return info
}

// ---------------------------------------------------------------------------
// 2.1/2.2 — generation: one tool per declared verb, schema from the
// declaration, method/path pre-bound, names verbatim.
// ---------------------------------------------------------------------------

func TestConnectionToolSourceGeneratesDeclaredVerbTools(t *testing.T) {
	recipe := registerConnectionTestRecipe(t, "https://api.example.test", func(id string) []domain.RecipeVerb {
		return []domain.RecipeVerb{
			{
				Tool:   id + ".get_thing",
				Method: "GET",
				Path:   "/v1/things/{thing_id}",
				Params: []domain.RecipeVerbParam{
					{Name: "thing_id", Type: domain.RecipeParamString, Required: true, In: domain.RecipeParamInPath},
					{Name: "depth", Type: domain.RecipeParamNumber, Required: false, In: domain.RecipeParamInQuery},
					{Name: "verbose", Type: domain.RecipeParamBoolean, Required: false, In: domain.RecipeParamInQuery},
				},
				Description: "Get one thing by id.",
			},
			{
				Tool:        id + ".ping",
				Method:      "GET",
				Path:        "/v1/ping",
				Description: "Liveness probe verb with no parameters.",
			},
		}
	}, nil)

	resolved := resolveVerbs(t, []HTTPConnectionRef{connectionRef(recipe)}, &stubConnectionCreds{token: "tok"}, nil)
	if len(resolved) != 2 {
		t.Fatalf("resolved %d tools, want 2", len(resolved))
	}

	// Names verbatim (contract §5) — no renaming pass, no prefixing.
	if got := toolInfo(t, resolved[0]).Name; got != recipe.ID+".get_thing" {
		t.Fatalf("tool name = %q, want %q", got, recipe.ID+".get_thing")
	}
	if got := toolInfo(t, resolved[1]).Name; got != recipe.ID+".ping" {
		t.Fatalf("tool name = %q, want %q", got, recipe.ID+".ping")
	}
	if got := toolInfo(t, resolved[0]).Desc; got != "Get one thing by id." {
		t.Fatalf("description not carried verbatim: %q", got)
	}

	// Parameter schema generated from the declaration: typed, path params
	// required, optional query params optional.
	js, err := toolInfo(t, resolved[0]).ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatalf("to json schema: %v", err)
	}
	thing, ok := js.Properties.Get("thing_id")
	if !ok || thing.Type != "string" {
		t.Fatalf("thing_id schema wrong: present=%v type=%q", ok, thing.Type)
	}
	depth, ok := js.Properties.Get("depth")
	if !ok || depth.Type != "number" {
		t.Fatalf("depth schema wrong: present=%v type=%q", ok, depth.Type)
	}
	verbose, ok := js.Properties.Get("verbose")
	if !ok || verbose.Type != "boolean" {
		t.Fatalf("verbose schema wrong: present=%v type=%q", ok, verbose.Type)
	}
	if len(js.Required) != 1 || js.Required[0] != "thing_id" {
		t.Fatalf("required = %v, want [thing_id]", js.Required)
	}

	// Pre-binding (D1): method and template fix at generation; the pin is
	// computed, not per call.
	tl := resolved[0].(*connectionVerbTool)
	if tl.method != "GET" || tl.template != "/v1/things/{thing_id}" {
		t.Fatalf("pre-binding drifted: method %q template %q", tl.method, tl.template)
	}
	if tl.pin.host != "api.example.test" || tl.pin.root != "/v1/things/" {
		t.Fatalf("pin = %+v, want host api.example.test root /v1/things/", tl.pin)
	}
}

// ---------------------------------------------------------------------------
// 2.2 — bind-time validation at call time: traversal, absolute URLs,
// separators; rejections happen before any dial.
// ---------------------------------------------------------------------------

func TestConnectionVerbToolRejectsInjectedParamValues(t *testing.T) {
	recipe := registerConnectionTestRecipe(t, "https://api.example.test", func(id string) []domain.RecipeVerb {
		return []domain.RecipeVerb{{
			Tool:   id + ".get_thing",
			Method: "GET",
			Path:   "/v1/things/{thing_id}",
			Params: []domain.RecipeVerbParam{{Name: "thing_id", Type: domain.RecipeParamString, Required: true, In: domain.RecipeParamInPath}},
		}}
	}, nil)
	srv := newRecordingServer(nil)
	defer srv.Close()

	// The recipe is pinned to the live server so a wrongly-accepted value
	// would actually dial it.
	recipe = registerConnectionTestRecipe(t, srv.URL, func(id string) []domain.RecipeVerb {
		return []domain.RecipeVerb{{
			Tool:   id + ".get_thing",
			Method: "GET",
			Path:   "/v1/things/{thing_id}",
			Params: []domain.RecipeVerbParam{{Name: "thing_id", Type: domain.RecipeParamString, Required: true, In: domain.RecipeParamInPath}},
		}}
	}, nil)
	resolved := resolveVerbs(t, []HTTPConnectionRef{connectionRef(recipe)}, &stubConnectionCreds{token: "tok"}, nil)

	for name, value := range map[string]string{
		"traversal":         "../../etc/passwd",
		"dot-segment":       "..",
		"separator":         "a/b",
		"backslash":         `a\b`,
		"absolute-url":      "https://evil.example.test/x",
		"protocol-relative": "//evil.example.test/x",
		"scheme-prefixed":   "file:///etc/passwd",
	} {
		if _, err := invoke(t, resolved[0], fmt.Sprintf(`{"thing_id":%q}`, value)); err == nil {
			t.Fatalf("%s: value %q was accepted", name, value)
		}
	}
	// Missing required parameter is a bind error too.
	if _, err := invoke(t, resolved[0], `{}`); err == nil {
		t.Fatal("missing required parameter accepted")
	}
	// Rejections happen before any network request: the provider saw nothing.
	if got := srv.hitCount(); got != 0 {
		t.Fatalf("provider saw %d calls from rejected bindings, want 0", got)
	}
}

func TestConnectionVerbToolRejectsTypeMismatches(t *testing.T) {
	recipe := registerConnectionTestRecipe(t, "https://api.example.test", func(id string) []domain.RecipeVerb {
		return []domain.RecipeVerb{{
			Tool:   id + ".list_things",
			Method: "GET",
			Path:   "/v1/things",
			Params: []domain.RecipeVerbParam{
				{Name: "depth", Type: domain.RecipeParamNumber, Required: false, In: domain.RecipeParamInQuery},
				{Name: "verbose", Type: domain.RecipeParamBoolean, Required: false, In: domain.RecipeParamInQuery},
			},
		}}
	}, nil)

	resolved := resolveVerbs(t, []HTTPConnectionRef{connectionRef(recipe)}, &stubConnectionCreds{token: "tok"}, nil)
	if _, err := invoke(t, resolved[0], `{"depth":"five"}`); err == nil {
		t.Fatal("string accepted for a number parameter")
	}
	if _, err := invoke(t, resolved[0], `{"verbose":"yes"}`); err == nil {
		t.Fatal("string accepted for a boolean parameter")
	}
}

// ---------------------------------------------------------------------------
// 2.2/2.3 — the happy dial: declared method/path/query on the pinned base,
// auth header composed server-side per the TokenScheme rule.
// ---------------------------------------------------------------------------

func TestConnectionVerbToolDialsPinnedBaseWithComposedHeader(t *testing.T) {
	srv := newRecordingServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"comments": [], "saw_path": %q, "saw_query": %q}`, r.URL.Path, r.URL.RawQuery)
	})
	defer srv.Close()

	creds := &stubConnectionCreds{token: "raw-token-value"}
	bearer := registerConnectionTestRecipe(t, srv.URL, func(id string) []domain.RecipeVerb {
		return []domain.RecipeVerb{{
			Tool:   id + ".get_comments",
			Method: "GET",
			Path:   "/v1/files/{file_key}/comments",
			Params: []domain.RecipeVerbParam{
				{Name: "file_key", Type: domain.RecipeParamString, Required: true, In: domain.RecipeParamInPath},
				{Name: "depth", Type: domain.RecipeParamNumber, Required: false, In: domain.RecipeParamInQuery},
			},
		}}
	}, func(r *domain.Recipe) { r.TokenScheme = "Bearer" })

	resolved := resolveVerbs(t, []HTTPConnectionRef{connectionRef(bearer)}, creds, nil)
	out, err := invoke(t, resolved[0], `{"file_key":"abc123","depth":2}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}

	calls := srv.recorded()
	if len(calls) != 1 {
		t.Fatalf("provider saw %d calls, want 1", len(calls))
	}
	call := calls[0]
	if call.method != "GET" {
		t.Fatalf("method = %q, want GET", call.method)
	}
	if call.path != "/v1/files/abc123/comments" {
		t.Fatalf("bound path = %q", call.path)
	}
	if call.rawQuery != "depth=2" {
		t.Fatalf("query = %q, want depth=2", call.rawQuery)
	}
	// The credential rode the header, composed per the TokenScheme rule
	// (mirroring materializeServer) — server-side, never in the arguments.
	if got := call.header.Get("X-Test-Token"); got != "Bearer raw-token-value" {
		t.Fatalf("auth header = %q, want %q", got, "Bearer raw-token-value")
	}

	// The provider's JSON returns verbatim (capped) — no envelope.
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("response is not the provider's verbatim JSON: %v", err)
	}

	// Raw-scheme recipes (TokenScheme empty) send the raw token.
	raw := registerConnectionTestRecipe(t, srv.URL, func(id string) []domain.RecipeVerb {
		return []domain.RecipeVerb{{Tool: id + ".get_me", Method: "GET", Path: "/v1/me"}}
	}, nil)
	resolved = resolveVerbs(t, []HTTPConnectionRef{connectionRef(raw)}, creds, nil)
	if _, err := invoke(t, resolved[0], `{}`); err != nil {
		t.Fatalf("raw-scheme invoke: %v", err)
	}
	last := srv.recorded()[len(srv.recorded())-1]
	if got := last.header.Get("X-Test-Token"); got != "raw-token-value" {
		t.Fatalf("raw-scheme header = %q, want the raw token", got)
	}
}

// Base-path prefixes pin into the root: a base URL with a path prefix joins
// and re-validates correctly.
func TestConnectionVerbToolJoinsBasePathPrefix(t *testing.T) {
	srv := newRecordingServer(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok": true}`)
	})
	defer srv.Close()

	recipe := registerConnectionTestRecipe(t, srv.URL+"/api", func(id string) []domain.RecipeVerb {
		return []domain.RecipeVerb{{
			Tool:   id + ".get_thing",
			Method: "GET",
			Path:   "/v1/things/{thing_id}",
			Params: []domain.RecipeVerbParam{{Name: "thing_id", Type: domain.RecipeParamString, Required: true, In: domain.RecipeParamInPath}},
		}}
	}, nil)

	resolved := resolveVerbs(t, []HTTPConnectionRef{connectionRef(recipe)}, &stubConnectionCreds{token: "tok"}, nil)
	if _, err := invoke(t, resolved[0], `{"thing_id":"abc"}`); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if got := srv.recorded()[0].path; got != "/api/v1/things/abc" {
		t.Fatalf("bound path = %q, want /api/v1/things/abc", got)
	}
}

// The dial base resolves from the connection's STORED origin
// (add-recipe-base-url tasks.md 2.3; the C2b-flagged runtime gap): a
// parametrized recipe's ref carries its origin and the runtime verb call
// dials it — never the recipe's pinned default. Every earlier test doubles
// as the non-parametrized regression: an empty ref.Origin resolves
// byte-identically to the declared BaseURL. A stored origin that no longer
// parses degrades skip-and-mark instead of dialing anything.
func TestConnectionVerbToolDialsStoredOriginNotRecipeDefault(t *testing.T) {
	defaultSrv := newRecordingServer(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("the recipe default origin was dialed instead of the connection's stored origin")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"wrong": true}`)
	})
	defer defaultSrv.Close()

	srv := newRecordingServer(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"stored_origin": true}`)
	})
	defer srv.Close()

	recipe := registerConnectionTestRecipe(t, defaultSrv.URL, func(id string) []domain.RecipeVerb {
		return []domain.RecipeVerb{{Tool: id + ".get_me", Method: "GET", Path: "/v1/me"}}
	}, func(r *domain.Recipe) {
		r.OriginParam = &domain.RecipeOriginParam{
			Name:    "Instance URL",
			Default: defaultSrv.URL,
			Help:    "The provider instance's origin (SaaS or self-managed).",
		}
	})

	ref := connectionRef(recipe)
	ref.Origin = srv.URL
	resolved := resolveVerbs(t, []HTTPConnectionRef{ref}, &stubConnectionCreds{token: "tok"}, nil)
	out, err := invoke(t, resolved[0], `{}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if !strings.Contains(out, `"stored_origin"`) {
		t.Fatalf("expected the stored origin to serve the call, got %q", out)
	}
	if got := srv.hitCount(); got != 1 {
		t.Fatalf("stored origin saw %d calls, want 1", got)
	}

	brokenRef := connectionRef(recipe)
	brokenRef.Origin = "not-an-origin"
	degraded, _, err := sourceWith([]HTTPConnectionRef{brokenRef}, &stubConnectionCreds{token: "tok"}).
		ToolsFor(context.Background(), "ws-1", "agent-1", nil)
	if err != nil {
		t.Fatalf("resolution must degrade, not fail: %v", err)
	}
	if len(degraded) != 0 {
		t.Fatalf("broken-origin connection contributed %d tools", len(degraded))
	}
	if defaultSrv.hitCount() != 0 {
		t.Fatalf("the recipe default origin saw %d calls, want 0", defaultSrv.hitCount())
	}
}

// ---------------------------------------------------------------------------
// 2.2 — off-host redirects refused; same-host redirects followed.
// ---------------------------------------------------------------------------

func TestConnectionVerbToolRefusesOffHostRedirect(t *testing.T) {
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("off-host redirect target was contacted")
		w.WriteHeader(http.StatusOK)
	}))
	defer evil.Close()

	srv := newRecordingServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/old" {
			http.Redirect(w, r, evil.URL+"/steal", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok": true}`)
	})
	defer srv.Close()

	recipe := registerConnectionTestRecipe(t, srv.URL, func(id string) []domain.RecipeVerb {
		return []domain.RecipeVerb{{Tool: id + ".get_old", Method: "GET", Path: "/v1/old"}}
	}, nil)

	resolved := resolveVerbs(t, []HTTPConnectionRef{connectionRef(recipe)}, &stubConnectionCreds{token: "tok"}, nil)
	if _, err := invoke(t, resolved[0], `{}`); err == nil {
		t.Fatal("off-host redirect was followed")
	} else if !strings.Contains(err.Error(), "refused redirect to a different host") {
		t.Fatalf("error does not name the off-host refusal: %v", err)
	}
}

func TestConnectionVerbToolFollowsSameHostRedirect(t *testing.T) {
	srv := newRecordingServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/old" {
			http.Redirect(w, r, "/v1/new", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"moved": true}`)
	})
	defer srv.Close()

	recipe := registerConnectionTestRecipe(t, srv.URL, func(id string) []domain.RecipeVerb {
		return []domain.RecipeVerb{{Tool: id + ".get_old", Method: "GET", Path: "/v1/old"}}
	}, nil)

	resolved := resolveVerbs(t, []HTTPConnectionRef{connectionRef(recipe)}, &stubConnectionCreds{token: "tok"}, nil)
	out, err := invoke(t, resolved[0], `{}`)
	if err != nil {
		t.Fatalf("same-host redirect should be followed: %v", err)
	}
	if !strings.Contains(out, `"moved"`) {
		t.Fatalf("did not land on the redirect target: %q", out)
	}
	if got := srv.hitCount(); got != 2 {
		t.Fatalf("provider saw %d calls, want 2 (original + redirect)", got)
	}
}

// ---------------------------------------------------------------------------
// 2.3 — credential hygiene: the token never reaches schema, description, or
// any error path, even on failures.
// ---------------------------------------------------------------------------

func TestConnectionVerbToolCredentialHygiene(t *testing.T) {
	const token = "super-secret-token-value-0987654321"

	srv := newRecordingServer(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"detail": "upstream exploded"}`)
	})
	defer srv.Close()

	recipe := registerConnectionTestRecipe(t, srv.URL, func(id string) []domain.RecipeVerb {
		return []domain.RecipeVerb{{
			Tool:        id + ".get_thing",
			Method:      "GET",
			Path:        "/v1/things/{thing_id}",
			Params:      []domain.RecipeVerbParam{{Name: "thing_id", Type: domain.RecipeParamString, Required: true, In: domain.RecipeParamInPath}},
			Description: "Get one thing by id.",
		}}
	}, nil)

	resolved := resolveVerbs(t, []HTTPConnectionRef{connectionRef(recipe)}, &stubConnectionCreds{token: token}, nil)

	// Schema and description carry no credential material.
	info := toolInfo(t, resolved[0])
	for _, surface := range []string{info.Name, info.Desc} {
		if strings.Contains(surface, token) {
			t.Fatal("credential material leaked into the tool schema surface")
		}
	}

	// The 500 path: status code + provider message only — no token, no
	// headers, even though the token rode the failing request's header.
	_, err := invoke(t, resolved[0], `{"thing_id":"abc"}`)
	if err == nil {
		t.Fatal("500 did not surface as a tool error")
	}
	if !strings.Contains(err.Error(), "status 500") || !strings.Contains(err.Error(), "upstream exploded") {
		t.Fatalf("provider error missing status/message: %v", err)
	}
	if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "X-Test-Token") {
		t.Fatalf("credential or header material leaked into the error: %v", err)
	}
	if got := srv.recorded()[0].header.Get("X-Test-Token"); got != token {
		t.Fatalf("credential was not injected server-side on the failing call: %q", got)
	}

	// A failing credential resolution degrades at assembly instead of
	// contributing tools.
	ref := connectionRef(recipe)
	degraded, _, err := sourceWith([]HTTPConnectionRef{ref}, &stubConnectionCreds{
		errs: map[string]error{ref.ConnectionID: errors.New("decrypt failed")},
	}).ToolsFor(context.Background(), "ws-1", "agent-1", nil)
	if err != nil {
		t.Fatalf("credential failure must degrade, not fail: %v", err)
	}
	if len(degraded) != 0 {
		t.Fatalf("credential-failed connection contributed %d tools", len(degraded))
	}
}

// ---------------------------------------------------------------------------
// 2.4 — response discipline mirrors the web fetch tool exactly.
// ---------------------------------------------------------------------------

func TestConnectionResponseDisciplineMirrorsWebFetch(t *testing.T) {
	big := `{"blob": "` + strings.Repeat("x", 2<<20) + `"}`
	srv := newRecordingServer(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/big":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, big)
		case "/binary":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte{0x00, 0x01, 0x02})
		case "/image":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte{0x89, 0x50})
		case "/text":
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			fmt.Fprint(w, "plain text answer")
		case "/json-suffix":
			w.Header().Set("Content-Type", "application/vnd.api+json")
			fmt.Fprint(w, `{"kind": "suffix"}`)
		case "/empty":
			w.Header().Set("Content-Type", "application/json")
		}
	})
	defer srv.Close()

	paths := []string{"/big", "/binary", "/image", "/text", "/json-suffix", "/empty"}
	recipe := registerConnectionTestRecipe(t, srv.URL, func(id string) []domain.RecipeVerb {
		verbs := make([]domain.RecipeVerb, 0, len(paths))
		for _, p := range paths {
			verbs = append(verbs, domain.RecipeVerb{
				Tool:   id + ".call_" + strings.Trim(p, "/"),
				Method: "GET",
				Path:   p,
			})
		}
		return verbs
	}, nil)

	resolved := resolveVerbs(t, []HTTPConnectionRef{connectionRef(recipe)}, &stubConnectionCreds{token: "tok"}, nil)
	byName := make(map[string]tool.BaseTool, len(resolved))
	for _, tl := range resolved {
		byName[toolInfo(t, tl).Name] = tl
	}

	// Oversized body: capped at exactly 1 MiB plus the exact web-fetch
	// truncation marker.
	out, err := invoke(t, byName[recipe.ID+".call_big"], `{}`)
	if err != nil {
		t.Fatalf("big invoke: %v", err)
	}
	if len(out) != maxConnectionResponseBytes+len(connectionTruncationMarker) {
		t.Fatalf("capped output = %d bytes, want %d", len(out), maxConnectionResponseBytes+len(connectionTruncationMarker))
	}
	if !strings.HasSuffix(out, connectionTruncationMarker) {
		t.Fatalf("truncation marker missing/mismatched: suffix %q", out[len(out)-64:])
	}

	// Binary and image content are refused; JSON, +json, and text pass.
	for _, refused := range []string{".call_binary", ".call_image"} {
		if _, err := invoke(t, byName[recipe.ID+refused], `{}`); err == nil || !strings.Contains(err.Error(), "neither JSON nor text") {
			t.Fatalf("%s not gated: %v", refused, err)
		}
	}
	if out, err := invoke(t, byName[recipe.ID+".call_text"], `{}`); err != nil || out != "plain text answer" {
		t.Fatalf("text response = %q, err %v", out, err)
	}
	if out, err := invoke(t, byName[recipe.ID+".call_json-suffix"], `{}`); err != nil || !strings.Contains(out, "suffix") {
		t.Fatalf("+json response = %q, err %v", out, err)
	}

	// Empty 2xx body mirrors the web fetch rule.
	if _, err := invoke(t, byName[recipe.ID+".call_empty"], `{}`); err == nil || !strings.Contains(err.Error(), "empty response body") {
		t.Fatalf("empty body = %v, want the mirrored empty-body error", err)
	}
}

func TestConnectionContentTypeGate(t *testing.T) {
	for _, allowed := range []string{"application/json", "application/vnd.api+json", "text/plain", "text/plain; charset=utf-8", "TEXT/HTML"} {
		if !connectionContentTypeAllowed(allowed) {
			t.Fatalf("%q should pass the gate", allowed)
		}
	}
	for _, refused := range []string{"", "application/octet-stream", "image/png", "application/x-www-form-urlencoded"} {
		if connectionContentTypeAllowed(refused) {
			t.Fatalf("%q should be refused by the gate", refused)
		}
	}
}

// ---------------------------------------------------------------------------
// Pin re-validation unit coverage (D2): the joined URL is re-validated
// structurally before any dial.
// ---------------------------------------------------------------------------

func TestValidatePinnedURL(t *testing.T) {
	pin := connectionPin{scheme: "https", host: "api.example.test", root: "/v1/things"}
	for name, urlStr := range map[string]string{
		"inside root":       "https://api.example.test/v1/things/abc/comments",
		"exact root":        "https://api.example.test/v1/things",
		"literal extension": "https://api.example.test/v1/thingsx",
		"with query":        "https://api.example.test/v1/things/abc?depth=2",
	} {
		u, err := url.Parse(urlStr)
		if err != nil {
			t.Fatalf("%s: parse: %v", name, err)
		}
		if err := validatePinnedURL(u, pin); err != nil {
			t.Fatalf("%s: rejected %q: %v", name, urlStr, err)
		}
	}
	for name, urlStr := range map[string]string{
		"scheme change": "http://api.example.test/v1/things/abc",
		"host change":   "https://evil.example.test/v1/things/abc",
		"escaped root":  "https://api.example.test/other/things/abc",
		"traversal":     "https://api.example.test/v1/../etc/passwd",
	} {
		u, err := url.Parse(urlStr)
		if err != nil {
			t.Fatalf("%s: parse: %v", name, err)
		}
		if err := validatePinnedURL(u, pin); err == nil {
			t.Fatalf("%s: accepted %q", name, urlStr)
		}
	}
}

// ---------------------------------------------------------------------------
// 2.5 — degradation: unresolvable credential, non-connected status,
// malformed refs, and name collisions all skip-and-mark; the run proceeds.
// ---------------------------------------------------------------------------

func TestConnectionToolSourceDegradesUnresolvableCredential(t *testing.T) {
	healthy := registerConnectionTestRecipe(t, "https://api.example.test", func(id string) []domain.RecipeVerb {
		return []domain.RecipeVerb{{Tool: id + ".healthy", Method: "GET", Path: "/v1/healthy"}}
	}, nil)
	broken := registerConnectionTestRecipe(t, "https://api.example.test", func(id string) []domain.RecipeVerb {
		return []domain.RecipeVerb{{Tool: id + ".broken", Method: "GET", Path: "/v1/broken"}}
	}, nil)

	creds := &stubConnectionCreds{token: "tok", errs: map[string]error{
		connectionRef(broken).ConnectionID: errors.New("decrypt failed"),
	}}
	resolved := resolveVerbs(t, []HTTPConnectionRef{connectionRef(healthy), connectionRef(broken)}, creds, nil)
	if len(resolved) != 1 || toolInfo(t, resolved[0]).Name != healthy.ID+".healthy" {
		names := make([]string, 0, len(resolved))
		for _, tl := range resolved {
			names = append(names, toolInfo(t, tl).Name)
		}
		t.Fatalf("resolved = %v, want only the healthy connection's verb", names)
	}
	// The canary consulted both connections at resolution time.
	if got := creds.consulted(); len(got) != 2 {
		t.Fatalf("credential canary consulted %v, want both connections", got)
	}
}

func TestConnectionToolSourceSkipsNonConnectedAndMalformed(t *testing.T) {
	connected := registerConnectionTestRecipe(t, "https://api.example.test", func(id string) []domain.RecipeVerb {
		return []domain.RecipeVerb{{Tool: id + ".connected", Method: "GET", Path: "/v1/connected"}}
	}, nil)
	errored := registerConnectionTestRecipe(t, "https://api.example.test", func(id string) []domain.RecipeVerb {
		return []domain.RecipeVerb{{Tool: id + ".errored", Method: "GET", Path: "/v1/errored"}}
	}, nil)
	legacy := registerConnectionTestRecipe(t, "https://api.example.test", func(id string) []domain.RecipeVerb {
		return []domain.RecipeVerb{{Tool: id + ".legacy", Method: "GET", Path: "/v1/legacy"}}
	}, nil)

	// Empty status normalizes to connected (the stored-shape rule); an error
	// status and a malformed ref (no recipe) degrade.
	legacyRef := connectionRef(legacy)
	legacyRef.Status = ""
	erroredRef := connectionRef(errored)
	erroredRef.Status = domain.ConnectionStatusError
	malformed := HTTPConnectionRef{ConnectionID: "conn-orphan", Service: "ghost", Status: domain.ConnectionStatusConnected}

	resolved := resolveVerbs(t,
		[]HTTPConnectionRef{connectionRef(connected), erroredRef, legacyRef, malformed},
		&stubConnectionCreds{token: "tok"}, nil)
	if len(resolved) != 2 {
		names := make([]string, 0, len(resolved))
		for _, tl := range resolved {
			names = append(names, toolInfo(t, tl).Name)
		}
		t.Fatalf("resolved = %v, want the connected + legacy-empty verbs only", names)
	}
}

func TestConnectionToolSourceSkipsCollidingVerbName(t *testing.T) {
	recipe := registerConnectionTestRecipe(t, "https://api.example.test", func(id string) []domain.RecipeVerb {
		return []domain.RecipeVerb{
			{Tool: id + ".collides", Method: "GET", Path: "/v1/collides"},
			{Tool: id + ".unique", Method: "GET", Path: "/v1/unique"},
		}
	}, nil)

	alreadyResolved := []tool.BaseTool{namedStubTool{raw: recipe.ID + ".collides"}}
	resolved := resolveVerbs(t, []HTTPConnectionRef{connectionRef(recipe)}, &stubConnectionCreds{token: "tok"}, alreadyResolved)
	if len(resolved) != 1 || toolInfo(t, resolved[0]).Name != recipe.ID+".unique" {
		t.Fatalf("collision handling resolved %d tools, want only the unique verb", len(resolved))
	}
}

// Undeclared-operation impossibility (spec: "Undeclared operation is
// impossible"): the resolved surface is exactly the declared verb list — no
// free-form request tool exists.
func TestConnectionToolSourceSurfaceIsExactlyDeclaredVerbs(t *testing.T) {
	recipe := registerConnectionTestRecipe(t, "https://api.example.test", func(id string) []domain.RecipeVerb {
		return []domain.RecipeVerb{
			{Tool: id + ".alpha", Method: "GET", Path: "/v1/alpha"},
			{Tool: id + ".beta", Method: "GET", Path: "/v1/beta/{b}", Params: []domain.RecipeVerbParam{{Name: "b", Type: domain.RecipeParamString, Required: true, In: domain.RecipeParamInPath}}},
		}
	}, nil)

	resolved := resolveVerbs(t, []HTTPConnectionRef{connectionRef(recipe)}, &stubConnectionCreds{token: "tok"}, nil)
	got := make(map[string]bool, len(resolved))
	for _, tl := range resolved {
		got[toolInfo(t, tl).Name] = true
	}
	if len(got) != 2 || !got[recipe.ID+".alpha"] || !got[recipe.ID+".beta"] {
		t.Fatalf("surface = %v, want exactly the declared verbs", got)
	}
	for name := range got {
		if !strings.HasPrefix(name, recipe.ID+".") {
			t.Fatalf("non-verb tool on the surface: %q", name)
		}
	}
}

func TestConnectionToolSourceListerErrorFailsResolution(t *testing.T) {
	_, _, err := NewConnectionToolSource(
		&stubConnectionLister{err: errors.New("store down")},
		&stubConnectionCreds{token: "tok"},
	).ToolsFor(context.Background(), "ws-1", "agent-1", nil)
	if err == nil || !strings.Contains(err.Error(), "store down") {
		t.Fatalf("lister failure must fail the resolution like any store error: %v", err)
	}
}

func TestConnectionToolSourceEmptyAttachmentList(t *testing.T) {
	resolved, _, err := sourceWith(nil, &stubConnectionCreds{token: "tok"}).ToolsFor(
		context.Background(), "ws-1", "agent-1", nil)
	if err != nil || len(resolved) != 0 {
		t.Fatalf("no attachments = (%v, %d), want (nil, 0)", err, len(resolved))
	}
}

// ---------------------------------------------------------------------------
// Runner pass: connection tools append after built-ins and MCP tools; the
// option wires the source; the default contributes nothing.
// ---------------------------------------------------------------------------

func TestRunnerResolveConnectionToolsAfterBuiltinsAndMCP(t *testing.T) {
	runner, ws, ag, req := setupMCPRunner(t, []string{"web.fetch"})

	recipe := registerConnectionTestRecipe(t, "https://api.example.test", func(id string) []domain.RecipeVerb {
		return []domain.RecipeVerb{
			{Tool: id + ".get_me", Method: "GET", Path: "/v1/me"},
			{Tool: id + ".get_file", Method: "GET", Path: "/v1/files/{file_key}",
				Params: []domain.RecipeVerbParam{{Name: "file_key", Type: domain.RecipeParamString, Required: true, In: domain.RecipeParamInPath}}},
		}
	}, nil)

	policy := &mcpStubPolicy{workspace: map[string][]domain.WorkspaceMCPServer{
		ws.ID: {wsServer(ws.ID, "srv-gh", "github", true, "", "", 0)},
	}}
	manager := &mcpStubManager{tools: map[string][]tool.BaseTool{
		"srv-gh": {namedStubTool{"create_issue"}},
	}}
	ag.EnabledMCPS = []string{"srv-gh"}
	runner.mcpPolicy, runner.mcpManager, runner.mcpStatus = policy, manager, &mcpStubStatus{}

	// The pinned seam wires through the field the option sets (the option
	// function itself is covered below).
	runner.connectionSource = NewConnectionToolSource(
		&stubConnectionLister{refs: []HTTPConnectionRef{connectionRef(recipe)}},
		&stubConnectionCreds{token: "tok"},
	)

	_, tools, err := runner.resolve(context.Background(), req, ws, ag, &domain.Role{Permissions: domain.OwnerPermissions})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	names := toolNamesOf(t, tools)
	want := []string{"web.fetch", "mcp__github__create_issue", recipe.ID + ".get_me", recipe.ID + ".get_file"}
	if len(names) != len(want) {
		t.Fatalf("resolved = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("resolved = %v, want %v (order: built-ins, MCP, connection verbs)", names, want)
		}
	}
}

func TestWithConnectionToolSourceOption(t *testing.T) {
	src := NewConnectionToolSource(&stubConnectionLister{}, &stubConnectionCreds{token: "tok"})

	r := &Runner{}
	WithConnectionToolSource(src)(r)
	if r.connectionSource != src {
		t.Fatal("option did not wire the source")
	}

	// A nil source never replaces a wired one (non-nil injected deps).
	WithConnectionToolSource(nil)(r)
	if r.connectionSource != src {
		t.Fatal("nil option replaced a wired source")
	}
}

func TestRunnerResolveConnectionToolsDefaultNoop(t *testing.T) {
	runner, ws, ag, req := setupMCPRunner(t, nil)
	_, tools, err := runner.resolve(context.Background(), req, ws, ag, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got := toolNamesOf(t, tools); len(got) != 0 {
		t.Fatalf("unwired runner resolved %v connection tools", got)
	}
}
