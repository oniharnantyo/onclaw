package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// APIClient is the harness's HTTP surface over a running onclaw server. It is
// deliberately a thin REST client — the harness holds no stores, so the same
// binary can score a pre-change server (the baseline) and a post-change one.
type APIClient struct {
	BaseURL string
	HTTP    *http.Client
}

// NewAPIClient builds a client; timeout <= 0 selects a generous default
// (non-streaming /v1 turns hold the connection for the whole model run).
func NewAPIClient(baseURL string, timeout time.Duration) *APIClient {
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	return &APIClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP:    &http.Client{Timeout: timeout},
	}
}

// apiError carries a non-2xx response so callers can branch on status codes
// (409 reuse paths, 404 optional endpoints).
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

// do performs one JSON request. token may be empty. out may be nil. Non-2xx
// returns an *apiError; 2xx-but-non-JSON is only tolerated when out is nil.
func (c *APIClient) do(ctx context.Context, method, path, token string, body, out any) error {
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
func (c *APIClient) Login(ctx context.Context, email, password string) (string, string, error) {
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

type Workspace struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// GetWorkspace fetches a workspace by slug; apiError 404 means unknown or
// non-member (the server's enumeration defense) — both read as "absent".
func (c *APIClient) GetWorkspace(ctx context.Context, token, slug string) (*Workspace, error) {
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
func (c *APIClient) CreateWorkspace(ctx context.Context, token, name, slug string) (*Workspace, error) {
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

type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

// CreateAdminUser pre-creates a user (superadmin-only endpoint). 409 (exists)
// is reported as (nil, nil) — callers treat it as already-provisioned.
func (c *APIClient) CreateAdminUser(ctx context.Context, token, email, name, password string) (*User, error) {
	var out struct {
		User *User `json:"user"`
	}
	body := map[string]string{"email": email, "name": name, "password": password}
	if err := c.do(ctx, http.MethodPost, "/api/v1/admin/users", token, body, &out); err != nil {
		var ae *apiError
		if asErr(err, &ae) && ae.Status == http.StatusConflict {
			return nil, nil
		}
		return nil, err
	}
	return out.User, nil
}

// ListRoles returns the workspace's roles as a name→id map.
func (c *APIClient) ListRoles(ctx context.Context, token, wsSlug string) (map[string]string, error) {
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

// AddMember adds an existing user to the workspace by email under roleName.
// 409 (already a member) is reported as nil error.
func (c *APIClient) AddMember(ctx context.Context, token, wsSlug, email, roleName string) error {
	roleID, err := c.roleID(ctx, token, wsSlug, roleName)
	if err != nil {
		return err
	}
	body := map[string]string{"email": email, "role_id": roleID}
	if err := c.do(ctx, http.MethodPost, "/api/v1/workspaces/"+url.PathEscape(wsSlug)+"/members", token, body, nil); err != nil {
		var ae *apiError
		if asErr(err, &ae) && ae.Status == http.StatusConflict {
			return nil
		}
		return err
	}
	return nil
}

func (c *APIClient) roleID(ctx context.Context, token, wsSlug, roleName string) (string, error) {
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

// ---- providers & agents ----

type Provider struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Enabled bool   `json:"enabled"`
}

// ListEnabledProviders returns the workspace's enabled model providers, in
// listing order.
func (c *APIClient) ListEnabledProviders(ctx context.Context, token, wsSlug string) ([]Provider, error) {
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

type Agent struct {
	ID    string   `json:"id"`
	Slug  string   `json:"slug"`
	Name  string   `json:"name"`
	Model string   `json:"model"`
	Tools []string `json:"tools"`
}

// GetAgent fetches an agent by slug (or id — the route accepts both).
func (c *APIClient) GetAgent(ctx context.Context, token, wsSlug, agentSlug string) (*Agent, error) {
	var out struct {
		Agent *Agent `json:"agent"`
	}
	path := "/api/v1/workspaces/" + url.PathEscape(wsSlug) + "/agents/" + url.PathEscape(agentSlug)
	if err := c.do(ctx, http.MethodGet, path, token, nil, &out); err != nil {
		return nil, err
	}
	if out.Agent == nil {
		return nil, fmt.Errorf("agent %q response missing agent object", agentSlug)
	}
	return out.Agent, nil
}

// evalAgentTools is the registry-tool allowlist the fixture agent is
// provisioned with (fix-memory-prefetch-matching D6). The runner resolves an
// agent's tools strictly from this allowlist — an empty one exposes zero
// registry tools, so the scoreboard's self-search leg (the model calling
// memory.search itself) died in `skill not found: memory`: with no search
// schema to call, the model could only reach for the skill middleware's
// generic skill tool, which resolves skills, not tools. The harness's
// evidence extraction (run.go extractSearchEvidence) only recognizes
// memory.search cards, so the fixture cannot measure memory quality without
// this tool exposed.
var evalAgentTools = []string{"memory.search"}

// CreateAgent registers the fixture agent under the given provider+model,
// carrying evalAgentTools as its exposed-tools allowlist. Agent creation runs
// live prompt generation against the provider, so this fails when the
// workspace has no working provider (live model keys).
func (c *APIClient) CreateAgent(ctx context.Context, token, wsSlug string, providerID, agentSlug, model string) (*Agent, error) {
	var out struct {
		Agent *Agent `json:"agent"`
	}
	body := map[string]any{
		"name":        "Memory Eval Agent",
		"slug":        agentSlug,
		"role":        "Memory evaluation fixture",
		"description": "Fixture agent for the wave-0 memory eval harness (integrate-agent-zero-memory D15).",
		"brief":       "Answers workspace questions for the LongMemEval-protocol scoreboard.",
		"provider_id": providerID,
		"model":       model,
		"tools":       evalAgentTools,
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

// PatchAgentModel repoints the agent at a model (and optionally a provider)
// on the reuse path. Empty fields are omitted.
func (c *APIClient) PatchAgentModel(ctx context.Context, token, wsSlug, agentID, providerID, model string) error {
	body := map[string]any{}
	if model != "" {
		body["model"] = model
	}
	if providerID != "" {
		body["provider_id"] = providerID
	}
	if len(body) == 0 {
		return nil
	}
	path := "/api/v1/workspaces/" + url.PathEscape(wsSlug) + "/agents/" + url.PathEscape(agentID)
	return c.do(ctx, http.MethodPatch, path, token, body, nil)
}

// PatchAgentTools replaces the agent's registry-tool allowlist on the reuse
// path: agents provisioned before the harness carried the allowlist store an
// empty tools list, which exposes zero registry tools and kills the
// self-search leg. The PATCH runs only when the stored allowlist is missing
// one of evalAgentTools (seeder's ensureAgent), so repeated seeds stay
// idempotent.
func (c *APIClient) PatchAgentTools(ctx context.Context, token, wsSlug, agentID string, tools []string) error {
	body := map[string]any{"tools": tools}
	path := "/api/v1/workspaces/" + url.PathEscape(wsSlug) + "/agents/" + url.PathEscape(agentID)
	return c.do(ctx, http.MethodPatch, path, token, body, nil)
}

// ---- chat keys ----

// ExchangeChatKey mints a workspace-scoped chat key for the token's user; the
// key's creator is the identity that scopes memory.search and ingestion.
func (c *APIClient) ExchangeChatKey(ctx context.Context, token, wsSlug string) (string, error) {
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
// aggregated Response JSON.
func (c *APIClient) ChatTurn(ctx context.Context, key, model, input, sessionID string) (map[string]any, error) {
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

// ---- memory notes (optional REST surface, added by the memory UI wave) ----

// Note is the tolerant view of a memory note row returned by the notes REST
// endpoint. The endpoint is being added concurrently; the harness accepts the
// field names it plausibly exposes and never hard-depends on the route.
type Note struct {
	ID         string `json:"id"`
	Content    string `json:"content"`
	Visibility string `json:"visibility"`
	UserID     string `json:"user_id"`
	AgentID    string `json:"agent_id"`
}

// ListNotes lists workspace memory notes with an optional text filter.
// (available=false, nil) means the endpoint is absent (404) — the harness
// then falls back to fixed-wait ingestion and transcript-only evidence.
func (c *APIClient) ListNotes(ctx context.Context, token, wsSlug, q string) ([]Note, bool, error) {
	path := "/api/v1/workspaces/" + url.PathEscape(wsSlug) + "/memory/notes"
	if q != "" {
		path += "?q=" + url.QueryEscape(q)
	}
	raw, err := c.listNotesRaw(ctx, token, path)
	if err != nil {
		var ae *apiError
		if asErr(err, &ae) && ae.Status == http.StatusNotFound {
			return nil, false, nil
		}
		return nil, false, err
	}
	notes := parseNotes(raw)
	return notes, true, nil
}

// listNotesRaw returns the raw 200 body for tolerant shape parsing.
func (c *APIClient) listNotesRaw(ctx context.Context, token, path string) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(ctx, http.MethodGet, path, token, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// parseNotes accepts a bare array or an envelope ({notes|items|data: [...]})
// and tolerates both "content" and "text" fields per note.
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

func asErr(err error, target **apiError) bool {
	ae, ok := err.(*apiError)
	if ok {
		*target = ae
	}
	return ok
}
