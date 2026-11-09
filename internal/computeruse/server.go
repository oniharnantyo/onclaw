package computeruse

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// The Computer Use MCP server (add-computer-use design D1, spec computer-use
// "Adapter server process" and "Curated tool surface"): a curated server,
// not a byte-forwarder — mcp-go server facing the parent, the driver proxy
// (proxy.go) facing the vendored daemon. Curation buys a fixed 20-tool
// surface instead of the driver's ~40, fixed-refusal discipline
// (refusals.go), the single-dispatcher lease (design D3), and a place to
// hang the observation lane (observe.go).
//
// Readiness is the permission warm-up gate (design D6, spec computer-use
// "Permission warm-up gate"): a spawn snapshot that is not ready serves ZERO
// tools — no daemon is running, so there is nothing to drive.

// ServerName is the MCP server's registered name; the spawning parent's
// namer surfaces the tools as mcp__computer-use__<tool>.
const ServerName = "computer-use"

// actionTimeout bounds one driver call; desktop actions (screenshot-bearing
// observation, verify) are slower than browser round trips, so the bound is
// the browseruse default doubled.
const actionTimeout = 60 * time.Second

// ServerConfig is the adapter's spawn-time snapshot — the mcp registry's
// computer-use spawn config (coordination contract): the daemon socket, the
// computer-use data directory (the fallback screenshots root), and the
// parent's readiness verdict. A comparable struct of scalars by contract.
type ServerConfig struct {
	// SocketPath is the driver daemon's private local socket.
	SocketPath string
	// ComputerDir is the computer-use data directory; the fallback
	// screenshots directory lives under screenshots/ inside it.
	ComputerDir string
	// Ready is the permission warm-up gate's verdict: false serves zero
	// tools.
	Ready bool
}

// computerTools carries the spawn-time snapshot and the driver proxy the
// handlers run against.
type computerTools struct {
	cfg ServerConfig

	// mu guards the lazy driver proxy and the capture latch.
	mu     sync.Mutex
	driver driverClient
	// capture is the latest observation's capture id — the fresh binding
	// coordinate actions must cite (spec computer-use "Targeting and
	// staleness"); hasCapture distinguishes "none yet" from "".
	capture    string
	hasCapture bool
}

// NewMCPServer builds the Computer Use MCP server: the curated 20-tool
// surface (design D4's table plus kill_app — coordination contract C4) when
// the snapshot says ready, and zero tools otherwise.
func NewMCPServer(cfg ServerConfig, version string) *server.MCPServer {
	t := &computerTools{cfg: cfg}
	s := server.NewMCPServer(ServerName, version)
	if !cfg.Ready {
		return s
	}
	for _, st := range t.toolset() {
		s.AddTool(st.Tool, st.Handler)
	}
	return s
}

// toolset defines the curated surface with its schemas and handlers (design
// D4's table, plus kill_app). One entry per row keeps the D4 table
// auditable line by line.
func (t *computerTools) toolset() []server.ServerTool {
	return []server.ServerTool{
		{Tool: listAppsTool(), Handler: t.serve(t.handleList)},
		{Tool: listWindowsTool(), Handler: t.serve(t.handleList)},
		{Tool: observeWindowTool(), Handler: t.serve(t.handleObserveWindow)},
		{Tool: observeDesktopTool(), Handler: t.serve(t.handleObserveDesktop)},
		{Tool: clickTool("click", "Click once at a target."), Handler: t.serve(t.handleClick)},
		{Tool: clickTool("double_click", "Double-click at a target."), Handler: t.serve(t.handleClick)},
		{Tool: clickTool("right_click", "Right-click at a target."), Handler: t.serve(t.handleClick)},
		{Tool: dragTool(), Handler: t.serve(t.handleDrag)},
		{Tool: scrollTool(), Handler: t.serve(t.handleScroll)},
		{Tool: typeTool(), Handler: t.serve(t.handleType)},
		{Tool: keyTool(), Handler: t.serve(t.handleKey)},
		{Tool: hotkeyTool(), Handler: t.serve(t.handleHotkey)},
		{Tool: setValueTool(), Handler: t.serve(t.handleSetValue)},
		{Tool: readClipboardTool(), Handler: t.serve(t.handleReadClipboard)},
		{Tool: writeClipboardTool(), Handler: t.serve(t.handleWriteClipboard)},
		{Tool: verifyTool(), Handler: t.serve(t.handleVerify)},
		{Tool: zoomTool(), Handler: t.serve(t.handleZoom)},
		{Tool: waitTool(), Handler: t.serve(t.handleWait)},
		{Tool: stopTool(), Handler: t.serve(t.handleStop)},
		{Tool: killAppTool(), Handler: t.serve(t.handleKillApp)},
	}
}

// driverToolName maps a curated tool to its vendored driver backing (design
// D4's "driver backing" column; re-verified at every pin bump — notes.md
// 7.3). The mapping is the adapter's entire coupling to the driver's MCP
// tool names.
var driverToolName = map[string]string{
	"list_apps":       "list_apps",
	"list_windows":    "list_windows",
	"observe_window":  "get_window_state",
	"observe_desktop": "get_desktop_state",
	"click":           "click",
	"double_click":    "double_click",
	"right_click":     "right_click",
	"drag":            "drag",
	"scroll":          "scroll",
	"type":            "type_text",
	"key":             "press_key",
	"hotkey":          "hotkey",
	"set_value":       "set_value",
	"read_clipboard":  "read_clipboard",
	"write_clipboard": "write_clipboard",
	"verify":          "verify_state",
	"zoom":            "zoom",
	"wait":            "wait",
	"stop":            "revoke",
	"kill_app":        "kill_app",
}

// serve runs one tool call under the process-wide dispatch lease (design
// D3): every tool — read-only included — holds the lease for its full
// driver call, because observation must not race an in-flight action's
// settle window. Contention answers the fixed busy refusal verbatim.
func (t *computerTools) serve(h server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := acquireDispatch(); err != nil {
			return errResult(err), nil
		}
		defer releaseDispatch()
		return h(ctx, req)
	}
}

// callDriver invokes one curated tool's driver backing and renders every
// failure as a readable action error (spec computer-use "Curated tool
// surface": action failures are readable tool errors and never fail the
// turn). The second return is non-nil exactly when the call failed.
func (t *computerTools) callDriver(ctx context.Context, toolName string, args map[string]any) (DriverResult, *mcp.CallToolResult) {
	cli, err := t.driverForCall(ctx)
	if err != nil {
		return DriverResult{}, errResult(err)
	}
	name, ok := driverToolName[toolName]
	if !ok {
		return DriverResult{}, errResult(fmt.Errorf("computer: unknown tool %q", toolName))
	}
	cctx, cancel := context.WithTimeout(ctx, actionTimeout)
	defer cancel()
	res, err := cli.CallTool(cctx, name, args)
	if err != nil {
		return DriverResult{}, errResult(actionError(err))
	}
	return res, nil
}

// actionError frames a driver-reported failure with the fixed guidance.
func actionError(err error) error {
	return fmt.Errorf("computer: %s — %v", ActionFailureGuidance, err)
}

// driverForCall resolves the cached driver proxy, dialing and pin-checking
// on first use: the vendored binary is re-verified before it ever runs
// (spec computer-use "Driver acquisition and integrity"), and a not-ready
// pin refuses with a readable reason.
func (t *computerTools) driverForCall(ctx context.Context) (driverClient, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.driver != nil {
		return t.driver, nil
	}
	pin := EnsureDriverDownloaded(ctx, DriversRoot())
	if !pin.Ready {
		return nil, fmt.Errorf("computer: the pinned driver is not available: %s", pin.Reason)
	}
	t.driver = newMCPProxy(pin.BinaryPath, t.cfg.SocketPath)
	return t.driver, nil
}

// Tool schemas. Each description teaches its targeting contract: element
// tokens come verbatim from the latest observation; coordinate actions cite
// the observation result's capture id; failures are readable and answered
// with a fresh observe.

const targetingNote = " Targets come from the latest observe result: address an element by its token verbatim, or use coordinates from that observation's capture id. After any failure, observe again."

func listAppsTool() mcp.Tool {
	return mcp.NewTool("list_apps",
		mcp.WithDescription("List the running applications with addressable element tokens for their windows and controls. Secure (credential) fields are annotated [secure]."),
	)
}

func listWindowsTool() mcp.Tool {
	return mcp.NewTool("list_windows",
		mcp.WithDescription("List the open windows across running applications with addressable element tokens. Secure (credential) fields are annotated [secure]."),
	)
}

func observeWindowTool() mcp.Tool {
	return mcp.NewTool("observe_window",
		mcp.WithDescription("Observe one window: its accessibility tree as text with addressable element tokens, plus a screenshot written to disk (the result carries path, pixel dimensions, and the capture id). Secure (credential) fields are annotated [secure]; the capture id is what coordinate actions must cite. Identify the window by pid from list_apps — the preferred, always-resolved target; an app name is best-effort and may be refused."+targetingNote),
		mcp.WithNumber("pid", mcp.Description("Process id of the owning app, from list_apps — the reliable targeting key.")),
		mcp.WithString("app", mcp.Description("Application name owning the window; best-effort, may be refused by the driver — prefer pid.")),
		mcp.WithString("title", mcp.Description("Window title to observe; omit for the app's main window.")),
	)
}

func observeDesktopTool() mcp.Tool {
	return mcp.NewTool("observe_desktop",
		mcp.WithDescription("Observe the whole primary desktop: the accessibility tree as text with addressable element tokens, plus a screenshot written to disk (path, pixel dimensions, and the capture id coordinate actions must cite). Secure (credential) fields are annotated [secure]."+targetingNote),
	)
}

func clickTool(name, verb string) mcp.Tool {
	return mcp.NewTool(name,
		mcp.WithDescription(verb+" Click an element by its token from the latest observation, or click coordinates from that observation's capture id."+targetingNote),
		mcp.WithString("element", mcp.Description("Element token from the latest observe result.")),
		mcp.WithNumber("x", mcp.Description("X coordinate in pixels from the cited capture.")),
		mcp.WithNumber("y", mcp.Description("Y coordinate in pixels from the cited capture.")),
		mcp.WithString("capture", mcp.Description("The capture id of the observation the coordinates came from (required for coordinate clicks).")),
	)
}

func dragTool() mcp.Tool {
	return mcp.NewTool("drag",
		mcp.WithDescription("Drag from one point to another with the mouse button held. Coordinates must cite the capture id of the observation they came from."+targetingNote),
		mcp.WithNumber("fromX", mcp.Description("Starting X coordinate in pixels.")),
		mcp.WithNumber("fromY", mcp.Description("Starting Y coordinate in pixels.")),
		mcp.WithNumber("toX", mcp.Description("Ending X coordinate in pixels.")),
		mcp.WithNumber("toY", mcp.Description("Ending Y coordinate in pixels.")),
		mcp.WithString("capture", mcp.Description("The capture id of the observation the coordinates came from (required).")),
	)
}

func scrollTool() mcp.Tool {
	return mcp.NewTool("scroll",
		mcp.WithDescription("Scroll the window under a point (or the frontmost window) one notch up or down. Coordinates must cite the capture id of the observation they came from."+targetingNote),
		mcp.WithString("direction", mcp.Description("Scroll direction."), mcp.Enum("up", "down")),
		mcp.WithNumber("amount", mcp.Description("Notches to scroll (default 3).")),
		mcp.WithNumber("x", mcp.Description("X coordinate in pixels; omit to scroll the frontmost window.")),
		mcp.WithNumber("y", mcp.Description("Y coordinate in pixels; omit to scroll the frontmost window.")),
		mcp.WithString("capture", mcp.Description("The capture id of the observation the coordinates came from (required when x/y are given).")),
	)
}

func typeTool() mcp.Tool {
	return mcp.NewTool("type",
		mcp.WithDescription("Type text into an element addressed by its token from the latest observation. Typing into a credential field is gated behind user approval by the host."+targetingNote),
		mcp.WithString("element", mcp.Description("Element token of the field to type into, from the latest observe result.")),
		mcp.WithString("text", mcp.Description("The text to type.")),
	)
}

func keyTool() mcp.Tool {
	return mcp.NewTool("key",
		mcp.WithDescription("Press a named key on the frontmost application, e.g. Enter, Tab, Escape, ArrowDown."),
		mcp.WithString("key", mcp.Description("Key to press, e.g. Enter, Tab, Escape, ArrowDown.")),
	)
}

func hotkeyTool() mcp.Tool {
	return mcp.NewTool("hotkey",
		mcp.WithDescription("Press a key combination, e.g. cmd+c or ctrl+shift+t."),
		mcp.WithString("keys", mcp.Description("The combination, e.g. cmd+c, ctrl+shift+t.")),
	)
}

func setValueTool() mcp.Tool {
	return mcp.NewTool("set_value",
		mcp.WithDescription("Set an element's value directly over accessibility — faster and more reliable than typing, where supported. Setting a credential field's value is gated behind user approval by the host."+targetingNote),
		mcp.WithString("element", mcp.Description("Element token of the field to set, from the latest observe result.")),
		mcp.WithString("value", mcp.Description("The value to set.")),
	)
}

func readClipboardTool() mcp.Tool {
	return mcp.NewTool("read_clipboard",
		mcp.WithDescription("Read the system clipboard's current text."),
	)
}

func writeClipboardTool() mcp.Tool {
	return mcp.NewTool("write_clipboard",
		mcp.WithDescription("Replace the system clipboard's text. Clipboard writes are gated behind user approval by the host."),
		mcp.WithString("text", mcp.Description("The text to put on the clipboard.")),
	)
}

func verifyTool() mcp.Tool {
	return mcp.NewTool("verify",
		mcp.WithDescription("Verify a stated condition against the live screen — e.g. that a dialog closed or a value shows. Answers with what the screen actually shows, so a failed verification is information, not an error."),
		mcp.WithString("expectation", mcp.Description("The condition to verify, in plain language.")),
	)
}

func zoomTool() mcp.Tool {
	return mcp.NewTool("zoom",
		mcp.WithDescription("Zoom into a region of the latest capture for a closer look: re-captures the region and writes it as a screenshot (path, pixel dimensions, and a fresh capture id)."+targetingNote),
		mcp.WithNumber("x", mcp.Description("Region X in pixels from the cited capture.")),
		mcp.WithNumber("y", mcp.Description("Region Y in pixels from the cited capture.")),
		mcp.WithNumber("width", mcp.Description("Region width in pixels.")),
		mcp.WithNumber("height", mcp.Description("Region height in pixels.")),
		mcp.WithString("capture", mcp.Description("The capture id of the observation the region came from (required).")),
	)
}

func waitTool() mcp.Tool {
	return mcp.NewTool("wait",
		mcp.WithDescription("Wait for the desktop interface to settle (animations, window transitions), or for a fixed delay in milliseconds (ms given)."),
		mcp.WithNumber("ms", mcp.Description("Fixed delay in milliseconds; omit to wait for settle instead.")),
	)
}

func stopTool() mcp.Tool {
	return mcp.NewTool("stop",
		mcp.WithDescription("Stop controlling the desktop: cancel any in-flight work and release control. Always safe to call and never gated."),
	)
}

func killAppTool() mcp.Tool {
	return mcp.NewTool("kill_app",
		mcp.WithDescription("Quit (and if needed force-quit) a running application by name. Destructive: the host gates this behind user approval."),
		mcp.WithString("app", mcp.Description("The application to quit.")),
	)
}

// Argument shapes. The JSON keys are the schemas' argument names; the
// element/app keys are the parent-side approval gate's read seam
// (coordination contracts C2/C4).

type windowArgs struct {
	App   string `json:"app"`
	Title string `json:"title"`
	// Pid is the reliable target: the driver resolves window state by
	// process id (the live smoke showed app-name lookups refused). A
	// pointer separates an absent field from zero.
	Pid *int `json:"pid,omitempty"`
}

type clickArgs struct {
	Element string `json:"element"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Capture string `json:"capture"`
}

type dragArgs struct {
	FromX   int    `json:"fromX"`
	FromY   int    `json:"fromY"`
	ToX     int    `json:"toX"`
	ToY     int    `json:"toY"`
	Capture string `json:"capture"`
}

type scrollArgs struct {
	Direction string `json:"direction"`
	Amount    int    `json:"amount"`
	X         int    `json:"x"`
	Y         int    `json:"y"`
	Capture   string `json:"capture"`
}

type typeArgs struct {
	Element string `json:"element"`
	Text    string `json:"text"`
}

type keyArgs struct {
	Key string `json:"key"`
}

type hotkeyArgs struct {
	Keys string `json:"keys"`
}

type setValueArgs struct {
	Element string `json:"element"`
	Value   string `json:"value"`
}

type writeClipboardArgs struct {
	Text string `json:"text"`
}

type verifyArgs struct {
	Expectation string `json:"expectation"`
}

type zoomArgs struct {
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Capture string `json:"capture"`
}

type waitArgs struct {
	Ms *int `json:"ms"`
}

type killAppArgs struct {
	App string `json:"app"`
}

// Handlers. Every handler is wrapped by serve's dispatch lease; domain
// failures come back as readable tool-error results so the calling turn
// continues — they are never JSON-RPC errors.

// handleList serves list_apps and list_windows: the driver's listing,
// secure-annotated (C2 — the parent's approval gate records these tokens).
func (t *computerTools) handleList(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	res, errRes := t.callDriver(ctx, req.Params.Name, nil)
	if errRes != nil {
		return errRes, nil
	}
	return jsonResult(map[string]any{
		"text": AnnotateSecure(res.Text, res.Elements),
	})
}

// handleObserveWindow serves observe_window.
func (t *computerTools) handleObserveWindow(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args windowArgs
	if err := req.BindArguments(&args); err != nil {
		return errResult(fmt.Errorf("computer: bad arguments: %w", err)), nil
	}
	driverArgs := map[string]any{}
	if args.Pid != nil {
		driverArgs["pid"] = *args.Pid
	}
	if args.App != "" {
		driverArgs["app"] = args.App
	}
	if args.Title != "" {
		driverArgs["title"] = args.Title
	}
	res, errRes := t.callDriver(ctx, req.Params.Name, driverArgs)
	if errRes != nil {
		return errRes, nil
	}
	return t.renderObservation(req, res)
}

// handleObserveDesktop serves observe_desktop (primary display only —
// design D9's documented v1 limit).
func (t *computerTools) handleObserveDesktop(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	res, errRes := t.callDriver(ctx, req.Params.Name, nil)
	if errRes != nil {
		return errRes, nil
	}
	return t.renderObservation(req, res)
}

// handleElementTarget resolves a click-family call's target: an element
// token when given, otherwise coordinates bound to a fresh capture (spec
// computer-use "Targeting and staleness").
func (t *computerTools) handleElementTarget(element string, x, y int, capture string) (map[string]any, *mcp.CallToolResult) {
	if el := strings.TrimSpace(element); el != "" {
		return map[string]any{"element": el}, nil
	}
	if err := t.checkCapture(capture); err != nil {
		return nil, errResult(err)
	}
	return map[string]any{"x": x, "y": y}, nil
}

// handleClick serves click, double_click, and right_click.
func (t *computerTools) handleClick(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args clickArgs
	if err := req.BindArguments(&args); err != nil {
		return errResult(fmt.Errorf("computer: bad arguments: %w", err)), nil
	}
	driverArgs, errRes := t.handleElementTarget(args.Element, args.X, args.Y, args.Capture)
	if errRes != nil {
		return errRes, nil
	}
	res, errRes := t.callDriver(ctx, req.Params.Name, driverArgs)
	if errRes != nil {
		return errRes, nil
	}
	return okResult(res)
}

// handleDrag serves drag.
func (t *computerTools) handleDrag(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args dragArgs
	if err := req.BindArguments(&args); err != nil {
		return errResult(fmt.Errorf("computer: bad arguments: %w", err)), nil
	}
	if err := t.checkCapture(args.Capture); err != nil {
		return errResult(err), nil
	}
	res, errRes := t.callDriver(ctx, req.Params.Name, map[string]any{
		"fromX": args.FromX, "fromY": args.FromY, "toX": args.ToX, "toY": args.ToY,
	})
	if errRes != nil {
		return errRes, nil
	}
	return okResult(res)
}

// handleScroll serves scroll.
func (t *computerTools) handleScroll(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args scrollArgs
	if err := req.BindArguments(&args); err != nil {
		return errResult(fmt.Errorf("computer: bad arguments: %w", err)), nil
	}
	switch args.Direction {
	case "up", "down":
	default:
		return errResult(fmt.Errorf("computer: direction must be up or down, got %q", args.Direction)), nil
	}
	driverArgs := map[string]any{"direction": args.Direction}
	if args.Amount > 0 {
		driverArgs["amount"] = args.Amount
	}
	if args.X != 0 || args.Y != 0 {
		if err := t.checkCapture(args.Capture); err != nil {
			return errResult(err), nil
		}
		driverArgs["x"] = args.X
		driverArgs["y"] = args.Y
	}
	res, errRes := t.callDriver(ctx, req.Params.Name, driverArgs)
	if errRes != nil {
		return errRes, nil
	}
	return okResult(res)
}

// handleType serves type.
func (t *computerTools) handleType(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args typeArgs
	if err := req.BindArguments(&args); err != nil {
		return errResult(fmt.Errorf("computer: bad arguments: %w", err)), nil
	}
	if strings.TrimSpace(args.Element) == "" {
		return errResult(fmt.Errorf("computer: element is required for type; observe first, then cite the field's token")), nil
	}
	if args.Text == "" {
		return errResult(errors.New("computer: text is required for type")), nil
	}
	res, errRes := t.callDriver(ctx, req.Params.Name, map[string]any{
		"element": strings.TrimSpace(args.Element), "text": args.Text,
	})
	if errRes != nil {
		return errRes, nil
	}
	return okResult(res)
}

// handleKey serves key.
func (t *computerTools) handleKey(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args keyArgs
	if err := req.BindArguments(&args); err != nil {
		return errResult(fmt.Errorf("computer: bad arguments: %w", err)), nil
	}
	if strings.TrimSpace(args.Key) == "" {
		return errResult(errors.New("computer: key is required for key")), nil
	}
	res, errRes := t.callDriver(ctx, req.Params.Name, map[string]any{"key": args.Key})
	if errRes != nil {
		return errRes, nil
	}
	return okResult(res)
}

// handleHotkey serves hotkey.
func (t *computerTools) handleHotkey(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args hotkeyArgs
	if err := req.BindArguments(&args); err != nil {
		return errResult(fmt.Errorf("computer: bad arguments: %w", err)), nil
	}
	if strings.TrimSpace(args.Keys) == "" {
		return errResult(errors.New("computer: keys is required for hotkey, e.g. cmd+c")), nil
	}
	res, errRes := t.callDriver(ctx, req.Params.Name, map[string]any{"keys": args.Keys})
	if errRes != nil {
		return errRes, nil
	}
	return okResult(res)
}

// handleSetValue serves set_value.
func (t *computerTools) handleSetValue(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args setValueArgs
	if err := req.BindArguments(&args); err != nil {
		return errResult(fmt.Errorf("computer: bad arguments: %w", err)), nil
	}
	if strings.TrimSpace(args.Element) == "" {
		return errResult(errors.New("computer: element is required for set_value; observe first, then cite the field's token")), nil
	}
	res, errRes := t.callDriver(ctx, req.Params.Name, map[string]any{
		"element": strings.TrimSpace(args.Element), "value": args.Value,
	})
	if errRes != nil {
		return errRes, nil
	}
	return okResult(res)
}

// handleReadClipboard serves read_clipboard.
func (t *computerTools) handleReadClipboard(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	res, errRes := t.callDriver(ctx, req.Params.Name, nil)
	if errRes != nil {
		return errRes, nil
	}
	return okResult(res)
}

// handleWriteClipboard serves write_clipboard.
func (t *computerTools) handleWriteClipboard(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args writeClipboardArgs
	if err := req.BindArguments(&args); err != nil {
		return errResult(fmt.Errorf("computer: bad arguments: %w", err)), nil
	}
	res, errRes := t.callDriver(ctx, req.Params.Name, map[string]any{"text": args.Text})
	if errRes != nil {
		return errRes, nil
	}
	return okResult(res)
}

// handleVerify serves verify.
func (t *computerTools) handleVerify(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args verifyArgs
	if err := req.BindArguments(&args); err != nil {
		return errResult(fmt.Errorf("computer: bad arguments: %w", err)), nil
	}
	if strings.TrimSpace(args.Expectation) == "" {
		return errResult(errors.New("computer: expectation is required for verify, in plain language")), nil
	}
	res, errRes := t.callDriver(ctx, req.Params.Name, map[string]any{"expectation": args.Expectation})
	if errRes != nil {
		return errRes, nil
	}
	return okResult(res)
}

// handleZoom serves zoom.
func (t *computerTools) handleZoom(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args zoomArgs
	if err := req.BindArguments(&args); err != nil {
		return errResult(fmt.Errorf("computer: bad arguments: %w", err)), nil
	}
	if err := t.checkCapture(args.Capture); err != nil {
		return errResult(err), nil
	}
	res, errRes := t.callDriver(ctx, req.Params.Name, map[string]any{
		"x": args.X, "y": args.Y, "width": args.Width, "height": args.Height,
	})
	if errRes != nil {
		return errRes, nil
	}
	return t.renderObservation(req, res)
}

// handleWait serves wait.
func (t *computerTools) handleWait(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args waitArgs
	if err := req.BindArguments(&args); err != nil {
		return errResult(fmt.Errorf("computer: bad arguments: %w", err)), nil
	}
	driverArgs := map[string]any{}
	if args.Ms != nil && *args.Ms > 0 {
		driverArgs["ms"] = *args.Ms
	}
	res, errRes := t.callDriver(ctx, req.Params.Name, driverArgs)
	if errRes != nil {
		return errRes, nil
	}
	return okResult(res)
}

// handleStop serves stop: the driver's revoke-equivalent plus the local
// latch (design D4) — after a stop, coordinate actions need a fresh observe.
func (t *computerTools) handleStop(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	res, errRes := t.callDriver(ctx, req.Params.Name, nil)
	if errRes != nil {
		return errRes, nil
	}
	t.clearCapture()
	return okResult(res)
}

// handleKillApp serves kill_app (coordination contract C4): the driver's
// app-kill. Always gated parent-side (design D7); the adapter runs it like
// any action once the parent released the call.
func (t *computerTools) handleKillApp(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args killAppArgs
	if err := req.BindArguments(&args); err != nil {
		return errResult(fmt.Errorf("computer: bad arguments: %w", err)), nil
	}
	if strings.TrimSpace(args.App) == "" {
		return errResult(errors.New("computer: app is required for kill_app")), nil
	}
	res, errRes := t.callDriver(ctx, req.Params.Name, map[string]any{"app": args.App})
	if errRes != nil {
		return errRes, nil
	}
	return okResult(res)
}

// okResult renders a successful action: the driver's text when it has one,
// a plain ok receipt otherwise.
func okResult(res DriverResult) (*mcp.CallToolResult, error) {
	if res.Text == "" {
		return jsonResult(map[string]any{"ok": true})
	}
	return jsonResult(map[string]any{"ok": true, "text": AnnotateSecure(res.Text, res.Elements)})
}

// errResult renders a domain error as a readable tool-error result.
func errResult(err error) *mcp.CallToolResult {
	return mcp.NewToolResultError(err.Error())
}
