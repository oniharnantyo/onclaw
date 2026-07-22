package tools_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/oniharnantyo/onclaw/internal/agent/tools"
)

// spillTestCfg is a configurable ToolGroupCfg double for spill tests.
type spillTestCfg struct {
	configs map[string]string
}

func (c *spillTestCfg) GetConfig(ctx context.Context, category string) (string, error) {
	if c == nil || c.configs == nil {
		return "", nil
	}
	return c.configs[category], nil
}

// spillTestArgs mirrors the input fields whose value drives title derivation.
type spillTestArgs struct {
	URL            string `json:"url"`
	Query          string `json:"query"`
	SeedEntityName string `json:"seed_entity_name"`
}

// makeSpillInner builds a fake InvokableTool returning the given result (or
// error) so the spill decorator can be exercised black-box.
func makeSpillInner(result string, err error) tool.InvokableTool {
	inner, _ := utils.InferTool("faketool", "a fake tool", func(ctx context.Context, in *spillTestArgs) (string, error) {
		return result, err
	})
	return inner
}

func spillTestScope(t *testing.T, configs map[string]string) *tools.Scope {
	t.Helper()
	ws, err := filepath.Abs(t.TempDir())
	if err != nil {
		t.Fatalf("abs workspace: %v", err)
	}
	return &tools.Scope{
		Workspace:    ws,
		ToolGroupCfg: &spillTestCfg{configs: configs},
		AgentName:    "agent",
		SessionID:    "sess",
	}
}

// spilledFilename extracts the trailing spill artifact filename from an envelope.
func spilledFilename(t *testing.T, out string) string {
	t.Helper()
	m := regexp.MustCompile(`Path: (\S+\.md)`).FindStringSubmatch(out)
	if len(m) < 2 {
		t.Fatalf("no spill path found in envelope: %q", out)
	}
	parts := strings.Split(m[1], "/")
	return parts[len(parts)-1]
}

func TestSpillUnderThresholdInline(t *testing.T) {
	scope := spillTestScope(t, nil)
	small := strings.Repeat("a", 100)
	spilled := tools.WrapFileSpill(makeSpillInner(small, nil), scope, "Web", "faketool")

	out, err := spilled.InvokableRun(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != small {
		t.Errorf("expected inline result verbatim, got %q", out)
	}
	// No spill directory should have been created.
	if _, statErr := os.Stat(filepath.Join(scope.Workspace, ".onclaw")); !os.IsNotExist(statErr) {
		t.Errorf("expected no .onclaw/ spill dir for inline result")
	}
}

func TestSpillOverThresholdWritesFileAndEnvelope(t *testing.T) {
	scope := spillTestScope(t, nil)
	big := strings.Repeat("x", 50000) // exceeds Web default 4096
	spilled := tools.WrapFileSpill(makeSpillInner(big, nil), scope, "Web", "faketool")

	out, err := spilled.InvokableRun(context.Background(), `{"url":"https://example.com/path/to/page"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Path:") || !strings.Contains(out, "tool_results") {
		t.Fatalf("expected spill envelope with path, got %q", out)
	}

	// The full content must be written to the session file.
	rel := regexp.MustCompile(`Path: (\S+\.md)`).FindStringSubmatch(out)[1]
	abs := filepath.Join(scope.Workspace, filepath.FromSlash(rel))
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("spill file not written: %v", err)
	}
	if string(data) != big {
		t.Errorf("spill file content mismatch (len got %d want %d)", len(data), len(big))
	}
}

func TestSpillPreviewIsTruncated(t *testing.T) {
	scope := spillTestScope(t, nil)
	big := strings.Repeat("y", 50000)
	spilled := tools.WrapFileSpill(makeSpillInner(big, nil), scope, "Web", "faketool")

	out, err := spilled.InvokableRun(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The full blob must not appear in the envelope.
	if strings.Contains(out, big) {
		t.Errorf("full result leaked into envelope")
	}
	// Preview section must be bounded.
	m := regexp.MustCompile(`Preview \(first bytes\):\n(.*?)\n\nUse the read_file`).FindStringSubmatch(out)
	if len(m) < 2 {
		t.Fatalf("could not locate preview in envelope: %q", out)
	}
	if len(m[1]) > 500 {
		t.Errorf("preview not truncated: %d bytes", len(m[1]))
	}
}

func TestSpillTitleDerivation(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{"url", `{"url":"https://example.com/path/to/page"}`, "example-com-path-to-page"},
		{"query", `{"query":"my search term"}`, "my-search-term"},
		{"seed", `{"seed_entity_name":"Entity A"}`, "entity-a"},
		{"fallback", `{"other":"ignored"}`, ""}, // content-head slug, checked loosely below
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scope := spillTestScope(t, nil)
			big := strings.Repeat("z", 50000)
			spilled := tools.WrapFileSpill(makeSpillInner(big, nil), scope, "Web", "faketool")
			out, err := spilled.InvokableRun(context.Background(), tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			fn := spilledFilename(t, out)
			if tc.expected == "" {
				// fallback: filename must still be a safe, non-empty slug.
				if strings.Count(fn, "_") < 2 {
					t.Errorf("expected timestamped slug filename, got %q", fn)
				}
				return
			}
			if !strings.Contains(fn, tc.expected) {
				t.Errorf("expected filename to contain slug %q, got %q", tc.expected, fn)
			}
		})
	}
}

func TestSpillTimestampUniqueness(t *testing.T) {
	scope := spillTestScope(t, nil)
	big := strings.Repeat("q", 50000)
	spilled := tools.WrapFileSpill(makeSpillInner(big, nil), scope, "Web", "faketool")

	ctx := context.Background()
	out1, _ := spilled.InvokableRun(ctx, `{"url":"https://a.example.com/x"}`)
	out2, _ := spilled.InvokableRun(ctx, `{"url":"https://b.example.com/y"}`)
	if spilledFilename(t, out1) == spilledFilename(t, out2) {
		t.Errorf("rapid spills produced identical filenames: %q", spilledFilename(t, out1))
	}
}

func TestSpillErrorPassthrough(t *testing.T) {
	scope := spillTestScope(t, nil)
	spilled := tools.WrapFileSpill(makeSpillInner("", fmt.Errorf("boom")), scope, "Web", "faketool")

	_, err := spilled.InvokableRun(context.Background(), `{}`)
	if err == nil {
		t.Fatal("expected inner error to pass through")
	}
	if _, statErr := os.Stat(filepath.Join(scope.Workspace, ".onclaw")); !os.IsNotExist(statErr) {
		t.Errorf("spill must not write a file on inner error")
	}
}

func TestSpillRedactionBeforeSpill(t *testing.T) {
	scope := spillTestScope(t, nil)
	secret := "sk-ABCDEFGHIJKLMNOPQRSTUVW"
	// Place the secret at the start (followed by a separator) so it lands
	// inside the 500-byte preview. The newline stops the greedy secret
	// pattern from consuming the rest of the body, keeping the result over
	// threshold so it actually spills.
	big := secret + "\n" + strings.Repeat("x", 50000)
	// Mimic the production chain: WrapFileSpill(WrapRedacted(inner)).
	redacted := tools.WrapRedacted(makeSpillInner(big, nil))
	spilled := tools.WrapFileSpill(redacted, scope, "Web", "faketool")

	out, err := spilled.InvokableRun(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out, secret) {
		t.Errorf("secret leaked into envelope: %q", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Errorf("expected redaction marker in envelope, got %q", out)
	}

	m := regexp.MustCompile(`Path: (\S+\.md)`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("envelope missing Path line; out=%q", out[:min(len(out), 200)])
	}
	rel := m[1]
	data, _ := os.ReadFile(filepath.Join(scope.Workspace, filepath.FromSlash(rel)))
	if strings.Contains(string(data), secret) {
		t.Errorf("secret leaked into spilled file")
	}
}

func TestSpillConfigOverrideBeatsDefault(t *testing.T) {
	// Small override threshold: a 500-byte result spills.
	scope := spillTestScope(t, map[string]string{"Memory": `{"spill_threshold_bytes":100}`})
	big := strings.Repeat("p", 500)
	spilled := tools.WrapFileSpill(makeSpillInner(big, nil), scope, "Memory", "faketool")
	out, err := spilled.InvokableRun(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "tool_results") {
		t.Errorf("expected spill with small override threshold, got %q", out)
	}

	// Large override threshold: the same result stays inline.
	scope2 := spillTestScope(t, map[string]string{"Memory": `{"spill_threshold_bytes":100000}`})
	spilled2 := tools.WrapFileSpill(makeSpillInner(big, nil), scope2, "Memory", "faketool")
	out2, err := spilled2.InvokableRun(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out2 != big {
		t.Errorf("expected inline result with large override threshold, got %q", out2)
	}
}

func TestSpillPathSanitization(t *testing.T) {
	ws, _ := filepath.Abs(t.TempDir())
	scope := &tools.Scope{
		Workspace:    ws,
		ToolGroupCfg: &spillTestCfg{},
		AgentName:    "../../etc",
		SessionID:    "a/../b",
	}
	abs, rel, err := tools.SpillArtifactPaths(scope, "web_fetch", "host-page", ".md")
	if err != nil {
		t.Fatalf("SpillArtifactPaths: %v", err)
	}
	if strings.Contains(rel, "..") {
		t.Errorf("spill path escaped via '..': %q", rel)
	}
	// Absolute spill path must remain inside the workspace.
	relToWs, err := filepath.Rel(ws, abs)
	if err != nil || strings.HasPrefix(relToWs, "..") {
		t.Errorf("spill path escaped workspace: abs=%q relToWs=%q", abs, relToWs)
	}
	// Regression: the agent/workspace prefix must NOT leak into the path, and
	// the onclaw-owned ".onclaw" namespace must appear at most once (no
	// re-nesting/doubling). The hostile agent name "../../etc" must not appear.
	if strings.Contains(rel, "/etc/") {
		t.Errorf("agent name leaked into spill path: %q", rel)
	}
	if strings.Count(rel, ".onclaw") > 1 {
		t.Errorf("spill path re-nests the .onclaw namespace (doubling): %q", rel)
	}
	// Only the session/tool/title participate; the session "a/../b"
	// collapses to "ab" (separators and ".." stripped) and lives under a
	// single .onclaw/ namespace, matching the .onclaw/transcripts convention.
	if !strings.Contains(rel, ".onclaw/sessions/ab/tool_results/") {
		t.Errorf("expected sanitized session under .onclaw/, got %q", rel)
	}
}

func TestSpillEnvelopeDedicatedPathLine(t *testing.T) {
	scope := spillTestScope(t, nil)
	big := strings.Repeat("x", 50000)
	spilled := tools.WrapFileSpill(makeSpillInner(big, nil), scope, "Web", "faketool")
	out, _ := spilled.InvokableRun(context.Background(), `{}`)

	lines := strings.Split(out, "\n")
	found := false
	for _, l := range lines {
		if strings.HasPrefix(l, "Path: ") && strings.Contains(l, "/tool_results/") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected spill path on a dedicated line, got %q", out)
	}
}

// An unknown tool-group category must never spill (it would otherwise spill
// every result). Results stay inline, preserving pre-change behavior for any
// un-migrated tool.
func TestSpillUnknownCategoryNeverSpills(t *testing.T) {
	scope := spillTestScope(t, nil)
	big := strings.Repeat("z", 50000)
	spilled := tools.WrapFileSpill(makeSpillInner(big, nil), scope, "UnknownCategory", "faketool")
	out, err := spilled.InvokableRun(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != big {
		t.Errorf("unknown category should stay inline, got envelope %q", out)
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Hello/World Foo":  "hello-world-foo",
		"example.com/path": "example-com-path",
		"":                 "result",
	}
	for in, want := range cases {
		if got := tools.Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWriteSpillFile(t *testing.T) {
	ws, _ := filepath.Abs(t.TempDir())
	scope := &tools.Scope{Workspace: ws, AgentName: "agent", SessionID: "sess"}
	rel, err := tools.WriteSpillFile(scope, "browser_screenshot", "host-page", ".png", []byte("fake-png"))
	if err != nil {
		t.Fatalf("WriteSpillFile: %v", err)
	}
	if !strings.Contains(rel, "tool_results") || !strings.HasSuffix(rel, ".png") {
		t.Errorf("unexpected spill rel path: %q", rel)
	}
	data, err := os.ReadFile(filepath.Join(ws, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("spill file not readable: %v", err)
	}
	if string(data) != "fake-png" {
		t.Errorf("spill file content mismatch: %q", data)
	}
}

// When the spill directory cannot be created (e.g. disk full / permission
// denied on the target hardware), the decorator must not re-inject the full
// oversized blob. It must truncate to the threshold and flag the failure.
func TestSpillFallbackOnDirCreateFailure(t *testing.T) {
	// Workspace is a regular file, so MkdirAll for the spill dir fails.
	wsFile := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(wsFile, []byte("x"), 0600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	scope := &tools.Scope{
		Workspace:    wsFile,
		ToolGroupCfg: &spillTestCfg{},
		AgentName:    "agent",
		SessionID:    "sess",
	}
	big := strings.Repeat("z", 50000)
	spilled := tools.WrapFileSpill(makeSpillInner(big, nil), scope, "Web", "faketool")
	out, err := spilled.InvokableRun(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out, big) {
		t.Errorf("spill dir-create failure re-injected full blob into context")
	}
	if !strings.Contains(out, "spill failed") {
		t.Errorf("expected explicit spill-failure notice, got %q", out)
	}
	if len([]byte(out)) > 4096+256 {
		t.Errorf("fallback result not truncated to threshold: %d bytes", len(out))
	}
}

// When the spill directory exists but is read-only, MkdirAll succeeds yet the
// final WriteFile fails. The fallback must still truncate and flag.
func TestSpillFallbackOnFileWriteFailure(t *testing.T) {
	ws, _ := filepath.Abs(t.TempDir())
	scope := &tools.Scope{
		Workspace:    ws,
		ToolGroupCfg: &spillTestCfg{},
		AgentName:    "agent",
		SessionID:    "sess",
	}
	toolResultsDir := filepath.Join(ws, ".onclaw", "sessions", "sess", "tool_results")
	if err := os.MkdirAll(toolResultsDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(toolResultsDir, 0500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(toolResultsDir, 0700)

	big := strings.Repeat("z", 50000)
	spilled := tools.WrapFileSpill(makeSpillInner(big, nil), scope, "Web", "faketool")
	out, err := spilled.InvokableRun(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out, big) {
		t.Errorf("spill write failure re-injected full blob into context")
	}
	if !strings.Contains(out, "spill failed") {
		t.Errorf("expected explicit spill-failure notice, got %q", out)
	}
	if len([]byte(out)) > 4096+256 {
		t.Errorf("fallback result not truncated to threshold: %d bytes", len(out))
	}
}

// WriteSpillFile must surface a directory-creation failure as an error rather
// than returning a bogus path.
func TestWriteSpillFileFailureReturnsError(t *testing.T) {
	wsFile := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(wsFile, []byte("x"), 0600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	scope := &tools.Scope{Workspace: wsFile, AgentName: "agent", SessionID: "sess"}
	rel, err := tools.WriteSpillFile(scope, "browser_screenshot", "host-page", ".png", []byte("fake-png"))
	if err == nil {
		t.Fatalf("expected error when workspace is a file, got rel %q", rel)
	}
	if rel != "" {
		t.Errorf("expected empty rel on failure, got %q", rel)
	}
}
