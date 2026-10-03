package driver

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// Fixture actor credentials. These are fixture accounts the seeder creates
// through the admin API on the server under test — they carry no privilege
// beyond their workspace Member role and are not secrets (the eval harness
// precedents the same pattern with fixed per-actor passwords).
const (
	CaseyEmail    = "casey@bench.local"
	CaseyName     = "Casey Bench"
	CaseyPassword = "OnclawBenchCasey#2026"
	RileyEmail    = "riley@bench.local"
	RileyName     = "Riley Bench"
	RileyPassword = "OnclawBenchRiley#2026"

	// FixtureRole is the workspace role granted to both fixture actors.
	FixtureRole = "Member"

	// DefaultProviderAPIKeyEnv names the env var the provider bootstrap reads
	// when Options.ProviderAPIKeyEnv is empty.
	DefaultProviderAPIKeyEnv = "ONCLAW_BENCH_PROVIDER_KEY"

	// Provider bootstrap defaults. The bench protocol pins the model lane
	// (GLM-5.3-flash via Z.AI, design D4); the endpoint is a public gateway
	// address, not a secret — the key itself always comes from the env var.
	DefaultProviderType    = "openai-compatible"
	DefaultProviderBaseURL = "https://api.z.ai/api/paas/v4"
	DefaultProviderName    = "bench-provider"
)

// benchCoreTools is the toolset the bench lanes assume exposed on the fixture
// agent. Under the denylist a required tool is exposed exactly when it is
// absent from the agent's DisabledTools, so the reuse path subtracts these
// names from a stored denylist that denies any of them (the eval harness's
// evalAgentTools repair, widened to the bench's full core set).
var benchCoreTools = []string{
	"files.write",
	"files.read",
	"shell",
	"web.search",
	"memory.search",
	"subagents",
	"background_shell",
}

// Options parameterizes one provisioning pass.
type Options struct {
	BaseURL       string
	AdminEmail    string
	AdminPassword string
	WorkspaceSlug string
	AgentSlug     string
	// Model pins the fixture agent's model (the scoreboard's model pin);
	// required — agent creation and every lane task are real model runs.
	Model string
	// ContextWindow optionally pins the agent's context window (the
	// compaction lane's dial); nil leaves the server default.
	ContextWindow *int
	// ProviderAPIKeyEnv names the env var holding the provider API key used
	// to bootstrap a provider when the workspace has none; empty selects
	// DefaultProviderAPIKeyEnv. The key itself never enters code.
	ProviderAPIKeyEnv string
	// Optional provider bootstrap knobs; empty selects the pinned-protocol
	// defaults above (GLM-5.3-flash via Z.AI's OpenAI-compatible endpoint).
	ProviderType    string
	ProviderBaseURL string
	ProviderName    string
}

// Actor is one fixture identity the bench drives turns as, with its session
// credentials captured at provisioning time.
type Actor struct {
	Email    string
	Name     string
	Password string
	Role     string
	Token    string
	ChatKey  string
}

// Handles reports what one Provision pass set up. Every credential is the
// live handle the suite's runner needs; ChatKey is the primary chat actor's
// (Casey's) key.
type Handles struct {
	AdminToken  string
	AdminUserID string
	// Actors is keyed by email (driver.CaseyEmail, driver.RileyEmail).
	Actors    map[string]*Actor
	ChatKey   string
	Workspace *Workspace
	Agent     *Agent
	// Reused is true when the workspace (and with it the actors and agent)
	// already existed and was reused rather than created.
	Reused   bool
	Warnings []string
}

// Provision seeds the fixture workspace through the real HTTP API only
// (design D3): create-or-reuse workspace by slug, the two fixture actors
// (admin-created, logged in, membership granted, chat keys exchanged), a
// working model provider (reuse an enabled one, else bootstrap from the env
// key), and the fixture agent (create-or-reuse by slug, repointed at the
// pinned model and repaired denylist on reuse). Every step is idempotent, so
// repeated Provision calls converge without duplicating server state.
//
// Agent creation runs live prompt generation against the provider, so the
// provider leg must resolve to working model keys or the pass fails with an
// actionable error.
func Provision(ctx context.Context, opts Options) (*Handles, error) {
	if opts.BaseURL == "" {
		return nil, fmt.Errorf("driver provision: BaseURL is required")
	}
	if opts.AdminEmail == "" || opts.AdminPassword == "" {
		return nil, fmt.Errorf("driver provision: AdminEmail/AdminPassword are required (the admin identity creates or reuses the workspace and provisions members)")
	}
	if opts.WorkspaceSlug == "" || opts.AgentSlug == "" {
		return nil, fmt.Errorf("driver provision: WorkspaceSlug and AgentSlug are required")
	}
	if opts.Model == "" {
		return nil, fmt.Errorf("driver provision: Model is required — the bench protocol pins the model in every scoreboard and agent creation runs live prompt generation against it")
	}

	client := NewClient(opts.BaseURL, 0)
	h := &Handles{Actors: map[string]*Actor{}, Warnings: []string{}}

	adminToken, adminUserID, err := client.Login(ctx, opts.AdminEmail, opts.AdminPassword)
	if err != nil {
		return nil, fmt.Errorf("driver provision: admin login: %w", err)
	}
	h.AdminToken, h.AdminUserID = adminToken, adminUserID

	// 1. Workspace: create-or-reuse by slug. A 404 reads as absent (the
	// server cannot distinguish unknown from non-member slugs).
	ws, err := client.GetWorkspace(ctx, adminToken, opts.WorkspaceSlug)
	switch {
	case err == nil:
		h.Reused = true
	case IsNotFound(err):
		ws, err = client.CreateWorkspace(ctx, adminToken, workspaceName(opts.WorkspaceSlug), opts.WorkspaceSlug)
		if err != nil {
			return nil, fmt.Errorf("driver provision: creating workspace %s: %w", opts.WorkspaceSlug, err)
		}
	default:
		return nil, fmt.Errorf("driver provision: looking up workspace %s: %w", opts.WorkspaceSlug, err)
	}
	h.Workspace = ws

	// 2. Fixture actors: create-or-reuse, then login + membership + chat key.
	for _, spec := range []struct{ email, name, password string }{
		{CaseyEmail, CaseyName, CaseyPassword},
		{RileyEmail, RileyName, RileyPassword},
	} {
		actor, err := ensureActor(ctx, client, adminToken, ws.Slug, spec.email, spec.name, spec.password)
		if err != nil {
			return nil, err
		}
		h.Actors[spec.email] = actor
	}
	// Casey is the primary chat actor: lanes default to driving turns as the
	// first fixture member; Riley exists for cross-member and scoping legs.
	h.ChatKey = h.Actors[CaseyEmail].ChatKey

	// 3. Provider: reuse an enabled one, else bootstrap from the env key.
	providerID, err := ensureProvider(ctx, client, adminToken, ws.Slug, opts, h)
	if err != nil {
		return nil, err
	}

	// 4. Fixture agent: create-or-reuse by slug.
	agent, err := ensureAgent(ctx, client, adminToken, ws.Slug, providerID, opts)
	if err != nil {
		return nil, err
	}
	h.Agent = agent

	return h, nil
}

// ensureActor creates the actor via the admin API when possible, then logs
// it in, grants membership, and exchanges its chat key (the eval harness's
// ensureActor pattern). Without admin rights the create may be refused — the
// login proves existence, and a login failure with the fixture password is
// reported with the reset instruction.
func ensureActor(ctx context.Context, client *Client, adminToken, wsSlug, email, name, password string) (*Actor, error) {
	_, err := client.CreateAdminUser(ctx, adminToken, email, name, password)
	if err != nil && !IsForbidden(err) && !IsConflict(err) {
		return nil, fmt.Errorf("driver provision: creating fixture user %s: %w", email, err)
	}

	token, _, err := client.Login(ctx, email, password)
	if err != nil {
		if IsUnauthorized(err) || IsForbidden(err) {
			return nil, fmt.Errorf("driver provision: fixture user %s exists but the fixture password does not match (or the credentials lack the rights to create it); reset the user or run with matching admin credentials: %w", email, err)
		}
		return nil, fmt.Errorf("driver provision: logging in fixture user %s: %w", email, err)
	}

	if err := client.AddMember(ctx, adminToken, wsSlug, email, FixtureRole); err != nil {
		return nil, fmt.Errorf("driver provision: adding %s to workspace %s: %w", email, wsSlug, err)
	}

	key, err := client.ExchangeChatKey(ctx, token, wsSlug)
	if err != nil {
		return nil, fmt.Errorf("driver provision: exchanging chat key for %s: %w", email, err)
	}
	return &Actor{Email: email, Name: name, Password: password, Role: FixtureRole, Token: token, ChatKey: key}, nil
}

// ensureProvider returns a usable provider id: the workspace's first enabled
// provider when one exists, else a freshly created one keyed from the env
// var named by Options.ProviderAPIKeyEnv. With neither, it fails with the
// actionable message the live path needs — agent creation runs live prompt
// generation, so no-provider cannot be worked around later.
func ensureProvider(ctx context.Context, client *Client, adminToken, wsSlug string, opts Options, h *Handles) (string, error) {
	providers, err := client.ListEnabledProviders(ctx, adminToken, wsSlug)
	if err != nil {
		return "", fmt.Errorf("driver provision: listing providers for workspace %s: %w", wsSlug, err)
	}
	if len(providers) > 0 {
		envName := opts.ProviderAPIKeyEnv
		if envName == "" {
			envName = DefaultProviderAPIKeyEnv
		}
		if os.Getenv(envName) != "" {
			h.Warnings = append(h.Warnings, fmt.Sprintf("workspace %s already has an enabled provider %q; the key in $%s was not used", wsSlug, providers[0].Name, envName))
		}
		return providers[0].ID, nil
	}

	envName := opts.ProviderAPIKeyEnv
	if envName == "" {
		envName = DefaultProviderAPIKeyEnv
	}
	key := os.Getenv(envName)
	if key == "" {
		return "", fmt.Errorf("driver provision: workspace %s has no enabled provider and $%s is not set — export %s=<api key for the pinned provider> (or configure a provider in the workspace first): agent creation runs live prompt generation against the provider, so the bench's live path needs working model keys", wsSlug, envName, envName)
	}

	pType := opts.ProviderType
	if pType == "" {
		pType = DefaultProviderType
	}
	baseURL := opts.ProviderBaseURL
	if baseURL == "" {
		baseURL = DefaultProviderBaseURL
	}
	pName := opts.ProviderName
	if pName == "" {
		pName = DefaultProviderName
	}
	payload := ProviderPayload{Type: pType, Name: pName, Key: &key, Enabled: boolPtr(true)}
	if baseURL != "" {
		payload.BaseURL = &baseURL
	}
	provider, err := client.CreateProvider(ctx, adminToken, wsSlug, payload)
	if err != nil {
		return "", fmt.Errorf("driver provision: creating provider %q in workspace %s (check the key in $%s): %w", pName, wsSlug, envName, err)
	}
	return provider.ID, nil
}

// ensureAgent creates-or-reuses the fixture agent. The reuse path pins the
// agent to the requested configuration: it repoints a divergent stored
// model, applies the requested context window, and subtracts benchCoreTools
// from a stored denylist that denies any of them — the PATCH replaces the
// denylist wholesale, so the repair only runs when the stored list actually
// denies a bench tool (repeated seeds stay idempotent and never clobber
// denylist entries the bench does not need).
func ensureAgent(ctx context.Context, client *Client, adminToken, wsSlug, providerID string, opts Options) (*Agent, error) {
	agent, err := client.GetAgent(ctx, adminToken, wsSlug, opts.AgentSlug)
	switch {
	case err == nil:
		patchModel := ""
		if opts.Model != "" && agent.Model != "" && agent.Model != opts.Model {
			patchModel = opts.Model
		}
		var patchWindow *int
		if opts.ContextWindow != nil && (agent.ContextWindow == nil || *agent.ContextWindow != *opts.ContextWindow) {
			patchWindow = opts.ContextWindow
		}
		var patchTools []string
		if denylistDeniesAny(agent.DisabledTools, benchCoreTools) {
			patchTools = subtractTools(agent.DisabledTools, benchCoreTools)
		}
		if patchModel != "" || patchWindow != nil || patchTools != nil {
			if err := client.PatchAgent(ctx, adminToken, wsSlug, agent.ID, patchModel, patchWindow, patchTools); err != nil {
				return nil, fmt.Errorf("driver provision: repointing agent %s (model %q, context window, tool denylist): %w", opts.AgentSlug, opts.Model, err)
			}
			if patchModel != "" {
				agent.Model = patchModel
			}
			if patchWindow != nil {
				agent.ContextWindow = patchWindow
			}
			if patchTools != nil {
				agent.DisabledTools = patchTools
			}
		}
		return agent, nil
	case IsNotFound(err):
		agent, cerr := client.CreateAgent(ctx, adminToken, wsSlug, providerID, opts.AgentSlug, fixtureAgentName, opts.Model)
		if cerr != nil {
			return nil, fmt.Errorf("driver provision: creating fixture agent %s (prompt generation runs against the provider — check its live keys): %w", opts.AgentSlug, cerr)
		}
		// The create body carries no context window; the pin lands via the
		// same PATCH path the reuse path uses.
		if opts.ContextWindow != nil {
			if err := client.PatchAgent(ctx, adminToken, wsSlug, agent.ID, "", opts.ContextWindow, nil); err != nil {
				return nil, fmt.Errorf("driver provision: pinning context window on agent %s: %w", opts.AgentSlug, err)
			}
			agent.ContextWindow = opts.ContextWindow
		}
		return agent, nil
	default:
		return nil, fmt.Errorf("driver provision: looking up agent %s: %w", opts.AgentSlug, err)
	}
}

const fixtureAgentName = "Bench Fixture Agent"

func boolPtr(b bool) *bool { return &b }

// denylistDeniesAny reports whether the stored denylist carries any of the
// named tools.
func denylistDeniesAny(denylist, want []string) bool {
	denied := make(map[string]struct{}, len(denylist))
	for _, t := range denylist {
		denied[t] = struct{}{}
	}
	for _, t := range want {
		if _, ok := denied[t]; ok {
			return true
		}
	}
	return false
}

// subtractTools returns the denylist with every named tool removed (order
// preserved) — the repaired denylist the reuse path PATCHes in.
func subtractTools(denylist, remove []string) []string {
	removeSet := make(map[string]struct{}, len(remove))
	for _, t := range remove {
		removeSet[t] = struct{}{}
	}
	kept := make([]string, 0, len(denylist))
	for _, t := range denylist {
		if _, ok := removeSet[t]; !ok {
			kept = append(kept, t)
		}
	}
	return kept
}

// workspaceName derives a human workspace name from the slug ("bench-ws" →
// "Bench Ws Workspace") — the eval harness's same convention.
func workspaceName(slug string) string {
	parts := strings.Split(slug, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	name := strings.Join(parts, " ")
	if name == "" {
		name = "Bench"
	}
	return name + " Workspace"
}
