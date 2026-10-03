package driver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

// newTestClient builds a client over the given handler with a short timeout
// so a hung handler fails the test, not the 10-minute default.
func newTestClient(t *testing.T, h http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, 5*time.Second)
}

func TestLogin(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"jwt-1","user":{"id":"u-1","email":"a@b.c"}}`))
	}))

	token, userID, err := client.Login(context.Background(), "a@b.c", "pw")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/auth/login" {
		t.Fatalf("request = %s %s, want POST /api/v1/auth/login", gotMethod, gotPath)
	}
	if gotBody["email"] != "a@b.c" || gotBody["password"] != "pw" {
		t.Fatalf("body = %v, want email/password echoed", gotBody)
	}
	if token != "jwt-1" || userID != "u-1" {
		t.Fatalf("Login = (%q, %q), want (jwt-1, u-1)", token, userID)
	}
}

func TestGetWorkspaceNotFoundReadsAsAbsent(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, "/api/v1/workspaces/") {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	}))

	_, err := client.GetWorkspace(context.Background(), "tok", "nope")
	if err == nil {
		t.Fatal("GetWorkspace: want error on 404")
	}
	if !IsNotFound(err) {
		t.Fatalf("GetWorkspace error = %v, want IsNotFound", err)
	}
	if IsConflict(err) || IsUnauthorized(err) {
		t.Fatalf("404 misclassified: %v", err)
	}
	var ae *apiError
	if !errors.As(err, &ae) || ae.Status != http.StatusNotFound {
		t.Fatalf("error is not *apiError 404: %v", err)
	}
}

func TestCreateAdminUserConflictIsAlreadyExists(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/admin/users" || r.Method != http.MethodPost {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusConflict)
	}))
	user, err := client.CreateAdminUser(context.Background(), "tok", "x@bench.local", "X", "pw")
	if err != nil || user != nil {
		t.Fatalf("CreateAdminUser on 409 = (%v, %v), want (nil, nil)", user, err)
	}
}

func TestAddMemberResolvesRoleAndToleratesConflict(t *testing.T) {
	var memberBody map[string]string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/roles"):
			_, _ = w.Write([]byte(`{"roles":[{"id":"r-owner","name":"Owner"},{"id":"r-member","name":"Member"}]}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/members"):
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &memberBody)
			w.WriteHeader(http.StatusConflict) // already a member
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))

	err := client.AddMember(context.Background(), "tok", "ws", "casey@bench.local", "Member")
	if err != nil {
		t.Fatalf("AddMember on 409: %v", err)
	}
	if memberBody["email"] != "casey@bench.local" || memberBody["role_id"] != "r-member" {
		t.Fatalf("member body = %v, want email + resolved Member role id", memberBody)
	}
}

func TestAddMemberUnknownRole(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"roles":[]}`))
	}))
	err := client.AddMember(context.Background(), "tok", "ws", "x@bench.local", "Owner")
	if err == nil || !strings.Contains(err.Error(), `no "Owner" role`) {
		t.Fatalf("AddMember unknown role = %v, want role-missing error", err)
	}
}

func TestCreateProviderPayloadMirrorsHandler(t *testing.T) {
	var got map[string]any
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/providers") || r.Method != http.MethodPost {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"provider":{"id":"p-1","name":"bench-provider","type":"openai-compatible","enabled":true}}`))
	}))

	key := "sk-from-env"
	base := "https://api.z.ai/api/paas/v4"
	p, err := client.CreateProvider(context.Background(), "tok", "ws", ProviderPayload{
		Type: "openai-compatible", Name: "bench-provider", Key: &key, BaseURL: &base, Enabled: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}
	if p.ID != "p-1" {
		t.Fatalf("provider id = %q, want p-1", p.ID)
	}
	if got["type"] != "openai-compatible" || got["name"] != "bench-provider" || got["key"] != key || got["base_url"] != base || got["enabled"] != true {
		t.Fatalf("provider payload = %v, want type/name/key/base_url/enabled mirrored", got)
	}
}

func TestPatchAgentFieldConventions(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		window   *int
		tools    []string
		wantBody map[string]any // nil = expect no request at all
	}{
		{
			name:     "nil tools omitted, model empty omitted",
			window:   intPtr(32000),
			wantBody: map[string]any{"context_window": float64(32000)},
		},
		{
			name:     "non-nil empty tools clears the denylist",
			tools:    []string{},
			wantBody: map[string]any{"disabled_tools": []any{}},
		},
		{
			name:     "model and tools together",
			model:    "glm-5.3-flash",
			tools:    []string{"grafana.query"},
			wantBody: map[string]any{"model": "glm-5.3-flash", "disabled_tools": []any{"grafana.query"}},
		},
		{
			name:     "everything empty skips the request",
			wantBody: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requested := false
			var got map[string]any
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requested = true
				if r.Method != http.MethodPatch || !strings.HasSuffix(r.URL.Path, "/agents/ag-1") {
					t.Errorf("request = %s %s", r.Method, r.URL.Path)
				}
				raw, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(raw, &got)
				_, _ = w.Write([]byte(`{}`))
			}))

			err := client.PatchAgent(context.Background(), "tok", "ws", "ag-1", tt.model, tt.window, tt.tools)
			if err != nil {
				t.Fatalf("PatchAgent: %v", err)
			}
			if tt.wantBody == nil {
				if requested {
					t.Fatalf("PatchAgent sent a request, want it skipped")
				}
				return
			}
			if !requested {
				t.Fatalf("PatchAgent sent no request, want %#v", tt.wantBody)
			}
			// DeepEqual, not ==: wantBody values include non-comparable
			// slices (disabled_tools).
			if !reflect.DeepEqual(got, tt.wantBody) {
				t.Fatalf("patch body = %#v, want %#v", got, tt.wantBody)
			}
		})
	}
}

func TestCreateWorkspaceHookAndDelete(t *testing.T) {
	var hookBody map[string]any
	deleted := ""
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/hooks"):
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &hookBody)
			_, _ = w.Write([]byte(`{"hook":{"id":"hook-9","name":"gate"},"match_count":null}`))
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/hooks/hook-9"):
			deleted = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))

	id, err := client.CreateWorkspaceHook(context.Background(), "tok", "ws", HookPayload{
		Name: "gate", Event: "pre_tool_use", Matcher: "shell", HandlerType: "http",
		Config: json.RawMessage(`{"url":"http://x"}`),
	})
	if err != nil {
		t.Fatalf("CreateWorkspaceHook: %v", err)
	}
	if id != "hook-9" {
		t.Fatalf("hook id = %q, want hook-9", id)
	}
	if hookBody["event"] != "pre_tool_use" || hookBody["handler_type"] != "http" || hookBody["matcher"] != "shell" {
		t.Fatalf("hook payload = %v, want event/matcher/handler_type mirrored", hookBody)
	}
	if err := client.DeleteWorkspaceHook(context.Background(), "tok", "ws", id); err != nil {
		t.Fatalf("DeleteWorkspaceHook: %v", err)
	}
	if !strings.HasSuffix(deleted, "/hooks/hook-9") {
		t.Fatalf("delete path = %q", deleted)
	}
}

func TestCreateConnectionPATAndOAuth(t *testing.T) {
	t.Run("pat recipe persists", func(t *testing.T) {
		var got map[string]any
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasSuffix(r.URL.Path, "/integrations/connections") || r.Method != http.MethodPost {
				t.Errorf("request = %s %s", r.Method, r.URL.Path)
			}
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &got)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"connection":{"id":"c-1","service":"github","status":"connected"}}`))
		}))
		conn, err := client.CreateConnection(context.Background(), "tok", "ws", ConnectionPayload{
			RecipeID: "github-pat", AccessLevel: "read", Token: "ghp-x",
		})
		if err != nil {
			t.Fatalf("CreateConnection: %v", err)
		}
		if conn.ID != "c-1" || conn.Service != "github" || conn.Status != "connected" {
			t.Fatalf("connection = %+v", conn)
		}
		if got["recipe_id"] != "github-pat" || got["token"] != "ghp-x" || got["access_level"] != "read" {
			t.Fatalf("connection payload = %v", got)
		}
	})

	t.Run("oauth recipe errors", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"authorize_url":"https://x/oauth"}`))
		}))
		_, err := client.CreateConnection(context.Background(), "tok", "ws", ConnectionPayload{RecipeID: "github-oauth"})
		if err == nil || !strings.Contains(err.Error(), "OAuth-based") {
			t.Fatalf("CreateConnection oauth = %v, want OAuth refusal error", err)
		}
	})
}

func TestChatTurnWireShape(t *testing.T) {
	var got map[string]any
	auth := ""
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" || r.Method != http.MethodPost {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		auth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		_, _ = w.Write([]byte(`{"status":"completed","output":[]}`))
	}))

	resp, err := client.ChatTurn(context.Background(), "ck-1", "bench-agent", "hello", "sess-1")
	if err != nil {
		t.Fatalf("ChatTurn: %v", err)
	}
	if auth != "Bearer ck-1" {
		t.Fatalf("authorization = %q, want chat key bearer", auth)
	}
	if got["model"] != "bench-agent" || got["input"] != "hello" || got["stream"] != false {
		t.Fatalf("body = %v", got)
	}
	meta, _ := got["metadata"].(map[string]any)
	if meta["onclaw_session"] != "sess-1" {
		t.Fatalf("metadata = %v, want onclaw_session sess-1", meta)
	}
	if resp["status"] != "completed" {
		t.Fatalf("response status = %v", resp["status"])
	}
}

func TestListNotesEndpointAbsent(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	notes, live, err := client.ListNotes(context.Background(), "tok", "ws", "")
	if err != nil || notes != nil || live {
		t.Fatalf("ListNotes on 404 = (%v, %v, %v), want (nil, false, nil)", notes, live, err)
	}
}

func TestHTTPErrorCarriesBody(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":"invalid request: name: required"}`))
	}))
	_, err := client.CreateWorkspace(context.Background(), "tok", "N", "s")
	if err == nil {
		t.Fatal("CreateWorkspace: want error on 422")
	}
	if !strings.Contains(err.Error(), "422") || !strings.Contains(err.Error(), "name") {
		t.Fatalf("error = %v, want status + body text", err)
	}
}

func intPtr(i int) *int { return &i }
