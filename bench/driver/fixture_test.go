package driver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeOnclaw is a stateful fake of the OnClaw API surface Provision touches:
// login, admin users, workspace, roles, members, providers, agents, chat-key
// exchange. State evolves across requests so create-or-reuse idempotency is
// exercised for real, not mocked per-call.
type fakeOnclaw struct {
	mu        sync.Mutex
	users     map[string]string // email -> accepted password
	wsSlug    string            // "" = workspace absent
	members   map[string]bool
	providers []map[string]any
	agent     map[string]any // nil = absent

	workspaceCreates int
	providerCreates  int
	agentCreates     int
	patches          int
	chatKeyExchanges int

	lastProviderCreate map[string]any
	lastAgentCreate    map[string]any
	lastPatch          map[string]any
}

func newFakeOnclaw(adminEmail, adminPassword string) *fakeOnclaw {
	return &fakeOnclaw{
		users:   map[string]string{adminEmail: adminPassword},
		members: map[string]bool{},
	}
}

func (f *fakeOnclaw) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	path := r.URL.Path
	switch {
	case r.Method == http.MethodPost && path == "/api/v1/auth/login":
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if pw, ok := f.users[body["email"]]; ok && pw == body["password"] {
			writeJSON(w, http.StatusOK, map[string]any{
				"token": "tok-" + body["email"],
				"user":  map[string]any{"id": "u-" + body["email"], "email": body["email"]},
			})
			return
		}
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid credentials"})

	case r.Method == http.MethodPost && path == "/api/v1/admin/users":
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, exists := f.users[body["email"]]; exists {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "exists"})
			return
		}
		f.users[body["email"]] = body["password"]
		writeJSON(w, http.StatusCreated, map[string]any{
			"user": map[string]any{"id": "u-" + body["email"], "email": body["email"], "name": body["name"]},
		})

	case r.Method == http.MethodGet && path == "/api/v1/workspaces/"+f.wsSlug && f.wsSlug != "":
		writeJSON(w, http.StatusOK, map[string]any{
			"workspace": map[string]any{"id": "ws-1", "slug": f.wsSlug, "name": f.wsSlug},
		})

	case r.Method == http.MethodPost && path == "/api/v1/workspaces":
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["slug"] == f.wsSlug {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "slug taken"})
			return
		}
		f.wsSlug = body["slug"]
		f.workspaceCreates++
		writeJSON(w, http.StatusCreated, map[string]any{
			"workspace": map[string]any{"id": "ws-1", "slug": f.wsSlug, "name": body["name"]},
		})

	case r.Method == http.MethodGet && strings.HasSuffix(path, "/roles"):
		writeJSON(w, http.StatusOK, map[string]any{
			"roles": []map[string]any{{"id": "role-member", "name": "Member"}},
		})

	case r.Method == http.MethodPost && strings.HasSuffix(path, "/members"):
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if f.members[body["email"]] {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "already a member"})
			return
		}
		f.members[body["email"]] = true
		writeJSON(w, http.StatusOK, map[string]any{"member": map[string]any{}})

	case r.Method == http.MethodGet && strings.HasSuffix(path, "/providers"):
		writeJSON(w, http.StatusOK, map[string]any{"providers": f.providers})

	case r.Method == http.MethodPost && strings.HasSuffix(path, "/providers"):
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.providerCreates++
		f.lastProviderCreate = body
		provider := map[string]any{"id": "prov-1", "name": body["name"], "type": body["type"], "enabled": true}
		f.providers = append(f.providers, provider)
		writeJSON(w, http.StatusCreated, map[string]any{"provider": provider})

	case r.Method == http.MethodGet && strings.Contains(path, "/agents/"):
		if f.agent != nil {
			writeJSON(w, http.StatusOK, map[string]any{"agent": f.agent})
			return
		}
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})

	case r.Method == http.MethodPost && strings.HasSuffix(path, "/agents"):
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		for _, required := range []string{"name", "slug", "role", "description", "brief", "provider_id", "model"} {
			if v, _ := body[required].(string); v == "" {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing " + required})
				return
			}
		}
		f.agentCreates++
		f.lastAgentCreate = body
		f.agent = map[string]any{
			"id": "ag-1", "slug": body["slug"], "name": body["name"],
			"provider_id": body["provider_id"], "model": body["model"],
			"disabled_tools": []any{},
		}
		writeJSON(w, http.StatusCreated, map[string]any{"agent": f.agent})

	case r.Method == http.MethodPatch && strings.Contains(path, "/agents/"):
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if len(body) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "no fields to update"})
			return
		}
		f.patches++
		f.lastPatch = body
		if v, ok := body["model"]; ok {
			f.agent["model"] = v
		}
		if v, ok := body["context_window"]; ok {
			f.agent["context_window"] = v
		}
		if v, ok := body["disabled_tools"]; ok {
			f.agent["disabled_tools"] = v
		}
		writeJSON(w, http.StatusOK, map[string]any{"agent": f.agent})

	case r.Method == http.MethodPost && strings.HasSuffix(path, "/api-keys/exchange"):
		auth := r.Header.Get("Authorization")
		f.chatKeyExchanges++
		writeJSON(w, http.StatusOK, map[string]any{"key": "ck-" + strings.TrimPrefix(auth, "Bearer tok-")})

	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "unhandled " + r.Method + " " + path})
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// benchOptions builds a hermetic Provision options set for the fake server.
func benchOptions(baseURL string) Options {
	return Options{
		BaseURL:           baseURL,
		AdminEmail:        "admin@bench.local",
		AdminPassword:     "admin-pw-1",
		WorkspaceSlug:     "bench-ws",
		AgentSlug:         "bench-agent",
		Model:             "glm-5.3-flash",
		ProviderAPIKeyEnv: "ONCLAW_TEST_PROVIDER_KEY",
	}
}

// seedPreexisting populates the fake with everything already provisioned —
// actors with matching passwords, memberships, an enabled provider, and an
// agent pinned to a stale model with a denylist that denies "shell".
func (f *fakeOnclaw) seedPreexisting() {
	f.wsSlug = "bench-ws"
	f.users[CaseyEmail] = CaseyPassword
	f.users[RileyEmail] = RileyPassword
	f.members[CaseyEmail] = true
	f.members[RileyEmail] = true
	f.providers = []map[string]any{
		{"id": "prov-existing", "name": "existing", "type": "openai-compatible", "enabled": true},
	}
	f.agent = map[string]any{
		"id": "ag-1", "slug": "bench-agent", "name": fixtureAgentName,
		"provider_id":    "prov-existing",
		"model":          "old-model",
		"disabled_tools": []any{"shell", "grafana.query"},
	}
}

func TestProvisionCreatesEverythingFresh(t *testing.T) {
	fake := newFakeOnclaw("admin@bench.local", "admin-pw-1")
	srv := httptest.NewServer(fake)
	defer srv.Close()
	t.Setenv("ONCLAW_TEST_PROVIDER_KEY", "sk-from-env")

	h, err := Provision(context.Background(), benchOptions(srv.URL))
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if h.Reused {
		t.Fatal("Reused = true on a fresh server, want false")
	}
	if fake.workspaceCreates != 1 {
		t.Fatalf("workspace creates = %d, want 1", fake.workspaceCreates)
	}
	if h.Workspace == nil || h.Workspace.Slug != "bench-ws" {
		t.Fatalf("workspace handle = %+v", h.Workspace)
	}
	if fake.providerCreates != 1 {
		t.Fatalf("provider creates = %d, want 1", fake.providerCreates)
	}
	if fake.lastProviderCreate["key"] != "sk-from-env" {
		t.Fatalf("provider key = %v, want the env value", fake.lastProviderCreate["key"])
	}
	if fake.agentCreates != 1 {
		t.Fatalf("agent creates = %d, want 1", fake.agentCreates)
	}
	if fake.lastAgentCreate["provider_id"] != "prov-1" || fake.lastAgentCreate["model"] != "glm-5.3-flash" {
		t.Fatalf("agent create = %v, want provider+model bound", fake.lastAgentCreate)
	}
	if fake.chatKeyExchanges != 2 {
		t.Fatalf("chat key exchanges = %d, want 2 (both actors)", fake.chatKeyExchanges)
	}
	if h.AdminToken == "" || h.AdminUserID != "u-admin@bench.local" {
		t.Fatalf("admin handle = (%q, %q)", h.AdminToken, h.AdminUserID)
	}
	casey := h.Actors[CaseyEmail]
	if casey == nil || casey.Token == "" || casey.ChatKey != "ck-casey@bench.local" {
		t.Fatalf("casey actor = %+v", casey)
	}
	if h.ChatKey != casey.ChatKey {
		t.Fatalf("ChatKey = %q, want the primary chat actor's key", h.ChatKey)
	}
	if h.Agent == nil || h.Agent.ID != "ag-1" || h.Agent.Model != "glm-5.3-flash" {
		t.Fatalf("agent handle = %+v", h.Agent)
	}
	if len(h.Warnings) != 0 {
		t.Fatalf("warnings = %v, want none on the fresh path", h.Warnings)
	}
}

func TestProvisionReusesAndRepairs(t *testing.T) {
	fake := newFakeOnclaw("admin@bench.local", "admin-pw-1")
	fake.seedPreexisting()
	srv := httptest.NewServer(fake)
	defer srv.Close()
	// No env key: the existing provider must satisfy the pass.

	window := 32000
	opts := benchOptions(srv.URL)
	opts.ContextWindow = &window
	h, err := Provision(context.Background(), opts)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if !h.Reused {
		t.Fatal("Reused = false against pre-provisioned state, want true")
	}
	if fake.providerCreates != 0 {
		t.Fatalf("provider creates = %d, want 0 on the reuse path", fake.providerCreates)
	}
	if fake.agentCreates != 0 {
		t.Fatalf("agent creates = %d, want 0 on the reuse path", fake.agentCreates)
	}
	if fake.patches != 1 {
		t.Fatalf("patches = %d, want exactly one repair PATCH", fake.patches)
	}
	if fake.lastPatch["model"] != "glm-5.3-flash" {
		t.Fatalf("patch model = %v, want the pinned model", fake.lastPatch["model"])
	}
	if fake.lastPatch["context_window"] != float64(32000) {
		t.Fatalf("patch context_window = %v, want 32000", fake.lastPatch["context_window"])
	}
	// The denylist repair subtracts the bench core tools and keeps the rest.
	gotTools, _ := fake.lastPatch["disabled_tools"].([]any)
	if len(gotTools) != 1 || gotTools[0] != "grafana.query" {
		t.Fatalf("patch disabled_tools = %v, want [grafana.query] (bench tools subtracted)", gotTools)
	}
	if h.Agent.Model != "glm-5.3-flash" {
		t.Fatalf("agent handle model = %q, want repointed", h.Agent.Model)
	}
	if len(h.Agent.DisabledTools) != 1 || h.Agent.DisabledTools[0] != "grafana.query" {
		t.Fatalf("agent handle denylist = %v, want repaired", h.Agent.DisabledTools)
	}
}

func TestProvisionReuseIdempotent(t *testing.T) {
	fake := newFakeOnclaw("admin@bench.local", "admin-pw-1")
	srv := httptest.NewServer(fake)
	defer srv.Close()
	t.Setenv("ONCLAW_TEST_PROVIDER_KEY", "sk-from-env")

	// First pass provisions; second pass must converge without writes.
	for i := 0; i < 2; i++ {
		if _, err := Provision(context.Background(), benchOptions(srv.URL)); err != nil {
			t.Fatalf("Provision pass %d: %v", i+1, err)
		}
	}
	if fake.workspaceCreates != 1 || fake.providerCreates != 1 || fake.agentCreates != 1 {
		t.Fatalf("creates across two passes = ws %d / provider %d / agent %d, want 1/1/1",
			fake.workspaceCreates, fake.providerCreates, fake.agentCreates)
	}
	if fake.patches != 0 {
		t.Fatalf("patches across two passes = %d, want 0 (state already matches)", fake.patches)
	}
}

func TestProvisionReuseIdempotentAfterRepair(t *testing.T) {
	fake := newFakeOnclaw("admin@bench.local", "admin-pw-1")
	fake.seedPreexisting()
	srv := httptest.NewServer(fake)
	defer srv.Close()

	window := 32000
	opts := benchOptions(srv.URL)
	opts.ContextWindow = &window
	for i := 0; i < 2; i++ {
		if _, err := Provision(context.Background(), opts); err != nil {
			t.Fatalf("Provision pass %d: %v", i+1, err)
		}
	}
	if fake.patches != 1 {
		t.Fatalf("patches across two passes = %d, want 1 (the repair lands once)", fake.patches)
	}
}

func TestProvisionFailsActionablyWithoutProvider(t *testing.T) {
	fake := newFakeOnclaw("admin@bench.local", "admin-pw-1")
	srv := httptest.NewServer(fake)
	defer srv.Close()
	// No providers, no env key.

	opts := benchOptions(srv.URL)
	opts.ProviderAPIKeyEnv = "ONCLAW_TEST_MISSING_KEY"
	_, err := Provision(context.Background(), opts)
	if err == nil {
		t.Fatal("Provision: want an error when no provider and no env key")
	}
	if !strings.Contains(err.Error(), "ONCLAW_TEST_MISSING_KEY") || !strings.Contains(err.Error(), "live prompt generation") {
		t.Fatalf("error = %v, want the env var named and the live-key reason stated", err)
	}
	if fake.agentCreates != 0 {
		t.Fatalf("agent creates = %d, want 0 (fail before agent creation)", fake.agentCreates)
	}
}

func TestProvisionValidatesRequiredFields(t *testing.T) {
	base := Options{BaseURL: "http://x", AdminEmail: "a", AdminPassword: "b", WorkspaceSlug: "w", AgentSlug: "g", Model: "m"}
	for _, tc := range []struct {
		name   string
		mutate func(Options) Options
	}{
		{"base url", func(o Options) Options { o.BaseURL = ""; return o }},
		{"admin email", func(o Options) Options { o.AdminEmail = ""; return o }},
		{"workspace slug", func(o Options) Options { o.WorkspaceSlug = ""; return o }},
		{"agent slug", func(o Options) Options { o.AgentSlug = ""; return o }},
		{"model", func(o Options) Options { o.Model = ""; return o }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Provision(context.Background(), tc.mutate(base)); err == nil {
				t.Fatalf("Provision without %s: want error", tc.name)
			}
		})
	}
}

func TestProvisionActorPasswordMismatchActionable(t *testing.T) {
	fake := newFakeOnclaw("admin@bench.local", "admin-pw-1")
	fake.wsSlug = "bench-ws"
	// The actor exists but with a different password.
	fake.users[CaseyEmail] = "someone-elses-password"
	srv := httptest.NewServer(fake)
	defer srv.Close()
	t.Setenv("ONCLAW_TEST_PROVIDER_KEY", "sk-from-env")

	_, err := Provision(context.Background(), benchOptions(srv.URL))
	if err == nil {
		t.Fatal("Provision: want an error when the fixture actor's password does not match")
	}
	if !strings.Contains(err.Error(), CaseyEmail) || !strings.Contains(err.Error(), "reset the user") {
		t.Fatalf("error = %v, want the actionable reset guidance", err)
	}
}

// compile-time guard: the fake must satisfy http.Handler.
var _ http.Handler = (*fakeOnclaw)(nil)
