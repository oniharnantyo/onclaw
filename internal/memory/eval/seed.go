package eval

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Seeder provisions the fixture workspace through the real HTTP API: the
// fixture actors, their memberships, the fixture agent (against an existing
// enabled provider — live model keys are required for prompt generation),
// every scripted session turn through the real chat path, and finally the
// bounded wait for background ingestion (the pipeline is async by design;
// the wait polls the notes API as the fixture owner — design D4 — because
// the caller-visible listing is the only HTTP surface that can observe
// ingestion).
//
// Nothing is ever promoted: Sari's private facts must stay user-visibility
// for the scope questions to mean anything.
type Seeder struct {
	client *APIClient
	// AdminEmail/AdminPassword are the --email/--password credentials: the
	// identity that creates or reuses the workspace and provisions members.
	AdminEmail    string
	AdminPassword string
	// PollInterval is the notes-poll cadence for the ingestion wait;
	// <= 0 selects the 5s default (tests shrink it).
	PollInterval time.Duration
}

// SeedOptions parameterizes one seeding pass.
type SeedOptions struct {
	WorkspaceSlug string
	AgentSlug     string
	Model         string        // optional provider model override for the agent
	IngestWait    time.Duration // bounded wait for background ingestion
	RunID         string        // session-id prefix for idempotent re-runs
}

// SeedResult reports what the seeding pass did.
type SeedResult struct {
	Workspace    *Workspace
	Agent        *Agent
	Reused       bool
	Sessions     int
	Turns        int
	NotesAPILive bool
	ScopeAudit   ScopeAudit
	Notes        []string // operational warnings, folded into the scoreboard
}

// actorKey bundles one actor's credentials and chat key for the run.
type actorKey struct {
	Email string
	Token string
	Key   string
}

// Seed runs the whole provisioning pass. It is idempotent per workspace slug:
// existing users/memberships/agent are reused, sessions are re-posted under a
// fresh run-scoped session id (a re-run seeds a second corpus — use a fresh
// workspace slug for a clean scoreboard).
func (s *Seeder) Seed(ctx context.Context, opts SeedOptions) (*SeedResult, error) {
	res := &SeedResult{Notes: []string{}}

	adminToken, _, err := s.client.Login(ctx, s.AdminEmail, s.AdminPassword)
	if err != nil {
		return nil, err
	}

	// 1. Workspace: create-or-reuse by slug.
	ws, err := s.client.GetWorkspace(ctx, adminToken, opts.WorkspaceSlug)
	switch {
	case err == nil:
		res.Reused = true
	case isNotFound(err):
		ws, err = s.client.CreateWorkspace(ctx, adminToken, workspaceName(opts.WorkspaceSlug), opts.WorkspaceSlug)
		if err != nil {
			return nil, fmt.Errorf("creating workspace %s: %w", opts.WorkspaceSlug, err)
		}
	default:
		return nil, fmt.Errorf("looking up workspace %s: %w", opts.WorkspaceSlug, err)
	}
	res.Workspace = ws

	// 2. Fixture actors: create (superadmin), else reuse by logging in.
	actors := map[string]*actorKey{}
	for _, a := range Fixture.Actors {
		ak, err := s.ensureActor(ctx, adminToken, ws.Slug, a)
		if err != nil {
			return nil, err
		}
		actors[a.Email] = ak
	}

	// 3. Fixture agent: create against the first enabled provider, or reuse.
	agent, err := s.ensureAgent(ctx, adminToken, ws.Slug, opts)
	if err != nil {
		return nil, err
	}
	res.Agent = agent

	// 4. Scripted sessions through the real chat path.
	for _, script := range Fixture.Sessions {
		ak := actors[script.Actor]
		if ak == nil {
			return nil, fmt.Errorf("session %s: no chat key for actor %s", script.ID, script.Actor)
		}
		sessionID := fmt.Sprintf("%s-%s", opts.RunID, script.ID)
		for i, turn := range script.Turns {
			resp, err := s.client.ChatTurn(ctx, ak.Key, agent.Slug, turn, sessionID)
			if err != nil {
				return nil, fmt.Errorf("session %s turn %d: %w", script.ID, i+1, err)
			}
			if st, _ := resp["status"].(string); st != "completed" {
				return nil, fmt.Errorf("session %s turn %d: run status %q (need completed): %v", script.ID, i+1, st, resp["error"])
			}
			res.Turns++
		}
		res.Sessions++
	}

	// 5. Bounded wait for background ingestion, then the store-level scope
	// audit through the notes API (when present). The wait polls as the
	// fixture owner (design D4): the notes API filters to the caller's
	// visible set and the admin identity holds no membership rows in the
	// fixture workspace, so an admin-token wait can never see the corpus
	// land. The scope audit below stays admin-driven on purpose.
	owner, ok := actors[fixtureOwnerEmail]
	if !ok || owner == nil {
		return nil, fmt.Errorf("ingestion wait: fixture owner %s missing from the provisioned actors (fixture wiring bug — refusing to wait as an identity that cannot see the notes)", fixtureOwnerEmail)
	}
	notesLive := s.waitForIngestion(ctx, owner.Token, ws.Slug, opts, res)
	res.NotesAPILive = notesLive
	if notesLive {
		res.ScopeAudit = s.scopeAudit(ctx, adminToken, ws.Slug)
	} else {
		res.ScopeAudit = ScopeAudit{Available: false, Note: "memory notes REST endpoint absent; scope asserted behaviorally only"}
	}
	return res, nil
}

// ensureActor creates the actor via the admin API when possible, then logs in
// and exchanges a chat key. Without superadmin rights the harness tolerates
// actors that already exist (login proves existence) and fails clearly
// otherwise.
func (s *Seeder) ensureActor(ctx context.Context, adminToken, wsSlug string, a Actor) (*actorKey, error) {
	_, err := s.client.CreateAdminUser(ctx, adminToken, a.Email, a.Name, a.Password)
	if err != nil && !isForbidden(err) && !isConflict(err) {
		return nil, fmt.Errorf("provisioning fixture user %s: %w", a.Email, err)
	}

	token, _, err := s.client.Login(ctx, a.Email, a.Password)
	if err != nil {
		if isUnauthorized(err) || isForbidden(err) {
			return nil, fmt.Errorf("fixture user %s exists but the fixture password does not match (or credentials lack superadmin rights to create it); reset the user or run with superadmin --email/--password: %w", a.Email, err)
		}
		return nil, fmt.Errorf("logging in fixture user %s: %w", a.Email, err)
	}

	if err := s.client.AddMember(ctx, adminToken, wsSlug, a.Email, a.Role); err != nil {
		return nil, fmt.Errorf("adding %s to workspace %s: %w", a.Email, wsSlug, err)
	}

	key, err := s.client.ExchangeChatKey(ctx, token, wsSlug)
	if err != nil {
		return nil, fmt.Errorf("exchanging chat key for %s: %w", a.Email, err)
	}
	return &actorKey{Email: a.Email, Token: token, Key: key}, nil
}

// ensureAgent creates-or-reuses the fixture agent. Creation runs live prompt
// generation against the provider, so a missing/broken provider fails here.
// The reuse path repairs agents that deny a tool the self-search leg needs:
// memory.search exposed is the fixture's load-bearing provisioning (the
// denylist exposes it unless the agent names it), so a stored denylist
// carrying one of evalAgentTools is patched to exclude them.
func (s *Seeder) ensureAgent(ctx context.Context, adminToken, wsSlug string, opts SeedOptions) (*Agent, error) {
	agent, err := s.client.GetAgent(ctx, adminToken, wsSlug, opts.AgentSlug)
	switch {
	case err == nil:
		if opts.Model != "" && agent.Model != "" && agent.Model != opts.Model {
			if err := s.client.PatchAgentModel(ctx, adminToken, wsSlug, agent.ID, "", opts.Model); err != nil {
				return nil, fmt.Errorf("repointing agent %s at model %s: %w", opts.AgentSlug, opts.Model, err)
			}
			agent.Model = opts.Model
		}
		if denylistDeniesAny(agent, evalAgentTools) {
			repaired := subtractAgentTools(agent.DisabledTools, evalAgentTools)
			if err := s.client.PatchAgentDisabledTools(ctx, adminToken, wsSlug, agent.ID, repaired); err != nil {
				return nil, fmt.Errorf("exposing %v on agent %s: %w", evalAgentTools, opts.AgentSlug, err)
			}
			agent.DisabledTools = repaired
		}
		return agent, nil
	case isNotFound(err):
		providers, perr := s.client.ListEnabledProviders(ctx, adminToken, wsSlug)
		if perr != nil {
			return nil, fmt.Errorf("listing providers for agent creation: %w", perr)
		}
		if len(providers) == 0 {
			return nil, fmt.Errorf("workspace %s has no enabled provider: configure one with live model keys first (the real run path requires them)", wsSlug)
		}
		agent, cerr := s.client.CreateAgent(ctx, adminToken, wsSlug, providers[0].ID, opts.AgentSlug, opts.Model)
		if cerr != nil {
			return nil, fmt.Errorf("creating fixture agent %s (prompt generation runs against the provider — check its live keys): %w", opts.AgentSlug, cerr)
		}
		return agent, nil
	default:
		return nil, fmt.Errorf("looking up agent %s: %w", opts.AgentSlug, err)
	}
}

// waitForIngestion polls the memory notes endpoint AS THE FIXTURE OWNER
// (design D4) until the corpus has landed or the bounded wait elapses. The
// notes API filters to the caller's visible set (shared + own-user rows), so
// the owner is the least-privileged principal that can actually observe
// ingestion — the admin identity holds no membership rows in the fixture
// workspace and would burn the whole budget believing the store empty. The
// wait ends early once ingestFloorNotes() notes are visible; otherwise it
// falls back to the original quiescence rule (three consecutive polls with a
// stable, non-zero count) and finally the deadline. When the endpoint is
// absent the harness waits the fixed budget and reports that nothing was
// observable — a zero-visibility timeout is a meaningful observation, not an
// error.
func (s *Seeder) waitForIngestion(ctx context.Context, ownerToken, wsSlug string, opts SeedOptions, res *SeedResult) bool {
	budget := opts.IngestWait
	if budget <= 0 {
		budget = 180 * time.Second
	}
	deadline := time.Now().Add(budget)
	if cd, ok := ctx.Deadline(); ok && cd.Before(deadline) {
		deadline = cd
	}

	notes, live, err := s.client.ListNotes(ctx, ownerToken, wsSlug, "")
	if err != nil {
		res.Notes = append(res.Notes, fmt.Sprintf("notes listing failed as the fixture owner %s (%v); proceeding after fixed wait", fixtureOwnerEmail, err))
		live = false
	}
	if !live {
		select {
		case <-time.After(budget):
		case <-ctx.Done():
		}
		res.Notes = append(res.Notes, fmt.Sprintf("memory notes REST endpoint absent; waited fixed %s for ingestion (nothing was pollable as the fixture owner)", budget))
		return false
	}

	floor := ingestFloorNotes()
	if floor > 0 && len(notes) >= floor {
		return true
	}

	const defaultPoll = 5 * time.Second
	poll := s.PollInterval
	if poll <= 0 {
		poll = defaultPoll
	}
	stable, last := 0, len(notes)
	for {
		if time.Now().After(deadline) {
			res.Notes = append(res.Notes, fmt.Sprintf("ingestion wait elapsed with %d notes visible to the fixture owner — pipeline stored nothing or visibility hides everything", last))
			return true
		}
		select {
		case <-time.After(poll):
		case <-ctx.Done():
			return true
		}
		notes, _, err := s.client.ListNotes(ctx, ownerToken, wsSlug, "")
		if err != nil {
			res.Notes = append(res.Notes, fmt.Sprintf("notes poll failed as the fixture owner %s (%v)", fixtureOwnerEmail, err))
			return true
		}
		if floor > 0 && len(notes) >= floor {
			return true
		}
		if len(notes) == last && last > 0 {
			stable++
			if stable >= 2 { // 3 consecutive identical counts
				return true
			}
			continue
		}
		stable, last = 0, len(notes)
	}
}

// fixtureOwnerEmail is the identity the ingestion wait polls as (design D4):
// the fixture actor whose scripted private session anchors the scope test.
// Unlike the admin, this identity owns note rows, so its notes-API view
// actually reflects ingestion landing.
const fixtureOwnerEmail = "sari@eval.local"

// ingestFloorNotes derives the note count the ingestion wait treats as "the
// corpus has landed" (design D4's early exit). The fixture encodes no exact
// expected count — extraction is model-derived (the live baseline turned the
// 18 scripted turns into 39 notes) — so the floor is a conservative
// derivative of the fixture's scripted "remember this" turns: one note per
// turn in the fact and private sessions, each of which states at least one
// durable fact (the noise session states none and extraction is asked to
// skip it). Extraction merging/dedup can push the real count below one per
// turn, and workspace visibility can keep the polling owner's view smaller
// than the whole corpus, so the floor is a floor, not an expectation: when
// the owner's visible set cannot reach it, the stable-count and deadline
// fallbacks in waitForIngestion terminate the wait instead. The floor can
// only shorten the wait, never lengthen it.
func ingestFloorNotes() int {
	floor := 0
	for _, sess := range Fixture.Sessions {
		if sess.Kind == "noise" {
			continue
		}
		floor += len(sess.Turns)
	}
	return floor
}

// scopeAudit drives the store-level cross-member visibility assertion through
// the notes REST API: as the workspace admin (not a party to Sari's DM) the
// private fact must not appear; as Sari it must. Both checks are soft —
// failures annotate the scoreboard rather than aborting the run.
func (s *Seeder) scopeAudit(ctx context.Context, adminToken, wsSlug string) ScopeAudit {
	audit := ScopeAudit{Available: true}

	probe := "emergency contact"
	adminNotes, _, err := s.client.ListNotes(ctx, adminToken, wsSlug, probe)
	if err != nil {
		audit.Note = fmt.Sprintf("notes API audit failed: %v", err)
		return audit
	}
	audit.BudiSees = countPrivateHits(adminNotes)

	sari := fixtureActorByEmail("sari@eval.local")
	if sari == nil {
		audit.Note = "sari-side audit skipped: fixture actor missing"
		return audit
	}
	sariToken, _, err := s.client.Login(ctx, sari.Email, sari.Password)
	if err != nil {
		audit.Note = fmt.Sprintf("sari-side audit skipped: %v", err)
		return audit
	}
	sariNotes, _, err := s.client.ListNotes(ctx, sariToken, wsSlug, probe)
	if err != nil {
		audit.Note = fmt.Sprintf("sari-side audit failed: %v", err)
		return audit
	}
	audit.SariSees = countPrivateHits(sariNotes)

	audit.Passed = audit.BudiSees == 0 && audit.SariSees >= 1
	if !audit.Passed {
		audit.Note = fmt.Sprintf("scope audit failed: third-party sees %d private rows, owner sees %d (want 0 and >=1)", audit.BudiSees, audit.SariSees)
	}
	return audit
}

// countPrivateHits counts notes whose content carries Sari's private markers.
func countPrivateHits(notes []Note) int {
	n := 0
	for _, note := range notes {
		content := strings.ToLower(note.Content)
		if strings.Contains(content, "0812-7788-9900") || strings.Contains(content, "sinta") {
			n++
		}
	}
	return n
}

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
		name = "Memory Eval"
	}
	return name + " Workspace"
}

// denylistDeniesAny reports whether the agent's stored denylist carries any
// of the named tools — under the denylist a required tool is exposed exactly
// when it is absent from DisabledTools (catalog minus denylist; the workspace
// gate defaults everything on for the seeder's fresh fixture workspaces), so
// denial is the only failure the reuse path must repair.
func denylistDeniesAny(a *Agent, want []string) bool {
	denied := make(map[string]struct{}, len(a.DisabledTools))
	for _, t := range a.DisabledTools {
		denied[t] = struct{}{}
	}
	for _, t := range want {
		if _, ok := denied[t]; ok {
			return true
		}
	}
	return false
}

// subtractAgentTools returns the denylist with every named tool removed
// (order preserved) — the repaired denylist the reuse path PATCHes in.
func subtractAgentTools(denylist, remove []string) []string {
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

func isNotFound(err error) bool     { return statusIs(err, 404) }
func isConflict(err error) bool     { return statusIs(err, 409) }
func isForbidden(err error) bool    { return statusIs(err, 403) }
func isUnauthorized(err error) bool { return statusIs(err, 401) }

// fixtureActorByEmail returns the fixture actor with the given email.
func fixtureActorByEmail(email string) *Actor {
	for i := range Fixture.Actors {
		if Fixture.Actors[i].Email == email {
			return &Fixture.Actors[i]
		}
	}
	return nil
}

func statusIs(err error, code int) bool {
	var ae *apiError
	if asErr(err, &ae) {
		return ae.Status == code
	}
	return false
}
