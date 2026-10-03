// Package driver is the bench's black-box HTTP surface over a running OnClaw
// server (add-harness-bench design D3): a port of the internal/memory/eval
// harness client, generalized to all tools. It is copied source, never an
// import — the nested module boundary (design D1) makes importing
// github.com/oniharnantyo/onclaw impossible to express, which is the point:
// the same bench binary must drive any server version without recompilation
// against its internals.
//
// The client is deliberately thin REST + JSON with bearer auth. Non-2xx
// responses surface as *apiError so callers (the fixture seeder, suite setup)
// can branch on status codes — 404 reads as absent, 409 as already-exists.
package driver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultTimeout is the client default (timeout <= 0 in NewClient selects
// it): non-streaming /v1 turns hold the connection for the whole model run,
// so the timeout must be generous, not request-shaped.
const DefaultTimeout = 10 * time.Minute

// Client is the bench's HTTP client. It holds no stores and no secrets —
// tokens and chat keys are passed per call by the caller.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// NewClient builds a client; timeout <= 0 selects DefaultTimeout.
func NewClient(baseURL string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP:    &http.Client{Timeout: timeout},
	}
}

// apiError carries a non-2xx response so callers can branch on status codes
// (409 reuse paths, 404 create-or-reuse paths, 401 credential mismatches).
type apiError struct {
	Status int
	Body   string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.Status, truncate(e.Body, 300))
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// IsNotFound reports whether err is the API's 404 — for workspaces and
// agents the server answers 404 for unknown AND cross-tenant identifiers
// (enumeration defense), so 404 is the "absent" signal everywhere.
func IsNotFound(err error) bool { return statusIs(err, http.StatusNotFound) }

// IsConflict reports whether err is the API's 409 — the already-exists signal
// the create-or-reuse paths ride.
func IsConflict(err error) bool { return statusIs(err, http.StatusConflict) }

// IsUnauthorized reports whether err is the API's 401.
func IsUnauthorized(err error) bool { return statusIs(err, http.StatusUnauthorized) }

// IsForbidden reports whether err is the API's 403.
func IsForbidden(err error) bool { return statusIs(err, http.StatusForbidden) }

func statusIs(err error, code int) bool {
	var ae *apiError
	return errors.As(err, &ae) && ae.Status == code
}

// do performs one JSON request. token may be empty. out may be nil. Non-2xx
// returns an *apiError; 2xx-but-non-JSON is only tolerated when out is nil.
func (c *Client) do(ctx context.Context, method, path, token string, body, out any) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &apiError{Status: resp.StatusCode, Body: string(raw)}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decoding %s %s response: %w (%s)", method, path, err, truncate(string(raw), 200))
	}
	return nil
}

// ---- identities ----

type loginResponse struct {
	Token string `json:"token"`
	User  struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	} `json:"user"`
}

// Login exchanges email/password for a JWT and returns (token, userID).
func (c *Client) Login(ctx context.Context, email, password string) (string, string, error) {
	var out loginResponse
	if err := c.do(ctx, http.MethodPost, "/api/v1/auth/login", "", map[string]string{"email": email, "password": password}, &out); err != nil {
		return "", "", fmt.Errorf("login %s: %w", email, err)
	}
	if out.Token == "" {
		return "", "", fmt.Errorf("login %s: empty token", email)
	}
	return out.Token, out.User.ID, nil
}

// ---- workspace ----

// Workspace is the tolerant view of a workspace row.
type Workspace struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// GetWorkspace fetches a workspace by slug; a 404 (unknown or non-member —
// the server's enumeration defense) is returned as the *apiError so the
// create-or-reuse caller can treat it as "absent".
func (c *Client) GetWorkspace(ctx context.Context, token, slug string) (*Workspace, error) {
	var out struct {
		Workspace *Workspace `json:"workspace"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/workspaces/"+url.PathEscape(slug), token, nil, &out); err != nil {
		return nil, err
	}
	if out.Workspace == nil {
		return nil, fmt.Errorf("workspace %q response missing workspace object", slug)
	}
	return out.Workspace, nil
}

// CreateWorkspace creates a workspace owned by the token's user.
func (c *Client) CreateWorkspace(ctx context.Context, token, name, slug string) (*Workspace, error) {
	var out struct {
		Workspace *Workspace `json:"workspace"`
	}
	body := map[string]any{"name": name, "slug": slug}
	if err := c.do(ctx, http.MethodPost, "/api/v1/workspaces", token, body, &out); err != nil {
		return nil, err
	}
	if out.Workspace == nil {
		return nil, fmt.Errorf("workspace create response missing workspace object")
	}
	return out.Workspace, nil
}

// ---- users & members ----

// User is the tolerant view of a user row.
type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

// CreateAdminUser pre-creates a user (instance-admin endpoint). 409 (exists)
// is reported as (nil, nil) — callers treat it as already-provisioned.
func (c *Client) CreateAdminUser(ctx context.Context, token, email, name, password string) (*User, error) {
	var out struct {
		User *User `json:"user"`
	}
	body := map[string]string{"email": email, "name": name, "password": password}
	if err := c.do(ctx, http.MethodPost, "/api/v1/admin/users", token, body, &out); err != nil {
		if IsConflict(err) {
			return nil, nil
		}
		return nil, err
	}
	return out.User, nil
}

// ListRoles returns the workspace's roles as a name→id map.
func (c *Client) ListRoles(ctx context.Context, token, wsSlug string) (map[string]string, error) {
	var out struct {
		Roles []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"roles"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/workspaces/"+url.PathEscape(wsSlug)+"/roles", token, nil, &out); err != nil {
		return nil, err
	}
	roles := map[string]string{}
	for _, r := range out.Roles {
		roles[r.Name] = r.ID
	}
	return roles, nil
}

// AddMember adds an existing user to the workspace by email under roleName
// (resolved through the workspace roles list). 409 (already a member) is
// reported as nil error — the idempotent-reuse signal.
func (c *Client) AddMember(ctx context.Context, token, wsSlug, email, roleName string) error {
	roleID, err := c.roleID(ctx, token, wsSlug, roleName)
	if err != nil {
		return err
	}
	body := map[string]string{"email": email, "role_id": roleID}
	if err := c.do(ctx, http.MethodPost, "/api/v1/workspaces/"+url.PathEscape(wsSlug)+"/members", token, body, nil); err != nil {
		if IsConflict(err) {
			return nil
		}
		return err
	}
	return nil
}

func (c *Client) roleID(ctx context.Context, token, wsSlug, roleName string) (string, error) {
	roles, err := c.ListRoles(ctx, token, wsSlug)
	if err != nil {
		return "", err
	}
	id, ok := roles[roleName]
	if !ok {
		return "", fmt.Errorf("workspace %s has no %q role (have %v)", wsSlug, roleName, keys(roles))
	}
	return id, nil
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// ---- providers ----

// Provider is the tolerant view of a provider configuration row.
type Provider struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	BaseURL string `json:"base_url"`
	Enabled bool   `json:"enabled"`
}

// ProviderPayload mirrors the server's provider create request (handlers
// CreateProviderRequest): pointers/omitempty so absent fields keep the
// server's defaults. Key is write-only — it never round-trips.
type ProviderPayload struct {
	Type            string  `json:"type"`
	Name            string  `json:"name"`
	BaseURL         *string `json:"base_url,omitempty"`
	Key             *string `json:"key,omitempty"`
	Enabled         *bool   `json:"enabled,omitempty"`
	CatalogProvider *string `json:"catalog_provider,omitempty"`
}

// ListEnabledProviders returns the workspace's enabled model providers, in
// listing order.
func (c *Client) ListEnabledProviders(ctx context.Context, token, wsSlug string) ([]Provider, error) {
	var out struct {
		Providers []Provider `json:"providers"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/workspaces/"+url.PathEscape(wsSlug)+"/providers", token, nil, &out); err != nil {
		return nil, err
	}
	enabled := []Provider{}
	for _, p := range out.Providers {
		if p.Enabled {
			enabled = append(enabled, p)
		}
	}
	return enabled, nil
}

// CreateProvider creates a workspace provider configuration. The response's
// Provider never carries the key (the server never echoes secrets).
func (c *Client) CreateProvider(ctx context.Context, token, wsSlug string, payload ProviderPayload) (*Provider, error) {
	var out struct {
		Provider *Provider `json:"provider"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/v1/workspaces/"+url.PathEscape(wsSlug)+"/providers", token, payload, &out); err != nil {
		return nil, err
	}
	if out.Provider == nil {
		return nil, fmt.Errorf("provider create response missing provider object")
	}
	return out.Provider, nil
}

// ---- agents ----

// Agent is the tolerant view of an agent row: the fields the fixture reads
// on the reuse path (model repoint, context-window pin, denylist repair).
type Agent struct {
	ID            string   `json:"id"`
	Slug          string   `json:"slug"`
	Name          string   `json:"name"`
	ProviderID    string   `json:"provider_id"`
	Model         string   `json:"model"`
	ContextWindow *int     `json:"context_window,omitempty"`
	DisabledTools []string `json:"disabled_tools"`
}

// GetAgent fetches an agent by slug (or id — the route accepts both).
func (c *Client) GetAgent(ctx context.Context, token, wsSlug, slugOrID string) (*Agent, error) {
	var out struct {
		Agent *Agent `json:"agent"`
	}
	path := "/api/v1/workspaces/" + url.PathEscape(wsSlug) + "/agents/" + url.PathEscape(slugOrID)
	if err := c.do(ctx, http.MethodGet, path, token, nil, &out); err != nil {
		return nil, err
	}
	if out.Agent == nil {
		return nil, fmt.Errorf("agent %q response missing agent object", slugOrID)
	}
	return out.Agent, nil
}

// CreateAgent registers an agent under the given provider+model. Agent
// creation runs live prompt generation against the provider, so this fails
// when the workspace has no working provider (live model keys).
func (c *Client) CreateAgent(ctx context.Context, token, wsSlug string, providerID, slug, name, model string) (*Agent, error) {
	var out struct {
		Agent *Agent `json:"agent"`
	}
	body := map[string]any{
		"name":        name,
		"slug":        slug,
		"role":        "Benchmark fixture agent",
		"description": "Fixture agent for the add-harness-bench benchmark, provisioned through the public API only.",
		"brief":       "Executes benchmark tasks across the bench lanes and reports results through its transcript.",
		"provider_id": providerID,
		"model":       model,
	}
	path := "/api/v1/workspaces/" + url.PathEscape(wsSlug) + "/agents"
	if err := c.do(ctx, http.MethodPost, path, token, body, &out); err != nil {
		return nil, err
	}
	if out.Agent == nil {
		return nil, fmt.Errorf("agent create response missing agent object")
	}
	return out.Agent, nil
}

// PatchAgent patches an agent with omitempty fields. Field conventions:
// model == "" omits the model, a nil contextWindow omits the context window,
// and a nil disabledTools slice leaves the denylist untouched while a
// non-nil slice (empty included) REPLACES it — the PATCH replaces the stored
// denylist wholesale. When every field is omitted the request is skipped
// client-side (the server 400s an empty patch) and nil is returned.
func (c *Client) PatchAgent(ctx context.Context, token, wsSlug, agentID, model string, contextWindow *int, disabledTools []string) error {
	body := map[string]any{}
	if model != "" {
		body["model"] = model
	}
	if contextWindow != nil {
		body["context_window"] = *contextWindow
	}
	if disabledTools != nil {
		body["disabled_tools"] = disabledTools
	}
	if len(body) == 0 {
		return nil
	}
	path := "/api/v1/workspaces/" + url.PathEscape(wsSlug) + "/agents/" + url.PathEscape(agentID)
	return c.do(ctx, http.MethodPatch, path, token, body, nil)
}

// ---- chat keys ----

// ExchangeChatKey mints a workspace-scoped chat key for the token's user;
// the key's creator is the identity that scopes memory.search and ingestion.
func (c *Client) ExchangeChatKey(ctx context.Context, token, wsSlug string) (string, error) {
	var out struct {
		Key string `json:"key"`
	}
	path := "/api/v1/workspaces/" + url.PathEscape(wsSlug) + "/api-keys/exchange"
	if err := c.do(ctx, http.MethodPost, path, token, nil, &out); err != nil {
		return "", err
	}
	if out.Key == "" {
		return "", fmt.Errorf("chat key exchange returned an empty key")
	}
	return out.Key, nil
}

// ---- chat (/v1 OpenResponses, non-streaming) ----

// ChatTurn drives one real turn through the agent runner: model is the agent
// slug, sessionID the metadata.onclaw_session binding. Returns the raw
// aggregated Response JSON — run it through ExtractTurn for the transcript.
func (c *Client) ChatTurn(ctx context.Context, key, model, input, sessionID string) (map[string]any, error) {
	body := map[string]any{
		"model":  model,
		"input":  input,
		"stream": false,
		"metadata": map[string]string{
			"onclaw_session": sessionID,
		},
	}
	var out map[string]any
	if err := c.do(ctx, http.MethodPost, "/v1/responses", key, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ---- setup surfaces: hooks ----

// HookPayload mirrors the server's hook create request (handlers
// hookRequest): matcher is the single-string form ("" or "*" match-all),
// config is handler-type-shaped raw JSON, and Enabled is a pointer so an
// omitted field keeps the server's enabled-by-true default.
type HookPayload struct {
	Name        string          `json:"name"`
	Event       string          `json:"event"`
	Matcher     string          `json:"matcher"`
	If          string          `json:"if,omitempty"`
	HandlerType string          `json:"handler_type"`
	Config      json.RawMessage `json:"config,omitempty"`
	TimeoutMS   int             `json:"timeout_ms,omitempty"`
	OnFailure   string          `json:"on_failure,omitempty"`
	Enabled     *bool           `json:"enabled,omitempty"`
}

// CreateWorkspaceHook persists a workspace-level hook and returns its id
// (the handle DeleteWorkspaceHook takes). Suite setup uses hooks to inject
// policy gates around bench turns.
func (c *Client) CreateWorkspaceHook(ctx context.Context, token, wsSlug string, payload HookPayload) (string, error) {
	var out struct {
		Hook struct {
			ID string `json:"id"`
		} `json:"hook"`
	}
	path := "/api/v1/workspaces/" + url.PathEscape(wsSlug) + "/hooks"
	if err := c.do(ctx, http.MethodPost, path, token, payload, &out); err != nil {
		return "", err
	}
	if out.Hook.ID == "" {
		return "", fmt.Errorf("hook create response missing hook id")
	}
	return out.Hook.ID, nil
}

// DeleteWorkspaceHook removes a workspace-level hook (setup teardown).
func (c *Client) DeleteWorkspaceHook(ctx context.Context, token, wsSlug, hookID string) error {
	path := "/api/v1/workspaces/" + url.PathEscape(wsSlug) + "/hooks/" + url.PathEscape(hookID)
	return c.do(ctx, http.MethodDelete, path, token, nil, nil)
}

// ---- setup surfaces: connections ----

// ConnectionPayload mirrors the server's connection create request (handlers
// connectRequest): exactly (recipe id, access level, token, origin) —
// endpoints come from the recipe registry, never user input.
type ConnectionPayload struct {
	RecipeID    string `json:"recipe_id"`
	AccessLevel string `json:"access_level"`
	Token       string `json:"token"`
	Origin      string `json:"origin"`
}

// Connection is the tolerant view of a connection row.
type Connection struct {
	ID      string `json:"id"`
	Service string `json:"service"`
	Status  string `json:"status"`
}

// CreateConnection connects a workspace service connection. A PAT recipe
// probes and persists synchronously (201, joined view); an OAuth recipe
// cannot be provisioned with a token and answers 200 with an authorize_url —
// that is an error here, not a connection.
func (c *Client) CreateConnection(ctx context.Context, token, wsSlug string, payload ConnectionPayload) (*Connection, error) {
	path := "/api/v1/workspaces/" + url.PathEscape(wsSlug) + "/integrations/connections"
	raw, err := c.doRaw(ctx, http.MethodPost, path, token, payload)
	if err != nil {
		return nil, err
	}
	var authorize struct {
		AuthorizeURL string `json:"authorize_url"`
	}
	if err := json.Unmarshal(raw, &authorize); err == nil && authorize.AuthorizeURL != "" {
		return nil, fmt.Errorf("recipe %q is OAuth-based and cannot be provisioned with a token (authorize_url %s)", payload.RecipeID, authorize.AuthorizeURL)
	}
	var out struct {
		Connection *Connection `json:"connection"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decoding connection create response: %w (%s)", err, truncate(string(raw), 200))
	}
	if out.Connection == nil {
		return nil, fmt.Errorf("connection create response missing connection object")
	}
	return out.Connection, nil
}

// DeleteConnection disconnects a workspace service connection (setup
// teardown for connection steps). Mirrors DeleteWorkspaceHook.
func (c *Client) DeleteConnection(ctx context.Context, token, wsSlug, connectionID string) error {
	path := "/api/v1/workspaces/" + url.PathEscape(wsSlug) + "/integrations/connections/" + url.PathEscape(connectionID)
	return c.do(ctx, http.MethodDelete, path, token, nil, nil)
}

// RawResponse performs one authenticated JSON request and returns the HTTP
// status code and raw body for ANY response — including non-2xx — without
// mapping status codes to errors. The api_effect verifier needs the observed
// status and body verbatim (an expected 404 is data, not an error). err is
// reserved for request-construction and transport failures.
func (c *Client) RawResponse(ctx context.Context, method, path, token string, body any) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, raw, nil
}

// ---- memory notes (optional REST surface, memory-lane only) ----

// Note is the tolerant view of a memory note row returned by the notes REST
// endpoint. The memory lane reads it for evidence; the fixture never
// hard-depends on the route (a 404 reads as endpoint-absent).
type Note struct {
	ID         string `json:"id"`
	Content    string `json:"content"`
	Visibility string `json:"visibility"`
	UserID     string `json:"user_id"`
	AgentID    string `json:"agent_id"`
}

// ListNotes lists workspace memory notes with an optional text filter.
// (available=false, nil) means the endpoint is absent (404) — callers fall
// back to fixed-wait ingestion and transcript-only evidence.
func (c *Client) ListNotes(ctx context.Context, token, wsSlug, q string) ([]Note, bool, error) {
	path := "/api/v1/workspaces/" + url.PathEscape(wsSlug) + "/memory/notes"
	if q != "" {
		path += "?q=" + url.QueryEscape(q)
	}
	raw, err := c.doRaw(ctx, http.MethodGet, path, token, nil)
	if err != nil {
		if IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return parseNotes(raw), true, nil
}

// parseNotes accepts a bare array or an envelope ({notes|items|data: [...]})
// and tolerates both "content" and "text" fields per note — the notes shape
// is not part of the pinned black-box contract.
func parseNotes(raw json.RawMessage) []Note {
	if len(raw) == 0 {
		return nil
	}
	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err != nil {
		var env struct {
			Notes []map[string]any `json:"notes"`
			Items []map[string]any `json:"items"`
			Data  []map[string]any `json:"data"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			return nil
		}
		switch {
		case env.Notes != nil:
			arr = env.Notes
		case env.Items != nil:
			arr = env.Items
		case env.Data != nil:
			arr = env.Data
		}
	}
	notes := make([]Note, 0, len(arr))
	for _, m := range arr {
		n := Note{}
		if v, ok := m["id"].(string); ok {
			n.ID = v
		}
		if v, ok := m["content"].(string); ok {
			n.Content = v
		} else if v, ok := m["text"].(string); ok {
			n.Content = v
		}
		if v, ok := m["visibility"].(string); ok {
			n.Visibility = v
		}
		if v, ok := m["user_id"].(string); ok {
			n.UserID = v
		}
		if v, ok := m["agent_id"].(string); ok {
			n.AgentID = v
		}
		notes = append(notes, n)
	}
	return notes
}

// doRaw performs one JSON request and returns the raw 200-2xx body for
// tolerant shape parsing (ListNotes, CreateConnection).
func (c *Client) doRaw(ctx context.Context, method, path, token string, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(ctx, method, path, token, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}
