package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cloudwego/eino/components/tool"
)

// Per-category default spill thresholds in bytes. These bound the size of a
// tool result injected into the model context and are decoupled from
// max_bytes (which bounds downloads/RAM). Each tool-group config schema also
// exposes spill_threshold_bytes so the value is hot-reloadable.
const (
	// Web results spill aggressively: on a small context window an inline page
	// (commonly 8-20KB of markdown) both eats the budget AND gets scrubbed to
	// "[cleared web_fetch]" when compaction fires, which makes the model re-fetch
	// in a loop. 4096 bytes (~1K tokens) keeps trivial results inline while
	// spilling real pages to a re-readable session file that survives compaction.
	defaultSpillThresholdWeb     = 4096
	defaultSpillThresholdMemory  = 16384
	defaultSpillThresholdBrowser = 16384
)

var spillDefaults = map[string]int{
	"Web":     defaultSpillThresholdWeb,
	"Memory":  defaultSpillThresholdMemory,
	"Browser": defaultSpillThresholdBrowser,
}

// spillPreviewBytes is the maximum number of result bytes echoed in the
// spill envelope's preview.
const spillPreviewBytes = 500

// spillCfg carries the per-call context the decorator needs to decide and
// perform a spill.
type spillCfg struct {
	scope    *Scope
	category string
	tool     string
}

// WrapFileSpill decorates an InvokableTool so that any success result larger
// than the configured spill threshold is written to a session-scoped file and
// replaced with a small envelope. Inner errors pass through unchanged; only the
// success path may spill. It is intended to wrap the already-redacted tool
// (WrapFileSpill(WrapRedacted(tool), ...)) so the spilled file and envelope
// preview never contain raw secrets.
func WrapFileSpill(inner tool.InvokableTool, scope *Scope, category, toolName string) tool.InvokableTool {
	return &fileSpillTool{InvokableTool: inner, cfg: spillCfg{scope: scope, category: category, tool: toolName}}
}

type fileSpillTool struct {
	tool.InvokableTool
	cfg spillCfg
}

func (f *fileSpillTool) InvokableRun(ctx context.Context, input string, opts ...tool.Option) (string, error) {
	out, err := f.InvokableTool.InvokableRun(ctx, input, opts...)
	if err != nil {
		return "", err
	}
	threshold := f.cfg.resolveSpillThreshold(ctx)
	resultBytes := len([]byte(out))
	spilled := resultBytes > threshold
	// Trace the spill decision for every tool call so the compaction-loop
	// investigation can confirm whether a result spilled to a file (and thus
	// survives compaction as a re-readable path) or stayed inline (and is later
	// scrubbed to "[cleared <tool>]"). threshold_bytes == 1073741824 means spill
	// is disabled entirely (no workspace or no tool-group config).
	slog.Info("tool_spill_decision",
		"tool", f.cfg.tool,
		"category", f.cfg.category,
		"result_bytes", resultBytes,
		"threshold_bytes", threshold,
		"spilled", spilled,
	)
	if !spilled {
		return out, nil
	}
	return f.spill(out, input, threshold)
}

// resolveSpillThreshold reads spill_threshold_bytes from the tool group config
// spillNeverThreshold is returned when no spill is desired (unknown category
// or no workspace to write into). It is far larger than any plausible result,
// so results are returned inline rather than spilled. This preserves the
// pre-change behavior for any un-migrated tool and avoids spilling everything
// when a category is missing from spillDefaults.
const spillNeverThreshold = 1 << 30

// (hot-reload-safe) and falls back to the per-category default.
func (c spillCfg) resolveSpillThreshold(ctx context.Context) int {
	def := spillDefaults[c.category]
	if def == 0 {
		def = spillNeverThreshold
	}
	// Without a workspace there is nowhere to spill to: keep results inline.
	if c.scope == nil || c.scope.ToolGroupCfg == nil {
		return spillNeverThreshold
	}
	cfgStr, err := c.scope.ToolGroupCfg.GetConfig(ctx, c.category)
	if err != nil || cfgStr == "" {
		return def
	}
	var tc struct {
		SpillThresholdBytes int `json:"spill_threshold_bytes"`
	}
	if err := json.Unmarshal([]byte(cfgStr), &tc); err != nil {
		return def
	}
	if tc.SpillThresholdBytes > 0 {
		return tc.SpillThresholdBytes
	}
	return def
}

// spillTimestamp returns a sortable, collision-resistant timestamp.
var spillSeq int64

func spillTimestamp() string {
	now := time.Now().UTC()
	base := now.Format("20060102-150405")
	ms := now.Nanosecond() / int(time.Millisecond)
	seq := atomic.AddInt64(&spillSeq, 1)
	return fmt.Sprintf("%s-%03d-%05d", base, ms, seq%100000)
}

// deriveTitle extracts a short, human-readable slug from the tool's input or
// result so the spilled file name is meaningful.
func deriveTitle(toolName, inputJSON, content string) string {
	var m map[string]json.RawMessage
	_ = json.Unmarshal([]byte(inputJSON), &m)
	pick := func(keys ...string) string {
		for _, k := range keys {
			if raw, ok := m[k]; ok {
				var s string
				if json.Unmarshal(raw, &s) == nil && s != "" {
					return s
				}
			}
		}
		return ""
	}
	var raw string
	switch {
	case pick("url") != "":
		raw = hostPath(pick("url"))
	case pick("query") != "":
		raw = pick("query")
	case pick("seed_entity_name") != "":
		raw = pick("seed_entity_name")
	case pick("skill") != "":
		raw = pick("skill")
	default:
		raw = content
	}
	return slugify(raw)
}

// hostPath reduces a URL to its host + path for a readable title.
func hostPath(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return rawURL
	}
	return u.Host + u.Path
}

var slugRe = regexp.MustCompile(`[^a-zA-Z0-9]+`)

// slugify sanitizes a string into a short path-safe slug.
func slugify(s string) string {
	s = strings.ReplaceAll(s, "/", " ")
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.ToLower(strings.Trim(s, "-"))
	if len(s) > 40 {
		s = s[:40]
	}
	if s == "" {
		s = "result"
	}
	return s
}

// Slugify exports slugify for tool packages that persist session artifacts.
func Slugify(s string) string { return slugify(s) }

// sanitizeComponent strips path separators and traversal sequences from a path
// component so it cannot escape the spill directory.
func sanitizeComponent(s string) string {
	s = strings.ReplaceAll(s, "..", "")
	s = strings.ReplaceAll(s, "/", "")
	s = strings.ReplaceAll(s, "\\", "")
	s = strings.ReplaceAll(s, string(os.PathSeparator), "")
	if s == "" {
		s = "unknown"
	}
	return s
}

// SpillArtifactPaths builds the absolute and workspace-relative paths for a
// session-scoped spill artifact. The relative path is safe for workspace-
// confined read_file. ext is e.g. ".md" or ".png".
//
// Artifacts live under <workspace>/.onclaw/sessions/<id>/tool_results/ so they
// share onclaw's data namespace with .onclaw/transcripts (and stay out of the
// user's project root), while remaining inside the workspace so read_file —
// which is confined to the workspace — can still reach them.
func SpillArtifactPaths(scope *Scope, toolName, title, ext string) (abs string, rel string, err error) {
	sid := sanitizeComponent(scope.SessionID)
	tool := sanitizeComponent(toolName)
	dir := filepath.Join(scope.Workspace, ".onclaw", "sessions", sid, "tool_results")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", "", err
	}
	name := fmt.Sprintf("%s_%s_%s%s", tool, spillTimestamp(), title, ext)
	abs = filepath.Join(dir, name)
	rel = filepath.ToSlash(filepath.Join(".onclaw", "sessions", sid, "tool_results", name))
	return abs, rel, nil
}

// WriteSpillFile writes data to a session-scoped spill artifact and returns the
// workspace-relative path for the envelope. Used by image/binary tools that
// hold raw bytes (e.g. browser_screenshot).
func WriteSpillFile(scope *Scope, toolName, title, ext string, data []byte) (string, error) {
	abs, rel, err := SpillArtifactPaths(scope, toolName, title, ext)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(abs, data, 0600); err != nil {
		return "", err
	}
	return rel, nil
}

// previewOf returns the first n bytes of content.
func previewOf(content string, n int) string {
	b := []byte(content)
	if len(b) <= n {
		return content
	}
	return string(b[:n])
}

// buildSpillEnvelope returns the small, stable result the agent receives when a
// result is spilled. The spill path is placed on a dedicated line so the
// summarization scrub can detect and preserve it (design Decision 9).
func buildSpillEnvelope(toolName string, resultSize, threshold int, relPath, preview string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Tool result too large to inline (%d bytes; spill threshold %d bytes). It has been written to a session file.\n", resultSize, threshold))
	sb.WriteString("Path: " + relPath + "\n")
	sb.WriteString("\nPreview (first bytes):\n")
	sb.WriteString(preview)
	sb.WriteString("\n\nUse the read_file tool with offset/limit to inspect the file.")
	return sb.String()
}

// spillFallback returns a safe inline stand-in for a result that could not be
// spilled to disk. The change exists to keep oversized results out of the
// model context, so even when the spill write fails we must not re-inject the
// full blob (the exact blowout this change prevents). We truncate to the
// threshold and surface the failure explicitly rather than failing silently.
func spillFallback(result string, threshold int) string {
	b := []byte(result)
	if len(b) > threshold {
		b = b[:threshold]
	}
	return string(b) + fmt.Sprintf("\n\n[spill failed; result truncated to %d bytes — full result could not be written to disk]", threshold)
}

// spill writes the result to a session file and returns the envelope. On any
// failure it falls back to a truncated, explicitly-flagged inline result so no
// data is silently re-injected (see spillFallback).
func (f *fileSpillTool) spill(result, input string, threshold int) (string, error) {
	title := deriveTitle(f.cfg.tool, input, result)
	abs, rel, err := SpillArtifactPaths(f.cfg.scope, f.cfg.tool, title, ".md")
	if err != nil {
		return spillFallback(result, threshold), nil
	}
	if err := os.WriteFile(abs, []byte(result), 0600); err != nil {
		return spillFallback(result, threshold), nil
	}
	preview := previewOf(result, spillPreviewBytes)
	return buildSpillEnvelope(f.cfg.tool, len([]byte(result)), threshold, rel, preview), nil
}
