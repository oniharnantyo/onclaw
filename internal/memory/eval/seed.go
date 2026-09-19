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
// bounded wait for background ingestion (the pipeline is async by design).
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
	// audit through the notes API (when present).
	notesLive := s.waitForIngestion(ctx, adminToken, ws.Slug, opts, res)
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

// waitForIngestion polls the memory notes endpoint until quiescent: three
// consecutive polls with a stable, non-zero note count, or the bounded wait
// elapses. When the endpoint is absent the harness waits the fixed budget and
// warns — a zero-note timeout is itself a meaningful (baseline-shaped)
// observation, not an error.
func (s *Seeder) waitForIngestion(ctx context.Context, token, wsSlug string, opts SeedOptions, res *SeedResult) bool {
	budget := opts.IngestWait
	if budget <= 0 {
		budget = 180 * time.Second
	}
	deadline := time.Now().Add(budget)
	if cd, ok := ctx.Deadline(); ok && cd.Before(deadline) {
		deadline = cd
	}

	notes, live, err := s.client.ListNotes(ctx, token, wsSlug, "")
	if err != nil {
		res.Notes = append(res.Notes, fmt.Sprintf("notes listing failed (%v); proceeding after fixed wait", err))
		live = false
	}
	if !live {
		select {
		case <-time.After(budget):
		case <-ctx.Done():
		}
		res.Notes = append(res.Notes, fmt.Sprintf("memory notes REST endpoint absent; waited fixed %s for ingestion", budget))
		return false
	}

	const defaultPoll = 5 * time.Second
	poll := s.PollInterval
	if poll <= 0 {
		poll = defaultPoll
	}
	stable, last := 0, len(notes)
	for {
		if time.Now().After(deadline) {
			res.Notes = append(res.Notes, fmt.Sprintf("ingestion wait elapsed with %d notes (pipeline may be idle — expected on a pre-change baseline)", last))
			return true
		}
		select {
		case <-time.After(poll):
		case <-ctx.Done():
			return true
		}
		notes, _, err := s.client.ListNotes(ctx, token, wsSlug, "")
		if err != nil {
			res.Notes = append(res.Notes, fmt.Sprintf("notes poll failed (%v)", err))
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
