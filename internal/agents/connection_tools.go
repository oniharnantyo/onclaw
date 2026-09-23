package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// The connection tool source (add-connection-http design.md D1): one tool per
// recipe-declared verb per attached http-kind connection, each a thin facade —
// pre-bound method, path template, generated parameter schema — over the
// shared request engine below (pin validation, server-side credential
// injection, response capping). Verbs only: no free-form request tool exists,
// so the model cannot invent an endpoint; the exposed surface is exactly the
// recipe's declared verb list.

// ConnectionCredentials is the runner-owned credential resolver seam
// (add-connection-http contract §3, the services.ModelCatalog precedent): the
// connections service satisfies it structurally; the composition root injects
// the service and this package never imports internal/services. Implementations
// return the DECRYPTED RAW token for a workspace-scoped connection — no scheme
// composition, the engine composes the header value at call time — and their
// errors never carry credential material.
type ConnectionCredentials interface {
	CredentialForConnection(ctx context.Context, workspaceID, connectionID string) (string, error)
}

// HTTPConnectionRef is one attached http-kind connection as the resolution
// pass consumes it: the connection row's identity, its attachment facts, and
// the registered recipe the connection was created from (base URL, auth
// header, scheme, declared verbs — the data verb generation needs). Recipe
// comes from domain.RecipeByID on the service side; nil or non-http recipes
// are malformed refs and degrade skip-and-mark like any other per-connection
// failure.
type HTTPConnectionRef struct {
	// ConnectionID is the workspace connection row id — the id the agent's
	// enabled_mcps attachment list carries for http kind (contract §2).
	ConnectionID string
	// Service is the recipe id (domain.Connection.Service).
	Service string
	// Status is the connection row's persisted status
	// (domain.ConnectionStatus*). Empty normalizes to connected, matching the
	// domain's stored-shape rule; any other non-connected status degrades.
	Status string
	// Origin is the connection's STORED resolved origin
	// (domain.Connection.Origin, add-recipe-base-url tasks.md 2.2) — the
	// immutable origin the connection was connected against. The verb base
	// resolves from it at generation (domain.ResolveRecipeBase below), so a
	// parametrized recipe's tools dial the connection's own instance and
	// never the recipe default; recipes without an origin parameter resolve
	// byte-identically to the declared BaseURL regardless of this value.
	Origin string
	// Recipe is the registered recipe behind the connection.
	Recipe *domain.Recipe
}

// AttachedConnectionLister is the runner-owned attachment seam: it resolves
// the agent's attached HTTP-kind connections for one run. The connections
// service implements it (resolve-as-server-first-else-connection lookup per
// contract §2, kind-checked to http); the runner only consumes it.
type AttachedConnectionLister interface {
	AttachedHTTPConnections(ctx context.Context, workspaceID, agentID string) ([]HTTPConnectionRef, error)
}

// connectionToolResolver is the runner-facing seam of the connection tool
// source (the mcp.ToolSource precedent): one call per run, in the same
// resolution pass as MCP tools and after them. resolved carries the
// already-resolved tools so verb names colliding with a built-in or private
// MCP tool can degrade per contract §5. *ConnectionToolSource implements it;
// the no-op default contributes nothing. Alongside the tools it returns the
// gate's origin annotations (add-integration-authority task 2.1): each
// resolved verb keyed by its name, carrying its connection identity — the
// connection gate consults the map at resolution and invocation.
type connectionToolResolver interface {
	ToolsFor(ctx context.Context, workspaceID, agentID string, resolved []tool.BaseTool) ([]tool.BaseTool, map[string]connectionToolOrigin, error)
}

// noopConnectionTools is the default source: no connections, contributes
// nothing. It keeps the runner's dependency non-nil at the point of use
// (composition-root wiring replaces it via WithConnectionToolSource).
type noopConnectionTools struct{}

func (noopConnectionTools) ToolsFor(context.Context, string, string, []tool.BaseTool) ([]tool.BaseTool, map[string]connectionToolOrigin, error) {
	return nil, nil, nil
}

// Response-discipline constants (add-connection-http D4): the cap, the read
// buffering, and the truncation marker are byte-identical to the web fetch
// tool's rules (internal/agents/tools/webfetch.go maxFetchBytes and
// truncationMarker) so turn budgets behave identically across lanes.
const (
	// maxConnectionResponseBytes caps the response body read; larger bodies
	// are truncated (mirrors tools/webfetch.go maxFetchBytes).
	maxConnectionResponseBytes = 1 << 20 // 1 MiB
	// connectionTruncationMarker is appended when the body exceeded the cap
	// (mirrors tools/webfetch.go truncationMarker).
	connectionTruncationMarker = "\n\n[truncated: response exceeded 1 MiB]"
	// connectionCallTimeout bounds one provider call (mirrors the web fetch
	// client's 30s bound).
	connectionCallTimeout = 30 * time.Second
	// maxConnectionRedirects bounds the redirect chain (mirrors the web fetch
	// client's cap); off-host hops are refused before this anyway.
	maxConnectionRedirects = 10
	// maxConnectionErrorExcerpt caps the provider body excerpt quoted in a
	// non-2xx tool error — a diagnostic, never a second response body.
	maxConnectionErrorExcerpt = 512
)

// ConnectionToolSource resolves the attached http-kind connections' declared
// verb tools for one run (add-connection-http D1, tasks.md 2.1). It consumes
// two narrow structural interfaces the connections service implements —
// AttachedConnectionLister and ConnectionCredentials — and yields the verbs
// under the pinned degradation rule: a connection whose credential cannot
// resolve, whose status is not connected, or whose ref is malformed is
// skipped and marked; the run proceeds.
type ConnectionToolSource struct {
	lister AttachedConnectionLister
	creds  ConnectionCredentials
	// client is the base provider-call client; each connection's tools dial
	// through a per-connection derivative whose redirect policy is pinned to
	// the connection's base host (D2).
	client *http.Client
}

// ConnectionToolSourceOption configures the source's defaultable knobs.
type ConnectionToolSourceOption func(*ConnectionToolSource)

// WithConnectionHTTPClient overrides the base HTTP client (testing seam). The
// per-connection redirect pinning applies regardless — only the timeout and
// transport are inherited.
func WithConnectionHTTPClient(c *http.Client) ConnectionToolSourceOption {
	return func(s *ConnectionToolSource) {
		if c != nil {
			s.client = c
		}
	}
}

// NewConnectionToolSource builds the source from its two granular seams
// (explicit positional DI — never an aggregate). The composition root wires
// the connections service, which implements both interfaces structurally.
func NewConnectionToolSource(lister AttachedConnectionLister, creds ConnectionCredentials, opts ...ConnectionToolSourceOption) *ConnectionToolSource {
	s := &ConnectionToolSource{
		lister: lister,
		creds:  creds,
		client: &http.Client{Timeout: connectionCallTimeout},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// ToolsFor implements connectionToolResolver: one tool per declared verb per
// attached http-kind connection, after the already-resolved set. Store-level
// lister failures fail the resolution like any other store error; every
// per-connection failure degrades skip-and-mark (D6) and never fails the run.
// Each resolved verb is annotated with its connection's identity for the
// connection gate (add-integration-authority task 2.1) — the recipe rides the
// annotation, so the gate's tier lookup reads the same declaration data the
// verb generation does.
func (s *ConnectionToolSource) ToolsFor(ctx context.Context, workspaceID, agentID string, resolved []tool.BaseTool) ([]tool.BaseTool, map[string]connectionToolOrigin, error) {
	refs, err := s.lister.AttachedHTTPConnections(ctx, workspaceID, agentID)
	if err != nil {
		return nil, nil, fmt.Errorf("list attached http connections: %w", err)
	}
	if len(refs) == 0 {
		return nil, nil, nil
	}

	taken := takenToolNames(ctx, resolved)
	var out []tool.BaseTool
	origins := map[string]connectionToolOrigin{}
	for _, ref := range refs {
		mark := func(reason string, err error) {
			args := []any{
				"workspace_id", workspaceID,
				"agent_id", agentID,
				"connection_id", ref.ConnectionID,
				"service", ref.Service,
				"reason", reason,
			}
			if err != nil {
				args = append(args, "error", err)
			}
			slog.WarnContext(ctx, "connection tool source: skipping connection's tools", args...)
		}
		if reason := ref.usable(); reason != "" {
			mark(reason, nil)
			continue
		}
		if ref.Status != "" && ref.Status != domain.ConnectionStatusConnected {
			mark("connection is not connected (status "+ref.Status+")", nil)
			continue
		}
		// Degradation canary (D6/§3): resolution is attempted here to decide
		// skip-or-include, and again fresh at every verb call — the value is
		// never captured into a generated tool, so refresh semantics can slot
		// into the seam later. Resolver errors carry no credential material.
		if _, err := s.creds.CredentialForConnection(ctx, workspaceID, ref.ConnectionID); err != nil {
			mark("credential unavailable", err)
			continue
		}

		// The dial base resolves from the connection's STORED origin
		// (add-recipe-base-url tasks.md 2.3 — immutability): a parametrized
		// recipe's verbs dial the connection's own instance, non-parametrized
		// recipes resolve byte-identically to the declared BaseURL. A stored
		// origin that no longer parses degrades skip-and-mark like any other
		// malformed surface.
		resolved, err := domain.ResolveRecipeBase(ref.Recipe, ref.Origin)
		if err != nil {
			mark("malformed recipe surface", err)
			continue
		}
		base, err := connectionBaseFor(resolved)
		if err != nil {
			mark("malformed recipe surface", err)
			continue
		}
		client := s.pinnedClient(base.host)
		origin := connectionToolOrigin{
			ConnectionID: ref.ConnectionID,
			Service:      ref.Service,
			ServiceName:  ref.Recipe.Service,
			Recipe:       ref.Recipe,
		}
		for _, verb := range ref.Recipe.Verbs {
			if _, collide := taken[verb.Tool]; collide {
				mark("verb tool name collides with an already-resolved tool: "+verb.Tool, nil)
				continue
			}
			taken[verb.Tool] = struct{}{}
			out = append(out, newConnectionVerbTool(ref, verb, base.pinFor(verb.Path), workspaceID, client, s.creds))
			origins[verb.Tool] = origin
		}
	}
	return out, origins, nil
}

// pinnedClient derives the per-connection client whose redirect policy is
// pinned to the connection's base host: redirects are resolved but refused on
// host change (D2 — the spec's off-host rule). Timeout and transport inherit
// from the base client.
func (s *ConnectionToolSource) pinnedClient(host string) *http.Client {
	return &http.Client{
		Timeout:   s.client.Timeout,
		Transport: s.client.Transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxConnectionRedirects {
				return errTooManyConnectionRedirects
			}
			if req.URL.Host != host {
				return &offHostRedirectError{host: req.URL.Host}
			}
			return nil
		},
	}
}

// usable reports a skip reason for a malformed ref: empty connection id, a
// nil recipe, a non-http kind, or a missing auth header. Recipe validation
// (contract §1) guarantees the rest of the surface; these are the facts the
// source itself depends on.
func (r HTTPConnectionRef) usable() string {
	switch {
	case r.ConnectionID == "":
		return "ref carries no connection id"
	case r.Recipe == nil:
		return "ref carries no recipe"
	case r.Recipe.Kind != domain.RecipeKindHTTP:
		return "recipe is not http kind"
	case r.Recipe.BaseURL == "":
		return "recipe declares no base url"
	case r.Recipe.TokenHeader == "":
		return "recipe declares no auth header"
	case len(r.Recipe.Verbs) == 0:
		return "recipe declares no verbs"
	}
	return ""
}

// takenToolNames collects the already-resolved tools' names for the collision
// pass (contract §5). Tools whose Info fails contribute no name — an unknown
// name cannot collide.
func takenToolNames(ctx context.Context, resolved []tool.BaseTool) map[string]struct{} {
	taken := make(map[string]struct{}, len(resolved))
	for _, t := range resolved {
		if t == nil {
			continue
		}
		if info, err := t.Info(ctx); err == nil && info != nil && info.Name != "" {
			taken[info.Name] = struct{}{}
		}
	}
	return taken
}

// connectionPin is the bind-time structural pin (add-connection-http D2):
// scheme, host, and the joined path's root — the base path plus the verb
// template's literal prefix before its first placeholder. Computed once at
// generation; every call re-validates the joined URL against it before any
// dial.
type connectionPin struct {
	scheme string
	host   string // host or host:port, as url.URL.Host carries it
	// basePath is the pinned base URL's path prefix every verb's joined path
	// extends.
	basePath string
	// root is basePath plus this verb template's literal prefix before its
	// first placeholder — the path floor the re-validation enforces.
	root string
}

// connectionBase is the connection-level half of the pin: scheme and host,
// shared by every verb of the connection, plus the base path the per-verb
// roots extend.
type connectionBase struct {
	scheme   string
	host     string
	basePath string
}

// connectionBaseFor parses the pinned base URL. Recipe validation guarantees
// an absolute http(s) URL without query or fragment (contract §1); the parse
// error path is kept total for malformed test or plugin data.
func connectionBaseFor(baseURL string) (connectionBase, error) {
	base, err := url.Parse(baseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return connectionBase{}, fmt.Errorf("recipe base url %q is not absolute", baseURL)
	}
	return connectionBase{scheme: base.Scheme, host: base.Host, basePath: base.Path}, nil
}

// pinFor extends the base with one verb template's literal root — the pin a
// single verb's calls re-validate against. Two verbs of one connection pin
// different roots; the scheme and host pin is shared.
func (b connectionBase) pinFor(template string) connectionPin {
	return connectionPin{
		scheme:   b.scheme,
		host:     b.host,
		basePath: b.basePath,
		root:     b.basePath + literalTemplatePrefix(template),
	}
}

// literalTemplatePrefix returns the template's literal leading segment —
// everything before the first {placeholder}, or the whole template when it
// declares none.
func literalTemplatePrefix(template string) string {
	if i := strings.IndexByte(template, '{'); i >= 0 {
		return template[:i]
	}
	return template
}

// errTooManyConnectionRedirects bounds the redirect chain (the web fetch
// client's rule).
var errTooManyConnectionRedirects = errors.New("too many redirects")

// offHostRedirectError marks a redirect whose target left the pinned base
// host (D2). The tool renders it as a clean tool error — the refused hop's
// full URL never enters the message.
type offHostRedirectError struct{ host string }

func (e *offHostRedirectError) Error() string {
	return "refused redirect to a different host (" + e.host + ")"
}

// connectionVerbTool is one generated verb facade: the method, path template,
// and parameter schema are pre-bound at generation; parameter values are
// validated and encoded at call time, the joined URL is re-validated against
// the pin, the credential rides a resolver function (never a captured
// string), and the response is capped exactly like web.fetch.
type connectionVerbTool struct {
	name        string
	description string
	method      string
	template    string
	params      []domain.RecipeVerbParam
	pin         connectionPin

	workspaceID  string
	connectionID string
	tokenHeader  string
	tokenScheme  string

	creds  ConnectionCredentials
	client *http.Client
}

// newConnectionVerbTool pre-binds one verb (add-connection-http D1): method
// and template fix here; only parameter values vary per call.
func newConnectionVerbTool(ref HTTPConnectionRef, verb domain.RecipeVerb, pin connectionPin, workspaceID string, client *http.Client, creds ConnectionCredentials) *connectionVerbTool {
	return &connectionVerbTool{
		name:        verb.Tool,
		description: verb.Description,
		method:      verb.Method,
		template:    verb.Path,
		params:      verb.Params,
		pin:         pin,

		workspaceID:  workspaceID,
		connectionID: ref.ConnectionID,
		tokenHeader:  ref.Recipe.TokenHeader,
		tokenScheme:  ref.Recipe.TokenScheme,

		creds:  creds,
		client: client,
	}
}

// Info returns the verb's schema surfaced to agentic models: the declared
// tool name verbatim (contract §5 — no renaming pass), the declared
// description verbatim, and the typed parameter schema generated from the
// declaration. Neither carries credential material — recipe validation
// guarantees the description, and the schema is derived from declared
// parameters only.
func (t *connectionVerbTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	params := make(map[string]*schema.ParameterInfo, len(t.params))
	for _, p := range t.params {
		params[p.Name] = &schema.ParameterInfo{
			Type:     recipeParamSchemaType(p.Type),
			Desc:     p.In + " parameter (" + p.Type + ")",
			Required: p.Required,
		}
	}
	return &schema.ToolInfo{
		Name:        t.name,
		Desc:        t.description,
		ParamsOneOf: schema.NewParamsOneOfByParams(params),
	}, nil
}

// recipeParamSchemaType maps the recipe parameter catalog onto JSON schema
// types (contract §5). Validation guarantees the catalog values; string is
// the safe default for anything else.
func recipeParamSchemaType(t string) schema.DataType {
	switch t {
	case domain.RecipeParamNumber:
		return schema.Number
	case domain.RecipeParamBoolean:
		return schema.Boolean
	default:
		return schema.String
	}
}

// InvokableRun binds the call: parse and type-check the arguments against the
// declaration, validate and encode path values (traversal and absolute-URL
// rejection, D2), join onto the pinned base, re-validate the joined URL
// structurally, resolve the credential fresh, attach the auth header
// server-side, and return the provider response capped like web.fetch.
func (t *connectionVerbTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	boundPath, rawQuery, err := t.bind(argumentsInJSON)
	if err != nil {
		return "", fmt.Errorf("%s: %w", t.name, err)
	}

	// Post-join pin re-validation (D2): the re-parsed, decoded view of the
	// joined URL must stay inside the pin before anything dials. The join
	// extends the pinned base path with the bound template.
	u, err := url.Parse(t.pin.scheme + "://" + t.pin.host + t.pin.basePath + boundPath + rawQuery)
	if err != nil {
		return "", fmt.Errorf("%s: bound url is malformed: %w", t.name, err)
	}
	if err := validatePinnedURL(u, t.pin); err != nil {
		return "", fmt.Errorf("%s: %w", t.name, err)
	}

	// Credential resolution per call (contract §3): the token exists only in
	// this frame and rides the request header — never a URL, never an error,
	// never the tool schema.
	token, err := t.creds.CredentialForConnection(ctx, t.workspaceID, t.connectionID)
	if err != nil {
		return "", fmt.Errorf("%s: resolve connection credential: %w", t.name, err)
	}
	headerValue := token
	if t.tokenScheme != "" {
		headerValue = t.tokenScheme + " " + token // the materializeServer composition rule (contract §1)
	}

	req, err := http.NewRequestWithContext(ctx, t.method, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("%s: build request: %w", t.name, err)
	}
	req.Header.Set(t.tokenHeader, headerValue)

	resp, err := t.client.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		var off *offHostRedirectError
		if errors.As(err, &off) {
			return "", fmt.Errorf("%s: %w", t.name, off)
		}
		if errors.Is(err, errTooManyConnectionRedirects) {
			return "", fmt.Errorf("%s: too many redirects", t.name)
		}
		return "", fmt.Errorf("%s: %w", t.name, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// Provider errors surface the status code and a size-capped body
		// excerpt only — never headers, never credential material (D3).
		body, _ := readCappedConnectionBody(resp.Body)
		excerpt := truncateConnectionExcerpt(strings.TrimSpace(body), maxConnectionErrorExcerpt)
		if excerpt == "" {
			return "", fmt.Errorf("%s: provider returned status %d", t.name, resp.StatusCode)
		}
		return "", fmt.Errorf("%s: provider returned status %d: %s", t.name, resp.StatusCode, excerpt)
	}

	// Content-type gating (D4): JSON and text are readable; anything else is
	// refused without quoting the header value.
	if !connectionContentTypeAllowed(resp.Header.Get("Content-Type")) {
		return "", fmt.Errorf("%s: provider returned a content type that is neither JSON nor text — no readable response", t.name)
	}

	body, truncated := readCappedConnectionBody(resp.Body)
	if body == "" {
		return "", fmt.Errorf("%s: empty response body", t.name)
	}
	if truncated {
		body += connectionTruncationMarker
	}
	// The provider's JSON (or text) verbatim, capped — matching how MCP tool
	// outputs already behave (add-connection-http design.md Risks).
	return body, nil
}

// bind type-checks the call's arguments against the declaration, validates
// and encodes path values, and joins the template plus the query string
// (add-connection-http D2, tasks.md 2.2). The returned path is the joined,
// escaped path (no query); rawQuery is "" or "?…". Values are validated
// before encoding and the join is structural by construction — every
// placeholder is a declared required path parameter (recipe validation), so
// no value can escape its segment.
func (t *connectionVerbTool) bind(argumentsInJSON string) (path, rawQuery string, err error) {
	var args map[string]json.RawMessage
	if strings.TrimSpace(argumentsInJSON) != "" {
		if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
			return "", "", fmt.Errorf("invalid arguments: %w", err)
		}
	}

	pathValues := make(map[string]string, len(t.params))
	query := url.Values{}
	for _, p := range t.params {
		raw, present := args[p.Name]
		if !present || string(raw) == "null" {
			if p.Required {
				return "", "", fmt.Errorf("missing required %s parameter %q", p.In, p.Name)
			}
			continue
		}

		var value string
		switch p.Type {
		case domain.RecipeParamNumber:
			var n float64
			if err := json.Unmarshal(raw, &n); err != nil {
				return "", "", fmt.Errorf("parameter %q must be a number", p.Name)
			}
			value = strconv.FormatFloat(n, 'f', -1, 64)
		case domain.RecipeParamBoolean:
			var b bool
			if err := json.Unmarshal(raw, &b); err != nil {
				return "", "", fmt.Errorf("parameter %q must be a boolean", p.Name)
			}
			value = strconv.FormatBool(b)
		default: // domain.RecipeParamString
			if err := json.Unmarshal(raw, &value); err != nil {
				return "", "", fmt.Errorf("parameter %q must be a string", p.Name)
			}
		}

		switch p.In {
		case domain.RecipeParamInPath:
			if err := validatePathParamValue(p.Name, value); err != nil {
				return "", "", err
			}
			pathValues[p.Name] = url.PathEscape(value)
		default: // domain.RecipeParamInQuery
			// Query values are structurally inert: url.Values percent-encodes
			// every metacharacter, so no value can alter the URL's
			// scheme/host/path — the post-join pin re-validation still guards
			// the whole URL.
			query.Set(p.Name, value)
		}
	}

	if len(query) > 0 {
		rawQuery = "?" + query.Encode()
	}
	return bindPathTemplate(t.template, pathValues), rawQuery, nil
}

// validatePathParamValue rejects a path parameter value that attempts
// traversal or carries an absolute URL (add-connection-http D2; the spec's
// "Parameter injection rejected" scenario) — before any encoding, before any
// network request. Path parameters bind as single URL segments: no
// separators, no dot segments, no protocol-relative or absolute URLs.
func validatePathParamValue(name, value string) error {
	switch {
	case value == "":
		return fmt.Errorf("path parameter %q is empty", name)
	case strings.ContainsAny(value, "/\\") || strings.Contains(value, ".."):
		return fmt.Errorf("path parameter %q must not contain path separators or traversal sequences", name)
	}
	if u, err := url.Parse(value); err == nil && (u.IsAbs() || u.Host != "") {
		return fmt.Errorf("path parameter %q must not carry an absolute URL", name)
	}
	return nil
}

// bindPathTemplate substitutes the encoded path values into the template's
// placeholders, in place. Every placeholder is a declared required path
// parameter (recipe validation), so the values map is complete.
func bindPathTemplate(template string, values map[string]string) string {
	var b strings.Builder
	b.Grow(len(template))
	for i := 0; i < len(template); {
		if template[i] == '{' {
			if j := strings.IndexByte(template[i:], '}'); j >= 0 {
				name := template[i+1 : i+j]
				if v, ok := values[name]; ok {
					b.WriteString(v) // already validated + escaped
					i += j + 1
					continue
				}
			}
		}
		b.WriteByte(template[i])
		i++
	}
	return b.String()
}

// validatePinnedURL re-validates the joined URL against the pin (D2 — the
// pin is structural at bind time): scheme and host must equal the pinned
// base, the decoded path must stay under the template's literal root, and no
// traversal segment may survive. This runs on the decoded view — what a
// server's router sees after percent-decoding.
func validatePinnedURL(u *url.URL, pin connectionPin) error {
	if u.Scheme != pin.scheme || u.Host != pin.host {
		return errors.New("bound request left the pinned base URL")
	}
	if !strings.HasPrefix(u.Path, pin.root) {
		return errors.New("bound path escaped the declared path template root")
	}
	for _, seg := range strings.Split(u.Path, "/") {
		if seg == ".." {
			return errors.New("bound path contains a traversal segment")
		}
	}
	return nil
}

// connectionContentTypeAllowed implements the D4 content-type gate: JSON
// (application/json or any +json structured syntax) and text pass; anything
// else — including a missing header — is refused.
func connectionContentTypeAllowed(contentType string) bool {
	mt := strings.ToLower(strings.TrimSpace(contentType))
	if i := strings.Index(mt, ";"); i >= 0 {
		mt = strings.TrimSpace(mt[:i])
	}
	if mt == "" {
		return false
	}
	if mt == "application/json" || strings.HasSuffix(mt, "+json") {
		return true
	}
	return strings.HasPrefix(mt, "text/")
}

// readCappedConnectionBody mirrors the web fetch tool's capped read loop
// (D4): same buffer size, same 1 MiB cap, same truncation flag semantics.
func readCappedConnectionBody(r io.Reader) (string, bool) {
	body := make([]byte, 0, 4096)
	buf := make([]byte, 32*1024)
	truncated := false
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if len(body)+n > maxConnectionResponseBytes {
				body = append(body, buf[:maxConnectionResponseBytes-len(body)]...)
				truncated = true
				break
			}
			body = append(body, buf[:n]...)
		}
		if err != nil {
			break
		}
	}
	return string(body), truncated
}

// truncateConnectionExcerpt caps a provider body excerpt at max runes,
// rune-safe, marking the cut.
func truncateConnectionExcerpt(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}
