package teams

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// stubAgentCreator records SpawnAgent calls and persists a real agent row —
// the fake store validates agent references on the roster write, so the stub
// must create the row the roster will point at.
type stubAgentCreator struct {
	st      store.Store
	agents  []domain.Agent
	request []SpawnAgentRequest
	err     error
}

func (s *stubAgentCreator) SpawnAgent(_ context.Context, req SpawnAgentRequest) (domain.Agent, error) {
	if s.err != nil {
		return domain.Agent{}, s.err
	}
	s.request = append(s.request, req)
	provs, err := s.st.Providers().ListForWorkspace(nil, req.WorkspaceID)
	if err != nil || len(provs) == 0 {
		return domain.Agent{}, fmt.Errorf("stub creator: no provider in workspace %s", req.WorkspaceID)
	}
	spawned := domain.Agent{
		WorkspaceID: req.WorkspaceID,
		Name:        req.Name,
		Slug:        req.Slug,
		Role:        req.Role,
		Brief:       req.Brief,
		ProviderID:  provs[0].ID,
		Model:       "gpt-4",
	}
	if err := s.st.Agents().Create(nil, &spawned); err != nil {
		return domain.Agent{}, err
	}
	s.agents = append(s.agents, spawned)
	return spawned, nil
}

// newMaterializeEnv seeds a workspace with one provider and three existing
// agents (distinct agents for the mix-binding slots), and returns the store
// plus the materializer wired to a stub creator.
func newMaterializeEnv(t *testing.T) (store.Store, *stubAgentCreator, *Materializer, *domain.Workspace, *domain.User) {
	t.Helper()
	st := storefake.New()
	ws := &domain.Workspace{Name: "Acme", Slug: "acme"}
	if err := st.Workspaces().Create(nil, ws); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	user := &domain.User{Email: "sarah@example.com", Name: "Sarah Chen"}
	if err := st.Users().Create(nil, user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	// The store requires the human channel member to be a workspace member,
	// so seed the owner role and the membership row.
	role := &domain.Role{WorkspaceID: ws.ID, Name: domain.RoleOwner, IsOwner: true, Permissions: domain.OwnerPermissions, BuiltIn: true}
	if err := st.Roles().Create(nil, role); err != nil {
		t.Fatalf("seed role: %v", err)
	}
	if err := st.Members().Add(nil, &domain.Member{WorkspaceID: ws.ID, UserID: user.ID, RoleID: role.ID}); err != nil {
		t.Fatalf("seed membership: %v", err)
	}
	prov := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "Test Provider", KeyCiphertext: "sk"}
	if err := st.Providers().Create(nil, prov); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	for _, seed := range []struct{ slug, name string }{{"beacon", "Beacon"}, {"atlas", "Atlas"}, {"monitor", "Monitor"}} {
		agent := &domain.Agent{WorkspaceID: ws.ID, Slug: seed.slug, Name: seed.name, ProviderID: prov.ID, Model: "gpt-4"}
		if err := st.Agents().Create(nil, agent); err != nil {
			t.Fatalf("seed agent %s: %v", seed.slug, err)
		}
	}

	creator := &stubAgentCreator{st: st}
	mat := NewMaterializer(st.Channels(), st.Agents(), creator)
	return st, creator, mat, ws, user
}

// TestMaterialize_HappyPathSpawnAndExistingMix runs the full Software Team:
// three slots spawn, three bind the existing agent; the channel carries the
// conventions prefill, the human creator joins, and the scrum master lands as
// facilitator.
func TestMaterialize_HappyPathSpawnAndExistingMix(t *testing.T) {
	st, creator, mat, ws, user := newMaterializeEnv(t)

	architect, err := st.Agents().BySlug(nil, ws.ID, "beacon")
	if err != nil {
		t.Fatalf("load seeded architect: %v", err)
	}
	backend, err := st.Agents().BySlug(nil, ws.ID, "atlas")
	if err != nil {
		t.Fatalf("load seeded backend: %v", err)
	}
	tester, err := st.Agents().BySlug(nil, ws.ID, "monitor")
	if err != nil {
		t.Fatalf("load seeded tester: %v", err)
	}

	res, err := mat.Materialize(context.Background(), ws.ID, user.ID, MaterializeRequest{
		TemplateID: "software-team",
		Name:       "Build Dark Mode",
		Slug:       "dark-mode",
		Purpose:    "Ship dark mode.",
		Slots: map[string]SlotBinding{
			"pm":           {Spawn: true},
			"architect":    {AgentID: architect.ID},
			"scrum-master": {Spawn: true},
			"frontend":     {Spawn: true},
			"backend":      {AgentID: backend.ID},
			"tester":       {AgentID: tester.ID},
		},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}

	if res.Channel.Slug != "dark-mode" || res.Channel.WorkspaceID != ws.ID {
		t.Errorf("unexpected channel: %+v", res.Channel)
	}
	if res.Channel.Conventions == "" {
		t.Error("channel must carry the template's conventions prefill (design D5)")
	}
	if res.Channel.CreatedBy == nil || *res.Channel.CreatedBy != user.ID {
		t.Errorf("expected created_by to carry the creator, got %+v", res.Channel.CreatedBy)
	}

	// Six agent slots + the human creator.
	if len(res.Members) != 7 {
		t.Fatalf("expected 7 roster rows (6 agents + human creator), got %d", len(res.Members))
	}
	if len(res.CreatedAgents) != 3 {
		t.Fatalf("expected 3 spawned agents, got %d", len(res.CreatedAgents))
	}

	// Role-informed spawn inputs: slot title is the name, the role prompt hint
	// rides the brief (promptgen's identity input).
	for _, req := range creator.request {
		if req.Name == "" || req.Brief == "" || req.Role == "" {
			t.Errorf("spawn request missing role-informed fields: %+v", req)
		}
	}

	// The facilitator slot materializes with the facilitator roster role.
	facilitators := 0
	for _, m := range res.Members {
		if m.Role == domain.ChannelMemberRoleFacilitator {
			facilitators++
			if m.MemberType != domain.ChannelMemberTypeAgent || m.AgentID == "" {
				t.Errorf("facilitator role on a non-agent row: %+v", m)
			}
		}
		if m.MemberType == domain.ChannelMemberTypeUser && m.UserID != user.ID {
			t.Errorf("unexpected human member: %+v", m)
		}
	}
	if facilitators != 1 {
		t.Errorf("expected exactly one facilitator member, got %d", facilitators)
	}

	// Roster specializations come from the slots.
	roster, err := st.Channels().ListChannelMembers(nil, ws.ID, res.Channel.ID)
	if err != nil {
		t.Fatalf("list roster: %v", err)
	}
	specs := map[string]bool{}
	for _, m := range roster {
		specs[m.Specialization] = true
	}
	if len(specs) < 6 {
		t.Errorf("expected six distinct specializations on the roster, got %+v", specs)
	}
}

// TestMaterialize_Validation covers the fielded 422 paths: unknown template
// (handled as not-found), blank name/slug, missing slot bindings, malformed
// bindings, and unknown slot ids.
func TestMaterialize_Validation(t *testing.T) {
	_, _, mat, ws, user := newMaterializeEnv(t)
	ctx := context.Background()

	base := func() MaterializeRequest {
		return MaterializeRequest{
			TemplateID: "software-team",
			Name:       "Team",
			Slug:       "team",
			Slots: map[string]SlotBinding{
				"pm": {Spawn: true}, "architect": {Spawn: true}, "scrum-master": {Spawn: true},
				"frontend": {Spawn: true}, "backend": {Spawn: true}, "tester": {Spawn: true},
			},
		}
	}

	// Unknown template is a not-found.
	_, err := mat.Materialize(ctx, ws.ID, user.ID, MaterializeRequest{Name: "x", Slug: "x"})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown template: expected ErrNotFound, got %v", err)
	}

	// Blank name and slug.
	_, err = mat.Materialize(ctx, ws.ID, user.ID, MaterializeRequest{TemplateID: "software-team"})
	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("blank identity: expected ValidationError, got %v", err)
	}
	if !hasField(verr, "name") || !hasField(verr, "slug") {
		t.Errorf("expected name+slug details, got %+v", verr.Fields)
	}

	// A missing slot is fielded and lists the missing id.
	req := base()
	delete(req.Slots, "tester")
	_, err = mat.Materialize(ctx, ws.ID, user.ID, req)
	if !errors.As(err, &verr) {
		t.Fatalf("missing slot: expected ValidationError, got %v", err)
	}
	if !hasField(verr, "slots") {
		t.Errorf("expected a slots detail, got %+v", verr.Fields)
	}
	if verr.Error() == "" || !errors.Is(err, err) {
		t.Error("validation error must render")
	}

	// A binding with neither agent_id nor spawn is fielded per slot.
	req = base()
	req.Slots["pm"] = SlotBinding{}
	_, err = mat.Materialize(ctx, ws.ID, user.ID, req)
	if !errors.As(err, &verr) {
		t.Fatalf("empty binding: expected ValidationError, got %v", err)
	}
	if !hasField(verr, "slots.pm") {
		t.Errorf("expected slots.pm detail, got %+v", verr.Fields)
	}

	// A binding with both agent_id and spawn is fielded per slot.
	req = base()
	req.Slots["pm"] = SlotBinding{AgentID: "a", Spawn: true}
	_, err = mat.Materialize(ctx, ws.ID, user.ID, req)
	if !errors.As(err, &verr) {
		t.Fatalf("both binding: expected ValidationError, got %v", err)
	}
	if !hasField(verr, "slots.pm") {
		t.Errorf("expected slots.pm detail, got %+v", verr.Fields)
	}

	// Unknown slot ids are rejected.
	req = base()
	req.Slots["ghost"] = SlotBinding{Spawn: true}
	_, err = mat.Materialize(ctx, ws.ID, user.ID, req)
	if !errors.As(err, &verr) {
		t.Fatalf("unknown slot: expected ValidationError, got %v", err)
	}
	if !hasField(verr, "slots.ghost") {
		t.Errorf("expected slots.ghost detail, got %+v", verr.Fields)
	}
}

// TestMaterialize_ExistingBindingOutsideWorkspace fails fast with a 404-class
// error and leaves no channel behind.
func TestMaterialize_ExistingBindingOutsideWorkspace(t *testing.T) {
	st, _, mat, ws, user := newMaterializeEnv(t)

	other := &domain.Workspace{Name: "Other", Slug: "other"}
	if err := st.Workspaces().Create(nil, other); err != nil {
		t.Fatalf("seed other workspace: %v", err)
	}
	otherProv := &domain.ProviderConfig{WorkspaceID: other.ID, Type: "openai", Name: "Other Provider", KeyCiphertext: "sk"}
	if err := st.Providers().Create(nil, otherProv); err != nil {
		t.Fatalf("seed other provider: %v", err)
	}
	foreign := &domain.Agent{WorkspaceID: other.ID, Slug: "beacon", Name: "Beacon", ProviderID: otherProv.ID, Model: "gpt-4"}
	if err := st.Agents().Create(nil, foreign); err != nil {
		t.Fatalf("seed foreign agent: %v", err)
	}

	req := MaterializeRequest{
		TemplateID: "software-team",
		Name:       "Team",
		Slug:       "team",
		Slots: map[string]SlotBinding{
			"pm": {Spawn: true}, "architect": {AgentID: foreign.ID}, "scrum-master": {Spawn: true},
			"frontend": {Spawn: true}, "backend": {Spawn: true}, "tester": {Spawn: true},
		},
	}
	_, err := mat.Materialize(context.Background(), ws.ID, user.ID, req)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("foreign binding: expected ErrNotFound, got %v", err)
	}
	channels, err := st.Channels().ListChannels(nil, ws.ID)
	if err != nil {
		t.Fatalf("list channels: %v", err)
	}
	if len(channels) != 0 {
		t.Errorf("fail-fast must leave no channel behind, got %+v", channels)
	}
}

// TestMaterialize_SlugConflict surfaces the store's channel slug conflict.
func TestMaterialize_SlugConflict(t *testing.T) {
	st, _, mat, ws, user := newMaterializeEnv(t)
	if err := st.Channels().CreateChannel(nil, &domain.Channel{WorkspaceID: ws.ID, Name: "Taken", Slug: "team"}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}

	req := MaterializeRequest{
		TemplateID: "software-team",
		Name:       "Team",
		Slug:       "team",
		Slots: map[string]SlotBinding{
			"pm": {Spawn: true}, "architect": {Spawn: true}, "scrum-master": {Spawn: true},
			"frontend": {Spawn: true}, "backend": {Spawn: true}, "tester": {Spawn: true},
		},
	}
	_, err := mat.Materialize(context.Background(), ws.ID, user.ID, req)
	if !errors.Is(err, domain.ErrChannelSlugConflict) {
		t.Fatalf("slug conflict: expected ErrChannelSlugConflict, got %v", err)
	}
}

// TestMaterialize_SpawnSlugCollision appends a numeric suffix when a spawned
// agent's derived slug is already taken in the workspace.
func TestMaterialize_SpawnSlugCollision(t *testing.T) {
	st, _, mat, ws, user := newMaterializeEnv(t)
	prov, err := st.Providers().ListForWorkspace(nil, ws.ID)
	if err != nil || len(prov) == 0 {
		t.Fatalf("seed provider lookup: %v %+v", err, prov)
	}
	if err := st.Agents().Create(nil, &domain.Agent{WorkspaceID: ws.ID, Slug: "pm", Name: "Existing PM", ProviderID: prov[0].ID, Model: "gpt-4"}); err != nil {
		t.Fatalf("seed pm-slug agent: %v", err)
	}

	req := MaterializeRequest{
		TemplateID: "software-team",
		Name:       "Team",
		Slug:       "team",
		Slots: map[string]SlotBinding{
			"pm": {Spawn: true}, "architect": {Spawn: true}, "scrum-master": {Spawn: true},
			"frontend": {Spawn: true}, "backend": {Spawn: true}, "tester": {Spawn: true},
		},
	}
	res, err := mat.Materialize(context.Background(), ws.ID, user.ID, req)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	for _, agent := range res.CreatedAgents {
		if agent.Slug == "pm" {
			t.Errorf("spawned agent collided with the existing pm slug: %+v", agent)
		}
	}
	found := false
	for _, agent := range res.CreatedAgents {
		if agent.Slug == "pm-2" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected the pm spawn to fall back to pm-2, got %+v", res.CreatedAgents)
	}
}

func hasField(err *ValidationError, field string) bool {
	for _, f := range err.Fields {
		if f.Field == field {
			return true
		}
	}
	return false
}
