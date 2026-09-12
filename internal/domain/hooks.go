package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// HookLevel is the governance level a hook lives at (D13). Hooks at every
// level reach their whole scope: instance hooks reach every workspace and
// cannot be weakened from below, workspace hooks reach every agent in the
// workspace, and agent hooks are private to their owning agent.
type HookLevel string

const (
	HookLevelInstance  HookLevel = "instance"
	HookLevelWorkspace HookLevel = "workspace"
	HookLevelAgent     HookLevel = "agent"
)

// HookEvent is one of the five lifecycle events hooks fire on (D1). Exactly
// two events may block: user_prompt_submit and pre_tool_use. run_finished
// fires on every terminal outcome with status-as-data (completed|failed|
// cancelled); every payload carries origin (user|scheduler|channel).
type HookEvent string

const (
	HookEventRunStarted       HookEvent = "run_started"
	HookEventUserPromptSubmit HookEvent = "user_prompt_submit"
	HookEventPreToolUse       HookEvent = "pre_tool_use"
	HookEventPostToolUse      HookEvent = "post_tool_use"
	HookEventRunFinished      HookEvent = "run_finished"
)

// HookHandlerType is one registered handler kind (D9). All four share one
// event payload and one decision contract; a successful execution without a
// decision means allow.
type HookHandlerType string

const (
	HookHandlerHTTP    HookHandlerType = "http"
	HookHandlerCommand HookHandlerType = "command"
	HookHandlerMCPTool HookHandlerType = "mcp_tool"
	HookHandlerPrompt  HookHandlerType = "prompt"
	HookHandlerScript  HookHandlerType = "script"
)

// HookFailurePolicy resolves any handler failure — timeout, error, or missing
// structured output where one is required (D9). allow is the fail-open
// default; block is for deliberate safety gates.
type HookFailurePolicy string

const (
	HookFailureAllow HookFailurePolicy = "allow"
	HookFailureBlock HookFailurePolicy = "block"
)

// HookStatus is the health of a hook's most recent delivery (D16).
type HookStatus string

const (
	HookStatusOK    HookStatus = "ok"
	HookStatusError HookStatus = "error"
)

// HookSource distinguishes builtin instance hooks synced from the binary
// (read-only, re-synced every start) from managed rows under superadmin CRUD
// (D15).
type HookSource string

const (
	HookSourceBuiltin HookSource = "builtin"
	HookSourceManaged HookSource = "managed"
)

// Hook timeout defaults and cap in milliseconds. Prompt evaluators get the
// higher default to accommodate model latency (D9, D12).
const (
	DefaultHookTimeoutMS       = 5000
	DefaultPromptHookTimeoutMS = 15000
	MaxHookTimeoutMS           = 60000
)

// MaxHookPatternLength caps regex-tier matcher strings and if-condition
// patterns (D19, D20). List-tier matchers have no length cap.
const MaxHookPatternLength = 256

// MaxHookScriptBytes caps a script hook's JavaScript source (D22): one string
// inside the encrypted config JSONB, bounded so one definition cannot bloat
// the store or the per-run compile.
const MaxHookScriptBytes = 65536

// Canonical whitelists for the level, event, and handler-type enums. Save
// validation and UI hints both read these; new values are added here and in
// the dispatcher/handlers together.
var (
	HookLevels = []HookLevel{
		HookLevelInstance,
		HookLevelWorkspace,
		HookLevelAgent,
	}
	HookEvents = []HookEvent{
		HookEventRunStarted,
		HookEventUserPromptSubmit,
		HookEventPreToolUse,
		HookEventPostToolUse,
		HookEventRunFinished,
	}
	HookHandlerTypes = []HookHandlerType{
		HookHandlerHTTP,
		HookHandlerCommand,
		HookHandlerMCPTool,
		HookHandlerPrompt,
		HookHandlerScript,
	}
)

// HookBase is the hook definition shared by all three governance levels.
type HookBase struct {
	Name  string    `json:"name"`
	Event HookEvent `json:"event"`
	// Matcher selects which occurrences of an event a hook applies to
	// (D19), Claude Code's single-string form. Empty and "*" are match-all;
	// otherwise the string is read tier-first (see validateHookMatcher):
	// charset-valid entries separated by ",", "|", or whitespace match
	// exactly or by trailing-".*" family, and anything else is an unanchored
	// RE2 regex. The matched value is event-aware — tool name on tool
	// events, origin on run-start/prompt events, status on run_finished.
	Matcher string `json:"matcher"`
	// If narrows a tool-event hook to specific inputs, Claude Code-style:
	// `ToolName(pattern)` where the name is an exact tool name or trailing-.*
	// family and the pattern is an unanchored RE2 regex matched against the
	// serialized tool input JSON. A non-matching input SKIPS the hook (no
	// execution, no audit row) — it never blocks. Only pre_tool_use and
	// post_tool_use accept it.
	If          string            `json:"if,omitempty"`
	HandlerType HookHandlerType   `json:"handler_type"`
	Config      json.RawMessage   `json:"config"`
	TimeoutMS   int               `json:"timeout_ms"`
	OnFailure   HookFailurePolicy `json:"on_failure"`
	Enabled     bool              `json:"enabled"`
	Position    int               `json:"position"`
	Status      HookStatus        `json:"status"`
	StatusError string            `json:"status_error"`
}

// InstanceHook is one mandatory instance-level hook. It applies to every
// workspace and agent, is invisible to workspace writes, and is the one
// deliberately workspace-unscoped entity (D13, D15).
type InstanceHook struct {
	HookBase
	ID        string     `json:"id"`
	Key       string     `json:"key"`
	Source    HookSource `json:"source"`
	Version   int        `json:"version"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// WorkspaceHook is one always-on hook for every agent in its workspace,
// gated only by its own Enabled switch (D13).
type WorkspaceHook struct {
	HookBase
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// AgentHook is one hook defined on and private to a single agent, applying
// to that agent automatically (D13).
type AgentHook struct {
	HookBase
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	AgentID     string    `json:"agent_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// HookExecution is one audit record of a hook evaluation: which hook, what
// decision, how long, and the handler-specific failure detail. HookID is
// nullable and HookName denormalized so records survive hook deletion (D16).
type HookExecution struct {
	ID          string    `json:"id"`
	HookID      *string   `json:"hook_id"`
	HookName    string    `json:"hook_name"`
	HookLevel   HookLevel `json:"hook_level"`
	WorkspaceID string    `json:"workspace_id"`
	Event       HookEvent `json:"event"`
	Decision    string    `json:"decision"`
	DurationMS  int64     `json:"duration_ms"`
	ExitCode    *int      `json:"exit_code"`
	HTTPStatus  *int      `json:"http_status"`
	Detail      string    `json:"detail"`
	TokenCount  *int64    `json:"token_count"`
	Origin      string    `json:"origin"`
	CreatedAt   time.Time `json:"created_at"`
}

// MCPServerExistsFunc reports whether a workspace-level MCP server with the
// given ID exists in the given workspace. It is injected into hook validation
// so the domain package does not import store packages. A nil checker skips
// the existence check.
type MCPServerExistsFunc func(workspaceID, serverID string) (bool, error)

// ValidateHook validates the shared hook definition: enums against the
// canonical whitelists, timeout budget, matcher coherence, and the handler
// config shape for the configured handler type. level selects governance-
// specific rules; workspaceID is the scope the MCP existence lookup runs
// against. Instance level skips the lookup entirely — instance hooks are
// workspace-unscoped, so a server's existence is inherently per-workspace.
func ValidateHook(level HookLevel, base *HookBase, workspaceID string, mcpServerExists MCPServerExistsFunc) error {
	if base == nil {
		return ErrInvalid
	}
	switch level {
	case HookLevelInstance, HookLevelWorkspace, HookLevelAgent:
	default:
		return fmt.Errorf("%w: level: unknown level %q", ErrInvalid, level)
	}
	if strings.TrimSpace(base.Name) == "" {
		return fmt.Errorf("%w: name: cannot be empty", ErrInvalid)
	}
	if err := validateHookEvent(base.Event); err != nil {
		return err
	}
	if err := validateHookHandlerType(base.HandlerType); err != nil {
		return err
	}
	if err := validateHookFailurePolicy(base.OnFailure); err != nil {
		return err
	}
	if base.TimeoutMS <= 0 {
		return fmt.Errorf("%w: timeout_ms: must be positive", ErrInvalid)
	}
	if base.TimeoutMS > MaxHookTimeoutMS {
		return fmt.Errorf("%w: timeout_ms: %d exceeds maximum of %d", ErrInvalid, base.TimeoutMS, MaxHookTimeoutMS)
	}
	if err := validateHookMatcher(base.Matcher); err != nil {
		return err
	}
	if err := ValidateHookIf(base.Event, base.If); err != nil {
		return err
	}
	return validateHookConfig(level, base, workspaceID, mcpServerExists)
}

// ValidateHookIf checks an if condition's shape (Claude Code parity):
// `ToolName(pattern)` — the name part follows the matcher's tool-entry rules
// (exact name or trailing-".*" family) and the pattern is a non-empty RE2
// regex within MaxHookPatternLength. Only tool events accept one.
func ValidateHookIf(event HookEvent, cond string) error {
	if cond == "" {
		return nil
	}
	if event != HookEventPreToolUse && event != HookEventPostToolUse {
		return fmt.Errorf("%w: if: only pre_tool_use and post_tool_use accept an if condition", ErrInvalid)
	}
	name, pattern, ok := SplitHookIf(cond)
	if !ok {
		return fmt.Errorf("%w: if: must be ToolName(pattern), got %q", ErrInvalid, cond)
	}
	if !hookToolEntryRegex.MatchString(name) {
		return fmt.Errorf("%w: if: tool name %q must be exact or a trailing-\".*\" family", ErrInvalid, name)
	}
	if pattern == "" {
		return fmt.Errorf("%w: if: pattern is required", ErrInvalid)
	}
	if len(pattern) > MaxHookPatternLength {
		return fmt.Errorf("%w: if: pattern exceeds %d characters", ErrInvalid, MaxHookPatternLength)
	}
	if _, err := regexp.Compile(pattern); err != nil {
		return fmt.Errorf("%w: if: pattern: %v", ErrInvalid, err)
	}
	return nil
}

// SplitHookIf splits an if condition at its first "(" requiring a ")"
// suffix, so patterns may contain parentheses. Only use after ValidateHookIf.
func SplitHookIf(cond string) (name, pattern string, ok bool) {
	name, rest, found := strings.Cut(cond, "(")
	if !found || !strings.HasSuffix(rest, ")") {
		return "", "", false
	}
	return name, strings.TrimSuffix(rest, ")"), true
}

// Validate checks the instance hook structurally: key, source, and version
// present, plus the shared definition rules. mcpServerExists is accepted for
// call-site uniformity with the other levels but never invoked — instance
// hooks cannot be checked against any single workspace's servers.
func (h *InstanceHook) Validate(mcpServerExists MCPServerExistsFunc) error {
	if h == nil {
		return ErrInvalid
	}
	if strings.TrimSpace(h.Key) == "" {
		return fmt.Errorf("%w: key: cannot be empty", ErrInvalid)
	}
	switch h.Source {
	case HookSourceBuiltin, HookSourceManaged:
	default:
		return fmt.Errorf("%w: source: unknown source %q", ErrInvalid, h.Source)
	}
	if h.Version < 1 {
		return fmt.Errorf("%w: version: must be a positive integer", ErrInvalid)
	}
	return ValidateHook(HookLevelInstance, &h.HookBase, "", mcpServerExists)
}

// Validate checks the workspace hook structurally: workspace scope present,
// plus the shared definition rules. mcpServerExists may be nil to skip the
// workspace-server existence check for mcp_tool hooks.
func (h *WorkspaceHook) Validate(mcpServerExists MCPServerExistsFunc) error {
	if h == nil {
		return ErrInvalid
	}
	if h.WorkspaceID == "" {
		return fmt.Errorf("%w: workspace id cannot be empty", ErrInvalid)
	}
	return ValidateHook(HookLevelWorkspace, &h.HookBase, h.WorkspaceID, mcpServerExists)
}

// Validate checks the agent hook structurally: workspace and agent scope
// present, plus the shared definition rules. mcpServerExists may be nil to
// skip the workspace-server existence check for mcp_tool hooks.
func (h *AgentHook) Validate(mcpServerExists MCPServerExistsFunc) error {
	if h == nil {
		return ErrInvalid
	}
	if h.WorkspaceID == "" {
		return fmt.Errorf("%w: workspace id cannot be empty", ErrInvalid)
	}
	if h.AgentID == "" {
		return fmt.Errorf("%w: agent id cannot be empty", ErrInvalid)
	}
	return ValidateHook(HookLevelAgent, &h.HookBase, h.WorkspaceID, mcpServerExists)
}

func validateHookEvent(event HookEvent) error {
	for _, e := range HookEvents {
		if e == event {
			return nil
		}
	}
	return fmt.Errorf("%w: event: unknown event %q", ErrInvalid, event)
}

func validateHookHandlerType(handlerType HookHandlerType) error {
	for _, t := range HookHandlerTypes {
		if t == handlerType {
			return nil
		}
	}
	return fmt.Errorf("%w: handler_type: unknown handler type %q", ErrInvalid, handlerType)
}

func validateHookFailurePolicy(policy HookFailurePolicy) error {
	switch policy {
	case "", HookFailureAllow, HookFailureBlock: // empty defaults to allow
		return nil
	default:
		return fmt.Errorf("%w: on_failure: unknown failure policy %q", ErrInvalid, policy)
	}
}

var (
	hookToolEntryRegex  = regexp.MustCompile(`^[A-Za-z0-9_.-]+(\.\*)?$`)
	hookEnvNameRegex    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	hookHeaderNameRegex = regexp.MustCompile(`^[A-Za-z0-9-]+$`)
)

// validateHookMatcher reads the matcher string with Claude Code's tiered
// interpretation (D19), adapted for dotted tool names: empty or "*" is
// match-all; otherwise the string splits on ",", "|", and whitespace, and if
// EVERY entry fits the tool-entry charset the matcher is a LIST of exact
// names and trailing-".*" families (dots are legal inside exact names, so
// "web.search" stays exact — family semantics are enforced at runtime, not
// here; domain validates charset only). Any other character puts the WHOLE
// unsplit string into the regex tier: unanchored RE2 within
// MaxHookPatternLength.
func validateHookMatcher(matcher string) error {
	if matcher == "" || matcher == "*" {
		return nil // match-all
	}
	for _, entry := range splitHookMatcherEntries(matcher) {
		if !hookToolEntryRegex.MatchString(entry) {
			return validateHookMatcherRegex(matcher)
		}
	}
	return nil // LIST tier: every entry is charset-valid
}

// validateHookMatcherRegex checks the regex tier: the whole matcher string as
// an unanchored RE2 regex within MaxHookPatternLength (D19).
func validateHookMatcherRegex(matcher string) error {
	if len(matcher) > MaxHookPatternLength {
		return fmt.Errorf("%w: matcher: exceeds maximum length of %d", ErrInvalid, MaxHookPatternLength)
	}
	if _, err := regexp.Compile(matcher); err != nil {
		return fmt.Errorf("%w: matcher: %v", ErrInvalid, err)
	}
	return nil
}

// splitHookMatcherEntries splits a matcher into its list-tier entries on
// ",", "|", and whitespace, collapsing repeated separators (Claude Code's
// list syntax). Runtime matching re-splits on its own; this is validation's
// reading of the tier boundary.
func splitHookMatcherEntries(matcher string) []string {
	return strings.FieldsFunc(matcher, func(r rune) bool {
		return r == ',' || r == '|' || unicode.IsSpace(r)
	})
}

// validateHookConfig validates the config object against the configured
// handler type. Structural only: command PATH lookup, SSRF guards, secret
// envelopes, and match-count reporting are NOT domain validation.
func validateHookConfig(level HookLevel, base *HookBase, workspaceID string, mcpServerExists MCPServerExistsFunc) error {
	fields, err := decodeHookConfig(base.Config)
	if err != nil {
		return err
	}
	switch base.HandlerType {
	case HookHandlerHTTP:
		return validateHTTPHookConfig(fields)
	case HookHandlerCommand:
		return validateCommandHookConfig(fields)
	case HookHandlerMCPTool:
		return validateMCPToolHookConfig(level, fields, workspaceID, mcpServerExists)
	case HookHandlerPrompt:
		return validatePromptHookConfig(base.Matcher, fields)
	case HookHandlerScript:
		return validateScriptHookConfig(fields)
	default:
		// Unreachable while the handler-type whitelist gates this switch;
		// new handler types must extend both that whitelist and here.
		return nil
	}
}

// decodeHookConfig decodes config into its top-level fields. Empty, null, or
// missing config yields a nil map; any non-empty non-object value is an error
// — config for every handler type must be a JSON object.
func decodeHookConfig(config json.RawMessage) (map[string]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(config)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return nil, fmt.Errorf("%w: config: must be a JSON object", ErrInvalid)
	}
	return fields, nil
}

// configString reads an optional string field; present distinguishes an
// absent key from an empty string.
func configString(fields map[string]json.RawMessage, key string) (value string, present bool, err error) {
	raw, ok := fields[key]
	if !ok {
		return "", false, nil
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", true, fmt.Errorf("%w: config.%s: must be a string", ErrInvalid, key)
	}
	return value, true, nil
}

func requireConfigString(fields map[string]json.RawMessage, key string) (string, error) {
	value, present, err := configString(fields, key)
	if err != nil {
		return "", err
	}
	if !present || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%w: config.%s: is required", ErrInvalid, key)
	}
	return value, nil
}

// hookConfigRow is one name-keyed row inside a hook config (command env, http
// headers). Values are validated as strings only; secret envelopes are
// applied by the settings layer, not here.
type hookConfigRow struct {
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
}

func decodeHookConfigRows(fields map[string]json.RawMessage, key string) ([]hookConfigRow, error) {
	raw, ok := fields[key]
	if !ok {
		return nil, nil
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	var rows []hookConfigRow
	if err := json.Unmarshal(trimmed, &rows); err != nil {
		return nil, fmt.Errorf("%w: config.%s: must be an array of {name, value} objects", ErrInvalid, key)
	}
	return rows, nil
}

func validateRowValueIsString(key, name string, value json.RawMessage) error {
	if len(bytes.TrimSpace(value)) == 0 {
		return nil
	}
	var s string
	if err := json.Unmarshal(value, &s); err != nil {
		return fmt.Errorf("%w: config.%s: value for %q must be a string", ErrInvalid, key, name)
	}
	return nil
}

func validateHTTPHookConfig(fields map[string]json.RawMessage) error {
	rawURL, err := requireConfigString(fields, "url")
	if err != nil {
		return err
	}
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return fmt.Errorf("%w: config.url: %q is not a valid URL", ErrInvalid, rawURL)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return fmt.Errorf("%w: config.url: scheme must be http or https", ErrInvalid)
	}
	rows, err := decodeHookConfigRows(fields, "headers")
	if err != nil {
		return err
	}
	for _, row := range rows {
		if !hookHeaderNameRegex.MatchString(row.Name) {
			return fmt.Errorf("%w: config.headers: name %q must match ^[A-Za-z0-9-]+$", ErrInvalid, row.Name)
		}
		if err := validateRowValueIsString("headers", row.Name, row.Value); err != nil {
			return err
		}
	}
	return nil
}

func validateCommandHookConfig(fields map[string]json.RawMessage) error {
	if _, err := requireConfigString(fields, "command"); err != nil {
		return err
	}
	if raw, ok := fields["args"]; ok {
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) {
			var args []string
			if err := json.Unmarshal(trimmed, &args); err != nil {
				return fmt.Errorf("%w: config.args: must be an array of strings", ErrInvalid)
			}
		}
	}
	rows, err := decodeHookConfigRows(fields, "env")
	if err != nil {
		return err
	}
	for _, row := range rows {
		if !hookEnvNameRegex.MatchString(row.Name) {
			return fmt.Errorf("%w: config.env: name %q must match ^[A-Za-z_][A-Za-z0-9_]*$", ErrInvalid, row.Name)
		}
		if err := validateRowValueIsString("env", row.Name, row.Value); err != nil {
			return err
		}
	}
	return nil
}

func validateMCPToolHookConfig(level HookLevel, fields map[string]json.RawMessage, workspaceID string, mcpServerExists MCPServerExistsFunc) error {
	// Config keys follow Claude Code vocabulary (D21): "server" and "tool".
	server, err := requireConfigString(fields, "server")
	if err != nil {
		return err
	}
	if _, err := requireConfigString(fields, "tool"); err != nil {
		return err
	}
	if level == HookLevelInstance || mcpServerExists == nil {
		return nil
	}
	exists, err := mcpServerExists(workspaceID, server)
	if err != nil {
		return fmt.Errorf("mcp server lookup: %w", err)
	}
	if !exists {
		return fmt.Errorf("%w: config.server: mcp server %q not found in workspace", ErrInvalid, server)
	}
	return nil
}

// validateScriptHookConfig validates the script handler's config (D22): the
// JavaScript source is the one required field, capped at MaxHookScriptBytes.
// Script hooks are EXEMPT from the prompt handler's non-match-all matcher
// rule — in-process evaluation is µs-cheap, so no matcher requirement exists
// for this handler type at all. Syntax checking is NOT domain validation; the
// save-time helper in internal/agents/hooks compiles the source separately.
func validateScriptHookConfig(fields map[string]json.RawMessage) error {
	script, err := requireConfigString(fields, "script")
	if err != nil {
		return err
	}
	if len(script) > MaxHookScriptBytes {
		return fmt.Errorf("%w: config.script: %d bytes exceeds maximum of %d", ErrInvalid, len(script), MaxHookScriptBytes)
	}
	return nil
}

func validatePromptHookConfig(matcher string, fields map[string]json.RawMessage) error {
	// A match-all prompt hook is an LLM call on every occurrence — a cost
	// incident (D12, D19). Require a narrowing matcher: non-empty and not
	// "*"; any other string selects at least one tier by construction.
	if matcher == "" || matcher == "*" {
		return fmt.Errorf("%w: matcher: prompt hooks require a non-match-all matcher (must not be empty or \"*\")", ErrInvalid)
	}
	if _, err := requireConfigString(fields, "provider"); err != nil {
		return err
	}
	if _, err := requireConfigString(fields, "model"); err != nil {
		return err
	}
	raw, ok := fields["max_invocations_per_run"]
	if !ok {
		return nil // absent = default cap, fine
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	var decoded any
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	if err := dec.Decode(&decoded); err != nil {
		return fmt.Errorf("%w: config.max_invocations_per_run: must be a positive integer", ErrInvalid)
	}
	number, isNumber := decoded.(json.Number)
	if !isNumber {
		return fmt.Errorf("%w: config.max_invocations_per_run: must be a positive integer", ErrInvalid)
	}
	limit, err := number.Int64()
	if err != nil || limit <= 0 {
		return fmt.Errorf("%w: config.max_invocations_per_run: must be a positive integer", ErrInvalid)
	}
	return nil
}
