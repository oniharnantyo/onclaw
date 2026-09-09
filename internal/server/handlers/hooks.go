package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	agenthooks "github.com/oniharnantyo/onclaw/internal/agents/hooks"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// DefaultHookExecutionsLimit is the page size for the per-hook execution
// history when the request carries no explicit limit.
const DefaultHookExecutionsLimit = 50

// ---------------------------------------------------------------------------
// Payload shapes
// ---------------------------------------------------------------------------

// hookRequest is the create payload shared by the workspace and agent levels:
// {name, event, matcher, if?, handler_type, config, timeout_ms?, on_failure?,
// enabled?}. matcher is Claude Code's single-string form — "" or "*" is
// match-all, "a|b" a list of exact/trailing-".*" entries, anything else an
// unanchored RE2 regex — and if ("ToolName(pattern)") narrows tool events by
// serialized input. Secret row values inside config (http headers, command
// env) arrive as plaintext or as the stored hint placeholder; they are sealed
// with the workspace ID as AAD before any store write.
type hookRequest struct {
	Name        string                   `json:"name"`
	Event       domain.HookEvent         `json:"event"`
	Matcher     string                   `json:"matcher"`
	If          string                   `json:"if,omitempty"`
	HandlerType domain.HookHandlerType   `json:"handler_type"`
	Config      json.RawMessage          `json:"config,omitempty"`
	TimeoutMS   int                      `json:"timeout_ms,omitempty"`
	OnFailure   domain.HookFailurePolicy `json:"on_failure,omitempty"`
	Enabled     *bool                    `json:"enabled,omitempty"`
}

// hookPatchRequest is the update payload. Every field is a pointer so an
// omitted field keeps the stored value (the MCP server PATCH convention);
// config overlays wholesale with the keep-stored secret merge below. matcher
// and if are strings like the create payload; an explicit "" matcher is
// match-all and an explicit "" if clears the condition.
type hookPatchRequest struct {
	Name        *string                   `json:"name,omitempty"`
	Event       *domain.HookEvent         `json:"event,omitempty"`
	Matcher     *string                   `json:"matcher,omitempty"`
	If          *string                   `json:"if,omitempty"`
	HandlerType *domain.HookHandlerType   `json:"handler_type,omitempty"`
	Config      *json.RawMessage          `json:"config,omitempty"`
	TimeoutMS   *int                      `json:"timeout_ms,omitempty"`
	OnFailure   *domain.HookFailurePolicy `json:"on_failure,omitempty"`
	Enabled     *bool                     `json:"enabled,omitempty"`
}

// hookReorderRequest is the reorder payload: the level's hook ids in their new
// list order (D14 — the list order IS the execution order).
type hookReorderRequest struct {
	IDs []string `json:"ids"`
}

// hookTestOverrides carries the synthetic event knobs for the dry-run
// endpoint. They never persist; event overrides the hook's own event only for
// the simulated delivery. tool_name/tool_args populate the synthetic tool
// invocation the handler (and an `if` condition, when the runtime evaluates
// one) sees — they are event data, not matcher input.
type hookTestOverrides struct {
	Event    *domain.HookEvent `json:"event,omitempty"`
	ToolName string            `json:"tool_name,omitempty"`
	ToolArgs string            `json:"tool_args,omitempty"`
	Origin   string            `json:"origin,omitempty"`
	Status   string            `json:"status,omitempty"`
}

// hookTestRequest is the dry-run payload: a full hook definition (saved or
// unsaved) plus the optional overrides object.
type hookTestRequest struct {
	ID string `json:"id,omitempty"`
	hookRequest
	Overrides hookTestOverrides `json:"overrides,omitempty"`
}

// adminHookRequest is the instance-admin create payload: managed instance
// hooks additionally carry the stable key. source and version are server-
// owned (managed, 1).
type adminHookRequest struct {
	Key string `json:"key"`
	hookRequest
}

// ---------------------------------------------------------------------------
// View shapes
// ---------------------------------------------------------------------------

// hookView is the read view of a hook at any level. Secret row values inside
// config are replaced by their last-4 hint (never plaintext, never
// ciphertext); the instance-only fields stay empty below the instance tier.
// The masked config round-trips: echoing the read shape back on PATCH keeps
// the stored secrets (see mergeHookConfigSecrets).
type hookView struct {
	ID          string                   `json:"id"`
	WorkspaceID string                   `json:"workspace_id,omitempty"`
	AgentID     string                   `json:"agent_id,omitempty"`
	Key         string                   `json:"key,omitempty"`
	Source      domain.HookSource        `json:"source,omitempty"`
	Version     int                      `json:"version,omitempty"`
	Name        string                   `json:"name"`
	Event       domain.HookEvent         `json:"event"`
	Matcher     string                   `json:"matcher"`
	If          string                   `json:"if,omitempty"`
	HandlerType domain.HookHandlerType   `json:"handler_type"`
	Config      json.RawMessage          `json:"config,omitempty"`
	TimeoutMS   int                      `json:"timeout_ms"`
	OnFailure   domain.HookFailurePolicy `json:"on_failure"`
	Enabled     bool                     `json:"enabled"`
	Position    int                      `json:"position"`
	Status      domain.HookStatus        `json:"status"`
	StatusError string                   `json:"status_error,omitempty"`
	CreatedAt   time.Time                `json:"created_at"`
	UpdatedAt   time.Time                `json:"updated_at"`
}

// hookMatchCountView is the save-time matcher report (D8): how many of the
// event's currently available values the matcher selects.
type hookMatchCountView struct {
	Matched int `json:"matched"`
	Of      int `json:"of"`
}

// newHookView builds the masked read view of any hook base. createdAt and
// updatedAt come from the level struct (HookBase itself carries none).
func newHookView(id, workspaceID, agentID, key string, source domain.HookSource, version int, base *domain.HookBase, createdAt, updatedAt time.Time, encKey []byte, aad []byte) hookView {
	return hookView{
		ID:          id,
		WorkspaceID: workspaceID,
		AgentID:     agentID,
		Key:         key,
		Source:      source,
		Version:     version,
		Name:        base.Name,
		Event:       base.Event,
		Matcher:     base.Matcher,
		If:          base.If,
		HandlerType: base.HandlerType,
		Config:      maskHookConfig(encKey, aad, base.Config),
		TimeoutMS:   base.TimeoutMS,
		OnFailure:   base.OnFailure,
		Enabled:     base.Enabled,
		Position:    base.Position,
		Status:      base.Status,
		StatusError: base.StatusError,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}
}

// ---------------------------------------------------------------------------
// Secret helpers (mirror internal/agents/secretrows.go, hook shapes)
// ---------------------------------------------------------------------------

// hookSecretPaths are the config paths holding name-keyed secret rows: the
// http handler's headers and the command handler's env (pinned shapes in
// internal/agents/hooks/secrets.go).
var hookSecretPaths = []string{"headers", "env"}

// hookConfigRowRaw is one name-keyed row inside a secret path; the value stays
// raw so non-string values round-trip byte-for-byte.
type hookConfigRowRaw struct {
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
}

// decodeHookConfigRowsRaw decodes cfg's object fields; ok=false when cfg is
// not a JSON object (handler-type validation owns that error).
func decodeHookConfigObject(cfg json.RawMessage) (map[string]json.RawMessage, bool) {
	trimmed := trimJSONSpace(cfg)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil, true
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return nil, false
	}
	return fields, true
}

func trimJSONSpace(raw json.RawMessage) []byte {
	return json.RawMessage(strings.TrimSpace(string(raw)))
}

// isHookSecretEnvelope reports whether a config value is already an encrypted
// envelope ("v1:...") rather than plaintext.
func isHookSecretEnvelope(value string) bool {
	return strings.HasPrefix(value, secrets.Version1Prefix+":")
}

// lastHookSecretChars returns the last n characters of a secret for the hint.
func lastHookSecretChars(value string, n int) string {
	if len(value) > n {
		return value[len(value)-n:]
	}
	return value
}

// hookSecretHint returns the client-safe last-4 hint for a stored secret row
// value: envelopes are decrypted to recover the hinted tail, undecryptable
// envelopes hint as empty rather than leaking, and plaintext rows (tests)
// hint from their own tail without the value ever being returned whole.
func hookSecretHint(encKey []byte, aad []byte, raw json.RawMessage) string {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	if value == "" {
		return ""
	}
	if isHookSecretEnvelope(value) {
		plaintext, err := secrets.Decrypt(encKey, aad, value)
		if err != nil {
			return ""
		}
		value = string(plaintext)
	}
	return lastHookSecretChars(value, 4)
}

// maskHookConfig returns the read-view config: every string secret row value
// at the known secret paths is replaced by its hint. All other fields pass
// through verbatim; a non-object config (handler validation's business)
// passes through untouched.
func maskHookConfig(encKey []byte, aad []byte, cfg json.RawMessage) json.RawMessage {
	fields, ok := decodeHookConfigObject(cfg)
	if !ok || fields == nil {
		return cfg
	}
	changed := false
	for _, path := range hookSecretPaths {
		raw, ok := fields[path]
		if !ok {
			continue
		}
		var rows []hookConfigRowRaw
		if err := json.Unmarshal(trimJSONSpace(raw), &rows); err != nil {
			continue
		}
		rowsChanged := false
		for i, row := range rows {
			if len(trimJSONSpace(row.Value)) == 0 {
				continue
			}
			hint := hookSecretHint(encKey, aad, row.Value)
			encoded, err := json.Marshal(hint)
			if err != nil {
				continue
			}
			rows[i].Value = encoded
			rowsChanged = true
		}
		if rowsChanged {
			if encoded, err := json.Marshal(rows); err == nil {
				fields[path] = encoded
				changed = true
			}
		}
	}
	if !changed {
		return cfg
	}
	out, err := json.Marshal(fields)
	if err != nil {
		return cfg
	}
	return out
}

// mergeHookConfigSecrets applies the keep-stored merge (D16) between the
// stored config (sealed envelopes) and the incoming request config: an
// incoming secret row whose value is empty OR equals the stored row's hint
// keeps the stored envelope verbatim; a different non-empty value is a
// replacement and is re-sealed. Rows are matched by name within each secret
// path. The result is sealed so it is store-ready.
func mergeHookConfigSecrets(encKey []byte, aadWorkspaceID string, stored, incoming json.RawMessage) (json.RawMessage, error) {
	if len(trimJSONSpace(incoming)) == 0 {
		return incoming, nil
	}
	aad := []byte(aadWorkspaceID)
	storedFields, ok := decodeHookConfigObject(stored)
	if !ok {
		storedFields = nil
	}
	storedRows := make(map[string]map[string]json.RawMessage)
	for _, path := range hookSecretPaths {
		raw, ok := storedFields[path]
		if !ok {
			continue
		}
		var rows []hookConfigRowRaw
		if err := json.Unmarshal(trimJSONSpace(raw), &rows); err != nil {
			continue
		}
		byName := make(map[string]json.RawMessage, len(rows))
		for _, row := range rows {
			byName[row.Name] = row.Value
		}
		storedRows[path] = byName
	}

	fields, ok := decodeHookConfigObject(incoming)
	if !ok {
		return nil, fmt.Errorf("%w: config: must be a JSON object", domain.ErrInvalid)
	}
	if fields == nil {
		return incoming, nil
	}
	changed := false
	for _, path := range hookSecretPaths {
		raw, ok := fields[path]
		if !ok {
			continue
		}
		var rows []hookConfigRowRaw
		if err := json.Unmarshal(trimJSONSpace(raw), &rows); err != nil {
			continue // handler-type validation owns the shape error
		}
		rowsChanged := false
		for i, row := range rows {
			var value string
			if err := json.Unmarshal(trimJSONSpace(row.Value), &value); err != nil {
				continue // non-string value: verbatim
			}
			storedValue, known := storedRows[path][row.Name]
			if !known {
				continue // new row: seal as a fresh secret
			}
			if value != "" && value != hookSecretHint(encKey, aad, storedValue) {
				continue // replaced value: seal as a fresh secret
			}
			// Empty or hint-echoed value: keep the stored envelope verbatim.
			rows[i].Value = storedValue
			rowsChanged = true
		}
		if rowsChanged {
			encoded, err := json.Marshal(rows)
			if err != nil {
				return nil, fmt.Errorf("encode hook config rows: %w", err)
			}
			fields[path] = encoded
			changed = true
		}
	}
	merged := incoming
	if changed {
		encoded, err := json.Marshal(fields)
		if err != nil {
			return nil, fmt.Errorf("encode hook config: %w", err)
		}
		merged = encoded
	}
	sealed, err := agenthooks.SealHookConfig(encKey, aadWorkspaceID, merged)
	if err != nil {
		return nil, err
	}
	return sealed, nil
}

// sealHookConfig seals an incoming create config's plaintext secret rows with
// the workspace ID as AAD.
func sealHookConfig(encKey []byte, aadWorkspaceID string, cfg json.RawMessage) (json.RawMessage, error) {
	return agenthooks.SealHookConfig(encKey, aadWorkspaceID, cfg)
}

// ---------------------------------------------------------------------------
// Validation helpers
// ---------------------------------------------------------------------------

// hookValidationDetails converts a domain hook validation error into
// field-level details. Domain errors are formatted "invalid request:
// <field>: <problem>"; the leading sentinel text and the first field segment
// are split off so the 422 details[] name the offending field (the mcp.go /
// tools.go envelope convention).
func hookValidationDetails(err error) (string, []ErrorDetail) {
	message := err.Error()
	prefix := domain.ErrInvalid.Error() + ": "
	if strings.HasPrefix(message, prefix) {
		message = strings.TrimPrefix(message, prefix)
	}
	field := ""
	if idx := strings.Index(message, ": "); idx > 0 {
		candidate := message[:idx]
		// Field paths are dotted identifiers without whitespace ("name",
		// "matcher.pattern", "config.max_invocations_per_run"); prose
		// segments ("mcp server lookup") stay in the message with no field.
		if candidate != "" && !strings.ContainsAny(candidate, " \t\n") {
			field = candidate
			message = message[idx+2:]
		}
	}
	return err.Error(), []ErrorDetail{{Field: field, Message: message}}
}

// respondHookValidationError writes the 422 field-level envelope.
func respondHookValidationError(c *gin.Context, err error) {
	message, details := hookValidationDetails(err)
	AbortWithError(c, http.StatusUnprocessableEntity, CodeInvalidRequest, message, details...)
}

// errCommandHookDisabled builds the fielded error for a command hook saved
// while the instance kill switch (ONCLAW_HOOKS_COMMAND_ENABLED) is off.
func errCommandHookDisabled() error {
	return fmt.Errorf("%w: handler_type: command hooks are disabled on this instance", domain.ErrInvalid)
}

// validateScriptHookSource compiles a script hook's JavaScript WITHOUT
// executing it (D22): a syntax error surfaces as a fielded 422 naming
// config.script and the first error's line/column in editor coordinates. It
// runs after domain validation, which guarantees config.script is a present,
// size-capped string, and sealing never touches config.script (only the
// headers/env rows are secret paths), so the source is readable here.
func validateScriptHookSource(cfg json.RawMessage) error {
	var conf struct {
		Script string `json:"script"`
	}
	if err := json.Unmarshal(trimJSONSpace(cfg), &conf); err != nil {
		return nil // unreachable: domain validation owns the config-shape error
	}
	return agenthooks.ValidateScript(conf.Script)
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// hookHandlers serves the agent lifecycle-hook REST surface (D13/D17): the
// workspace and agent levels under the workspace scope, and the instance
// admin surface for managed rows. Reads require hooks.read and writes
// hooks.write (enforced by the router's permission guards); instance routes
// additionally sit behind the master-tenant middleware.
type hookHandlers struct {
	hooks          store.HookStore
	agents         store.AgentStore
	mcpServers     store.WorkspaceMCPServers
	registry       *agenthooks.Registry
	toolValues     agenthooks.ToolValueSource
	encKey         []byte
	commandEnabled bool
}

// NewHookHandlers creates a new hookHandlers instance. registry is the shared
// handler registry (also backing the runtime dispatcher); toolValues feeds
// the save-time match count; commandEnabled mirrors the registry's kill
// switch so save validation rejects command hooks the runtime would skip.
func NewHookHandlers(hooks store.HookStore, agents store.AgentStore, mcpServers store.WorkspaceMCPServers, registry *agenthooks.Registry, toolValues agenthooks.ToolValueSource, encKey []byte, commandEnabled bool) *hookHandlers {
	return &hookHandlers{
		hooks:          hooks,
		agents:         agents,
		mcpServers:     mcpServers,
		registry:       registry,
		toolValues:     toolValues,
		encKey:         encKey,
		commandEnabled: commandEnabled,
	}
}

// mcpServerExistsFunc wires the workspace MCP store into domain hook
// validation (workspace-level servers only; agent-private servers are
// unreachable from workspace and agent hooks alike).
func (h *hookHandlers) mcpServerExistsFunc(ctx context.Context) domain.MCPServerExistsFunc {
	return func(workspaceID, serverID string) (bool, error) {
		row, err := h.mcpServers.Get(ctx, workspaceID, serverID)
		if err != nil {
			return false, err
		}
		return row != nil, nil
	}
}

// validateDefinition runs domain validation for the level plus the
// command-kill-switch rule, returning the raw error for the 422 shaping.
func (h *hookHandlers) validateDefinition(ctx context.Context, level domain.HookLevel, workspaceID string, base *domain.HookBase) error {
	var exists domain.MCPServerExistsFunc
	if level != domain.HookLevelInstance {
		exists = h.mcpServerExistsFunc(ctx)
	}
	if err := domain.ValidateHook(level, base, workspaceID, exists); err != nil {
		return err
	}
	if base.HandlerType == domain.HookHandlerCommand && !h.commandEnabled {
		return errCommandHookDisabled()
	}
	if base.HandlerType == domain.HookHandlerScript {
		if err := validateScriptHookSource(base.Config); err != nil {
			return err
		}
	}
	return nil
}

// countMatches reports the save-time matcher selection, tolerating a failing
// value source (the count is advisory; validation already passed).
func (h *hookHandlers) countMatches(ctx context.Context, base *domain.HookBase, workspaceID string) (int, int, error) {
	return agenthooks.CountMatches(ctx, base.Event, h.toolValues, workspaceID, base.Matcher)
}

// buildHookBase applies the request defaults (D8/D9/D12/D19): the matcher and
// if strings pass through verbatim (an absent matcher is match-all — "A hook
// with no matcher SHALL apply to all occurrences"), timeout 5s (15s for
// prompt evaluators), failure policy allow, enabled true.
func buildHookBase(req *hookRequest) domain.HookBase {
	base := domain.HookBase{
		Name:        strings.TrimSpace(req.Name),
		Event:       req.Event,
		Matcher:     req.Matcher,
		If:          req.If,
		HandlerType: req.HandlerType,
		Config:      req.Config,
		TimeoutMS:   req.TimeoutMS,
		OnFailure:   req.OnFailure,
		Enabled:     true,
	}
	if base.TimeoutMS <= 0 {
		if base.HandlerType == domain.HookHandlerPrompt {
			base.TimeoutMS = domain.DefaultPromptHookTimeoutMS
		} else {
			base.TimeoutMS = domain.DefaultHookTimeoutMS
		}
	}
	if base.OnFailure == "" {
		base.OnFailure = domain.HookFailureAllow
	}
	if req.Enabled != nil {
		base.Enabled = *req.Enabled
	}
	return base
}

// applyHookPatch overlays the request's present fields onto the stored base
// and reports whether anything was requested (the "no fields to update" 400
// guard mirrors the MCP handlers). matcher and if are plain strings: an
// explicit "" matcher is match-all and an explicit "" if clears the
// condition, so pointer presence alone decides the overlay.
func applyHookPatch(base *domain.HookBase, req *hookPatchRequest) bool {
	changed := false
	if req.Name != nil && strings.TrimSpace(*req.Name) != "" && strings.TrimSpace(*req.Name) != base.Name {
		base.Name = strings.TrimSpace(*req.Name)
		changed = true
	}
	if req.Event != nil && *req.Event != base.Event {
		base.Event = *req.Event
		changed = true
	}
	if req.Matcher != nil && *req.Matcher != base.Matcher {
		base.Matcher = *req.Matcher
		changed = true
	}
	if req.If != nil && *req.If != base.If {
		base.If = *req.If
		changed = true
	}
	if req.HandlerType != nil && *req.HandlerType != base.HandlerType {
		base.HandlerType = *req.HandlerType
		changed = true
	}
	// Config is NOT overlaid here: the callers must first run the keep-stored
	// secret merge against the STORED config, which this function must not
	// clobber. They assign the merged result to base.Config themselves.
	if req.Config != nil {
		changed = true
	}
	if req.TimeoutMS != nil && *req.TimeoutMS > 0 && *req.TimeoutMS != base.TimeoutMS {
		base.TimeoutMS = *req.TimeoutMS
		changed = true
	}
	if req.OnFailure != nil && *req.OnFailure != "" && *req.OnFailure != base.OnFailure {
		base.OnFailure = *req.OnFailure
		changed = true
	}
	if req.Enabled != nil && *req.Enabled != base.Enabled {
		base.Enabled = *req.Enabled
		changed = true
	}
	return changed
}

func bindHookRequest(c *gin.Context) (*hookRequest, bool) {
	var req hookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return nil, false
	}
	return &req, true
}

func bindHookPatchRequest(c *gin.Context) (*hookPatchRequest, bool) {
	var req hookPatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return nil, false
	}
	return &req, true
}

// newHookDeliveryID mints the synthetic dry-run delivery id.
func newHookDeliveryID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("hook-test-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}

// respondWithMatchCount writes the save response: the hook view plus the
// advisory match-count report ({"matched": N, "of": M}, D8).
func respondWithMatchCount(c *gin.Context, status int, view hookView, matched, total int, countErr error) {
	body := gin.H{"hook": view}
	if countErr != nil {
		body["match_count"] = nil
	} else {
		body["match_count"] = hookMatchCountView{Matched: matched, Of: total}
	}
	RespondJSON(c, status, body)
}

// -------------------------------------------------------------------------
// Workspace level (hooks.read for reads; hooks.write for writes)
// -------------------------------------------------------------------------

// ListWorkspaceHooks returns the read-only instance section (D13: mandatory
// visibility, no control from below) plus the workspace's own hooks.
func (h *hookHandlers) ListWorkspaceHooks(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	ctx := c.Request.Context()

	instanceRows, err := h.hooks.ListInstanceHooks(ctx)
	if err != nil {
		RespondError(c, err)
		return
	}
	instance := make([]hookView, 0, len(instanceRows))
	for i := range instanceRows {
		row := &instanceRows[i]
		instance = append(instance, newHookView(row.ID, "", "", row.Key, row.Source, row.Version, &row.HookBase, row.CreatedAt, row.UpdatedAt, h.encKey, nil))
	}

	rows, err := h.hooks.ListWorkspaceHooks(ctx, ws.ID)
	if err != nil {
		RespondError(c, err)
		return
	}
	items := make([]hookView, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		items = append(items, newHookView(row.ID, row.WorkspaceID, "", "", "", 0, &row.HookBase, row.CreatedAt, row.UpdatedAt, h.encKey, []byte(row.WorkspaceID)))
	}
	RespondOK(c, gin.H{"instance": instance, "hooks": items})
}

// CreateWorkspaceHook validates, seals, counts, and persists a workspace hook.
func (h *hookHandlers) CreateWorkspaceHook(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	req, ok := bindHookRequest(c)
	if !ok {
		return
	}

	base := buildHookBase(req)
	sealed, err := sealHookConfig(h.encKey, ws.ID, base.Config)
	if err != nil {
		RespondError(c, err)
		return
	}
	base.Config = sealed
	if err := h.validateDefinition(c.Request.Context(), domain.HookLevelWorkspace, ws.ID, &base); err != nil {
		respondHookValidationError(c, err)
		return
	}

	hook := &domain.WorkspaceHook{WorkspaceID: ws.ID, HookBase: base}
	if err := h.hooks.CreateWorkspaceHook(c.Request.Context(), hook); err != nil {
		RespondError(c, err)
		return
	}
	matched, total, countErr := h.countMatches(c.Request.Context(), &hook.HookBase, ws.ID)
	respondWithMatchCount(c, http.StatusCreated, newHookView(hook.ID, hook.WorkspaceID, "", "", "", 0, &hook.HookBase, hook.CreatedAt, hook.UpdatedAt, h.encKey, []byte(ws.ID)), matched, total, countErr)
}

// PatchWorkspaceHook updates a workspace hook with the keep-stored secret
// merge and a fresh match count.
func (h *hookHandlers) PatchWorkspaceHook(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	id := c.Param("id")
	req, ok := bindHookPatchRequest(c)
	if !ok {
		return
	}

	ctx := c.Request.Context()
	row, err := h.hooks.GetWorkspaceHook(ctx, ws.ID, id)
	if err != nil {
		RespondError(c, err)
		return
	}
	if row == nil {
		RespondError(c, domain.ErrNotFound)
		return
	}

	if !applyHookPatch(&row.HookBase, req) {
		RespondError(c, fmt.Errorf("%w: no fields to update", domain.ErrInvalid))
		return
	}
	if req.Config != nil {
		merged, err := mergeHookConfigSecrets(h.encKey, ws.ID, row.Config, *req.Config)
		if err != nil {
			respondHookValidationError(c, err)
			return
		}
		row.Config = merged
	}
	if err := h.validateDefinition(ctx, domain.HookLevelWorkspace, ws.ID, &row.HookBase); err != nil {
		respondHookValidationError(c, err)
		return
	}
	if err := h.hooks.UpdateWorkspaceHook(ctx, row); err != nil {
		RespondError(c, err)
		return
	}
	matched, total, countErr := h.countMatches(ctx, &row.HookBase, ws.ID)
	respondWithMatchCount(c, http.StatusOK, newHookView(row.ID, row.WorkspaceID, "", "", "", 0, &row.HookBase, row.CreatedAt, row.UpdatedAt, h.encKey, []byte(ws.ID)), matched, total, countErr)
}

// DeleteWorkspaceHook removes a workspace hook; its execution history
// survives (D16).
func (h *hookHandlers) DeleteWorkspaceHook(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	id := c.Param("id")

	if err := h.hooks.DeleteWorkspaceHook(c.Request.Context(), ws.ID, id); err != nil {
		RespondError(c, err)
		return
	}
	RespondNoContent(c)
}

// ReorderWorkspaceHooks sets the workspace level's list order (D14).
func (h *hookHandlers) ReorderWorkspaceHooks(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	var req hookReorderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}
	if err := h.hooks.RepositionWorkspaceHooks(c.Request.Context(), ws.ID, req.IDs); err != nil {
		RespondError(c, err)
		return
	}
	RespondNoContent(c)
}

// ListWorkspaceHookExecutions returns execution history. Two forms share the
// handler: `/hooks/executions` lists the workspace's whole audit trail and
// `/hooks/:id/executions` scopes to one hook. History survives hook deletion
// (D16): the hook-id filter matches detached (NULL hook_id) rows never —
// deleted hooks' records surface through the workspace-wide form, which is
// also why an unknown id is an empty list, not a 404.
func (h *hookHandlers) ListWorkspaceHookExecutions(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	limit := DefaultHookExecutionsLimit
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			RespondError(c, fmt.Errorf("%w: limit: must be an integer", domain.ErrInvalid))
			return
		}
		limit = parsed
	}
	var hookID *string
	if id := c.Param("id"); id != "" {
		hookID = &id
	}
	execs, err := h.hooks.ListHookExecutions(c.Request.Context(), ws.ID, hookID, limit)
	if err != nil {
		RespondError(c, err)
		return
	}
	if execs == nil {
		execs = []domain.HookExecution{}
	}
	RespondOK(c, gin.H{"executions": execs})
}

// TestWorkspaceHook performs the dry run (D18): a REAL handler execution of a
// synthetic event with a unique delivery id and the payload's timeout budget,
// returning the decision, duration, and handler-specific detail. No audit row
// is written and no hook is persisted.
func (h *hookHandlers) TestWorkspaceHook(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	var req hookTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	base := buildHookBase(&req.hookRequest)
	if req.ID != "" {
		if stored, err := h.hooks.GetWorkspaceHook(c.Request.Context(), ws.ID, req.ID); err == nil && stored != nil {
			if merged, err := mergeHookConfigSecrets(h.encKey, ws.ID, stored.Config, base.Config); err == nil {
				base.Config = merged
			}
		}
	} else {
		sealed, err := sealHookConfig(h.encKey, ws.ID, base.Config)
		if err != nil {
			RespondError(c, err)
			return
		}
		base.Config = sealed
	}
	if err := h.validateDefinition(c.Request.Context(), domain.HookLevelWorkspace, ws.ID, &base); err != nil {
		respondHookValidationError(c, err)
		return
	}

	event := base.Event
	if req.Overrides.Event != nil {
		event = *req.Overrides.Event
	}
	origin := req.Overrides.Origin
	if origin == "" {
		origin = "user"
	}
	status := req.Overrides.Status
	if status == "" {
		status = "completed"
	}
	ev := agenthooks.Event{
		Event:      string(event),
		DeliveryID: newHookDeliveryID(),
		Origin:     origin,
		Status:     status,
		Workspace:  agenthooks.EventRef{ID: ws.ID, Name: ws.Name},
		Agent:      agenthooks.EventRef{Name: "hook-test"},
		SessionID:  "hook-test",
	}
	switch event {
	case domain.HookEventPreToolUse, domain.HookEventPostToolUse:
		ev.Tool = &agenthooks.EventTool{Name: req.Overrides.ToolName, CallID: "hook-test", Args: req.Overrides.ToolArgs}
	}

	started := time.Now()
	result, execErr := h.registry.Execute(c.Request.Context(), base.HandlerType, base.Config, ev, agenthooks.HookRef{Name: base.Name, Level: domain.HookLevelWorkspace}, time.Duration(base.TimeoutMS)*time.Millisecond)
	durationMS := time.Since(started).Milliseconds()

	// A handler type this binary cannot run (not yet registered, or unknown)
	// is a clean validation-style rejection, not a fake failure result.
	if execErr != nil && (errors.Is(execErr, agenthooks.ErrHandlerNotRegistered) || errors.Is(execErr, agenthooks.ErrUnknownHandlerType)) {
		respondHookValidationError(c, fmt.Errorf("%w: handler_type: %v", domain.ErrInvalid, execErr))
		return
	}

	detail := gin.H{}
	if result.ExitCode != nil {
		detail["exit_code"] = *result.ExitCode
	}
	if result.HTTPStatus != nil {
		detail["http_status"] = *result.HTTPStatus
	}
	if result.TokenCount != nil {
		detail["token_count"] = *result.TokenCount
	}
	// ConsoleLines is the script handler's captured console buffer (D22) —
	// nil for every other handler type, so the key is omitted there like the
	// detail fields above.
	if len(result.ConsoleLines) > 0 {
		detail["console_lines"] = result.ConsoleLines
	}

	body := gin.H{"duration_ms": durationMS, "detail": detail}
	if execErr != nil {
		body["decision"] = "failure"
		body["reason"] = result.Reason
		body["error"] = execErr.Error()
	} else {
		body["decision"] = result.Decision
		body["reason"] = result.Reason
	}
	RespondOK(c, body)
}

// -------------------------------------------------------------------------
// Agent level (hooks.read / hooks.write; private to the owning agent, D13)
// -------------------------------------------------------------------------

// resolveHookAgent resolves the agent addressed by the route (slug first, ID
// fallback — the mcpServerHandlers convention); unknown slugs and agents of
// another workspace are domain.ErrNotFound indistinguishably.
func (h *hookHandlers) resolveHookAgent(ctx context.Context, workspaceID, identifier string) (*domain.Agent, error) {
	if workspaceID == "" || identifier == "" {
		return nil, domain.ErrNotFound
	}
	agent, err := h.agents.BySlug(ctx, workspaceID, identifier)
	if err == nil {
		return agent, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	return h.agents.ByID(ctx, workspaceID, identifier)
}

// ListAgentLevelHooks returns the agent config modal's three sections (D13):
// read-only instance and workspace visibility plus the agent's private hooks.
func (h *hookHandlers) ListAgentLevelHooks(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	ctx := c.Request.Context()

	agent, err := h.resolveHookAgent(ctx, ws.ID, c.Param("agent"))
	if err != nil {
		RespondError(c, err)
		return
	}

	instanceRows, err := h.hooks.ListInstanceHooks(ctx)
	if err != nil {
		RespondError(c, err)
		return
	}
	instance := make([]hookView, 0, len(instanceRows))
	for i := range instanceRows {
		row := &instanceRows[i]
		instance = append(instance, newHookView(row.ID, "", "", row.Key, row.Source, row.Version, &row.HookBase, row.CreatedAt, row.UpdatedAt, h.encKey, nil))
	}

	wsRows, err := h.hooks.ListWorkspaceHooks(ctx, ws.ID)
	if err != nil {
		RespondError(c, err)
		return
	}
	workspace := make([]hookView, 0, len(wsRows))
	for i := range wsRows {
		row := &wsRows[i]
		workspace = append(workspace, newHookView(row.ID, row.WorkspaceID, "", "", "", 0, &row.HookBase, row.CreatedAt, row.UpdatedAt, h.encKey, []byte(row.WorkspaceID)))
	}

	agentRows, err := h.hooks.ListAgentHooks(ctx, ws.ID, agent.ID)
	if err != nil {
		RespondError(c, err)
		return
	}
	items := make([]hookView, 0, len(agentRows))
	for i := range agentRows {
		row := &agentRows[i]
		items = append(items, newHookView(row.ID, row.WorkspaceID, row.AgentID, "", "", 0, &row.HookBase, row.CreatedAt, row.UpdatedAt, h.encKey, []byte(row.WorkspaceID)))
	}
	RespondOK(c, gin.H{"instance": instance, "workspace": workspace, "agent": items})
}

// CreateAgentHook attaches a private hook to the agent (validation identical
// to the workspace level; the seal AAD is the agent's workspace).
func (h *hookHandlers) CreateAgentHook(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	ctx := c.Request.Context()

	agent, err := h.resolveHookAgent(ctx, ws.ID, c.Param("agent"))
	if err != nil {
		RespondError(c, err)
		return
	}
	req, ok := bindHookRequest(c)
	if !ok {
		return
	}

	base := buildHookBase(req)
	sealed, err := sealHookConfig(h.encKey, ws.ID, base.Config)
	if err != nil {
		RespondError(c, err)
		return
	}
	base.Config = sealed
	if err := h.validateDefinition(ctx, domain.HookLevelAgent, ws.ID, &base); err != nil {
		respondHookValidationError(c, err)
		return
	}

	hook := &domain.AgentHook{WorkspaceID: ws.ID, AgentID: agent.ID, HookBase: base}
	if err := h.hooks.CreateAgentHook(ctx, hook); err != nil {
		RespondError(c, err)
		return
	}
	matched, total, countErr := h.countMatches(ctx, &hook.HookBase, ws.ID)
	respondWithMatchCount(c, http.StatusCreated, newHookView(hook.ID, hook.WorkspaceID, hook.AgentID, "", "", 0, &hook.HookBase, hook.CreatedAt, hook.UpdatedAt, h.encKey, []byte(ws.ID)), matched, total, countErr)
}

// PatchAgentHook updates an agent hook with the keep-stored secret merge.
func (h *hookHandlers) PatchAgentHook(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	ctx := c.Request.Context()

	agent, err := h.resolveHookAgent(ctx, ws.ID, c.Param("agent"))
	if err != nil {
		RespondError(c, err)
		return
	}
	id := c.Param("id")
	req, ok := bindHookPatchRequest(c)
	if !ok {
		return
	}

	row, err := h.hooks.GetAgentHook(ctx, ws.ID, agent.ID, id)
	if err != nil {
		RespondError(c, err)
		return
	}
	if row == nil {
		RespondError(c, domain.ErrNotFound)
		return
	}

	if !applyHookPatch(&row.HookBase, req) {
		RespondError(c, fmt.Errorf("%w: no fields to update", domain.ErrInvalid))
		return
	}
	if req.Config != nil {
		merged, err := mergeHookConfigSecrets(h.encKey, ws.ID, row.Config, *req.Config)
		if err != nil {
			respondHookValidationError(c, err)
			return
		}
		row.Config = merged
	}
	if err := h.validateDefinition(ctx, domain.HookLevelAgent, ws.ID, &row.HookBase); err != nil {
		respondHookValidationError(c, err)
		return
	}
	if err := h.hooks.UpdateAgentHook(ctx, row); err != nil {
		RespondError(c, err)
		return
	}
	matched, total, countErr := h.countMatches(ctx, &row.HookBase, ws.ID)
	respondWithMatchCount(c, http.StatusOK, newHookView(row.ID, row.WorkspaceID, row.AgentID, "", "", 0, &row.HookBase, row.CreatedAt, row.UpdatedAt, h.encKey, []byte(ws.ID)), matched, total, countErr)
}

// DeleteAgentHook removes the agent's private hook.
func (h *hookHandlers) DeleteAgentHook(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	ctx := c.Request.Context()

	agent, err := h.resolveHookAgent(ctx, ws.ID, c.Param("agent"))
	if err != nil {
		RespondError(c, err)
		return
	}
	if err := h.hooks.DeleteAgentHook(ctx, ws.ID, agent.ID, c.Param("id")); err != nil {
		RespondError(c, err)
		return
	}
	RespondNoContent(c)
}

// ReorderAgentHooks sets the agent level's list order (D14).
func (h *hookHandlers) ReorderAgentHooks(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	ctx := c.Request.Context()

	agent, err := h.resolveHookAgent(ctx, ws.ID, c.Param("agent"))
	if err != nil {
		RespondError(c, err)
		return
	}
	var req hookReorderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}
	if err := h.hooks.RepositionAgentHooks(ctx, ws.ID, agent.ID, req.IDs); err != nil {
		RespondError(c, err)
		return
	}
	RespondNoContent(c)
}

// -------------------------------------------------------------------------
// Instance admin surface (master-tenant control plane; D13/D15/D17)
// -------------------------------------------------------------------------

// AdminListHooks returns the read-only builtin listing plus the managed rows.
func (h *hookHandlers) AdminListHooks(c *gin.Context) {
	rows, err := h.hooks.ListInstanceHooks(c.Request.Context())
	if err != nil {
		RespondError(c, err)
		return
	}
	builtin := make([]hookView, 0)
	managed := make([]hookView, 0)
	for i := range rows {
		row := &rows[i]
		view := newHookView(row.ID, "", "", row.Key, row.Source, row.Version, &row.HookBase, row.CreatedAt, row.UpdatedAt, h.encKey, nil)
		if row.Source == domain.HookSourceBuiltin {
			builtin = append(builtin, view)
		} else {
			managed = append(managed, view)
		}
	}
	RespondOK(c, gin.H{"builtin": builtin, "managed": managed})
}

// AdminCreateHook persists a managed instance hook. source is forced to
// managed (builtin rows are owned by the release sync pipeline, D15) and the
// version to 1. Instance secrets bind to the empty AAD — instance hooks are
// workspace-unscoped, so the dispatcher must open them with the same empty
// scope.
func (h *hookHandlers) AdminCreateHook(c *gin.Context) {
	var req adminHookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	base := buildHookBase(&req.hookRequest)
	sealed, err := sealHookConfig(h.encKey, "", base.Config)
	if err != nil {
		RespondError(c, err)
		return
	}
	base.Config = sealed
	if err := h.validateDefinition(c.Request.Context(), domain.HookLevelInstance, "", &base); err != nil {
		respondHookValidationError(c, err)
		return
	}

	hook := &domain.InstanceHook{Key: strings.TrimSpace(req.Key), Source: domain.HookSourceManaged, Version: 1, HookBase: base}
	if err := h.hooks.CreateInstanceHook(c.Request.Context(), hook); err != nil {
		RespondError(c, err)
		return
	}
	RespondCreated(c, gin.H{"hook": newHookView(hook.ID, "", "", hook.Key, hook.Source, hook.Version, &hook.HookBase, hook.CreatedAt, hook.UpdatedAt, h.encKey, nil)})
}

// AdminPatchHook updates a managed instance hook with the keep-stored secret
// merge. Builtin rows are read-only (D15) and reject writes.
func (h *hookHandlers) AdminPatchHook(c *gin.Context) {
	id := c.Param("id")
	req, ok := bindHookPatchRequest(c)
	if !ok {
		return
	}

	ctx := c.Request.Context()
	row, err := h.hooks.GetInstanceHook(ctx, id)
	if err != nil {
		RespondError(c, err)
		return
	}
	if row == nil {
		RespondError(c, domain.ErrNotFound)
		return
	}
	if row.Source == domain.HookSourceBuiltin {
		AbortWithError(c, http.StatusConflict, CodeConflict, "builtin instance hooks are read-only; ship a new version via the sync pipeline")
		return
	}

	if !applyHookPatch(&row.HookBase, req) {
		RespondError(c, fmt.Errorf("%w: no fields to update", domain.ErrInvalid))
		return
	}
	if req.Config != nil {
		merged, err := mergeHookConfigSecrets(h.encKey, "", row.Config, *req.Config)
		if err != nil {
			respondHookValidationError(c, err)
			return
		}
		row.Config = merged
	}
	if err := h.validateDefinition(ctx, domain.HookLevelInstance, "", &row.HookBase); err != nil {
		respondHookValidationError(c, err)
		return
	}
	if err := h.hooks.UpdateInstanceHook(ctx, row); err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, gin.H{"hook": newHookView(row.ID, "", "", row.Key, row.Source, row.Version, &row.HookBase, row.CreatedAt, row.UpdatedAt, h.encKey, nil)})
}

// AdminDeleteHook removes a managed instance hook; builtin rows reject.
func (h *hookHandlers) AdminDeleteHook(c *gin.Context) {
	id := c.Param("id")
	ctx := c.Request.Context()

	row, err := h.hooks.GetInstanceHook(ctx, id)
	if err != nil {
		RespondError(c, err)
		return
	}
	if row == nil {
		RespondError(c, domain.ErrNotFound)
		return
	}
	if row.Source == domain.HookSourceBuiltin {
		AbortWithError(c, http.StatusConflict, CodeConflict, "builtin instance hooks are read-only; they are removed by the sync pipeline")
		return
	}
	if err := h.hooks.DeleteInstanceHook(ctx, id); err != nil {
		RespondError(c, err)
		return
	}
	RespondNoContent(c)
}

// AdminReorderHooks sets the instance level's list order (D14).
func (h *hookHandlers) AdminReorderHooks(c *gin.Context) {
	var req hookReorderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}
	if err := h.hooks.RepositionInstanceHooks(c.Request.Context(), req.IDs); err != nil {
		RespondError(c, err)
		return
	}
	RespondNoContent(c)
}
