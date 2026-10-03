package server_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/ingest"
	"github.com/oniharnantyo/onclaw/internal/memory"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/server"
	"github.com/oniharnantyo/onclaw/internal/services"
)

// -----------------------------------------------------------------------------
// add-recipe-base-url tasks.md 6.2 — the fixture-origin smoke at the
// handler/router layer: the gitlab recipe (http kind, PAT, PRIVATE-TOKEN,
// /api/v4 verb paths) connects against a mock GitLab REST origin (an
// httptest server serving the built-in recipe's declared paths), the probe
// connects, one read verb and one write verb execute through the REAL
// connection tool source the router wires (agent attachment by connection id,
// scripted agentic model), and origin uniqueness holds over the REST API —
// the same origin twice is a 409 naming the origin, a different origin
// connects side by side.
//
// Unlike the sibling connections suites, nothing about the gitlab recipe is
// re-registered: the built-in card's own declared paths, tiers, token header,
// and origin parameter are exactly what the mock serves and what the
// assertions read.
// -----------------------------------------------------------------------------

const (
	gitlabSmokeToken = "glpat-gitlab-smoke-token-1357"
	gitlabSmokeHint  = "1357"
)

// gitlabHit is one request the mock GitLab origin received.
type gitlabHit struct {
	Method string
	Path   string
	Query  string
	Token  string // the PRIVATE-TOKEN header value verbatim
}

// mockGitLab is a fixture GitLab REST origin: it serves the built-in gitlab
// recipe's declared endpoints — the probe and current-user call
// (GET /api/v4/user), the read verb surface's project listing
// (GET /api/v4/projects), and the write verb surface's issue creation
// (POST /api/v4/projects/{id}/issues) — and records every hit so the test can
// assert the resolved origin, the exact declared paths, and the PRIVATE-TOKEN
// header the recipe pins.
type mockGitLab struct {
	srv  *httptest.Server
	mu   sync.Mutex
	hits []gitlabHit
}

func newMockGitLab(t *testing.T) *mockGitLab {
	t.Helper()
	m := &mockGitLab{}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		m.hits = append(m.hits, gitlabHit{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.RawQuery,
			Token:  r.Header.Get("PRIVATE-TOKEN"),
		})
		m.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v4/user":
			_, _ = w.Write([]byte(`{"id":1,"username":"onclaw-bot","name":"OnClaw Bot"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v4/projects":
			_, _ = w.Write([]byte(`[{"id":42,"name":"ops","path_with_namespace":"acme/ops"}]`))
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v4/projects/") && strings.HasSuffix(r.URL.Path, "/issues"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":9001,"iid":7,"project_id":42,"title":"from-onclaw"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"404 Not Found"}`))
		}
	}))
	t.Cleanup(m.srv.Close)
	return m
}

// captured snapshots the recorded hits.
func (m *mockGitLab) captured() []gitlabHit {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]gitlabHit(nil), m.hits...)
}

// gitlabVerbModel is the scripted agentic model: while the conversation
// carries no tool result yet it emits the scripted connection-verb call (or
// plain text when none is scripted); once a result arrives it finishes with
// text. Stateless across runs — both tier runs share the instance through the
// model factory, and each run re-scripts its call (the gate-endpoint test's
// scripted-model shape).
type gitlabVerbModel struct {
	mu   sync.Mutex
	call *schema.FunctionToolCall
}

func (m *gitlabVerbModel) script(name, arguments string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if name == "" {
		m.call = nil
		return
	}
	m.call = &schema.FunctionToolCall{CallID: "call-gitlab-smoke", Name: name, Arguments: arguments}
}

func (m *gitlabVerbModel) done() *schema.AgenticMessage {
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			{Type: schema.ContentBlockTypeAssistantGenText, AssistantGenText: &schema.AssistantGenText{Text: "done"}},
		},
	}
}

func (m *gitlabVerbModel) Generate(_ context.Context, input []*schema.AgenticMessage, _ ...model.Option) (*schema.AgenticMessage, error) {
	for _, msg := range input {
		if msg == nil {
			continue
		}
		for _, block := range msg.ContentBlocks {
			if block != nil && block.FunctionToolResult != nil {
				return m.done(), nil
			}
		}
	}
	m.mu.Lock()
	call := m.call
	m.mu.Unlock()
	if call == nil {
		return m.done(), nil
	}
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			{Type: schema.ContentBlockTypeFunctionToolCall, FunctionToolCall: call},
		},
	}, nil
}

func (m *gitlabVerbModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	sr, sw := schema.Pipe[*schema.AgenticMessage](1)
	_ = sw.Send(msg, nil)
	sw.Close()
	return sr, nil
}

// gitlabOriginEnv assembles the test env whose router carries a connections
// service over the fake stores and a runner wired to the SAME service through
// the router's own NewConnectionToolSource adapter — the production wiring
// shape (router.go's fallback assembly), so a run's connection verb calls
// resolve attachments and credentials from the rows the HTTP connect created.
// The scripted model factory returns the shared gitlabVerbModel.
func gitlabOriginEnv(t *testing.T, scripted *gitlabVerbModel) (*testEnv, *agents.Runner, string) {
	t.Helper()
	var runner *agents.Runner
	var onClawDir string
	env := setupTestEnv(t, func(o *server.RouterOptions) {
		settings := agents.NewMCPSettingsService(o.Store.WorkspaceMCPServers(), o.Store.AgentMCPServers(), o.Store.Agents(), o.EncryptionKey)
		connectionsSvc := services.NewConnectionsService(
			o.Store.Connections(),
			o.Store.WorkspaceMCPServers(),
			o.Store.Agents(),
			settings,
			o.Store.OAuthApps(),
			o.EncryptionKey,
			"",
		)
		o.Connections = connectionsSvc

		onClawDir = t.TempDir()
		memLog := slog.New(slog.NewTextHandler(io.Discard, nil))
		memWorker := memory.NewWorker(
			memory.NewGister(o.Store.MemoryEvents(), o.Store.SessionEvents(), o.Store.MemoryEntities(), o.Store.Providers(), o.EncryptionKey, agents.DefaultAgenticModelFactory, memLog),
			memory.NewGate(o.Store.MemoryNotes(), o.Store.MemoryEntities(), o.Store.Memories(), o.Store.Providers(), o.EncryptionKey, agents.DefaultAgenticModelFactory, memLog),
			memory.NewProviderEmbedder(o.Store.Providers(), o.Store.ToolSettings(), o.EncryptionKey, providers.NewRegistry()),
			o.Store.MemoryEmbeddings(),
			memLog,
		)
		ingestWorker := ingest.NewWorker(memLog, ingest.WithConsumers(memWorker))
		memGate := memory.NewIntentGate(o.Store.Providers(), o.EncryptionKey, agents.DefaultAgenticModelFactory, memLog)
		runner = agents.NewRunner(
			o.Store.Workspaces(), o.Store.Agents(), o.Store.Users(), o.Store.Members(), o.Store.Roles(),
			o.Store.Providers(), o.Store.SessionEvents(), o.Store.SessionCheckpoints(), o.Store.Memories(), o.Store.AgentSessions(),
			o.Store.GatewayLinks(), ingestWorker, newTestMemorySearcher(o.Store), memGate,
			o.EncryptionKey, onClawDir,
			agents.WithAgenticModelFactory(func(context.Context, string, providers.Credential, string) (agents.Model, error) {
				return scripted, nil
			}),
			// The exact wiring the router's fallback assembly builds: the verb
			// tools ride the real connection tool source over the same
			// connections service the REST handlers use.
			agents.WithConnectionToolSource(server.NewConnectionToolSource(connectionsSvc)),
			agents.WithConnectionOriginLookup(connectionsSvc),
		)
		o.Runner = runner
	})
	return env, runner, onClawDir
}

// runGitlabTurn submits one user run (the owner holds integrations.write, so
// the write tier passes the connection gate's invocation-time authority) and
// drains the stream to EOF, returning the transcript events.
func runGitlabTurn(ctx context.Context, t *testing.T, runner *agents.Runner, wsID, agentID, userID, sessionID, input string) []*agents.TranscriptEvent {
	t.Helper()
	stream, err := runner.Run(ctx, agents.ExecRequest{
		WorkspaceID: wsID,
		AgentID:     agentID,
		SessionID:   sessionID,
		UserID:      userID,
		Input:       input,
	})
	if err != nil {
		t.Fatalf("run %s: %v", sessionID, err)
	}
	var events []*agents.TranscriptEvent
	for {
		ev, err := stream.Recv()
		if err != nil {
			return events
		}
		events = append(events, ev)
	}
}

// toolResultOf returns the transcript's finished result for one tool name.
func toolResultOf(t *testing.T, events []*agents.TranscriptEvent, toolName string) string {
	t.Helper()
	for _, ev := range events {
		if ev.Kind == agents.TranscriptEventToolCallFinished && ev.ToolResult != nil && ev.ToolResult.Name == toolName {
			return ev.ToolResult.Result
		}
	}
	t.Fatalf("transcript carries no tool result for %s", toolName)
	return ""
}

// TestConnectionsGitLabOriginSmoke drives the tasks.md 6.2 smoke end to end.
func TestConnectionsGitLabOriginSmoke(t *testing.T) {
	ctx := context.Background()
	gitlab1 := newMockGitLab(t)
	gitlab2 := newMockGitLab(t)
	scripted := &gitlabVerbModel{}
	env, runner, onClawDir := gitlabOriginEnv(t, scripted)

	owner, ownerToken := createTestUser(t, env, "gitlab-smoke-owner@example.com", "Owner", "pwd")
	ws, ownerRole, _, _ := createTestWorkspaceWithRoles(t, env, "gitlab-smoke-ws", "GitLab Smoke WS")
	addMember(t, env, ws.ID, owner.ID, ownerRole.ID)

	base := "/api/v1/workspaces/gitlab-smoke-ws/integrations"

	// --- Connect against the fixture origin: the probe (the recipe's
	// declared current-user call) dials THIS origin with the PRIVATE-TOKEN
	// header and the connection lands connected, storing the origin. ---
	var conn struct {
		ID          string `json:"id"`
		Service     string `json:"service"`
		Origin      string `json:"origin"`
		AccessLevel string `json:"access_level"`
		Status      string `json:"status"`
		TokenHint   string `json:"token_hint"`
	}
	t.Run("connect against the mock origin probes connected and stores the origin", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, base+"/connections", ownerToken, map[string]any{
			"recipe_id":    "gitlab",
			"access_level": domain.ConnectionAccessReadWrite,
			"token":        gitlabSmokeToken,
			"origin":       gitlab1.srv.URL,
		})
		if w.Code != http.StatusCreated {
			t.Fatalf("connect expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var res struct {
			Connection struct {
				ID          string `json:"id"`
				Service     string `json:"service"`
				Origin      string `json:"origin"`
				AccessLevel string `json:"access_level"`
				Status      string `json:"status"`
				TokenHint   string `json:"token_hint"`
			} `json:"connection"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("decode connect response: %v (%s)", err, w.Body.String())
		}
		conn = res.Connection
		if conn.ID == "" || conn.Service != "gitlab" {
			t.Fatalf("unexpected connection view: %+v", conn)
		}
		if conn.Origin != gitlab1.srv.URL {
			t.Errorf("expected the fixture origin %q stored, got %q", gitlab1.srv.URL, conn.Origin)
		}
		if conn.Status != "connected" {
			t.Errorf("expected a probe-gated connected status, got %q", conn.Status)
		}
		if conn.AccessLevel != domain.ConnectionAccessReadWrite {
			t.Errorf("expected read_write stored, got %q", conn.AccessLevel)
		}
		if conn.TokenHint != gitlabSmokeHint {
			t.Errorf("expected the last-4 hint %q, got %q", gitlabSmokeHint, conn.TokenHint)
		}
		if strings.Contains(w.Body.String(), gitlabSmokeToken) {
			t.Error("connect response leaked the plaintext token")
		}

		// Exactly one upstream call so far: the probe, on the declared path,
		// carrying the PAT verbatim (no scheme — PRIVATE-TOKEN rides raw).
		hits := gitlab1.captured()
		if len(hits) != 1 {
			t.Fatalf("expected exactly one probe call, got %+v", hits)
		}
		if hits[0].Method != http.MethodGet || hits[0].Path != "/api/v4/user" {
			t.Errorf("expected the declared probe GET /api/v4/user, got %s %s", hits[0].Method, hits[0].Path)
		}
		if hits[0].Token != gitlabSmokeToken {
			t.Errorf("expected the PAT in PRIVATE-TOKEN, got %q", hits[0].Token)
		}
	})

	// --- Attach by CONNECTION id through the existing enabled_mcps patch
	// (http kind materializes no server row), the same hand-off the connect
	// dialog performs. ---
	var agent struct {
		ID string `json:"id"`
	}
	t.Run("attach the connection to an agent by connection id", func(t *testing.T) {
		wProv := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/gitlab-smoke-ws/providers", ownerToken, map[string]any{
			"type": "openai", "name": "OpenAI Prod", "key": "sk-test-key-openai",
		})
		var provRes struct {
			Provider struct {
				ID string `json:"id"`
			} `json:"provider"`
		}
		if err := json.Unmarshal(wProv.Body.Bytes(), &provRes); err != nil || provRes.Provider.ID == "" {
			t.Fatalf("provider create: %d %s", wProv.Code, wProv.Body.String())
		}
		wAgent := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/gitlab-smoke-ws/agents", ownerToken, map[string]any{
			"name": "Atlas", "slug": "atlas", "role": "tester", "brief": "brief",
			"provider_id": provRes.Provider.ID, "model": "gpt-4o",
		})
		if wAgent.Code != http.StatusCreated {
			t.Fatalf("agent create: %d %s", wAgent.Code, wAgent.Body.String())
		}
		var agentRes struct {
			Agent struct {
				ID string `json:"id"`
			} `json:"agent"`
		}
		if err := json.Unmarshal(wAgent.Body.Bytes(), &agentRes); err != nil {
			t.Fatalf("decode agent: %v", err)
		}
		agent.ID = agentRes.Agent.ID
		if agent.ID == "" {
			t.Fatalf("agent create returned no id: %s", wAgent.Body.String())
		}

		wPatch := doRequest(env.router, http.MethodPatch, "/api/v1/workspaces/gitlab-smoke-ws/agents/atlas", ownerToken, map[string]any{
			"enabled_mcps": []string{conn.ID},
		})
		if wPatch.Code != http.StatusOK {
			t.Fatalf("enabled_mcps patch: %d %s", wPatch.Code, wPatch.Body.String())
		}
		// The runner's workspace files need the agent directory to exist.
		agentDir := domain.AgentWorkspaceDir(domain.WorkspaceRoot(onClawDir), ws.Slug, "atlas")
		if err := os.MkdirAll(agentDir, 0o755); err != nil {
			t.Fatalf("mkdir agent dir: %v", err)
		}
	})

	// --- Read verb: gitlab.list_projects dials the RESOLVED origin's
	// declared /api/v4/projects with the token. ---
	t.Run("read verb gitlab.list_projects dials the fixture origin with the token", func(t *testing.T) {
		scripted.script("gitlab.list_projects", `{}`)
		events := runGitlabTurn(ctx, t, runner, ws.ID, agent.ID, owner.ID, "sess-gitlab-read", "list the projects")
		result := toolResultOf(t, events, "gitlab.list_projects")
		if !strings.Contains(result, "acme/ops") {
			t.Errorf("expected the mock project body in the tool result, got %q", result)
		}

		hits := gitlab1.captured()
		if len(hits) != 2 {
			t.Fatalf("expected probe + read on the first origin, got %+v", hits)
		}
		read := hits[1]
		if read.Method != http.MethodGet || read.Path != "/api/v4/projects" {
			t.Errorf("expected the declared GET /api/v4/projects, got %s %s", read.Method, read.Path)
		}
		if read.Token != gitlabSmokeToken {
			t.Errorf("expected the PAT in PRIVATE-TOKEN on the read verb, got %q", read.Token)
		}
	})

	// --- Write verb: gitlab.create_issue posts through the declared path
	// template with its query parameters and the token. ---
	t.Run("write verb gitlab.create_issue dials the fixture origin with the token", func(t *testing.T) {
		scripted.script("gitlab.create_issue", `{"project_id":"42","title":"Smoke write"}`)
		events := runGitlabTurn(ctx, t, runner, ws.ID, agent.ID, owner.ID, "sess-gitlab-write", "create an issue")
		result := toolResultOf(t, events, "gitlab.create_issue")
		if !strings.Contains(result, `"iid":7`) {
			t.Errorf("expected the mock issue body in the tool result, got %q", result)
		}

		hits := gitlab1.captured()
		if len(hits) != 3 {
			t.Fatalf("expected probe + read + write on the first origin, got %+v", hits)
		}
		write := hits[2]
		if write.Method != http.MethodPost || write.Path != "/api/v4/projects/42/issues" {
			t.Errorf("expected the declared POST /api/v4/projects/42/issues, got %s %s", write.Method, write.Path)
		}
		if !strings.Contains(write.Query, "title=") {
			t.Errorf("expected the write verb's query parameters, got %q", write.Query)
		}
		if write.Token != gitlabSmokeToken {
			t.Errorf("expected the PAT in PRIVATE-TOKEN on the write verb, got %q", write.Token)
		}
	})

	// --- Same origin again: the (service, origin) uniqueness conflict names
	// the origin and the existing connection. ---
	t.Run("same origin twice is a 409 naming the origin", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, base+"/connections", ownerToken, map[string]any{
			"recipe_id": "gitlab", "token": "another-pat", "origin": gitlab1.srv.URL,
		})
		if w.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict, got %d: %s", w.Code, w.Body.String())
		}
		var envErr server.ErrorEnvelope
		_ = json.Unmarshal(w.Body.Bytes(), &envErr)
		if envErr.Error.Code != server.CodeConflict {
			t.Errorf("expected conflict code, got %q", envErr.Error.Code)
		}
		if !strings.Contains(envErr.Error.Message, gitlab1.srv.URL) {
			t.Errorf("expected the conflict to name the origin, got %q", envErr.Error.Message)
		}
		if !strings.Contains(envErr.Error.Message, conn.ID) {
			t.Errorf("expected the conflict to name the existing connection, got %q", envErr.Error.Message)
		}
		// The rejected connect probed nothing and stored nothing.
		if hits := gitlab1.captured(); len(hits) != 3 {
			t.Errorf("the conflict must precede any probe, got %+v", hits)
		}
		wList := doRequest(env.router, http.MethodGet, base+"/connections", ownerToken, nil)
		var list struct {
			Connections []struct {
				ID string `json:"id"`
			} `json:"connections"`
		}
		_ = json.Unmarshal(wList.Body.Bytes(), &list)
		if len(list.Connections) != 1 {
			t.Errorf("expected only the first connection to survive, got %d", len(list.Connections))
		}
	})

	// --- Different origin: an independent connection, probed on its own
	// instance, side by side with the first. ---
	t.Run("different origin connects side by side", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, base+"/connections", ownerToken, map[string]any{
			"recipe_id": "gitlab", "token": gitlabSmokeToken, "origin": gitlab2.srv.URL,
		})
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 on the second origin, got %d: %s", w.Code, w.Body.String())
		}
		var res struct {
			Connection struct {
				ID     string `json:"id"`
				Origin string `json:"origin"`
				Status string `json:"status"`
			} `json:"connection"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("decode: %v (%s)", err, w.Body.String())
		}
		if res.Connection.ID == "" || res.Connection.ID == conn.ID {
			t.Fatalf("expected an independent connection row, got %+v", res.Connection)
		}
		if res.Connection.Origin != gitlab2.srv.URL {
			t.Errorf("expected the second origin %q stored, got %q", gitlab2.srv.URL, res.Connection.Origin)
		}
		if res.Connection.Status != "connected" {
			t.Errorf("expected the second origin's probe to connect, got %q", res.Connection.Status)
		}

		// The second origin saw its own probe and nothing else; the first
		// origin is untouched by the second connect.
		hits2 := gitlab2.captured()
		if len(hits2) != 1 || hits2[0].Path != "/api/v4/user" || hits2[0].Token != gitlabSmokeToken {
			t.Errorf("expected one probe on the second origin, got %+v", hits2)
		}
		if hits := gitlab1.captured(); len(hits) != 3 {
			t.Errorf("the second connect must not dial the first origin, got %+v", hits)
		}

		// Both connections stand side by side, each carrying its own origin.
		wList := doRequest(env.router, http.MethodGet, base+"/connections", ownerToken, nil)
		var list struct {
			Connections []struct {
				ID     string `json:"id"`
				Origin string `json:"origin"`
			} `json:"connections"`
		}
		if err := json.Unmarshal(wList.Body.Bytes(), &list); err != nil {
			t.Fatalf("decode list: %v (%s)", err, wList.Body.String())
		}
		if len(list.Connections) != 2 {
			t.Fatalf("expected both origins side by side, got %d: %s", len(list.Connections), wList.Body.String())
		}
		origins := map[string]bool{}
		for _, c := range list.Connections {
			origins[c.Origin] = true
		}
		if !origins[gitlab1.srv.URL] || !origins[gitlab2.srv.URL] {
			t.Errorf("expected both fixture origins in the list, got %+v", list.Connections)
		}
	})
}
