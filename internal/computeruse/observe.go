package computeruse

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// The observation screenshot lane (add-computer-use coordination contract
// C1, spec computer-use "Observation screenshots and the marker contract"):
// the parent stamps every observe/zoom call's _meta with the calling
// session's workspace screenshots directory (the absolute write target) and
// its workspace-relative facing path; the adapter writes the capture's PNG
// into the absolute dir when it is a clean absolute path (otherwise a
// fallback under the computer dir given at spawn) and answers with the path,
// the pixel dimensions, and the marker text the runtime's observation lane
// parses. The marker carries the relative facing form when the host supplied
// one, so a relocated workspace root cannot strand a stale absolute path in
// the persisted transcript. The meta keys mirror the parent-side consts
// (internal/agents/mcp.go) — the seam stays import-decoupled, like
// browseruse's.

// MetaScreenshotDir is the _meta key carrying the absolute screenshots
// directory to write PNGs into (the parent stamps it per calling session;
// the model neither sees nor influences it).
const MetaScreenshotDir = "buddies.dev/screenshot-dir"

// MetaScreenshotRel is the _meta key carrying the workspace-relative facing
// path of that directory (`sessions/<session id>/computer`) — the marker
// text's prefix so the persisted transcript stays relocation-safe.
const MetaScreenshotRel = "buddies.dev/screenshot-rel"

// ObservationResult is the observe/zoom payload (spec computer-use
// "Observation returns tree and screenshot"): the accessibility tree as text
// with addressable elements, the capture id coordinate actions bind to, and
// the written screenshot's path, pixel dimensions, and marker.
type ObservationResult struct {
	Tree           string `json:"tree"`
	Capture        string `json:"capture,omitempty"`
	ScreenshotPath string `json:"screenshotPath,omitempty"`
	Width          int    `json:"width,omitempty"`
	Height         int    `json:"height,omitempty"`
	Marker         string `json:"marker,omitempty"`
}

// screenshotTarget is one screenshot-bearing call's resolved write target.
type screenshotTarget struct {
	dir string // absolute directory the PNG is written into
	rel string // workspace-relative facing prefix when the host supplied one
}

// resolveScreenshotTarget resolves where one call's PNG lands: the _meta dir
// when the host supplied a clean absolute path, otherwise the fallback under
// the computer dir given at spawn. Mirrors browseruse's screenshotDir _meta
// handling. The model-visible schema never mentions the directory — the
// adapter accepts it only from the host's request metadata.
func resolveScreenshotTarget(req mcp.CallToolRequest, computerDir string) screenshotTarget {
	if req.Params.Meta != nil {
		if raw, ok := req.Params.Meta.AdditionalFields[MetaScreenshotDir].(string); ok && raw != "" {
			cleaned := filepath.Clean(raw)
			if filepath.IsAbs(cleaned) && !strings.Contains(cleaned, "..") {
				target := screenshotTarget{dir: cleaned}
				if rel, ok := req.Params.Meta.AdditionalFields[MetaScreenshotRel].(string); ok && rel != "" {
					target.rel = rel
				}
				return target
			}
		}
	}
	return screenshotTarget{dir: filepath.Join(computerDir, "screenshots")}
}

// writeCapture writes one PNG capture into the target's directory and
// returns the model-facing fields. The "%d.png" unix-millis filename is
// load-bearing: the chat screenshot bound read serves exactly files matching
// ^\d+\.png$ from the resolved directory (the browseruse lane's same jail).
// A second capture within the same millisecond bumps the stem by one — still
// all digits — so captures never overwrite each other.
func writeCapture(target screenshotTarget, data []byte) (capture, path string, width, height int, marker string, err error) {
	if err := os.MkdirAll(target.dir, 0o700); err != nil {
		return "", "", 0, 0, "", fmt.Errorf("computer: create screenshot dir: %w", err)
	}
	stem := time.Now().UnixMilli()
	for {
		path = filepath.Join(target.dir, fmt.Sprintf("%d.png", stem))
		if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
			break
		}
		stem++
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", "", 0, 0, "", fmt.Errorf("computer: write screenshot: %w", err)
	}
	dim, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", "", 0, 0, "", fmt.Errorf("computer: decode screenshot: %w", err)
	}
	name := fmt.Sprintf("%d.png", stem)
	capture = fmt.Sprintf("%d", stem)
	if target.rel != "" {
		marker = fmt.Sprintf("[screenshot: %s/%s]", target.rel, name)
	} else {
		marker = fmt.Sprintf("[screenshot: %s]", path)
	}
	return capture, path, dim.Width, dim.Height, marker, nil
}

// AnnotateSecure appends " [secure]" immediately after every standalone
// occurrence of each secure element's token (coordination contract C2): the
// parent's approval gate parses exactly this form. Tokens are non-whitespace
// opaque strings; a token embedded in a longer run is never annotated, and
// an already-annotated occurrence is passed through once — idempotent, so a
// driver that annotates on its own cannot double up.
func AnnotateSecure(text string, elements []DriverElement) string {
	for _, el := range elements {
		if el.Secure {
			text = annotateToken(text, el.Token)
		}
	}
	return text
}

// annotateToken rewrites one token's standalone occurrences in text.
func annotateToken(text, token string) string {
	if token == "" || strings.ContainsAny(token, " \t\n\r\v\f") {
		return text
	}
	var b strings.Builder
	annotation := " [secure]"
	for {
		i := strings.Index(text, token)
		if i < 0 {
			b.WriteString(text)
			return b.String()
		}
		end := i + len(token)
		if strings.HasPrefix(text[end:], annotation) {
			// Already annotated: copy the annotation through exactly once.
			b.WriteString(text[:end])
			b.WriteString(annotation)
			text = text[end+len(annotation):]
			continue
		}
		standalone := (i == 0 || isTokenBoundary(text[i-1])) &&
			(end == len(text) || isTokenBoundary(text[end]))
		b.WriteString(text[:end])
		if standalone {
			b.WriteString(annotation)
		}
		text = text[end:]
	}
}

// isTokenBoundary reports whether the byte ends or opens a token run.
// Whitespace is a boundary; every other byte (including UTF-8 continuation
// bytes) is not — an embedded hit inside a longer run is skipped rather than
// mis-annotated.
func isTokenBoundary(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return false
}

// renderObservation post-processes one screenshot-bearing driver result: the
// tree is secure-annotated (C2) and the PNG — when the driver supplied one —
// is written into the resolved dir, the capture id remembered as the fresh
// binding coordinate actions must cite, and the result carries path, pixel
// dimensions, and marker (C1).
func (t *computerTools) renderObservation(req mcp.CallToolRequest, res DriverResult) (*mcp.CallToolResult, error) {
	out := ObservationResult{Tree: AnnotateSecure(res.Text, res.Elements)}
	if len(res.ImagePNG) > 0 {
		target := resolveScreenshotTarget(req, t.cfg.ComputerDir)
		capture, path, w, h, marker, err := writeCapture(target, res.ImagePNG)
		if err != nil {
			return errResult(err), nil
		}
		t.rememberCapture(capture)
		out.Capture, out.ScreenshotPath, out.Width, out.Height, out.Marker =
			capture, path, w, h, marker
	}
	return jsonResult(out)
}

// rememberCapture latches the latest capture id: the binding coordinate
// actions are checked against (spec computer-use "Targeting and staleness").
func (t *computerTools) rememberCapture(capture string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.capture = capture
	t.hasCapture = true
}

// clearCapture drops the binding — stop's local latch (design D4): after a
// stop, no coordinate action may fire against the old screen state without a
// fresh observe.
func (t *computerTools) clearCapture() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.capture = ""
	t.hasCapture = false
}

// checkCapture binds one coordinate action to a fresh capture: the capture id
// is required (coordinates only mean something against an observation this
// adapter has seen) and a non-latest capture is refused with the fixed
// stale-capture refusal so the model re-observes.
func (t *computerTools) checkCapture(capture string) error {
	if strings.TrimSpace(capture) == "" {
		return errors.New(MissingCaptureRefusal)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.hasCapture || capture != t.capture {
		return errors.New(StaleCaptureRefusal)
	}
	return nil
}

// jsonResult renders a payload as the tool's JSON text result.
func jsonResult(res any) (*mcp.CallToolResult, error) {
	out, err := json.Marshal(res)
	if err != nil {
		return errResult(fmt.Errorf("computer: encode result: %w", err)), nil
	}
	return mcp.NewToolResultText(string(out)), nil
}
