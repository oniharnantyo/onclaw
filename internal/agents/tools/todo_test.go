package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
)

// runTodoTool runs one tool call through the InvokableTool surface the
// constructors return as BaseTool. Named so it cannot collide with the other
// tool files' package-level test helpers.
func runTodoTool(t *testing.T, base tool.BaseTool, args string) (string, error) {
	t.Helper()
	invokable, ok := base.(tool.InvokableTool)
	if !ok {
		t.Fatalf("tool %T does not implement InvokableTool", base)
	}
	return invokable.InvokableRun(context.Background(), args)
}

// decodeTodoEcho decodes the tools' shared {items, revision} echo.
func decodeTodoEcho(t *testing.T, out string) todoEcho {
	t.Helper()
	var echo todoEcho
	if err := json.Unmarshal([]byte(out), &echo); err != nil {
		t.Fatalf("decode result %q: %v", out, err)
	}
	return echo
}

// newTodoToolsForRoot constructs the write and read tools over one fresh
// workspace root.
func newTodoToolsForRoot(t *testing.T) (write, read tool.BaseTool, root string) {
	t.Helper()
	root = t.TempDir()
	w, err := NewTodoWrite(root)
	if err != nil {
		t.Fatalf("new todo_write: %v", err)
	}
	r, err := NewTodoRead(root)
	if err != nil {
		t.Fatalf("new todo_read: %v", err)
	}
	return w, r, root
}

// TestTodoWrite_ThenReadAcrossTurns is the persist contract: a plan written
// in one turn is read back unchanged by fresh tool instances over the same
// workspace root in a later turn, and a rewrite there bumps the revision.
func TestTodoWrite_ThenReadAcrossTurns(t *testing.T) {
	write1, _, root := newTodoToolsForRoot(t)

	seed := `{"items":[
		{"key":"a","text":"Do A","status":"pending"},
		{"key":"b","text":"Do B","status":"failed","reason":"blocked on creds"}
	]}`
	out, err := runTodoTool(t, write1, seed)
	if err != nil {
		t.Fatalf("turn 1 write: %v", err)
	}
	written := decodeTodoEcho(t, out)
	if written.Revision != 1 || len(written.Items) != 2 {
		t.Fatalf("expected revision 1 with 2 items, got revision %d with %d items", written.Revision, len(written.Items))
	}

	// Turn 2: fresh instances over the same workspace root — the same wiring
	// a later turn performs. The plan must be exactly what turn 1 wrote.
	write2, read2, err := rebuildTodoTools(root)
	if err != nil {
		t.Fatalf("turn 2 construction: %v", err)
	}
	out, err = runTodoTool(t, read2, `{}`)
	if err != nil {
		t.Fatalf("turn 2 read: %v", err)
	}
	roundTripped := decodeTodoEcho(t, out)
	if !reflect.DeepEqual(written, roundTripped) {
		t.Fatalf("plan did not persist across turns:\nwritten:     %+v\nround-tripped: %+v", written, roundTripped)
	}

	// Turn 2 rewrite: dropped key gone, status updated, revision bumped.
	rewrite := `{"items":[{"key":"a","text":"Do A","status":"done"}]}`
	out, err = runTodoTool(t, write2, rewrite)
	if err != nil {
		t.Fatalf("turn 2 rewrite: %v", err)
	}
	rewritten := decodeTodoEcho(t, out)
	if rewritten.Revision != 2 || len(rewritten.Items) != 1 || rewritten.Items[0].Status != todoStatusDone {
		t.Fatalf("expected revision 2 with item a done, got revision %d with %+v", rewritten.Revision, rewritten.Items)
	}

	// A third turn reads the rewritten plan, still through fresh instances.
	_, read3, err := rebuildTodoTools(root)
	if err != nil {
		t.Fatalf("turn 3 construction: %v", err)
	}
	out, err = runTodoTool(t, read3, `{}`)
	if err != nil {
		t.Fatalf("turn 3 read: %v", err)
	}
	if got := decodeTodoEcho(t, out); !reflect.DeepEqual(rewritten, got) {
		t.Fatalf("rewritten plan did not persist: %+v vs %+v", rewritten, got)
	}
}

// rebuildTodoTools constructs fresh todo tools over an existing root,
// mirroring the per-turn wiring.
func rebuildTodoTools(root string) (tool.BaseTool, tool.BaseTool, error) {
	w, err := NewTodoWrite(root)
	if err != nil {
		return nil, nil, err
	}
	r, err := NewTodoRead(root)
	if err != nil {
		return nil, nil, err
	}
	return w, r, nil
}

// TestTodoWrite_ValidationRejectsAndLeavesState pins the rejection contract:
// unknown status, missing key/text, duplicate keys, unknown fields — every
// rejection is an error naming the fault, and the stored plan is unchanged.
func TestTodoWrite_ValidationRejectsAndLeavesState(t *testing.T) {
	write, read, _ := newTodoToolsForRoot(t)

	if _, err := runTodoTool(t, write, `{"items":[{"key":"a","text":"Do A","status":"active"}]}`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	rejections := []string{
		`{"items":[{"key":"a","text":"Do A","status":"finished"}]}`,                                       // unknown status
		`{"items":[{"key":"a","text":"Do A","status":"DONE"}]}`,                                           // case-sensitive enum
		`{"items":[{"key":"","text":"No key","status":"done"}]}`,                                          // missing key
		`{"items":[{"key":"x","text":"","status":"done"}]}`,                                               // missing text
		`{"items":[{"key":"a","text":"Dup","status":"done"},{"key":"a","text":"A2","status":"pending"}]}`, // duplicate key
		`{"items":[],"note":"extra field"}`,                                                               // unknown top-level field
		`{"items":"three"}`,                                                                               // wrong shape
	}
	for i, args := range rejections {
		if _, err := runTodoTool(t, write, args); err == nil {
			t.Fatalf("rejection %d: expected an error for %s", i, args)
		}
	}

	out, err := runTodoTool(t, read, `{}`)
	if err != nil {
		t.Fatalf("read after rejections: %v", err)
	}
	state := decodeTodoEcho(t, out)
	if state.Revision != 1 || len(state.Items) != 1 || state.Items[0].Status != todoStatusActive {
		t.Fatalf("expected untouched stored state, got revision %d with %+v", state.Revision, state.Items)
	}
}

// TestTodoWrite_EmptyListClears pins the clear semantics: an empty item list
// is a valid rewrite that empties the plan and persists the empty plan.
func TestTodoWrite_EmptyListClears(t *testing.T) {
	write, read, _ := newTodoToolsForRoot(t)

	if _, err := runTodoTool(t, write, `{"items":[{"key":"a","text":"A","status":"pending"}]}`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	out, err := runTodoTool(t, write, `{"items":[]}`)
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if state := decodeTodoEcho(t, out); len(state.Items) != 0 {
		t.Fatalf("expected an empty plan after clear, got %+v", state.Items)
	}
	if strings.Contains(out, "null") {
		t.Errorf("items must encode as a list, never null: %s", out)
	}
	out, err = runTodoTool(t, read, `{}`)
	if err != nil {
		t.Fatalf("read after clear: %v", err)
	}
	if got := decodeTodoEcho(t, out); len(got.Items) != 0 || got.Revision != 2 {
		t.Fatalf("expected the cleared plan to persist, got revision %d with %+v", got.Revision, got.Items)
	}
}

// TestTodoRead_FreshWorkspaceReturnsEmpty pins the fresh state: no plan file
// yet means an empty list at revision 0, not an error.
func TestTodoRead_FreshWorkspaceReturnsEmpty(t *testing.T) {
	_, read, _ := newTodoToolsForRoot(t)

	out, err := runTodoTool(t, read, `{}`)
	if err != nil {
		t.Fatalf("read fresh workspace: %v", err)
	}
	state := decodeTodoEcho(t, out)
	if state.Revision != 0 || len(state.Items) != 0 || state.Items == nil {
		t.Fatalf("expected revision 0 with an empty (non-null) item list, got %s", out)
	}
}

// TestTodoTools_WorkspaceRootValidation pins constructor behavior: an empty
// root fails, a missing root self-heals into a directory, and a plain file
// occupying the root fails.
func TestTodoTools_WorkspaceRootValidation(t *testing.T) {
	if _, err := NewTodoWrite(""); err == nil {
		t.Error("expected empty-root construction error for todo_write")
	}
	if _, err := NewTodoRead(""); err == nil {
		t.Error("expected empty-root construction error for todo_read")
	}

	missing := filepath.Join(t.TempDir(), "absent")
	if _, err := NewTodoRead(missing); err != nil {
		t.Errorf("missing root should self-heal at construction, got %v", err)
	}
	if info, err := os.Stat(missing); err != nil || !info.IsDir() {
		t.Errorf("missing root should be created at construction, stat err: %v", err)
	}

	file := filepath.Join(t.TempDir(), "plain.txt")
	if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := NewTodoWrite(file); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("expected not-a-directory construction error, got %v", err)
	}
}
