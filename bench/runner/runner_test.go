package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw-bench/driver"
	"github.com/oniharnantyo/onclaw-bench/scoreboard"
)

// controlFileRe extracts the runner's control-file path (the
// {{control_file}} substitution — a full temp path) from a recorded
// command-hook script body.
var controlFileRe = regexp.MustCompile(`\S*onclaw-bench-block-[0-9]+\.txt`)

// fakeOnclaw is a hermetic stand-in for the OnClaw server surface the runner
// drives: provisioning endpoints (driver), the /v1 chat path, and the hook /
// connection / agent-patch setup surfaces. It records every mutation so the
// tests can assert the runner's observable behavior — substituted control
// URLs, teardown calls, baseline restores — without any live model.
type fakeOnclaw struct {
	mu sync.Mutex

	agentModel     string // model the fixture agent reports
	agentWindow    *int   // context window the fixture agent reports (PATCH-applied)
	measuredStatus string // run status returned for measured turns

	measuredCalls int                  // calls whose session is a measured bench session
	failNext      int                  // number of upcoming measured calls to answer 500
	hookCreated   int                  // created hook count
	hookDeleted   int                  // deleted hook count
	hookPayloads  []driver.HookPayload // create payloads in order
	// hookScripts records command-hook script contents read from disk at
	// create time; controlFiles records control-file paths referenced by a
	// script and whether the file existed at create time.
	hookScripts  map[string]string
	controlFiles map[string]bool
	connCreated  int              // created connections
	connDeleted  int              // deleted connections
	patches      []map[string]any // agent PATCH bodies in order
}

func newFakeOnclaw() *fakeOnclaw {
	return &fakeOnclaw{
		agentModel:     "glm-test-model",
		measuredStatus: "completed",
		hookScripts:    map[string]string{},
		controlFiles:   map[string]bool{},
	}
}

func (f *fakeOnclaw) handler() http.Handler {
	mux := http.NewServeMux()

	// auth
	mux.HandleFunc("POST /api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Email string `json:"email"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		writeJSON(w, http.StatusOK, map[string]any{
			"token": "tok-" + in.Email,
			"user":  map[string]any{"id": "user-" + in.Email, "email": in.Email},
		})
	})
	mux.HandleFunc("POST /api/v1/admin/users", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{"user": map[string]any{"id": "user-new"}})
	})

	const ws = "bench-eval"
	base := "/api/v1/workspaces/" + ws

	mux.HandleFunc("GET "+base, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"workspace": map[string]any{"id": "ws-1", "slug": ws, "name": "Bench Eval Workspace"}})
	})
	mux.HandleFunc("GET "+base+"/roles", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"roles": []any{map[string]any{"id": "role-member", "name": "Member"}}})
	})
	mux.HandleFunc("POST "+base+"/members", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"member": map[string]any{}})
	})
	mux.HandleFunc("POST "+base+"/api-keys/exchange", func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer tok-")
		writeJSON(w, http.StatusOK, map[string]any{"key": "ck-" + token})
	})
	mux.HandleFunc("GET "+base+"/providers", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"providers": []any{map[string]any{
			"id": "prov-1", "name": "fake-provider", "type": "openai-compatible", "enabled": true,
		}}})
	})

	mux.HandleFunc("GET "+base+"/agents/bench-agent", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		model, window := f.agentModel, f.agentWindow
		f.mu.Unlock()
		agent := map[string]any{
			"id": "agent-1", "slug": "bench-agent", "name": "Bench Fixture Agent",
			"provider_id": "prov-1", "model": model, "disabled_tools": []any{},
		}
		if window != nil {
			agent["context_window"] = *window
		}
		writeJSON(w, http.StatusOK, map[string]any{"agent": agent})
	})
	mux.HandleFunc("PATCH "+base+"/agents/agent-1", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.patches = append(f.patches, body)
		if m, ok := body["model"].(string); ok && m != "" {
			f.agentModel = m
		}
		if n, ok := body["context_window"].(float64); ok {
			w := int(n)
			f.agentWindow = &w
		}
		f.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"agent": map[string]any{}})
	})

	mux.HandleFunc("POST "+base+"/hooks", func(w http.ResponseWriter, r *http.Request) {
		var payload driver.HookPayload
		_ = json.NewDecoder(r.Body).Decode(&payload)
		switch payload.HandlerType {
		case "http":
			if !strings.Contains(string(payload.Config), "http://127.0.0.1:") {
				http.Error(w, "hook config must point at the bench control server", http.StatusBadRequest)
				return
			}
		case "command":
			// The runner registers command handlers as /bin/sh exec form
			// pointing at a materialized temp script; record the script
			// content (read from the shared filesystem at create time) and
			// whether a referenced control file existed.
			var cfg struct {
				Command string   `json:"command"`
				Args    []string `json:"args"`
			}
			_ = json.Unmarshal(payload.Config, &cfg)
			if cfg.Command != "/bin/sh" || len(cfg.Args) != 1 {
				http.Error(w, "command hook config must be /bin/sh exec form with one script arg", http.StatusBadRequest)
				return
			}
			content, err := os.ReadFile(cfg.Args[0])
			if err != nil {
				http.Error(w, "command hook script does not exist at create time", http.StatusBadRequest)
				return
			}
			f.mu.Lock()
			f.hookScripts[cfg.Args[0]] = string(content)
			if m := controlFileRe.FindString(string(content)); m != "" {
				_, statErr := os.Stat(m)
				f.controlFiles[m] = statErr == nil
			}
			f.mu.Unlock()
		default:
			http.Error(w, "unsupported hook handler type", http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.hookCreated++
		id := fmt.Sprintf("hook-%d", f.hookCreated)
		f.hookPayloads = append(f.hookPayloads, payload)
		f.mu.Unlock()
		writeJSON(w, http.StatusCreated, map[string]any{"hook": map[string]any{"id": id}})
	})
	mux.HandleFunc("DELETE "+base+"/hooks/{id}", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		f.hookDeleted++
		f.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
	})

	mux.HandleFunc("POST "+base+"/integrations/connections", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		f.connCreated++
		f.mu.Unlock()
		writeJSON(w, http.StatusCreated, map[string]any{"connection": map[string]any{"id": "conn-1", "service": "github", "status": "connected"}})
	})
	mux.HandleFunc("DELETE "+base+"/integrations/connections/conn-1", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		f.connDeleted++
		f.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
	})

	// The /v1 chat path: setup sessions get a plain completed turn; measured
	// bench sessions get the scripted transcript (tool call + answer).
	mux.HandleFunc("POST /v1/responses", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Metadata struct {
				OnclawSession string `json:"onclaw_session"`
			} `json:"metadata"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		session := in.Metadata.OnclawSession
		if !strings.HasPrefix(session, "bench-") {
			writeJSON(w, http.StatusOK, chatResponse("completed"))
			return
		}

		// Measured session: injectable failures land here only, so a 500 on
		// the measured turn is exactly what the retry tests observe. Every
		// measured attempt counts, failures included.
		f.mu.Lock()
		defer f.mu.Unlock()
		f.measuredCalls++
		if f.failNext > 0 {
			f.failNext--
			http.Error(w, "upstream 500", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, chatResponse(f.measuredStatus))
	})

	return mux
}

// chatResponse builds a /v1/responses transcript: one write_file tool call
// with its paired output, then the folded answer message.
func chatResponse(status string) map[string]any {
	output := []any{
		map[string]any{
			"type":      "function_call",
			"name":      "write_file",
			"call_id":   "call-1",
			"arguments": `{"file_path":"/workspace/notes.md","content":"alpha bravo"}`,
		},
		map[string]any{
			"type":    "onclaw.function_call_output",
			"call_id": "call-1",
			"result":  "wrote /workspace/notes.md",
		},
		map[string]any{
			"type": "message",
			"content": []any{
				map[string]any{"type": "output_text", "text": "Wrote the note to the file."},
			},
		},
	}
	return map[string]any{"status": status, "output": output}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// e2eSuite exercises every deterministic check kind plus the setup-step
// surface: seeded chat turns, an agent denylist override, a blocking hook,
// and a connection.
const e2eSuite = `{
  "suite_version": "1",
  "lane": "tool-error",
  "description": "hermetic end-to-end fixture",
  "judge_allowed": false,
  "tasks": [
    {
      "id": "note-write",
      "title": "Note write",
      "summary": "Seeded context, then a file write the transcript must show.",
      "tools_used": ["files.write"],
      "setup": [
        {"type": "chat_turns", "actor": "casey", "session": "context", "turns": ["Seeding: the vault file is notes.md."]},
        {"type": "agent_config", "disabled_tools": ["web.search"]},
        {"type": "hook", "name": "bench-block-web", "event": "pre_tool_use", "matcher_tool": "web.search", "handler": {"type": "http", "url": "{{control_url}}/block"}},
        {"type": "hook", "name": "bench-block-shell-cmd", "event": "pre_tool_use", "matcher_tool": "execute", "handler": {"type": "command", "command": "echo 'blocked by bench control hook' >&2\nexit 2"}},
        {"type": "connection", "name": "bench-conn", "recipe": "github_pat", "config": {"token": "fake-token", "access_level": "read"}}
      ],
      "turns": [
        {"actor": "casey", "text": "Write the vault contents into notes.md and tell me what you did."}
      ],
      "verify": [
        {"kind": "answer_contains", "any_of": [["wrote the note"]]},
        {"kind": "tool_called", "tool": "files.write", "args_contains": "notes.md"},
        {"kind": "transcript_file_write", "path": "notes.md", "content_contains": "alpha bravo"},
        {"kind": "tool_not_called", "tool": "web.search"},
        {"kind": "tool_not_called", "tool": "execute"},
        {"kind": "api_effect", "method": "GET", "path": "/api/v1/workspaces/{{workspace}}/agents/{{agent}}", "status": 200, "json_contains": ["bench-agent"]}
      ]
    }
  ]
}`

// TestRunEndToEnd drives one task end to end against the fake server:
// provisioning, setup steps in order, measured turns, every deterministic
// check, teardown + baseline restore, and scoreboard assembly.
func TestRunEndToEnd(t *testing.T) {
	fake := newFakeOnclaw()
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	dir := t.TempDir()
	suitePath := writeTempFile(t, dir, "e2e.json", e2eSuite)
	outPath := writeTempOut(t)

	board, err := Run(context.Background(), Options{
		BaseURL:       srv.URL,
		AdminEmail:    "admin@test",
		AdminPassword: "pw",
		WorkspaceSlug: "bench-eval",
		AgentSlug:     "bench-agent",
		Model:         "glm-test-model",
		SuiteSpec:     suitePath,
		Out:           outPath,
		Seeds:         1,
		Retries:       0,
		Backoff:       10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Task verdict + checks.
	if len(board.Tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(board.Tasks))
	}
	res := board.Tasks[0]
	if res.Status != scoreboard.StatusPass {
		t.Fatalf("status = %s (checks %+v answer %q error %q), want pass", res.Status, res.Checks, res.Answer, res.Error)
	}
	if len(res.Checks) != 6 {
		t.Fatalf("checks = %d, want 6", len(res.Checks))
	}
	for _, c := range res.Checks {
		if !c.Passed {
			t.Errorf("check %s failed: %s", c.Kind, c.Detail)
		}
	}
	if res.ToolCalls != 1 || !strings.Contains(res.Answer, "Wrote the note") {
		t.Fatalf("transcript pooling off: tool_calls=%d answer=%q", res.ToolCalls, res.Answer)
	}

	// Scoreboard identity fields.
	if board.SuiteVersion != "1" || board.ModelPin != "glm-test-model" {
		t.Fatalf("identity fields: suite=%q model=%q", board.SuiteVersion, board.ModelPin)
	}
	if board.OnclawGitSHA == "" {
		t.Fatal("git sha must be recorded (unknown on error, never empty)")
	}
	if board.Seeds != 1 || board.RunID == "" || board.InfraErrors != 0 {
		t.Fatalf("run fields: seeds=%d run=%q infra=%d", board.Seeds, board.RunID, board.InfraErrors)
	}
	if board.Overall.PassRate != 1.0 || len(board.Lanes) != 1 || board.Lanes[0].Lane != "tool-error" {
		t.Fatalf("aggregates: overall=%+v lanes=%+v", board.Overall, board.Lanes)
	}

	// The artifact on disk: indented JSON with a trailing newline that parses
	// back as a scoreboard.
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading %s: %v", outPath, err)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Fatal("scoreboard file must end with a trailing newline")
	}
	var reloaded scoreboard.Scoreboard
	if err := json.Unmarshal(data, &reloaded); err != nil {
		t.Fatalf("scoreboard file does not parse: %v", err)
	}
	if len(reloaded.Tasks) != 1 || reloaded.Tasks[0].Status != scoreboard.StatusPass {
		t.Fatalf("reloaded scoreboard rows: %+v", reloaded.Tasks)
	}

	// Setup surface behavior: both hooks (http + command) created and deleted
	// afterwards; connection created and deleted.
	fake.mu.Lock()
	created, deleted := fake.hookCreated, fake.hookDeleted
	payloads := append([]driver.HookPayload(nil), fake.hookPayloads...)
	scripts := map[string]string{}
	for k, v := range fake.hookScripts {
		scripts[k] = v
	}
	controlFiles := map[string]bool{}
	for k, v := range fake.controlFiles {
		controlFiles[k] = v
	}
	connC, connD := fake.connCreated, fake.connDeleted
	fake.mu.Unlock()
	if created != 2 || deleted != 2 {
		t.Fatalf("hook lifecycle: created=%d deleted=%d, want 2/2", created, deleted)
	}
	if connC != 1 || connD != 1 {
		t.Fatalf("connection lifecycle: created=%d deleted=%d, want 1/1", connC, connD)
	}

	// The command hook: registered as /bin/sh exec form pointing at a real
	// temp script whose content is the verbatim suite body; teardown removed
	// the script after the hook.
	var cmdPayload *driver.HookPayload
	for i := range payloads {
		if payloads[i].HandlerType == "command" {
			cmdPayload = &payloads[i]
			break
		}
	}
	if cmdPayload == nil {
		t.Fatal("no command handler hook was registered")
	}
	var cfg struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	if err := json.Unmarshal(cmdPayload.Config, &cfg); err != nil {
		t.Fatalf("command hook config does not parse: %v", err)
	}
	const cmdBody = "echo 'blocked by bench control hook' >&2\nexit 2"
	if cfg.Command != "/bin/sh" || len(cfg.Args) != 1 {
		t.Fatalf("command hook config = %s, want /bin/sh exec form with one script arg", cmdPayload.Config)
	}
	scriptPath := cfg.Args[0]
	if filepath.Dir(scriptPath) != filepath.Clean(os.TempDir()) ||
		!strings.HasPrefix(filepath.Base(scriptPath), "onclaw-bench-hook-") ||
		!strings.HasSuffix(scriptPath, ".sh") {
		t.Fatalf("script path %s is not a bench hook temp script under the temp dir", scriptPath)
	}
	if got := scripts[scriptPath]; got != cmdBody {
		t.Fatalf("script content = %q, want the substituted body %q", got, cmdBody)
	}
	if len(controlFiles) != 0 {
		t.Fatalf("a body without {{control_file}} must not create a control file, got %v", controlFiles)
	}
	if _, err := os.Stat(scriptPath); !os.IsNotExist(err) {
		t.Fatalf("command hook script %s still exists after teardown", scriptPath)
	}

	// Baseline restore: the task's denylist override is repaired back to the
	// pinned baseline (empty denylist) in a final PATCH.
	fake.mu.Lock()
	lastPatch := fake.patches[len(fake.patches)-1]
	fake.mu.Unlock()
	tools, ok := lastPatch["disabled_tools"].([]any)
	if !ok || len(tools) != 0 {
		t.Fatalf("final patch should restore the empty baseline denylist, got %v", lastPatch)
	}
}

// TestRunRetriesInfraErrors pins the infra classification: a 5xx during the
// measured turn is infra-error, retried within the budget with backoff, and
// the successful attempt records a pass.
func TestRunRetriesInfraErrors(t *testing.T) {
	fake := newFakeOnclaw()
	fake.failNext = 1
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	dir := t.TempDir()
	suitePath := writeTempFile(t, dir, "e2e.json", e2eSuite)

	board, err := Run(context.Background(), Options{
		BaseURL: srv.URL, AdminEmail: "admin@test", AdminPassword: "pw",
		WorkspaceSlug: "bench-eval", AgentSlug: "bench-agent", Model: "glm-test-model",
		SuiteSpec: suitePath, Out: writeTempOut(t), Seeds: 1, Retries: 2, Backoff: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if board.Tasks[0].Status != scoreboard.StatusPass {
		t.Fatalf("status = %s, want pass after retry", board.Tasks[0].Status)
	}
	if board.Tasks[0].Error != "" {
		t.Fatalf("a passing task must not carry an error, got %q", board.Tasks[0].Error)
	}
	fake.mu.Lock()
	calls := fake.measuredCalls
	fake.mu.Unlock()
	if calls != 2 {
		t.Fatalf("measured calls = %d, want 2 (one 500 + the retry)", calls)
	}
}

// TestRunExhaustedRetriesAreInfra pins that an exhausted retry budget leaves
// the row as infra-error with the cause, and infra errors are tallied.
func TestRunExhaustedRetriesAreInfra(t *testing.T) {
	fake := newFakeOnclaw()
	inner := fake.handler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/responses" {
			http.Error(w, "upstream down", http.StatusInternalServerError)
			return
		}
		inner.ServeHTTP(w, r)
	}))
	defer srv.Close()

	dir := t.TempDir()
	suitePath := writeTempFile(t, dir, "e2e.json", e2eSuite)

	board, err := Run(context.Background(), Options{
		BaseURL: srv.URL, AdminEmail: "admin@test", AdminPassword: "pw",
		WorkspaceSlug: "bench-eval", AgentSlug: "bench-agent", Model: "glm-test-model",
		SuiteSpec: suitePath, Out: writeTempOut(t), Seeds: 1, Retries: 1, Backoff: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	res := board.Tasks[0]
	if res.Status != scoreboard.StatusInfraError {
		t.Fatalf("status = %s, want infra-error", res.Status)
	}
	if !strings.Contains(res.Error, "500") {
		t.Fatalf("infra row should carry the cause, got %q", res.Error)
	}
	if board.InfraErrors != 1 || board.Overall.TasksRun != 0 {
		t.Fatalf("infra tally: infra_errors=%d tasks_run=%d (infra excluded from the denominator)", board.InfraErrors, board.Overall.TasksRun)
	}
}

// TestRunFailedTurnIsRecordedNotRetried pins the FAIL classification: a /v1
// run that answers 200 with status != completed fails the task once — no
// retry, error recorded, infra tally untouched.
func TestRunFailedTurnIsRecordedNotRetried(t *testing.T) {
	fake := newFakeOnclaw()
	fake.measuredStatus = "prompt_blocked"
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	dir := t.TempDir()
	suitePath := writeTempFile(t, dir, "e2e.json", e2eSuite)

	board, err := Run(context.Background(), Options{
		BaseURL: srv.URL, AdminEmail: "admin@test", AdminPassword: "pw",
		WorkspaceSlug: "bench-eval", AgentSlug: "bench-agent", Model: "glm-test-model",
		SuiteSpec: suitePath, Out: writeTempOut(t), Seeds: 1, Retries: 3, Backoff: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	res := board.Tasks[0]
	if res.Status != scoreboard.StatusFail {
		t.Fatalf("status = %s, want fail", res.Status)
	}
	if !strings.Contains(res.Error, "prompt_blocked") {
		t.Fatalf("fail row should carry the run status cause, got %q", res.Error)
	}
	if board.InfraErrors != 0 {
		t.Fatalf("a task failure is never an infra error, got %d", board.InfraErrors)
	}
	fake.mu.Lock()
	calls := fake.measuredCalls
	fake.mu.Unlock()
	if calls != 1 {
		t.Fatalf("measured calls = %d, want 1 (failures are not retried)", calls)
	}
}

// breakerSuiteJSON builds a nine-task suite so a run outlives the six-failure
// breaker trip.
func breakerSuiteJSON() string {
	var b strings.Builder
	b.WriteString(`{"suite_version":"1","lane":"tool-error","description":"breaker fixture","judge_allowed":false,"tasks":[`)
	for i := 0; i < 9; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"id":"task-%02d","title":"T","summary":"Filler task for the breaker test.","tools_used":["files.write"],"setup":[],"turns":[{"actor":"casey","text":"do the thing"}],"verify":[{"kind":"answer_contains","any_of":[["wrote the note"]]}]}`, i)
	}
	b.WriteString("]}")
	return b.String()
}

// TestProviderCircuitBreakerAbortsRun pins the breaker: a run whose measured
// turns fail server-side six consecutive times aborts with errProviderDead,
// no scoreboard is written, and the run stops AT the limit instead of burning
// the rest of the suite on a dead endpoint.
func TestProviderCircuitBreakerAbortsRun(t *testing.T) {
	fake := newFakeOnclaw()
	fake.measuredStatus = "failed"
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	dir := t.TempDir()
	suitePath := writeTempFile(t, dir, "breaker.json", breakerSuiteJSON())

	board, err := Run(context.Background(), Options{
		BaseURL: srv.URL, AdminEmail: "admin@test", AdminPassword: "pw",
		WorkspaceSlug: "bench-eval", AgentSlug: "bench-agent", Model: "glm-test-model",
		SuiteSpec: suitePath, Out: writeTempOut(t), Seeds: 1, Retries: 1, Backoff: 1 * time.Millisecond,
	})
	if !errors.Is(err, errProviderDead) {
		t.Fatalf("want errProviderDead, got %v", err)
	}
	if board != nil {
		t.Fatal("no scoreboard must be written when the breaker trips")
	}
	fake.mu.Lock()
	calls := fake.measuredCalls
	fake.mu.Unlock()
	if calls != providerDeadStreakLimit {
		t.Fatalf("measured calls = %d, want %d (run must stop at the limit)", calls, providerDeadStreakLimit)
	}
}

// TestResumeContinuesAbortedRun pins the resume contract: an aborted run
// journals its completed (task, seed) slots; a resumed run skips them, runs
// only the remainder, and the assembled scoreboard covers both phases.
func TestResumeContinuesAbortedRun(t *testing.T) {
	fake := newFakeOnclaw()
	fake.measuredStatus = "failed"
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	dir := t.TempDir()
	suitePath := writeTempFile(t, dir, "breaker.json", breakerSuiteJSON())
	outPath := writeTempOut(t)
	runOpts := Options{
		BaseURL: srv.URL, AdminEmail: "admin@test", AdminPassword: "pw",
		WorkspaceSlug: "bench-eval", AgentSlug: "bench-agent", Model: "glm-test-model",
		SuiteSpec: suitePath, Out: outPath, Seeds: 1, Retries: 1, Backoff: 1 * time.Millisecond,
	}

	if _, err := Run(context.Background(), runOpts); !errors.Is(err, errProviderDead) {
		t.Fatalf("phase 1: want errProviderDead, got %v", err)
	}

	fake.mu.Lock()
	fake.measuredStatus = "completed"
	fake.mu.Unlock()
	board, err := Run(context.Background(), Options(func() Options {
		o := runOpts
		o.Resume = true
		return o
	}()))
	if err != nil {
		t.Fatalf("phase 2 (resume): %v", err)
	}
	// The breaker discarded its 6 dead-streak results from the journal, so
	// the resumed run re-measures them against the recovered endpoint: the
	// scoreboard holds all 9 tasks, all passing (3 measured pre-flap + 6
	// re-measured post-resume — outage windows never reach a scoreboard).
	if len(board.Tasks) != 9 {
		t.Fatalf("resumed scoreboard has %d tasks, want 9", len(board.Tasks))
	}
	for i, res := range board.Tasks {
		if res.Status != scoreboard.StatusPass {
			t.Fatalf("task[%d] status = %s, want pass (dead-streak results must not survive the breaker)", i, res.Status)
		}
	}
}

// TestRunSeedOnlyAndScoreOnly pins the provisioning split: seed-only
// provisions and exits (no tasks, no scoreboard); score-only skips
// provisioning (reads the existing fixture directly, no provider bootstrap)
// and still scores.
func TestRunSeedOnlyAndScoreOnly(t *testing.T) {
	fake := newFakeOnclaw()
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	dir := t.TempDir()
	suitePath := writeTempFile(t, dir, "e2e.json", e2eSuite)

	// Seed-only: no --model needed to provision? Provision requires the
	// model pin; seed-only carries it and exits before any task runs.
	board, err := Run(context.Background(), Options{
		BaseURL: srv.URL, AdminEmail: "admin@test", AdminPassword: "pw",
		WorkspaceSlug: "bench-eval", AgentSlug: "bench-agent", Model: "glm-test-model",
		SeedOnly: true,
	})
	if err != nil {
		t.Fatalf("seed-only Run: %v", err)
	}
	if board != nil {
		t.Fatalf("seed-only must not produce a scoreboard, got %+v", board)
	}

	// Score-only: reads the fixture (login + GetAgent + chat keys), scores
	// the suite. No model flag: the stored agent's model is the pin.
	board, err = Run(context.Background(), Options{
		BaseURL: srv.URL, AdminEmail: "admin@test", AdminPassword: "pw",
		WorkspaceSlug: "bench-eval", AgentSlug: "bench-agent",
		SuiteSpec: suitePath, Out: writeTempOut(t), Seeds: 1, Retries: 0, Backoff: 5 * time.Millisecond,
		ScoreOnly: true,
	})
	if err != nil {
		t.Fatalf("score-only Run: %v", err)
	}
	if board.ModelPin != "glm-test-model" {
		t.Fatalf("score-only model pin = %q, want the stored agent's model", board.ModelPin)
	}
	if board.Tasks[0].Status != scoreboard.StatusPass {
		t.Fatalf("score-only status = %s, want pass", board.Tasks[0].Status)
	}
}

// TestDryRunRecordsIdentity pins tasks.md 2.2's dry-run acceptance: a valid
// scoreboard with suite version, git sha, and model pin recorded — and no
// server contact at all (no base URL is even provided a reachable server).
func TestDryRunRecordsIdentity(t *testing.T) {
	dir := t.TempDir()
	suitePath := writeTempFile(t, dir, "stub.json", e2eSuite)
	outPath := writeTempOut(t)

	board, err := Run(context.Background(), Options{
		BaseURL:   "http://127.0.0.1:1", // nothing listens here
		Model:     "glm-test-model",
		SuiteSpec: suitePath,
		Out:       outPath,
		Seeds:     3,
		DryRun:    true,
	})
	if err != nil {
		t.Fatalf("dry-run Run: %v", err)
	}
	if board.ModelPin != "glm-test-model" || board.SuiteVersion != "1" {
		t.Fatalf("dry-run identity: model=%q suite=%q", board.ModelPin, board.SuiteVersion)
	}
	if board.OnclawGitSHA == "" {
		t.Fatal("dry-run must record the git sha")
	}
	if board.Seeds != 3 || len(board.Tasks) != 3 {
		t.Fatalf("dry-run plan: seeds=%d rows=%d, want 3/3", board.Seeds, len(board.Tasks))
	}
	for _, r := range board.Tasks {
		if r.Status != scoreboard.StatusFail || r.Error != "dry-run (no model call)" || len(r.Checks) != 0 {
			t.Fatalf("dry-run row %+v, want recorded fail with empty checks", r)
		}
	}
	if board.Overall.TasksRun != 3 || board.Overall.PassRate != 0 {
		t.Fatalf("dry-run aggregates: %+v", board.Overall)
	}
}

// TestDryRunRejectsBadSuites pins that dry-run still lints (it is the lint
// entry point of the protocol) and validates options.
func TestDryRunRejectsBadSuites(t *testing.T) {
	dir := t.TempDir()
	broken := strings.Replace(e2eSuite, `"any_of": [["wrote the note"]]`, `"any_of": [["wrote the note"],[""]]`, 1)
	suitePath := writeTempFile(t, dir, "broken.json", broken)

	if _, err := Run(context.Background(), Options{
		Model: "m", SuiteSpec: suitePath, Out: writeTempOut(t), Seeds: 1, DryRun: true,
	}); err == nil || !strings.Contains(err.Error(), "lint") {
		t.Fatalf("dry-run with a lint-broken suite should fail naming lint, got %v", err)
	}
}

// TestBackoffDelay pins the bounded exponential backoff arithmetic.
func TestBackoffDelay(t *testing.T) {
	base := 5 * time.Second
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 5 * time.Second},
		{1, 10 * time.Second},
		{2, 20 * time.Second},
		{3, 40 * time.Second},
		{4, 60 * time.Second},
		{9, 60 * time.Second}, // capped
	}
	for _, tc := range tests {
		if got := backoffDelay(base, tc.attempt); got != tc.want {
			t.Errorf("backoffDelay(attempt %d) = %s, want %s", tc.attempt, got, tc.want)
		}
	}
}

// commandHookControlSuite exercises the {{control_file}} provisioning path:
// a command hook whose body references the placeholder, so the runner must
// create the control file, substitute its path into the script, and remove
// both files at teardown.
const commandHookControlSuite = `{
  "suite_version": "1",
  "lane": "tool-error",
  "description": "command hook with a control file",
  "judge_allowed": false,
  "tasks": [
    {
      "id": "cmd-control-file",
      "title": "Command hook control file",
      "summary": "A command hook whose script body branches on the bench control file.",
      "tools_used": ["files.write"],
      "setup": [
        {"type": "hook", "name": "bench-cmd-control", "event": "pre_tool_use", "matcher_tool": "execute", "handler": {"type": "command", "command": "cat {{control_file}} >&2\nexit 2"}}
      ],
      "turns": [
        {"actor": "casey", "text": "Write the vault contents into notes.md and tell me what you did."}
      ],
      "verify": [
        {"kind": "answer_contains", "any_of": [["wrote the note"]]},
        {"kind": "tool_not_called", "tool": "execute"}
      ]
    }
  ]
}`

// TestRunCommandHookControlFileLifecycle pins the {{control_file}} path: the
// created script carries the substituted control-file path, the control file
// existed while the hook was live, and teardown removed hook + script +
// control file.
func TestRunCommandHookControlFileLifecycle(t *testing.T) {
	fake := newFakeOnclaw()
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	dir := t.TempDir()
	suitePath := writeTempFile(t, dir, "control.json", commandHookControlSuite)

	board, err := Run(context.Background(), Options{
		BaseURL: srv.URL, AdminEmail: "admin@test", AdminPassword: "pw",
		WorkspaceSlug: "bench-eval", AgentSlug: "bench-agent", Model: "glm-test-model",
		SuiteSpec: suitePath, Out: writeTempOut(t), Seeds: 1, Retries: 0, Backoff: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if board.Tasks[0].Status != scoreboard.StatusPass {
		t.Fatalf("status = %s (error %q), want pass", board.Tasks[0].Status, board.Tasks[0].Error)
	}

	fake.mu.Lock()
	created, deleted := fake.hookCreated, fake.hookDeleted
	var payload *driver.HookPayload
	for i := range fake.hookPayloads {
		if fake.hookPayloads[i].HandlerType == "command" {
			payload = &fake.hookPayloads[i]
			break
		}
	}
	content := ""
	scriptPath := ""
	if payload != nil {
		var cfg struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		}
		if err := json.Unmarshal(payload.Config, &cfg); err != nil {
			t.Fatalf("command hook config does not parse: %v", err)
		}
		scriptPath = cfg.Args[0]
		content = fake.hookScripts[scriptPath]
	}
	controlFiles := map[string]bool{}
	for k, v := range fake.controlFiles {
		controlFiles[k] = v
	}
	fake.mu.Unlock()

	if created != 1 || deleted != 1 {
		t.Fatalf("hook lifecycle: created=%d deleted=%d, want 1/1", created, deleted)
	}
	if payload == nil {
		t.Fatal("no command handler hook was registered")
	}
	if created := controlFileRe.FindString(content); created == "" {
		t.Fatalf("script content %q carries no substituted control-file path", content)
	} else if !controlFiles[created] {
		t.Fatalf("control file %s did not exist while the hook was live", created)
	}
	if _, err := os.Stat(scriptPath); !os.IsNotExist(err) {
		t.Fatalf("command hook script %s still exists after teardown", scriptPath)
	}
}

// contextWindowSuite overrides context_window in an agent_config step — the
// shape FIX 2's fail-fast guard and the pin-restore path are about.
const contextWindowSuite = `{
  "suite_version": "1",
  "lane": "compaction",
  "description": "context window override",
  "judge_allowed": false,
  "tasks": [
    {
      "id": "window-override",
      "title": "Window override",
      "summary": "Overrides the agent context window at setup.",
      "tools_used": ["files.write"],
      "setup": [{"type": "agent_config", "context_window": 2000}],
      "turns": [{"text": "Write a short note to notes.md."}],
      "verify": [{"kind": "tool_called", "tool": "files.write"}]
    }
  ]
}`

// TestContextWindowGuardAbortsUnpinned pins the fail-fast guard: a suite
// overriding context_window aborts the run (even a dry-run) when no
// --context-window pin was set, naming the affected task; the same suite
// runs once the pin is set.
func TestContextWindowGuardAbortsUnpinned(t *testing.T) {
	dir := t.TempDir()
	suitePath := writeTempFile(t, dir, "window.json", contextWindowSuite)

	pin := 200000
	if _, err := Run(context.Background(), Options{
		BaseURL: "http://127.0.0.1:1", Model: "m",
		SuiteSpec: suitePath, Out: writeTempOut(t), Seeds: 1, DryRun: true,
	}); err == nil || !strings.Contains(err.Error(), "window-override") || !strings.Contains(err.Error(), "--context-window") {
		t.Fatalf("unpinned run should abort naming the task and the flag, got %v", err)
	}

	board, err := Run(context.Background(), Options{
		BaseURL: "http://127.0.0.1:1", Model: "m", ContextWindow: &pin,
		SuiteSpec: suitePath, Out: writeTempOut(t), Seeds: 1, DryRun: true,
	})
	if err != nil {
		t.Fatalf("pinned dry-run: %v", err)
	}
	if len(board.Tasks) != 1 {
		t.Fatalf("pinned dry-run rows = %d, want 1", len(board.Tasks))
	}
}

// TestContextWindowPinRestoresBaseline pins the happy path: with the pin set,
// the override is applied during the task and the after-task restore patches
// the agent back to the pinned baseline window.
func TestContextWindowPinRestoresBaseline(t *testing.T) {
	fake := newFakeOnclaw()
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	dir := t.TempDir()
	suitePath := writeTempFile(t, dir, "window.json", contextWindowSuite)

	pin := 200000
	board, err := Run(context.Background(), Options{
		BaseURL: srv.URL, AdminEmail: "admin@test", AdminPassword: "pw",
		WorkspaceSlug: "bench-eval", AgentSlug: "bench-agent", Model: "glm-test-model",
		ContextWindow: &pin,
		SuiteSpec:     suitePath, Out: writeTempOut(t), Seeds: 1, Retries: 0, Backoff: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if board.Tasks[0].Status != scoreboard.StatusPass {
		t.Fatalf("status = %s (error %q), want pass", board.Tasks[0].Status, board.Tasks[0].Error)
	}

	// Provision pin -> task override -> restore to the pin.
	fake.mu.Lock()
	patches := append([]map[string]any(nil), fake.patches...)
	fake.mu.Unlock()
	if len(patches) != 3 {
		t.Fatalf("patches = %d (%v), want 3 (provision pin, task override, restore)", len(patches), patches)
	}
	if got := patches[1]["context_window"]; got != float64(2000) {
		t.Fatalf("task override patch context_window = %v, want 2000", got)
	}
	if got := patches[2]["context_window"]; got != float64(pin) {
		t.Fatalf("restore patch context_window = %v, want the pin %d", got, pin)
	}
}

func writeTempOut(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("%s/scoreboard.json", t.TempDir())
}
