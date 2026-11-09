package computeruse

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

// The curated-server handler tests (add-computer-use tasks 2.5, design D4,
// spec computer-use "Curated tool surface" and "Observation screenshots and
// the marker contract"): every tool class runs against a stub driver client —
// the happy path plus one error path per class — with the C1 marker/marker
// contract and the C2 secure annotations asserted directly.

// stubDriver is the driverClient seam: per-driver-tool canned results and
// errors, plus the call log the targeting assertions read.
type stubDriver struct {
	mu      sync.Mutex
	results map[string]DriverResult
	errs    map[string]error
	calls   []stubDriverCall
}

type stubDriverCall struct {
	name string
	args map[string]any
}

func newStubDriver() *stubDriver {
	return &stubDriver{results: map[string]DriverResult{}, errs: map[string]error{}}
}

func (s *stubDriver) set(name string, res DriverResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results[name] = res
}

func (s *stubDriver) fail(name string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errs[name] = err
}

func (s *stubDriver) CallTool(_ context.Context, name string, args map[string]any) (DriverResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, stubDriverCall{name: name, args: args})
	if err := s.errs[name]; err != nil {
		return DriverResult{}, err
	}
	return s.results[name], nil
}

func (s *stubDriver) Close() error { return nil }

// recorded returns the calls made to one driver tool name.
func (s *stubDriver) recorded(name string) []stubDriverCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []stubDriverCall
	for _, c := range s.calls {
		if c.name == name {
			out = append(out, c)
		}
	}
	return out
}

// testPNG renders a distinct 4x2 PNG.
func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	img.Set(0, 0, color.RGBA{R: 0xF0, A: 0xFF})
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

// callTool drives one curated tool handler with JSON-style arguments and an
// optional _meta, returning the result.
func callTool(t *testing.T, h func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error), name string, args map[string]any, meta map[string]any) mcp.CallToolResult {
	t.Helper()
	params := mcp.CallToolParams{Name: name, Arguments: args}
	if meta != nil {
		params.Meta = mcp.NewMetaFromMap(meta)
	}
	res, err := h(context.Background(), mcp.CallToolRequest{Params: params})
	require.NoError(t, err, "domain failures are tool errors, never JSON-RPC errors")
	require.NotNil(t, res)
	return *res
}

// resultText flattens a tool result's text content.
func resultText(t *testing.T, res mcp.CallToolResult) string {
	t.Helper()
	require.NotEmpty(t, res.Content, "every tool result carries its message")
	text, ok := res.Content[0].(mcp.TextContent)
	require.True(t, ok, "content must be text, got %T", res.Content[0])
	return text.Text
}

// decodeResult unmarshals a JSON result payload.
func decodeResult(t *testing.T, res mcp.CallToolResult, out any) {
	t.Helper()
	require.False(t, res.IsError, "expected a success result, got error: %s", resultText(t, res))
	require.NoError(t, json.Unmarshal([]byte(resultText(t, res)), out))
}

// newTestTools builds computerTools with the stub injected as the driver (the
// pin check is bypassed — the stub IS the resolved driver) and the drivers
// root pointed at scratch space so an accidental pin walk cannot touch the
// user's home.
func newTestTools(t *testing.T, driver *stubDriver) *computerTools {
	t.Helper()
	t.Setenv(EnvDriversRoot, t.TempDir())
	releaseDispatch() // the process-wide lease is free before the first call
	tools := &computerTools{cfg: ServerConfig{
		SocketPath:  "/tmp/cua-test.sock",
		ComputerDir: t.TempDir(),
		Ready:       true,
	}}
	tools.driver = driver
	t.Cleanup(func() { releaseDispatch() })
	return tools
}

// metaSession builds the parent's C1 _meta: the absolute session dir and the
// workspace-relative facing path.
func metaSession(dir, rel string) map[string]any {
	return map[string]any{MetaScreenshotDir: dir, MetaScreenshotRel: rel}
}

// TestNewMCPServerZeroToolsWhenNotReady pins the permission warm-up gate
// (spec computer-use "Permission warm-up gate"): a not-ready spawn snapshot
// serves zero tools.
func TestNewMCPServerZeroToolsWhenNotReady(t *testing.T) {
	srv := NewMCPServer(ServerConfig{SocketPath: "/tmp/s", ComputerDir: t.TempDir(), Ready: false}, "test")
	res := listTools(t, srv)
	require.Empty(t, res.Tools, "a not-ready server offers zero tools")
}

// wantCuratedTools is design D4's table plus kill_app (contract C4).
var wantCuratedTools = []string{
	"list_apps", "list_windows",
	"observe_window", "observe_desktop",
	"click", "double_click", "right_click", "drag", "scroll",
	"type", "key", "hotkey", "set_value",
	"read_clipboard", "write_clipboard",
	"verify", "zoom", "wait", "stop", "kill_app",
}

// TestNewMCPServerCuratedSurface: the ready server exposes exactly the
// curated surface, every tool with a description teaching its contract.
func TestNewMCPServerCuratedSurface(t *testing.T) {
	srv := NewMCPServer(ServerConfig{SocketPath: "/tmp/s", ComputerDir: t.TempDir(), Ready: true}, "test")
	res := listTools(t, srv)
	names := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
		require.NotEmpty(t, tool.Description, "tool %s must teach its targeting contract", tool.Name)
	}
	require.ElementsMatch(t, wantCuratedTools, names)
}

// TestBusyRefusalVerbatim: with the dispatch lease held, every tool —
// read-only included — answers exactly the fixed busy refusal.
func TestBusyRefusalVerbatim(t *testing.T) {
	driver := newStubDriver()
	tools := newTestTools(t, driver)
	require.NoError(t, acquireDispatch()) // hold the lease "another agent" holds

	for _, name := range []string{"list_apps", "click", "stop"} {
		res := callTool(t, tools.serve(tools.handleList), name, nil, nil)
		require.True(t, res.IsError)
		require.Equal(t, BusyRefusal, resultText(t, res), "the busy refusal is fixed verbatim")
	}
	require.Empty(t, driver.recorded("list_apps"), "a refused call never reaches the driver")
}

// TestObserveWindowHappyC1: the observation carries the annotated tree, the
// capture id, path, pixel dimensions, and the relative-form marker (C1).
func TestObserveWindowHappyC1(t *testing.T) {
	driver := newStubDriver()
	driver.set("get_window_state", DriverResult{
		Text:     "btn-1 button Submit\npw-1 textbox Password",
		ImagePNG: testPNG(t),
		Elements: []DriverElement{{Token: "pw-1", Secure: true}},
	})
	tools := newTestTools(t, driver)
	sessionDir := t.TempDir()
	rel := "sessions/s1/computer"

	res := callTool(t, tools.handleObserveWindow, "observe_window", map[string]any{"app": "Notes"}, metaSession(sessionDir, rel))
	var out ObservationResult
	decodeResult(t, res, &out)

	require.Equal(t, "btn-1 button Submit\npw-1 [secure] textbox Password", out.Tree,
		"the secure annotation rides immediately after the token (C2)")
	require.NotEmpty(t, out.Capture)
	require.NotEmpty(t, out.Marker)
	require.Contains(t, out.Marker, "[screenshot: "+rel+"/")
	require.Contains(t, out.Marker, ".png]")
	require.Equal(t, 4, out.Width)
	require.Equal(t, 2, out.Height)
	require.FileExists(t, out.ScreenshotPath)
	require.Contains(t, out.ScreenshotPath, sessionDir)
	require.Regexp(t, `^\d+$`, out.Capture, "the capture id is the png filename stem")

	// The fresh capture id is the coordinate binding.
	require.NoError(t, tools.checkCapture(out.Capture))
}

// TestObserveWindowFallbackDirWithoutMeta: no _meta means the fallback under
// the computer dir, and the marker carries the plain written path.
func TestObserveWindowFallbackDirWithoutMeta(t *testing.T) {
	driver := newStubDriver()
	driver.set("get_window_state", DriverResult{Text: "tree", ImagePNG: testPNG(t)})
	tools := newTestTools(t, driver)

	res := callTool(t, tools.handleObserveWindow, "observe_window", nil, nil)
	var out ObservationResult
	decodeResult(t, res, &out)

	require.Contains(t, out.ScreenshotPath, tools.cfg.ComputerDir)
	require.Contains(t, out.ScreenshotPath, "screenshots")
	require.Contains(t, out.Marker, "[screenshot: "+out.ScreenshotPath+"]",
		"without the rel key the marker carries the plain written path")
}

// TestObserveWindowEscapedMetaRejected: a _meta dir that is not a clean
// absolute path falls back like an absent one.
func TestObserveWindowEscapedMetaRejected(t *testing.T) {
	driver := newStubDriver()
	driver.set("get_window_state", DriverResult{Text: "tree", ImagePNG: testPNG(t)})
	tools := newTestTools(t, driver)

	res := callTool(t, tools.handleObserveWindow, "observe_window", nil, metaSession("../escape", "sessions/s1/computer"))
	var out ObservationResult
	decodeResult(t, res, &out)
	require.Contains(t, out.ScreenshotPath, tools.cfg.ComputerDir, "a relative meta dir falls back")
}

// TestObserveDriverErrorReadable: a driver failure is a readable tool error
// carrying the fixed guidance — the turn continues.
func TestObserveDriverErrorReadable(t *testing.T) {
	driver := newStubDriver()
	driver.fail("get_window_state", context.DeadlineExceeded)
	tools := newTestTools(t, driver)

	res := callTool(t, tools.handleObserveWindow, "observe_window", nil, nil)
	require.True(t, res.IsError)
	text := resultText(t, res)
	require.Contains(t, text, ActionFailureGuidance)
	require.Contains(t, text, "computer:")
}

// TestClickByElementToken: an element click forwards the token verbatim and
// reports ok with the driver's receipt.
func TestClickByElementToken(t *testing.T) {
	driver := newStubDriver()
	driver.set("click", DriverResult{Text: "clicked"})
	tools := newTestTools(t, driver)

	res := callTool(t, tools.handleClick, "click", map[string]any{"element": "btn-1"}, nil)
	var out map[string]any
	decodeResult(t, res, &out)
	require.Equal(t, true, out["ok"])
	require.Equal(t, "clicked", out["text"])

	calls := driver.recorded("click")
	require.Len(t, calls, 1)
	require.Equal(t, map[string]any{"element": "btn-1"}, calls[0].args)
}

// TestClickByCoordinatesRequiresFreshCapture: coordinates without a capture
// id are refused; a stale capture id gets the fixed stale refusal; the fresh
// id passes.
func TestClickByCoordinatesRequiresFreshCapture(t *testing.T) {
	driver := newStubDriver()
	driver.set("get_window_state", DriverResult{Text: "tree", ImagePNG: testPNG(t)})
	tools := newTestTools(t, driver)

	// No capture given at all.
	res := callTool(t, tools.handleClick, "click", map[string]any{"x": 10, "y": 20}, nil)
	require.True(t, res.IsError)
	require.Equal(t, MissingCaptureRefusal, resultText(t, res))

	// Observe once, then click with a wrong (stale) id.
	obs := callTool(t, tools.handleObserveWindow, "observe_window", nil, nil)
	var out ObservationResult
	decodeResult(t, obs, &out)
	res = callTool(t, tools.handleClick, "click", map[string]any{"x": 10, "y": 20, "capture": "12345"}, nil)
	require.True(t, res.IsError)
	require.Equal(t, StaleCaptureRefusal, resultText(t, res))

	// The fresh id passes and reaches the driver as bare coordinates.
	driver.set("click", DriverResult{})
	res = callTool(t, tools.handleClick, "click", map[string]any{"x": 10, "y": 20, "capture": out.Capture}, nil)
	var ok map[string]any
	decodeResult(t, res, &ok)
	require.Equal(t, true, ok["ok"])
	calls := driver.recorded("click")
	require.Len(t, calls, 1)
	require.Equal(t, map[string]any{"x": 10, "y": 20}, calls[0].args)
}

// TestClickDriverErrorReadable: an unknown element token (the driver's
// fail-closed staleness) is a readable error guiding re-observation.
func TestClickDriverErrorReadable(t *testing.T) {
	driver := newStubDriver()
	driver.fail("click", context.Canceled)
	tools := newTestTools(t, driver)

	res := callTool(t, tools.handleClick, "click", map[string]any{"element": "gone-1"}, nil)
	require.True(t, res.IsError)
	require.Contains(t, resultText(t, res), ActionFailureGuidance)
}

// TestDragScrollCoordinateBinding: drag and point-scroll are bound to a
// fresh capture like click.
func TestDragScrollCoordinateBinding(t *testing.T) {
	driver := newStubDriver()
	driver.set("drag", DriverResult{})
	driver.set("scroll", DriverResult{})
	tools := newTestTools(t, driver)

	res := callTool(t, tools.handleDrag, "drag", map[string]any{"fromX": 1, "fromY": 2, "toX": 3, "toY": 4}, nil)
	require.True(t, res.IsError)
	require.Equal(t, MissingCaptureRefusal, resultText(t, res))

	res = callTool(t, tools.handleScroll, "scroll", map[string]any{"direction": "up", "x": 5, "y": 6, "capture": "nope"}, nil)
	require.True(t, res.IsError)
	require.Equal(t, StaleCaptureRefusal, resultText(t, res))

	res = callTool(t, tools.handleScroll, "scroll", map[string]any{"direction": "sideways"}, nil)
	require.True(t, res.IsError)
	require.Contains(t, resultText(t, res), "direction must be up or down")

	// Both pass with no capture when no coordinates ride (frontmost scroll).
	res = callTool(t, tools.handleScroll, "scroll", map[string]any{"direction": "down"}, nil)
	var ok map[string]any
	decodeResult(t, res, &ok)
	require.Equal(t, true, ok["ok"])
}

// TestKeyboardTools: key and hotkey happy paths and their missing-argument
// errors.
func TestKeyboardTools(t *testing.T) {
	driver := newStubDriver()
	driver.set("press_key", DriverResult{})
	driver.set("hotkey", DriverResult{})
	tools := newTestTools(t, driver)

	res := callTool(t, tools.handleKey, "key", map[string]any{"key": "Enter"}, nil)
	var ok map[string]any
	decodeResult(t, res, &ok)
	require.Equal(t, true, ok["ok"])
	require.Equal(t, map[string]any{"key": "Enter"}, driver.recorded("press_key")[0].args)

	res = callTool(t, tools.handleKey, "key", nil, nil)
	require.True(t, res.IsError)
	require.Contains(t, resultText(t, res), "key is required")

	res = callTool(t, tools.handleHotkey, "hotkey", map[string]any{"keys": "cmd+c"}, nil)
	decodeResult(t, res, &ok)
	require.Equal(t, true, ok["ok"])
	require.Equal(t, map[string]any{"keys": "cmd+c"}, driver.recorded("hotkey")[0].args)

	res = callTool(t, tools.handleHotkey, "hotkey", nil, nil)
	require.True(t, res.IsError)
}

// TestTypeAndSetValue: the credential-typing seam (C4's gated tools) forward
// element + text/value verbatim; missing elements are readable errors.
func TestTypeAndSetValue(t *testing.T) {
	driver := newStubDriver()
	driver.set("type_text", DriverResult{})
	driver.set("set_value", DriverResult{})
	tools := newTestTools(t, driver)

	res := callTool(t, tools.handleType, "type", map[string]any{"element": "user-1", "text": "ada"}, nil)
	var ok map[string]any
	decodeResult(t, res, &ok)
	require.Equal(t, true, ok["ok"])
	require.Equal(t, map[string]any{"element": "user-1", "text": "ada"}, driver.recorded("type_text")[0].args)

	res = callTool(t, tools.handleType, "type", map[string]any{"element": "", "text": "x"}, nil)
	require.True(t, res.IsError)
	require.Contains(t, resultText(t, res), "element is required")

	res = callTool(t, tools.handleType, "type", map[string]any{"element": "user-1"}, nil)
	require.True(t, res.IsError)
	require.Contains(t, resultText(t, res), "text is required")

	res = callTool(t, tools.handleSetValue, "set_value", map[string]any{"element": "user-1", "value": "ada@lovelace.dev"}, nil)
	decodeResult(t, res, &ok)
	require.Equal(t, true, ok["ok"])
	require.Equal(t, map[string]any{"element": "user-1", "value": "ada@lovelace.dev"}, driver.recorded("set_value")[0].args)

	res = callTool(t, tools.handleSetValue, "set_value", map[string]any{"value": "x"}, nil)
	require.True(t, res.IsError)
}

// TestClipboardTools: read and write.
func TestClipboardTools(t *testing.T) {
	driver := newStubDriver()
	driver.set("read_clipboard", DriverResult{Text: "clipboard text"})
	driver.set("write_clipboard", DriverResult{})
	tools := newTestTools(t, driver)

	res := callTool(t, tools.handleReadClipboard, "read_clipboard", nil, nil)
	var out map[string]any
	decodeResult(t, res, &out)
	require.Equal(t, "clipboard text", out["text"])

	res = callTool(t, tools.handleWriteClipboard, "write_clipboard", map[string]any{"text": "hello"}, nil)
	var ok map[string]any
	decodeResult(t, res, &ok)
	require.Equal(t, true, ok["ok"])
	require.Equal(t, map[string]any{"text": "hello"}, driver.recorded("write_clipboard")[0].args)
}

// TestVerifyAndList: verify forwards the expectation; the list tools carry
// the C2 annotations.
func TestVerifyAndList(t *testing.T) {
	driver := newStubDriver()
	driver.set("verify_state", DriverResult{Text: "the dialog is gone"})
	driver.set("list_apps", DriverResult{
		Text:     "app-1 app Notes\nfld-2 textbox Search",
		Elements: []DriverElement{{Token: "fld-2", Secure: true}},
	})
	tools := newTestTools(t, driver)

	res := callTool(t, tools.handleVerify, "verify", map[string]any{"expectation": "the dialog is closed"}, nil)
	var out map[string]any
	decodeResult(t, res, &out)
	require.Equal(t, "the dialog is gone", out["text"])

	res = callTool(t, tools.handleVerify, "verify", nil, nil)
	require.True(t, res.IsError)

	res = callTool(t, tools.handleList, "list_apps", nil, nil)
	decodeResult(t, res, &out)
	require.Equal(t, "app-1 app Notes\nfld-2 [secure] textbox Search", out["text"],
		"the list tools ride the same secure annotations the parent records (C2)")
}

// TestZoomAndStop: zoom is capture-bound and re-marks a fresh capture; stop
// clears the binding so old coordinates cannot fire again.
func TestZoomAndStop(t *testing.T) {
	driver := newStubDriver()
	driver.set("get_desktop_state", DriverResult{Text: "desktop tree", ImagePNG: testPNG(t)})
	driver.set("zoom", DriverResult{Text: "zoomed", ImagePNG: testPNG(t)})
	driver.set("revoke", DriverResult{})
	tools := newTestTools(t, driver)

	// Zoom without any observation yet: no binding to cite.
	res := callTool(t, tools.handleZoom, "zoom", map[string]any{"x": 0, "y": 0, "width": 10, "height": 10, "capture": "9"}, nil)
	require.True(t, res.IsError)
	require.Equal(t, StaleCaptureRefusal, resultText(t, res))

	obs := callTool(t, tools.handleObserveDesktop, "observe_desktop", nil, nil)
	var desktop ObservationResult
	decodeResult(t, obs, &desktop)
	require.Equal(t, "desktop tree", desktop.Tree)
	require.NotEmpty(t, desktop.Capture)

	res = callTool(t, tools.handleZoom, "zoom", map[string]any{
		"x": 0, "y": 0, "width": 10, "height": 10, "capture": desktop.Capture,
	}, nil)
	var zoom ObservationResult
	decodeResult(t, res, &zoom)
	require.Equal(t, "zoomed", zoom.Tree)
	require.NotEmpty(t, zoom.Marker)
	require.NotEqual(t, desktop.Capture, zoom.Capture, "the zoom mints a fresh capture id")

	// Stop clears the binding: the desktop capture is now stale.
	res = callTool(t, tools.handleStop, "stop", nil, nil)
	var ok map[string]any
	decodeResult(t, res, &ok)
	require.Equal(t, true, ok["ok"])
	require.Len(t, driver.recorded("revoke"), 1, "stop drives the revoke-equivalent")

	res = callTool(t, tools.handleClick, "click", map[string]any{"x": 1, "y": 2, "capture": desktop.Capture}, nil)
	require.True(t, res.IsError)
	require.Equal(t, StaleCaptureRefusal, resultText(t, res))
}

// TestWaitAndKillApp: wait forwards ms; kill_app requires the app name and
// forwards it (the parent-side approval gate is the C4 seam).
func TestWaitAndKillApp(t *testing.T) {
	driver := newStubDriver()
	driver.set("wait", DriverResult{})
	driver.set("kill_app", DriverResult{})
	tools := newTestTools(t, driver)

	ms := 250
	res := callTool(t, tools.handleWait, "wait", map[string]any{"ms": ms}, nil)
	var ok map[string]any
	decodeResult(t, res, &ok)
	require.Equal(t, true, ok["ok"])
	require.Equal(t, map[string]any{"ms": 250}, driver.recorded("wait")[0].args)

	res = callTool(t, tools.handleWait, "wait", nil, nil)
	decodeResult(t, res, &ok)
	require.Equal(t, true, ok["ok"], "wait without ms waits for settle")

	res = callTool(t, tools.handleKillApp, "kill_app", map[string]any{"app": "Ghost"}, nil)
	decodeResult(t, res, &ok)
	require.Equal(t, true, ok["ok"])
	require.Equal(t, map[string]any{"app": "Ghost"}, driver.recorded("kill_app")[0].args)

	res = callTool(t, tools.handleKillApp, "kill_app", nil, nil)
	require.True(t, res.IsError)
	require.Contains(t, resultText(t, res), "app is required")
}

// TestDriverNotReadyReadable: a not-ready pin (here: a tampered install,
// seeded offline) refuses the call with a readable reason instead of failing
// the turn — and never launches anything.
func TestDriverNotReadyReadable(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvDriversRoot, root)
	// Seed the install dir with a binary that fails its pin, so the public
	// EnsureDriverDownloaded path refuses offline (no download attempted).
	binaryName := "cua-driver"
	if runtime.GOOS == "windows" {
		binaryName = "cua-driver.exe"
	}
	installDir := filepath.Join(root, "cua-driver", pinnedVersion)
	require.NoError(t, os.MkdirAll(installDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(installDir, binaryName), []byte("tampered"), 0o755))

	tools := &computerTools{cfg: ServerConfig{SocketPath: "/tmp/s", ComputerDir: t.TempDir(), Ready: true}}

	res := callTool(t, tools.handleList, "list_apps", nil, nil)
	require.True(t, res.IsError)
	require.Contains(t, resultText(t, res), "the pinned driver is not available")
	require.Contains(t, resultText(t, res), "failed its SHA-256 pin")
}

// TestAnnotateSecure pins the C2 rendering contract: " [secure]" rides
// immediately after the token, boundary safety, idempotence, whitespace
// tokens skipped.
func TestAnnotateSecure(t *testing.T) {
	require.Equal(t,
		"tok-1 [secure] button Save",
		AnnotateSecure("tok-1 button Save", []DriverElement{{Token: "tok-1", Secure: true}}))
	// A token inside a longer run is never annotated.
	require.Equal(t,
		"token-1 stays",
		AnnotateSecure("token-1 stays", []DriverElement{{Token: "ken-1", Secure: true}}))
	// Already annotated passes through exactly once.
	require.Equal(t,
		"tok-1 [secure] stays",
		AnnotateSecure("tok-1 [secure] stays", []DriverElement{{Token: "tok-1", Secure: true}}))
	// Whitespace-bearing tokens are skipped (C2: tokens are non-whitespace).
	require.Equal(t,
		"two words",
		AnnotateSecure("two words", []DriverElement{{Token: "two words", Secure: true}}))
	// Non-secure elements are untouched.
	require.Equal(t,
		"tok-1 button Save",
		AnnotateSecure("tok-1 button Save", []DriverElement{{Token: "tok-1", Secure: false}}))
}

// listTools drives the server's HandleMessage with a tools/list request and
// returns the decoded result (the browseruse server_test pattern). A server
// with zero tools answers the protocol error "tools not supported" — no
// tools capability is advertised — which the helper reports as an empty
// surface.
func listTools(t *testing.T, srv interface {
	HandleMessage(context.Context, json.RawMessage) mcp.JSONRPCMessage
}) mcp.ListToolsResult {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
	})
	require.NoError(t, err)
	resp := srv.HandleMessage(context.Background(), raw)
	if errResp, ok := resp.(mcp.JSONRPCError); ok {
		require.Contains(t, errResp.Error.Message, "tools not supported",
			"a zero-tool server advertises no tools capability")
		return mcp.ListToolsResult{}
	}
	toolResult, ok := resp.(mcp.JSONRPCResponse)
	require.True(t, ok, "tools/list must answer with a response, got %T", resp)
	result, ok := toolResult.Result.(mcp.ListToolsResult)
	require.True(t, ok, "tools/list result must decode, got %T", toolResult.Result)
	return result
}
