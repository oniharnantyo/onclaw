package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/server"
)

// -----------------------------------------------------------------------------
// MCP endpoints (change integrate-mcp-servers): workspace registry, agent
// private servers, probes, and the enabled_mcps rename.
// -----------------------------------------------------------------------------

// mcpSecretRow mirrors the API read view of one env/header row: the write-only
// value never round-trips — reads carry only value_hint.
type mcpSecretRow struct {
	Name      string `json:"name"`
	ValueHint string `json:"value_hint"`
}

// mcpServerRow mirrors the API read view of one registered MCP server.
type mcpServerRow struct {
	ID          string         `json:"id"`
	WorkspaceID string         `json:"workspace_id"`
	AgentID     string         `json:"agent_id"`
	Name        string         `json:"name"`
	Transport   string         `json:"transport"`
	Command     string         `json:"command"`
	Args        []string       `json:"args"`
	Env         []mcpSecretRow `json:"env"`
	URL         string         `json:"url"`
	Headers     []mcpSecretRow `json:"headers"`
	Enabled     bool           `json:"enabled"`
	Status      string         `json:"status"`
	StatusError string         `json:"status_error"`
	ToolCount   int            `json:"tool_count"`
}

type mcpServerResponse struct {
	Server mcpServerRow `json:"server"`
}

type mcpServerListResponse struct {
	Servers []mcpServerRow `json:"servers"`
}

func decodeMCPServer(t *testing.T, body []byte) mcpServerRow {
	t.Helper()
	var res mcpServerResponse
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("failed to decode mcp server response: %v (%s)", err, body)
	}
	return res.Server
}

// compileMCPMockServer builds the in-process mcp-go stdio stub server once for
// the whole test binary (same testdata binary the mcp package tests drive).
var (
	mcpMockOnce sync.Once
	mcpMockBin  string
	mcpMockErr  error
)

func compileMCPMockServer(t *testing.T) string {
	t.Helper()
	mcpMockOnce.Do(func() {
		_, thisFile, _, ok := runtime.Caller(0)
		if !ok {
			mcpMockErr = errors.New("cannot locate test source dir")
			return
		}
		pkgDir := filepath.Dir(thisFile)
		goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
		if _, err := os.Stat(goBin); err != nil {
			goBin = "go"
		}
		dir, err := os.MkdirTemp("", "onclaw-mcp-mock")
		if err != nil {
			mcpMockErr = err
			return
		}
		bin := filepath.Join(dir, "mockmcpserver")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, goBin, "build", "-o", bin, "../agents/mcp/testdata/mockmcpserver")
		cmd.Dir = pkgDir
		if out, err := cmd.CombinedOutput(); err != nil {
			mcpMockErr = fmt.Errorf("compile mockmcpserver: %v\n%s", err, out)
			return
		}
		mcpMockBin = bin
	})
	if mcpMockErr != nil {
		t.Fatalf("mock server unavailable: %v", mcpMockErr)
	}
	return mcpMockBin
}

const (
	mcpMockEnvName  = "ONCLAW_MCP_TEST_MARKER"
	mcpMockEnvValue = "secret-123"
	mcpMockArg      = "--marker=child-args-123"
)

// mcpTestEnvMembers wires owner/admin/member/non-member users and their roles
// for one workspace — the standard permission-matrix fixture.
func mcpTestEnvMembers(t *testing.T, env *testEnv, slug string) (ownerToken, adminToken, memberToken, nonMemberToken string) {
	t.Helper()
	ownerUser, ownerToken := createTestUser(t, env, slug+"-owner@example.com", "Owner", "pwd")
	adminUser, adminToken := createTestUser(t, env, slug+"-admin@example.com", "Admin", "pwd")
	memberUser, memberToken := createTestUser(t, env, slug+"-member@example.com", "Member", "pwd")
	_, nonMemberToken = createTestUser(t, env, slug+"-outsider@example.com", "Outsider", "pwd")

	ws, ownerRole, adminRole, memberRole := createTestWorkspaceWithRoles(t, env, slug, "MCP WS "+slug)
	addMember(t, env, ws.ID, ownerUser.ID, ownerRole.ID)
	addMember(t, env, ws.ID, adminUser.ID, adminRole.ID)
	addMember(t, env, ws.ID, memberUser.ID, memberRole.ID)
	return ownerToken, adminToken, memberToken, nonMemberToken
}

// Scenario matrix: Member reads the registry / Member cannot configure /
// non-members get 404 across the board / Cross-tenant server is not found.
func TestWorkspaceMCPServers_PermissionMatrix(t *testing.T) {
	env := setupTestEnv(t)
	ownerToken, _, memberToken, nonMemberToken := mcpTestEnvMembers(t, env, "mcp-perm-ws")

	// Owner registers one unreachable server (create persists with the probe's
	// error status — the spec's Unreachable server scenario).
	wCreate := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/mcp-perm-ws/mcp-servers", ownerToken, map[string]any{
		"name":      "GitHub",
		"transport": "stdio",
		"command":   "/nonexistent/onclaw-mcp-binary-xyz",
	})
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for mcp server create, got %d: %s", wCreate.Code, wCreate.Body.String())
	}
	created := decodeMCPServer(t, wCreate.Body.Bytes())
	if created.ID == "" {
		t.Fatal("expected a store-assigned id")
	}

	serverPath := "/api/v1/workspaces/mcp-perm-ws/mcp-servers/" + created.ID

	t.Run("non-member gets 404 across all endpoints", func(t *testing.T) {
		endpoints := []struct {
			method string
			path   string
			body   any
		}{
			{http.MethodGet, "/api/v1/workspaces/mcp-perm-ws/mcp-servers", nil},
			{http.MethodPost, "/api/v1/workspaces/mcp-perm-ws/mcp-servers", map[string]any{"name": "X", "transport": "stdio", "command": "x"}},
			{http.MethodPatch, serverPath, map[string]any{"enabled": false}},
			{http.MethodDelete, serverPath, nil},
			{http.MethodPost, serverPath + "/probe", nil},
		}
		for _, ep := range endpoints {
			w := doRequest(env.router, ep.method, ep.path, nonMemberToken, ep.body)
			if w.Code != http.StatusNotFound {
				t.Errorf("%s %s expected 404 for non-member, got %d: %s", ep.method, ep.path, w.Code, w.Body.String())
			}
		}
	})

	t.Run("member reads but cannot configure", func(t *testing.T) {
		wList := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/mcp-perm-ws/mcp-servers", memberToken, nil)
		if wList.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on member list, got %d: %s", wList.Code, wList.Body.String())
		}
		var list mcpServerListResponse
		if err := json.Unmarshal(wList.Body.Bytes(), &list); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		if len(list.Servers) != 1 || list.Servers[0].ID != created.ID {
			t.Fatalf("expected the registered server in the member's list, got %+v", list.Servers)
		}

		writes := []struct {
			method string
			path   string
			body   any
		}{
			{http.MethodPost, "/api/v1/workspaces/mcp-perm-ws/mcp-servers", map[string]any{"name": "Nope", "transport": "stdio", "command": "x"}},
			{http.MethodPatch, serverPath, map[string]any{"enabled": false}},
			{http.MethodDelete, serverPath, nil},
			{http.MethodPost, serverPath + "/probe", nil},
		}
		for _, wr := range writes {
			w := doRequest(env.router, wr.method, wr.path, memberToken, wr.body)
			if w.Code != http.StatusForbidden {
				t.Errorf("%s %s expected 403 for member, got %d: %s", wr.method, wr.path, w.Code, w.Body.String())
			}
		}
	})

	t.Run("cross-tenant server is not found", func(t *testing.T) {
		wsB, ownerRoleB, _, _ := createTestWorkspaceWithRoles(t, env, "mcp-perm-ws-b", "MCP WS B")
		userB, tokenB := createTestUser(t, env, "mcp-perm-b@example.com", "User B", "pwd")
		addMember(t, env, wsB.ID, userB.ID, ownerRoleB.ID)

		for _, ep := range []struct {
			method string
			path   string
		}{
			{http.MethodPatch, "/api/v1/workspaces/mcp-perm-ws-b/mcp-servers/" + created.ID},
			{http.MethodDelete, "/api/v1/workspaces/mcp-perm-ws-b/mcp-servers/" + created.ID},
			{http.MethodPost, "/api/v1/workspaces/mcp-perm-ws-b/mcp-servers/" + created.ID + "/probe"},
		} {
			w := doRequest(env.router, ep.method, ep.path, tokenB, map[string]any{})
			if w.Code != http.StatusNotFound {
				t.Errorf("%s cross-tenant expected 404, got %d: %s", ep.method, w.Code, w.Body.String())
			}
		}

		// The server is untouched: the owner still sees it.
		wList := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/mcp-perm-ws/mcp-servers", ownerToken, nil)
		if !strings.Contains(wList.Body.String(), created.ID) {
			t.Error("cross-tenant attempts must not have deleted the server")
		}
	})
}

// Scenario coverage: Register a stdio server (201, no credentials echoed),
// Unreachable server (create persists with error status), Duplicate name
// rejected (fielded), Transport config validated per transport (fielded),
// Empty value keeps the stored secret, Master switch pauses, delete removes.
func TestWorkspaceMCPServers_CRUDValidationAndSecrecy(t *testing.T) {
	env := setupTestEnv(t)
	ownerToken, _, _, _ := mcpTestEnvMembers(t, env, "mcp-crud-ws")

	baseURL := "/api/v1/workspaces/mcp-crud-ws/mcp-servers"

	t.Run("transport config validated per transport with fielded errors", func(t *testing.T) {
		wNoCmd := doRequest(env.router, http.MethodPost, baseURL, ownerToken, map[string]any{
			"name": "Broken", "transport": "stdio",
		})
		if wNoCmd.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 for stdio without command, got %d: %s", wNoCmd.Code, wNoCmd.Body.String())
		}
		if !strings.Contains(wNoCmd.Body.String(), `"command"`) {
			t.Errorf("expected a fielded error naming command, got %s", wNoCmd.Body.String())
		}

		wNoURL := doRequest(env.router, http.MethodPost, baseURL, ownerToken, map[string]any{
			"name": "Broken", "transport": "streamable_http",
		})
		if wNoURL.Code != http.StatusUnprocessableEntity || !strings.Contains(wNoURL.Body.String(), `"url"`) {
			t.Fatalf("expected 422 fielding url, got %d: %s", wNoURL.Code, wNoURL.Body.String())
		}

		wBadTransport := doRequest(env.router, http.MethodPost, baseURL, ownerToken, map[string]any{
			"name": "Broken", "transport": "carrier_pigeon",
		})
		if wBadTransport.Code != http.StatusUnprocessableEntity || !strings.Contains(wBadTransport.Body.String(), `"transport"`) {
			t.Fatalf("expected 422 fielding transport, got %d: %s", wBadTransport.Code, wBadTransport.Body.String())
		}

		wBlankName := doRequest(env.router, http.MethodPost, baseURL, ownerToken, map[string]any{
			"name": "  ", "transport": "stdio", "command": "x",
		})
		if wBlankName.Code != http.StatusUnprocessableEntity || !strings.Contains(wBlankName.Body.String(), `"name"`) {
			t.Fatalf("expected 422 fielding name, got %d: %s", wBlankName.Code, wBlankName.Body.String())
		}
	})

	// Register a stdio server with a secret env var; the probe fails (binary
	// does not exist) but the create still persists (Unreachable server).
	var created mcpServerRow
	t.Run("create persists an unreachable stdio server with hinted secrets", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, baseURL, ownerToken, map[string]any{
			"name":      "GitHub",
			"transport": "stdio",
			"command":   "/nonexistent/onclaw-mcp-binary-xyz",
			"args":      []string{"-y", "@modelcontextprotocol/server-github"},
			"env":       []map[string]any{{"name": "GITHUB_TOKEN", "value": "ghp-supersecret-9999"}},
		})
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
		}
		created = decodeMCPServer(t, w.Body.Bytes())

		if created.Transport != "stdio" || created.Command != "/nonexistent/onclaw-mcp-binary-xyz" {
			t.Errorf("unexpected connection echo: %+v", created)
		}
		if len(created.Env) != 1 || created.Env[0].Name != "GITHUB_TOKEN" {
			t.Fatalf("expected the env row echoed by name, got %+v", created.Env)
		}
		if created.Env[0].ValueHint == "" || strings.Contains(created.Env[0].ValueHint, "supersecret") {
			t.Errorf("expected a last-4 hint, got %+v", created.Env[0])
		}
		if strings.Contains(w.Body.String(), "ghp-supersecret-9999") {
			t.Fatal("create response leaked the plaintext secret")
		}
		if created.Status != domain.MCPStatusError || created.StatusError == "" {
			t.Errorf("expected a persisted error probe status, got %q/%q", created.Status, created.StatusError)
		}
		if !created.Enabled {
			t.Error("servers default to enabled")
		}
	})

	t.Run("duplicate name rejected with a fielded error", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, baseURL, ownerToken, map[string]any{
			"name": "github", "transport": "stdio", "command": "x",
		})
		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 for case-insensitive duplicate name, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"name"`) || !strings.Contains(w.Body.String(), "GitHub") {
			t.Errorf("expected the fielded error to name the conflicting server, got %s", w.Body.String())
		}
	})

	t.Run("update keeps the stored secret on an empty value and pauses", func(t *testing.T) {
		serverPath := baseURL + "/" + created.ID

		// Full-form edit: empty env value keeps the stored secret.
		wPatch := doRequest(env.router, http.MethodPatch, serverPath, ownerToken, map[string]any{
			"name":      "GitHub Renamed",
			"transport": "stdio",
			"command":   "/nonexistent/onclaw-mcp-binary-xyz",
			"args":      []string{"-y", "@modelcontextprotocol/server-github"},
			"env":       []map[string]any{{"name": "GITHUB_TOKEN", "value": ""}},
		})
		if wPatch.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on patch, got %d: %s", wPatch.Code, wPatch.Body.String())
		}
		patched := decodeMCPServer(t, wPatch.Body.Bytes())
		if patched.Name != "GitHub Renamed" {
			t.Errorf("expected the rename to persist, got %q", patched.Name)
		}
		if len(patched.Env) != 1 || patched.Env[0].ValueHint == "" {
			t.Fatalf("expected the stored secret kept with its hint, got %+v", patched.Env)
		}
		if strings.Contains(wPatch.Body.String(), "ghp-supersecret-9999") {
			t.Fatal("patch response leaked the plaintext secret")
		}

		// Pure master-switch patch (the pane's pause/resume shape).
		wPause := doRequest(env.router, http.MethodPatch, serverPath, ownerToken, map[string]any{
			"enabled": false,
		})
		if wPause.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on pause patch, got %d: %s", wPause.Code, wPause.Body.String())
		}
		if paused := decodeMCPServer(t, wPause.Body.Bytes()); paused.Enabled {
			t.Error("expected the master switch paused")
		}
		if !strings.Contains(wPause.Body.String(), "GITHUB_TOKEN") {
			t.Error("a pure enabled patch must keep the connection intact")
		}

		wEmpty := doRequest(env.router, http.MethodPatch, serverPath, ownerToken, map[string]any{})
		if wEmpty.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for an empty patch, got %d: %s", wEmpty.Code, wEmpty.Body.String())
		}

		wUnknown := doRequest(env.router, http.MethodPatch, baseURL+"/00000000-0000-0000-0000-000000000000", ownerToken, map[string]any{"enabled": false})
		if wUnknown.Code != http.StatusNotFound {
			t.Errorf("expected 404 for an unknown server id, got %d", wUnknown.Code)
		}
	})

	t.Run("delete removes the row", func(t *testing.T) {
		serverPath := baseURL + "/" + created.ID
		wDel := doRequest(env.router, http.MethodDelete, serverPath, ownerToken, nil)
		if wDel.Code != http.StatusNoContent {
			t.Fatalf("expected 204 No Content on delete, got %d: %s", wDel.Code, wDel.Body.String())
		}
		wList := doRequest(env.router, http.MethodGet, baseURL, ownerToken, nil)
		if strings.Contains(wList.Body.String(), created.ID) {
			t.Error("expected the deleted server gone from the list")
		}
		wProbe := doRequest(env.router, http.MethodPost, serverPath+"/probe", ownerToken, nil)
		if wProbe.Code != http.StatusNotFound {
			t.Errorf("expected 404 probing a deleted server, got %d", wProbe.Code)
		}
	})
}

// Scenario: Successful probe — a server created against the reachable stub
// reports connected with the stub's tool count, persisted on the row; the
// re-probe endpoint behaves identically.
func TestWorkspaceMCPServers_ProbeAgainstStub(t *testing.T) {
	env := setupTestEnv(t)
	ownerToken, _, _, _ := mcpTestEnvMembers(t, env, "mcp-probe-ws")

	bin := compileMCPMockServer(t)
	baseURL := "/api/v1/workspaces/mcp-probe-ws/mcp-servers"

	wCreate := doRequest(env.router, http.MethodPost, baseURL, ownerToken, map[string]any{
		"name":      "Mock",
		"transport": "stdio",
		"command":   bin,
		"args":      []string{mcpMockArg},
		"env":       []map[string]any{{"name": mcpMockEnvName, "value": mcpMockEnvValue}},
	})
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", wCreate.Code, wCreate.Body.String())
	}
	created := decodeMCPServer(t, wCreate.Body.Bytes())
	if created.Status != domain.MCPStatusConnected {
		t.Fatalf("expected the on-save probe to connect, got %q (%s)", created.Status, created.StatusError)
	}
	// The shared stub lists 4 tools: test_tool, echo_env, echo_args, and
	// dump_env (added by openspec/changes/fix-stdio-env-leak for child-env
	// assertions).
	if created.ToolCount != 4 {
		t.Errorf("expected the stub's 4 tools, got %d", created.ToolCount)
	}
	if len(created.Env) != 1 || created.Env[0].ValueHint != "-123" {
		t.Errorf("expected the last-4 hint of the env secret, got %+v", created.Env)
	}
	if strings.Contains(wCreate.Body.String(), mcpMockEnvValue) {
		t.Fatal("probe response leaked the plaintext secret")
	}

	// Status persists: a plain list shows connected + tool count.
	wList := doRequest(env.router, http.MethodGet, baseURL, ownerToken, nil)
	if !strings.Contains(wList.Body.String(), `"status":"connected"`) || !strings.Contains(wList.Body.String(), `"tool_count":4`) {
		t.Errorf("expected the probe status persisted on the row, got %s", wList.Body.String())
	}

	// Re-probe on demand.
	wProbe := doRequest(env.router, http.MethodPost, baseURL+"/"+created.ID+"/probe", ownerToken, nil)
	if wProbe.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on re-probe, got %d: %s", wProbe.Code, wProbe.Body.String())
	}
	reprobed := decodeMCPServer(t, wProbe.Body.Bytes())
	if reprobed.Status != domain.MCPStatusConnected || reprobed.ToolCount != 4 {
		t.Errorf("expected connected/4 on re-probe, got %q/%d", reprobed.Status, reprobed.ToolCount)
	}
}

// The probe is bounded: an unreachable server cannot hold the response past
// the configured bound, and the failure persists as the row's status.
func TestWorkspaceMCPServers_ProbeIsBounded(t *testing.T) {
	env := setupTestEnv(t, func(opts *server.RouterOptions) {
		opts.MCPProbeTimeout = 300 * time.Millisecond
	})
	ownerToken, _, _, _ := mcpTestEnvMembers(t, env, "mcp-bound-ws")

	baseURL := "/api/v1/workspaces/mcp-bound-ws/mcp-servers"

	start := time.Now()
	wCreate := doRequest(env.router, http.MethodPost, baseURL, ownerToken, map[string]any{
		"name":      "Hanging",
		"transport": "stdio",
		"command":   "sleep",
		"args":      []string{"30"},
	})
	elapsed := time.Since(start)

	if wCreate.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created despite the failing probe, got %d: %s", wCreate.Code, wCreate.Body.String())
	}
	if elapsed > 5*time.Second {
		t.Fatalf("probe was not bounded by the configured timeout: %v", elapsed)
	}
	created := decodeMCPServer(t, wCreate.Body.Bytes())
	if created.Status != domain.MCPStatusError {
		t.Fatalf("expected the bounded probe to persist an error status, got %q", created.Status)
	}
	if created.StatusError == "" {
		t.Error("expected the probe failure message persisted")
	}

	// A re-probe is bounded the same way.
	start = time.Now()
	wProbe := doRequest(env.router, http.MethodPost, baseURL+"/"+created.ID+"/probe", ownerToken, nil)
	elapsed = time.Since(start)
	if wProbe.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on the bounded re-probe, got %d: %s", wProbe.Code, wProbe.Body.String())
	}
	if elapsed > 5*time.Second {
		t.Fatalf("re-probe was not bounded: %v", elapsed)
	}
	if reprobed := decodeMCPServer(t, wProbe.Body.Bytes()); reprobed.Status != domain.MCPStatusError {
		t.Errorf("expected the re-probe error status, got %q", reprobed.Status)
	}
}

// mcpBirthEnv births a workspace (provider + starter agent) through the API,
// then wires admin/member/non-member into the API-created built-in roles —
// the permission fixture for birth-created workspaces.
func mcpBirthEnv(t *testing.T, env *testEnv, wsSlug string) (ownerToken, memberToken, nonMemberToken string) {
	t.Helper()
	// D4: workspace birth is superadmin-only; the creator wears the built-in
	// Owner role of the newborn workspace.
	ownerUser, ownerToken := seedTestSuperadmin(t, env, wsSlug+"-owner@onclaw.local", "Owner", "pwd")
	_ = ownerUser
	w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces", ownerToken, map[string]any{
		"name": "MCP WS " + wsSlug,
		"slug": wsSlug,
		"provider": map[string]any{
			"type": "openai",
			"name": "OpenAI",
			"key":  "sk-" + wsSlug,
		},
		"starter_agent": map[string]any{
			"name":  "Atlas",
			"slug":  "atlas",
			"role":  "helper",
			"brief": "Help with things",
			"model": "gpt-4o",
		},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("birth failed: %d %s", w.Code, w.Body.String())
	}

	ctx := context.Background()
	ws, err := env.store.Workspaces().BySlug(ctx, wsSlug)
	if err != nil {
		t.Fatalf("lookup birthed workspace: %v", err)
	}
	roles, err := env.store.Roles().ListForWorkspace(ctx, ws.ID)
	if err != nil {
		t.Fatalf("list workspace roles: %v", err)
	}
	var adminRole, memberRole *domain.Role
	for i := range roles {
		switch roles[i].Name {
		case domain.RoleAdmin:
			adminRole = &roles[i]
		case domain.RoleMember:
			memberRole = &roles[i]
		}
	}
	if adminRole == nil || memberRole == nil {
		t.Fatalf("expected birthed built-in roles, got %+v", roles)
	}

	adminUser, _ := createTestUser(t, env, wsSlug+"-admin@example.com", "Admin", "pwd")
	memberUser, memberToken := createTestUser(t, env, wsSlug+"-member@example.com", "Member", "pwd")
	_, nonMemberToken = createTestUser(t, env, wsSlug+"-outsider@example.com", "Outsider", "pwd")
	addMember(t, env, ws.ID, adminUser.ID, adminRole.ID)
	addMember(t, env, ws.ID, memberUser.ID, memberRole.ID)
	return ownerToken, memberToken, nonMemberToken
}

// Scenario: Private servers require agent edit rights — plus the tenancy
// matrix for agent-addressed endpoints and private-server lifecycle.
func TestAgentPrivateMCPServers_Matrix(t *testing.T) {
	env := setupTestEnv(t)
	ownerToken, memberToken, nonMemberToken := mcpBirthEnv(t, env, "mcp-priv-ws")

	agentBase := "/api/v1/workspaces/mcp-priv-ws/agents/atlas/mcp-servers"

	var created mcpServerRow
	t.Run("owner attaches a private server; it is invisible in the registry", func(t *testing.T) {
		wCreate := doRequest(env.router, http.MethodPost, agentBase, ownerToken, map[string]any{
			"name":      "Atlas-Private",
			"transport": "stdio",
			"command":   "/nonexistent/onclaw-mcp-binary-xyz",
			"env":       []map[string]any{{"name": "PRIVATE_TOKEN", "value": "tok-private-4321"}},
		})
		if wCreate.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created for private server, got %d: %s", wCreate.Code, wCreate.Body.String())
		}
		created = decodeMCPServer(t, wCreate.Body.Bytes())
		if created.AgentID == "" {
			t.Error("expected agent_id set on a private server")
		}
		if len(created.Env) != 1 || created.Env[0].ValueHint == "" || strings.Contains(created.Env[0].ValueHint, "private") {
			t.Errorf("expected a hinted secret row, got %+v", created.Env)
		}
		if created.Status != domain.MCPStatusError {
			t.Errorf("expected the failed on-save probe persisted, got %q", created.Status)
		}

		// The workspace registry must not list private servers.
		wRegistry := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/mcp-priv-ws/mcp-servers", ownerToken, nil)
		if strings.Contains(wRegistry.Body.String(), "Atlas-Private") {
			t.Error("private servers must not appear in the workspace registry")
		}
	})

	t.Run("member reads but cannot configure private servers", func(t *testing.T) {
		wList := doRequest(env.router, http.MethodGet, agentBase, memberToken, nil)
		if wList.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for member list (agents.read), got %d: %s", wList.Code, wList.Body.String())
		}

		serverPath := agentBase + "/" + created.ID
		writes := []struct {
			method string
			path   string
			body   any
		}{
			{http.MethodPost, agentBase, map[string]any{"name": "Nope", "transport": "stdio", "command": "x"}},
			{http.MethodPatch, serverPath, map[string]any{"enabled": false}},
			{http.MethodDelete, serverPath, nil},
			{http.MethodPost, serverPath + "/probe", nil},
		}
		for _, wr := range writes {
			w := doRequest(env.router, wr.method, wr.path, memberToken, wr.body)
			if w.Code != http.StatusForbidden {
				t.Errorf("%s expected 403 for member without agents.write, got %d: %s", wr.method, w.Code, w.Body.String())
			}
		}

		wNonMember := doRequest(env.router, http.MethodGet, agentBase, nonMemberToken, nil)
		if wNonMember.Code != http.StatusNotFound {
			t.Errorf("expected 404 for non-member, got %d", wNonMember.Code)
		}
	})

	t.Run("other-workspace agent slug is not found", func(t *testing.T) {
		wsB, ownerRoleB, _, _ := createTestWorkspaceWithRoles(t, env, "mcp-priv-ws-b", "MCP Priv B")
		userB, tokenB := createTestUser(t, env, "mcp-priv-b@example.com", "User B", "pwd")
		addMember(t, env, wsB.ID, userB.ID, ownerRoleB.ID)

		w := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/mcp-priv-ws-b/agents/atlas/mcp-servers", tokenB, nil)
		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404 for another workspace's agent slug, got %d", w.Code)
		}
	})

	t.Run("unknown agent slug is not found", func(t *testing.T) {
		w := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/mcp-priv-ws/agents/ghost/mcp-servers", ownerToken, nil)
		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404 for an unknown agent slug, got %d", w.Code)
		}
	})

	t.Run("private servers die with the agent", func(t *testing.T) {
		wDel := doRequest(env.router, http.MethodDelete, "/api/v1/workspaces/mcp-priv-ws/agents/atlas", ownerToken, nil)
		if wDel.Code != http.StatusNoContent {
			t.Fatalf("expected 204 deleting the agent, got %d: %s", wDel.Code, wDel.Body.String())
		}

		// A fresh agent reusing the slug starts with no private servers; the
		// birthed provider outlives the agent and backs the recreate.
		wProvs := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/mcp-priv-ws/providers", ownerToken, nil)
		if wProvs.Code != http.StatusOK {
			t.Fatalf("provider list failed: %d %s", wProvs.Code, wProvs.Body.String())
		}
		var provList struct {
			Providers []struct {
				ID string `json:"id"`
			} `json:"providers"`
		}
		if err := json.Unmarshal(wProvs.Body.Bytes(), &provList); err != nil || len(provList.Providers) == 0 {
			t.Fatalf("expected the birthed provider, got %s (%v)", wProvs.Body.String(), err)
		}

		wAgent := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/mcp-priv-ws/agents", ownerToken, map[string]any{
			"name": "Atlas", "slug": "atlas", "role": "helper", "brief": "Help again",
			"provider_id": provList.Providers[0].ID, "model": "gpt-4o",
		})
		if wAgent.Code != http.StatusCreated {
			t.Fatalf("agent recreate failed: %d %s", wAgent.Code, wAgent.Body.String())
		}

		wList := doRequest(env.router, http.MethodGet, agentBase, ownerToken, nil)
		if wList.Code != http.StatusOK {
			t.Fatalf("expected 200 listing the fresh agent's private servers, got %d", wList.Code)
		}
		var list mcpServerListResponse
		if err := json.Unmarshal(wList.Body.Bytes(), &list); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		if len(list.Servers) != 0 {
			t.Errorf("expected the deleted agent's private servers gone, got %+v", list.Servers)
		}
	})
}

// Task 4.1: the starter-agent flow carries enabled_mcps and ignores the
// legacy disabled_mcps denylist.
func TestWorkspaces_StarterAgentEnabledMCPS(t *testing.T) {
	env := setupTestEnv(t)
	// D4: workspace birth is superadmin-only.
	user, token := seedTestSuperadmin(t, env, "starter-mcp@onclaw.local", "Starter MCP", "password123")
	_ = user

	w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces", token, map[string]any{
		"name": "Starter MCP WS",
		"slug": "starter-mcp-ws",
		"provider": map[string]any{
			"type": "openai",
			"name": "OpenAI",
			"key":  "sk-starter-mcp",
		},
		"starter_agent": map[string]any{
			"name":          "Starter Bot",
			"slug":          "starter-bot",
			"role":          "onboarding-guide",
			"brief":         "Welcome users and answer basic onboarding questions",
			"model":         "gpt-4o",
			"enabled_mcps":  []string{"srv-a", "srv-b"},
			"disabled_mcps": []string{"srv-z"},
		},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for birth, got %d: %s", w.Code, w.Body.String())
	}

	var res struct {
		StarterAgent domain.Agent `json:"starter_agent"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode birth response: %v", err)
	}
	if len(res.StarterAgent.EnabledMCPS) != 2 || res.StarterAgent.EnabledMCPS[0] != "srv-a" || res.StarterAgent.EnabledMCPS[1] != "srv-b" {
		t.Errorf("expected enabled_mcps persisted as provided, got %+v", res.StarterAgent.EnabledMCPS)
	}
	if strings.Contains(w.Body.String(), "disabled_mcps") {
		t.Error("disabled_mcps must not appear in responses")
	}
}
