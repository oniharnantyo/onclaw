//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// chSeedChannelWorkspace creates a plain workspace for channel store tests.
func chSeedChannelWorkspace(t *testing.T, ctx context.Context, s store.Store, slug string) *domain.Workspace {
	t.Helper()
	ws := &domain.Workspace{Slug: slug, Name: "Channel WS " + slug}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("unexpected create workspace error: %v", err)
	}
	return ws
}

// chSeedUser creates a user account.
func chSeedUser(t *testing.T, ctx context.Context, s store.Store, email, name string) *domain.User {
	t.Helper()
	u := &domain.User{Email: email, Name: name}
	if err := s.Users().Create(ctx, u); err != nil {
		t.Fatalf("unexpected create user error: %v", err)
	}
	return u
}

// chSeedWorkspaceMember adds the user to the workspace (find-or-create the
// Owner builtin role for the role binding).
func chSeedWorkspaceMember(t *testing.T, ctx context.Context, s store.Store, ws *domain.Workspace, userID string) {
	t.Helper()
	role, err := s.Roles().FindByName(ctx, ws.ID, domain.RoleOwner)
	if errors.Is(err, domain.ErrNotFound) {
		role = &domain.Role{WorkspaceID: ws.ID, Name: domain.RoleOwner, IsOwner: true, Permissions: domain.OwnerPermissions, BuiltIn: true}
		if err := s.Roles().Create(ctx, role); err != nil {
			t.Fatalf("unexpected create role error: %v", err)
		}
	} else if err != nil {
		t.Fatalf("unexpected find role error: %v", err)
	}
	if err := s.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: userID, RoleID: role.ID}); err != nil {
		t.Fatalf("unexpected add member error: %v", err)
	}
}

// chSeedChannelAgent creates a workspace-scoped agent for channel member tests.
func chSeedChannelAgent(t *testing.T, ctx context.Context, s store.Store, ws *domain.Workspace, slug string) *domain.Agent {
	t.Helper()
	p := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "OpenAI " + slug, Enabled: true}
	if err := s.Providers().Create(ctx, p); err != nil {
		t.Fatalf("unexpected create provider error: %v", err)
	}
	a := &domain.Agent{WorkspaceID: ws.ID, Slug: slug, Name: "Agent " + slug, ProviderID: p.ID, Model: "gpt-4o"}
	if err := s.Agents().Create(ctx, a); err != nil {
		t.Fatalf("unexpected create agent error: %v", err)
	}
	return a
}

// chSeedChannel creates a channel in the workspace.
func chSeedChannel(t *testing.T, ctx context.Context, s store.Store, ws *domain.Workspace, name, slug string) *domain.Channel {
	t.Helper()
	c := &domain.Channel{WorkspaceID: ws.ID, Name: name, Slug: slug, Purpose: "header line"}
	if err := s.Channels().CreateChannel(ctx, c); err != nil {
		t.Fatalf("unexpected create channel error: %v", err)
	}
	return c
}

func TestIntegration_ChannelStore_ChannelCRUD(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws1 := chSeedChannelWorkspace(t, ctx, s, "ws-chan-1")
	ws2 := chSeedChannelWorkspace(t, ctx, s, "ws-chan-2")

	// 1. Create a full channel row.
	channel := &domain.Channel{
		WorkspaceID: ws1.ID,
		Name:        "Production Ops",
		Slug:        "ops",
		Purpose:     "Coordinate production incident response.",
		Conventions: "Keep threads short; page via cron digests.",
	}
	if err := s.Channels().CreateChannel(ctx, channel); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	if channel.ID == "" {
		t.Fatal("expected channel ID to be assigned")
	}
	if channel.CreatedAt.IsZero() || channel.UpdatedAt.IsZero() {
		t.Fatal("expected timestamps to be set")
	}

	// 2. Get roundtrips the row.
	got, err := s.Channels().ChannelByID(ctx, ws1.ID, channel.ID)
	if err != nil {
		t.Fatalf("unexpected get error: %v", err)
	}
	if got.Name != "Production Ops" || got.Slug != "ops" || got.Purpose != "Coordinate production incident response." {
		t.Fatalf("unexpected channel: %+v", got)
	}
	if got.Conventions != "Keep threads short; page via cron digests." || got.CreatedBy != nil {
		t.Fatalf("unexpected conventions/created_by: %+v", got)
	}

	// 3. BySlug: exact and case-insensitive; unknown and cross-tenant are ErrNotFound.
	bySlug, err := s.Channels().ChannelBySlug(ctx, ws1.ID, "ops")
	if err != nil || bySlug.ID != channel.ID {
		t.Fatalf("unexpected by-slug result: %+v, err: %v", bySlug, err)
	}
	bySlugUpper, err := s.Channels().ChannelBySlug(ctx, ws1.ID, "OPS")
	if err != nil || bySlugUpper.ID != channel.ID {
		t.Fatalf("expected case-insensitive by-slug lookup, got %+v, err: %v", bySlugUpper, err)
	}
	if _, err := s.Channels().ChannelBySlug(ctx, ws1.ID, "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown slug, got %v", err)
	}
	if _, err := s.Channels().ChannelBySlug(ctx, ws2.ID, "ops"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant slug, got %v", err)
	}

	// 4. Cross-tenant and unknown lookups are indistinguishable (ErrNotFound).
	if _, err := s.Channels().ChannelByID(ctx, ws2.ID, channel.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant get, got %v", err)
	}
	if _, err := s.Channels().ChannelByID(ctx, ws1.ID, uuid.NewString()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown id, got %v", err)
	}
	if _, err := s.Channels().ChannelByID(ctx, ws1.ID, "invalid-uuid"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for invalid uuid, got %v", err)
	}

	// 5. List is workspace-scoped and ordered.
	second := chSeedChannel(t, ctx, s, ws1, "Incidents", "incidents")
	list1, err := s.Channels().ListChannels(ctx, ws1.ID)
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	if len(list1) != 2 || list1[0].ID != channel.ID || list1[1].ID != second.ID {
		t.Fatalf("expected 2 channels in ws1 ordered [ops, incidents], got %+v", list1)
	}
	list2, err := s.Channels().ListChannels(ctx, ws2.ID)
	if err != nil || len(list2) != 0 {
		t.Fatalf("expected 0 channels in ws2, got %d, err: %v", len(list2), err)
	}

	// 6. Validation errors on Create.
	if err := s.Channels().CreateChannel(ctx, nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for nil channel, got %v", err)
	}
	if err := s.Channels().CreateChannel(ctx, &domain.Channel{WorkspaceID: ws1.ID, Name: "Bad Slug", Slug: "Bad Slug"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for bad slug, got %v", err)
	}
	if err := s.Channels().CreateChannel(ctx, &domain.Channel{WorkspaceID: ws1.ID, Name: "  ", Slug: "blank-name"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for blank name, got %v", err)
	}
	if err := s.Channels().CreateChannel(ctx, &domain.Channel{WorkspaceID: "", Name: "Orphan", Slug: "orphan"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for missing workspace, got %v", err)
	}
	// Unknown workspace surfaces as ErrNotFound (FK violation).
	if err := s.Channels().CreateChannel(ctx, &domain.Channel{WorkspaceID: uuid.NewString(), Name: "Orphan", Slug: "orphan"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown workspace, got %v", err)
	}

	// 7. Update replaces editable fields only; created_at/created_by persist.
	got.Purpose = "Incident coordination."
	got.Name = "Production Operations"
	origCreated := got.CreatedAt
	origUpdated := got.UpdatedAt
	if err := s.Channels().UpdateChannel(ctx, got); err != nil {
		t.Fatalf("unexpected update error: %v", err)
	}
	reloaded, err := s.Channels().ChannelByID(ctx, ws1.ID, channel.ID)
	if err != nil {
		t.Fatalf("unexpected get after update: %v", err)
	}
	if reloaded.Name != "Production Operations" || reloaded.Purpose != "Incident coordination." {
		t.Fatalf("unexpected editable fields after update: %+v", reloaded)
	}
	if !reloaded.CreatedAt.Equal(origCreated) {
		t.Fatalf("expected created_at preserved: %v vs %v", origCreated, reloaded.CreatedAt)
	}
	if !reloaded.UpdatedAt.After(origUpdated) {
		t.Fatalf("expected updated_at to advance: %v vs %v", origUpdated, reloaded.UpdatedAt)
	}

	// Update on unknown id / cross-tenant is ErrNotFound.
	if err := s.Channels().UpdateChannel(ctx, &domain.Channel{
		ID: uuid.NewString(), WorkspaceID: ws1.ID, Name: "Ghost", Slug: "ghost",
	}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown update, got %v", err)
	}
	cross := *got
	cross.WorkspaceID = ws2.ID
	if err := s.Channels().UpdateChannel(ctx, &cross); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant update, got %v", err)
	}

	// 8. Delete is scoped; unknown and cross-tenant deletes are ErrNotFound.
	if err := s.Channels().DeleteChannel(ctx, ws2.ID, channel.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant delete, got %v", err)
	}
	if err := s.Channels().DeleteChannel(ctx, ws1.ID, uuid.NewString()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown delete, got %v", err)
	}
	if err := s.Channels().DeleteChannel(ctx, ws1.ID, channel.ID); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}
	if _, err := s.Channels().ChannelByID(ctx, ws1.ID, channel.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

// The schema's UNIQUE (workspace_id, slug) is exact-only (migration 000030);
// case-insensitive uniqueness is enforced at the store level with a lower(slug)
// pre-check, matching the fake. Exact duplicates additionally hit the DB
// constraint and map to the same sentinel as a backstop.
func TestIntegration_ChannelStore_SlugUniqueness(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws1 := chSeedChannelWorkspace(t, ctx, s, "ws-chan-slug-1")
	ws2 := chSeedChannelWorkspace(t, ctx, s, "ws-chan-slug-2")

	first := chSeedChannel(t, ctx, s, ws1, "Production Ops", "ops")

	// Exact duplicate rejected.
	err := s.Channels().CreateChannel(ctx, &domain.Channel{WorkspaceID: ws1.ID, Name: "Other Ops", Slug: "ops"})
	if !errors.Is(err, domain.ErrChannelSlugConflict) {
		t.Fatalf("expected ErrChannelSlugConflict for exact duplicate, got %v", err)
	}

	// Slug validation enforces lowercase kebab-case before uniqueness — a
	// case-variant slug is rejected as invalid at the store API. The
	// store-level lower(slug) pre-check and constraint remain the
	// case-insensitive backstop for unvalidated paths.
	err = s.Channels().CreateChannel(ctx, &domain.Channel{WorkspaceID: ws1.ID, Name: "Other Ops", Slug: "OPS"})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for non-kebab-case slug, got %v", err)
	}

	// Same slug in another workspace is fine.
	if err := s.Channels().CreateChannel(ctx, &domain.Channel{WorkspaceID: ws2.ID, Name: "Ops Two", Slug: "ops"}); err != nil {
		t.Fatalf("expected same slug in different workspace to succeed, got %v", err)
	}

	// Renaming onto another channel's slug rejected.
	second := chSeedChannel(t, ctx, s, ws1, "Incidents", "incidents")
	second.Slug = "ops"
	if err := s.Channels().UpdateChannel(ctx, second); !errors.Is(err, domain.ErrChannelSlugConflict) {
		t.Fatalf("expected ErrChannelSlugConflict for rename conflict, got %v", err)
	}

	// Keeping your own slug is a no-op rename.
	first.Slug = "ops"
	if err := s.Channels().UpdateChannel(ctx, first); err != nil {
		t.Fatalf("expected no-op rename of own slug to succeed, got %v", err)
	}
	got, err := s.Channels().ChannelByID(ctx, ws1.ID, first.ID)
	if err != nil {
		t.Fatalf("unexpected get error: %v", err)
	}
	if got.Slug != "ops" {
		t.Fatalf("expected unchanged slug, got %q", got.Slug)
	}
}

func TestIntegration_ChannelStore_Members(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws1 := chSeedChannelWorkspace(t, ctx, s, "ws-chan-mem-1")
	ws2 := chSeedChannelWorkspace(t, ctx, s, "ws-chan-mem-2")

	sarah := chSeedUser(t, ctx, s, "sarah@example.com", "Sarah")
	chSeedWorkspaceMember(t, ctx, s, ws1, sarah.ID)
	other := chSeedUser(t, ctx, s, "outsider@example.com", "Outsider")
	chSeedWorkspaceMember(t, ctx, s, ws2, other.ID)
	atlas := chSeedChannelAgent(t, ctx, s, ws1, "atlas")
	alien := chSeedChannelAgent(t, ctx, s, ws2, "alien-agent")

	channel := chSeedChannel(t, ctx, s, ws1, "Production Ops", "ops")

	// 1. Add a human and an agent with specializations.
	sarahMember := &domain.ChannelMember{
		WorkspaceID:    ws1.ID,
		ChannelID:      channel.ID,
		MemberType:     domain.ChannelMemberTypeUser,
		UserID:         sarah.ID,
		Specialization: "incident commander",
	}
	if err := s.Channels().AddChannelMember(ctx, sarahMember); err != nil {
		t.Fatalf("unexpected add user error: %v", err)
	}
	if sarahMember.ID == "" || sarahMember.AddedAt.IsZero() {
		t.Fatal("expected member id and added_at to be assigned")
	}
	atlasMember := &domain.ChannelMember{
		WorkspaceID:    ws1.ID,
		ChannelID:      channel.ID,
		MemberType:     domain.ChannelMemberTypeAgent,
		AgentID:        atlas.ID,
		Specialization: "metrics & dashboards",
	}
	if err := s.Channels().AddChannelMember(ctx, atlasMember); err != nil {
		t.Fatalf("unexpected add agent error: %v", err)
	}

	// 2. Duplicates rejected with the sentinel.
	err := s.Channels().AddChannelMember(ctx, &domain.ChannelMember{
		WorkspaceID: ws1.ID, ChannelID: channel.ID, MemberType: domain.ChannelMemberTypeUser, UserID: sarah.ID,
	})
	if !errors.Is(err, domain.ErrDuplicateChannelMember) {
		t.Fatalf("expected ErrDuplicateChannelMember for duplicate user, got %v", err)
	}
	err = s.Channels().AddChannelMember(ctx, &domain.ChannelMember{
		WorkspaceID: ws1.ID, ChannelID: channel.ID, MemberType: domain.ChannelMemberTypeAgent, AgentID: atlas.ID,
	})
	if !errors.Is(err, domain.ErrDuplicateChannelMember) {
		t.Fatalf("expected ErrDuplicateChannelMember for duplicate agent, got %v", err)
	}

	// 3. References outside the workspace are ErrNotFound (fake parity).
	err = s.Channels().AddChannelMember(ctx, &domain.ChannelMember{
		WorkspaceID: ws1.ID, ChannelID: channel.ID, MemberType: domain.ChannelMemberTypeUser, UserID: other.ID,
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for non-member user, got %v", err)
	}
	err = s.Channels().AddChannelMember(ctx, &domain.ChannelMember{
		WorkspaceID: ws1.ID, ChannelID: channel.ID, MemberType: domain.ChannelMemberTypeAgent, AgentID: alien.ID,
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-workspace agent, got %v", err)
	}

	// 4. Validation errors.
	if err := s.Channels().AddChannelMember(ctx, nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for nil member, got %v", err)
	}
	err = s.Channels().AddChannelMember(ctx, &domain.ChannelMember{
		WorkspaceID: ws1.ID, ChannelID: channel.ID, MemberType: domain.ChannelMemberTypeUser, UserID: sarah.ID, AgentID: atlas.ID,
	})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for both refs, got %v", err)
	}
	err = s.Channels().AddChannelMember(ctx, &domain.ChannelMember{
		WorkspaceID: ws1.ID, ChannelID: channel.ID, MemberType: "robot", UserID: sarah.ID,
	})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for unknown member type, got %v", err)
	}
	err = s.Channels().AddChannelMember(ctx, &domain.ChannelMember{
		WorkspaceID: ws1.ID, ChannelID: uuid.NewString(), MemberType: domain.ChannelMemberTypeUser, UserID: sarah.ID,
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown channel, got %v", err)
	}

	// 5. ByRef resolves roster rows; unknown triples are ErrNotFound.
	byRef, err := s.Channels().ChannelMemberByRef(ctx, ws1.ID, channel.ID, domain.ChannelMemberTypeUser, sarah.ID)
	if err != nil || byRef.ID != sarahMember.ID || byRef.Specialization != "incident commander" {
		t.Fatalf("unexpected by-ref user result: %+v, err: %v", byRef, err)
	}
	byRef, err = s.Channels().ChannelMemberByRef(ctx, ws1.ID, channel.ID, domain.ChannelMemberTypeAgent, atlas.ID)
	if err != nil || byRef.ID != atlasMember.ID {
		t.Fatalf("unexpected by-ref agent result: %+v, err: %v", byRef, err)
	}
	if _, err := s.Channels().ChannelMemberByRef(ctx, ws1.ID, channel.ID, domain.ChannelMemberTypeUser, other.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown by-ref, got %v", err)
	}
	if _, err := s.Channels().ChannelMemberByRef(ctx, ws2.ID, channel.ID, domain.ChannelMemberTypeUser, sarah.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant by-ref, got %v", err)
	}
	if _, err := s.Channels().ChannelMemberByRef(ctx, ws1.ID, channel.ID, "robot", sarah.ID); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for unknown by-ref type, got %v", err)
	}

	// 6. List is channel-scoped in add order.
	members, err := s.Channels().ListChannelMembers(ctx, ws1.ID, channel.ID)
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	if len(members) != 2 || members[0].ID != sarahMember.ID || members[1].ID != atlasMember.ID {
		t.Fatalf("expected 2 members in add order, got %+v", members)
	}

	// 7. Specialization update rewrites only the note.
	if err := s.Channels().UpdateChannelMemberSpecialization(ctx, ws1.ID, channel.ID, atlasMember.ID, "stakeholder comms"); err != nil {
		t.Fatalf("unexpected specialization update error: %v", err)
	}
	byRef, err = s.Channels().ChannelMemberByRef(ctx, ws1.ID, channel.ID, domain.ChannelMemberTypeAgent, atlas.ID)
	if err != nil || byRef.Specialization != "stakeholder comms" || byRef.AgentID != atlas.ID {
		t.Fatalf("unexpected member after specialization update: %+v, err: %v", byRef, err)
	}
	if err := s.Channels().UpdateChannelMemberSpecialization(ctx, ws2.ID, channel.ID, atlasMember.ID, "hijack"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant specialization update, got %v", err)
	}

	// 8. Remove is scoped; unknown and cross-tenant removes are ErrNotFound.
	if err := s.Channels().RemoveChannelMember(ctx, ws2.ID, channel.ID, sarahMember.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant remove, got %v", err)
	}
	if err := s.Channels().RemoveChannelMember(ctx, ws1.ID, channel.ID, uuid.NewString()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown remove, got %v", err)
	}
	if err := s.Channels().RemoveChannelMember(ctx, ws1.ID, channel.ID, sarahMember.ID); err != nil {
		t.Fatalf("unexpected remove error: %v", err)
	}
	if _, err := s.Channels().ChannelMemberByRef(ctx, ws1.ID, channel.ID, domain.ChannelMemberTypeUser, sarah.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for removed member by-ref, got %v", err)
	}
	members, err = s.Channels().ListChannelMembers(ctx, ws1.ID, channel.ID)
	if err != nil || len(members) != 1 || members[0].ID != atlasMember.ID {
		t.Fatalf("expected 1 member after remove, got %+v, err: %v", members, err)
	}
}

func TestIntegration_ChannelStore_Messages(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws1 := chSeedChannelWorkspace(t, ctx, s, "ws-chan-msg-1")
	ws2 := chSeedChannelWorkspace(t, ctx, s, "ws-chan-msg-2")

	sarah := chSeedUser(t, ctx, s, "sarah-msg@example.com", "Sarah")
	chSeedWorkspaceMember(t, ctx, s, ws1, sarah.ID)
	stranger := chSeedUser(t, ctx, s, "stranger-msg@example.com", "Stranger")
	chSeedWorkspaceMember(t, ctx, s, ws2, stranger.ID)
	atlas := chSeedChannelAgent(t, ctx, s, ws1, "atlas")

	channel := chSeedChannel(t, ctx, s, ws1, "Production Ops", "ops")

	// 1. Root human message: seq 1, no run link, empty mentions.
	root := &domain.ChannelMessage{
		WorkspaceID:  ws1.ID,
		ChannelID:    channel.ID,
		AuthorType:   domain.ChannelMemberTypeUser,
		AuthorUserID: sarah.ID,
		Body:         "Payment latency spiked — @atlas can you analyze?",
	}
	if err := s.Channels().InsertChannelMessage(ctx, root); err != nil {
		t.Fatalf("unexpected insert error: %v", err)
	}
	if root.Seq != 1 {
		t.Fatalf("expected seq 1, got %d", root.Seq)
	}
	if root.CreatedAt.IsZero() {
		t.Fatal("expected created_at to be set")
	}

	// 2. Agent reply in the chain: run link, mentions, run summary.
	sessionID := fmt.Sprintf("chan_%s_%s", channel.ID, atlas.ID)
	turnID := "turn-1"
	reply := &domain.ChannelMessage{
		WorkspaceID:   ws1.ID,
		ChannelID:     channel.ID,
		AuthorType:    domain.ChannelMemberTypeAgent,
		AuthorAgentID: atlas.ID,
		Body:          "P95 doubled after the 12:04 deploy.",
		Mentions:      []domain.Mention{{Type: domain.ChannelMemberTypeUser, ID: sarah.ID, Handle: "sarah"}},
		SessionID:     &sessionID,
		TurnID:        &turnID,
		RunSummary:    &domain.RunSummary{Tools: map[string]int{"grafana.query": 2}, DurationMS: 4200},
		RootMessageID: &root.ID,
		ChainDepth:    1,
	}
	if err := s.Channels().InsertChannelMessage(ctx, reply); err != nil {
		t.Fatalf("unexpected insert reply error: %v", err)
	}
	if reply.Seq != 2 {
		t.Fatalf("expected seq 2, got %d", reply.Seq)
	}

	// 3. Second chain reply and an untagged message.
	deepReply := &domain.ChannelMessage{
		WorkspaceID:   ws1.ID,
		ChannelID:     channel.ID,
		AuthorType:    domain.ChannelMemberTypeAgent,
		AuthorAgentID: atlas.ID,
		Body:          "Rolling back the deploy.",
		RootMessageID: &root.ID,
		ChainDepth:    2,
		RunSummary:    &domain.RunSummary{Tools: map[string]int{}, DurationMS: 900},
	}
	if err := s.Channels().InsertChannelMessage(ctx, deepReply); err != nil {
		t.Fatalf("unexpected insert deep reply error: %v", err)
	}
	untagged := &domain.ChannelMessage{
		WorkspaceID:  ws1.ID,
		ChannelID:    channel.ID,
		AuthorType:   domain.ChannelMemberTypeUser,
		AuthorUserID: sarah.ID,
		Body:         "Thanks both.",
	}
	if err := s.Channels().InsertChannelMessage(ctx, untagged); err != nil {
		t.Fatalf("unexpected insert untagged error: %v", err)
	}
	if untagged.Seq != 4 {
		t.Fatalf("expected seq 4, got %d", untagged.Seq)
	}

	// 4. Full feed read: ascending seq, mentions and jsonb roundtrip.
	feed, err := s.Channels().ListChannelMessages(ctx, store.ListChannelMessagesParams{WorkspaceID: ws1.ID, ChannelID: channel.ID})
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	if len(feed) != 4 || feed[0].ID != root.ID || feed[3].ID != untagged.ID {
		t.Fatalf("expected 4 messages in seq order, got %+v", feed)
	}
	gotReply := feed[1]
	if gotReply.SessionID == nil || *gotReply.SessionID != sessionID || gotReply.TurnID == nil || *gotReply.TurnID != turnID {
		t.Fatalf("expected run link roundtrip, got %+v", gotReply)
	}
	if gotReply.RunSummary == nil || gotReply.RunSummary.DurationMS != 4200 || gotReply.RunSummary.Tools["grafana.query"] != 2 {
		t.Fatalf("expected run summary roundtrip, got %+v", gotReply.RunSummary)
	}
	if len(gotReply.Mentions) != 1 || gotReply.Mentions[0].Type != domain.ChannelMemberTypeUser || gotReply.Mentions[0].ID != sarah.ID || gotReply.Mentions[0].Handle != "sarah" {
		t.Fatalf("expected mentions roundtrip, got %+v", gotReply.Mentions)
	}
	if root := feed[0]; root.RunSummary != nil || root.SessionID != nil || len(root.Mentions) != 0 {
		t.Fatalf("expected nil run link and empty mentions on human root, got %+v", root)
	}

	// 5. Cursor pagination: exclusive AfterSeq and limit.
	page, err := s.Channels().ListChannelMessages(ctx, store.ListChannelMessagesParams{WorkspaceID: ws1.ID, ChannelID: channel.ID, AfterSeq: 1, Limit: 2})
	if err != nil {
		t.Fatalf("unexpected cursor list error: %v", err)
	}
	if len(page) != 2 || page[0].Seq != 2 || page[1].Seq != 3 {
		t.Fatalf("expected seq [2, 3] after cursor 1 with limit 2, got %+v", page)
	}
	tail, err := s.Channels().ListChannelMessages(ctx, store.ListChannelMessagesParams{WorkspaceID: ws1.ID, ChannelID: channel.ID, AfterSeq: page[1].Seq})
	if err != nil || len(tail) != 1 || tail[0].Seq != 4 {
		t.Fatalf("expected seq [4] after cursor 3, got %+v, err: %v", tail, err)
	}

	// 6. Chain lookup by root: only chained replies, in seq order.
	chain, err := s.Channels().ChannelMessagesByRoot(ctx, ws1.ID, channel.ID, root.ID)
	if err != nil {
		t.Fatalf("unexpected chain lookup error: %v", err)
	}
	if len(chain) != 2 || chain[0].ID != reply.ID || chain[1].ID != deepReply.ID {
		t.Fatalf("expected chain [reply, deepReply], got %+v", chain)
	}
	none, err := s.Channels().ChannelMessagesByRoot(ctx, ws1.ID, channel.ID, untagged.ID)
	if err != nil || len(none) != 0 {
		t.Fatalf("expected empty chain for childless root, got %+v, err: %v", none, err)
	}
	none, err = s.Channels().ChannelMessagesByRoot(ctx, ws1.ID, channel.ID, "not-a-uuid")
	if err != nil || len(none) != 0 {
		t.Fatalf("expected empty chain for malformed root, got %+v, err: %v", none, err)
	}

	// 7. Tenant isolation: another workspace sees nothing.
	feed2, err := s.Channels().ListChannelMessages(ctx, store.ListChannelMessagesParams{WorkspaceID: ws2.ID, ChannelID: channel.ID})
	if err != nil || len(feed2) != 0 {
		t.Fatalf("expected empty feed for cross-tenant read, got %+v, err: %v", feed2, err)
	}

	// 8. Validation and reference errors on insert.
	if err := s.Channels().InsertChannelMessage(ctx, nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for nil message, got %v", err)
	}
	bothAuthors := &domain.ChannelMessage{
		WorkspaceID: ws1.ID, ChannelID: channel.ID,
		AuthorType: domain.ChannelMemberTypeUser, AuthorUserID: sarah.ID, AuthorAgentID: atlas.ID,
		Body: "hi",
	}
	if err := s.Channels().InsertChannelMessage(ctx, bothAuthors); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for both authors, got %v", err)
	}
	noAuthor := &domain.ChannelMessage{WorkspaceID: ws1.ID, ChannelID: channel.ID, AuthorType: domain.ChannelMemberTypeUser, Body: "hi"}
	if err := s.Channels().InsertChannelMessage(ctx, noAuthor); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for missing author, got %v", err)
	}
	emptyBody := &domain.ChannelMessage{WorkspaceID: ws1.ID, ChannelID: channel.ID, AuthorType: domain.ChannelMemberTypeUser, AuthorUserID: sarah.ID, Body: "   "}
	if err := s.Channels().InsertChannelMessage(ctx, emptyBody); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for blank body, got %v", err)
	}
	// Author without a workspace membership is ErrNotFound (fake parity).
	outsiderMsg := &domain.ChannelMessage{
		WorkspaceID: ws1.ID, ChannelID: channel.ID,
		AuthorType: domain.ChannelMemberTypeUser, AuthorUserID: stranger.ID, Body: "intruding",
	}
	if err := s.Channels().InsertChannelMessage(ctx, outsiderMsg); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for non-member author, got %v", err)
	}
	// Unknown channel is ErrNotFound.
	ghostMsg := &domain.ChannelMessage{
		WorkspaceID: ws1.ID, ChannelID: uuid.NewString(),
		AuthorType: domain.ChannelMemberTypeUser, AuthorUserID: sarah.ID, Body: "ghost",
	}
	if err := s.Channels().InsertChannelMessage(ctx, ghostMsg); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown channel, got %v", err)
	}
	// Unknown chain root is ErrNotFound.
	unknownRoot := uuid.NewString()
	orphan := &domain.ChannelMessage{
		WorkspaceID: ws1.ID, ChannelID: channel.ID,
		AuthorType: domain.ChannelMemberTypeUser, AuthorUserID: sarah.ID, Body: "orphan",
		RootMessageID: &unknownRoot,
	}
	if err := s.Channels().InsertChannelMessage(ctx, orphan); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown root, got %v", err)
	}
}

func TestIntegration_ChannelStore_Cascade(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws := chSeedChannelWorkspace(t, ctx, s, "ws-chan-cascade")

	sarah := chSeedUser(t, ctx, s, "sarah-cascade@example.com", "Sarah")
	chSeedWorkspaceMember(t, ctx, s, ws, sarah.ID)
	atlas := chSeedChannelAgent(t, ctx, s, ws, "atlas-cascade")

	channel := chSeedChannel(t, ctx, s, ws, "Incidents", "incidents")
	member := &domain.ChannelMember{
		WorkspaceID: ws.ID, ChannelID: channel.ID,
		MemberType: domain.ChannelMemberTypeUser, UserID: sarah.ID,
	}
	if err := s.Channels().AddChannelMember(ctx, member); err != nil {
		t.Fatalf("unexpected add member error: %v", err)
	}
	msg := &domain.ChannelMessage{
		WorkspaceID: ws.ID, ChannelID: channel.ID,
		AuthorType: domain.ChannelMemberTypeUser, AuthorUserID: sarah.ID, Body: "hello",
	}
	if err := s.Channels().InsertChannelMessage(ctx, msg); err != nil {
		t.Fatalf("unexpected insert error: %v", err)
	}

	// 1. Deleting the agent cascades its roster rows.
	agentMember := &domain.ChannelMember{
		WorkspaceID: ws.ID, ChannelID: channel.ID,
		MemberType: domain.ChannelMemberTypeAgent, AgentID: atlas.ID,
	}
	if err := s.Channels().AddChannelMember(ctx, agentMember); err != nil {
		t.Fatalf("unexpected add agent member error: %v", err)
	}
	if err := s.Agents().Delete(ctx, ws.ID, atlas.ID); err != nil {
		t.Fatalf("unexpected delete agent error: %v", err)
	}
	members, err := s.Channels().ListChannelMembers(ctx, ws.ID, channel.ID)
	if err != nil || len(members) != 1 || members[0].ID != member.ID {
		t.Fatalf("expected agent roster row cascaded away, got %+v, err: %v", members, err)
	}

	// 2. Deleting the channel cascades the roster and the feed.
	if err := s.Channels().DeleteChannel(ctx, ws.ID, channel.ID); err != nil {
		t.Fatalf("unexpected delete channel error: %v", err)
	}
	members, err = s.Channels().ListChannelMembers(ctx, ws.ID, channel.ID)
	if err != nil || len(members) != 0 {
		t.Fatalf("expected 0 members after channel cascade, got %+v, err: %v", members, err)
	}
	feed, err := s.Channels().ListChannelMessages(ctx, store.ListChannelMessagesParams{WorkspaceID: ws.ID, ChannelID: channel.ID})
	if err != nil || len(feed) != 0 {
		t.Fatalf("expected empty feed after channel cascade, got %+v, err: %v", feed, err)
	}
}

func TestIntegration_ChannelStore_UpdateRunSummary(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws := chSeedChannelWorkspace(t, ctx, s, "ws-chan-summary-1")
	sarah := chSeedUser(t, ctx, s, "sarah-summary@example.com", "Sarah")
	chSeedWorkspaceMember(t, ctx, s, ws, sarah.ID)
	atlas := chSeedChannelAgent(t, ctx, s, ws, "atlas")
	beacon := chSeedChannelAgent(t, ctx, s, ws, "beacon")
	channel := chSeedChannel(t, ctx, s, ws, "Ops", "ops-summary")

	session := func(agentID string) string { return fmt.Sprintf("chan_%s_%s", channel.ID, agentID) }

	// Two posts by atlas's first run (one turn) and one by a second run of
	// the same deterministic session (a different turn), plus another
	// agent's post that must never match.
	turn1, turn2 := "turn-1", "turn-2"
	rows := []*domain.ChannelMessage{
		{WorkspaceID: ws.ID, ChannelID: channel.ID, AuthorType: domain.ChannelMemberTypeAgent, AuthorAgentID: atlas.ID,
			Body: "first turn post", SessionID: strPtr(session(atlas.ID)), TurnID: &turn1},
		{WorkspaceID: ws.ID, ChannelID: channel.ID, AuthorType: domain.ChannelMemberTypeAgent, AuthorAgentID: atlas.ID,
			Body: "same turn channel.post", SessionID: strPtr(session(atlas.ID)), TurnID: &turn1},
		{WorkspaceID: ws.ID, ChannelID: channel.ID, AuthorType: domain.ChannelMemberTypeAgent, AuthorAgentID: atlas.ID,
			Body: "second run of the same session", SessionID: strPtr(session(atlas.ID)), TurnID: &turn2},
		{WorkspaceID: ws.ID, ChannelID: channel.ID, AuthorType: domain.ChannelMemberTypeAgent, AuthorAgentID: beacon.ID,
			Body: "other agent", SessionID: strPtr(session(beacon.ID)), TurnID: &turn1},
	}
	for i, row := range rows {
		if err := s.Channels().InsertChannelMessage(ctx, row); err != nil {
			t.Fatalf("unexpected insert error on row %d: %v", i, err)
		}
	}

	// Writeback scoped to (workspace, channel, session, turn): only the two
	// first-turn rows of atlas's session update.
	summary := domain.RunSummary{Tools: map[string]int{"grafana.query": 3, "files.write": 1}, DurationMS: 1500}
	if err := s.Channels().UpdateChannelMessageRunSummary(ctx, ws.ID, channel.ID, session(atlas.ID), turn1, summary); err != nil {
		t.Fatalf("unexpected update error: %v", err)
	}

	feed, err := s.Channels().ListChannelMessages(ctx, store.ListChannelMessagesParams{WorkspaceID: ws.ID, ChannelID: channel.ID})
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	byBody := make(map[string]domain.ChannelMessage, len(feed))
	for _, msg := range feed {
		byBody[msg.Body] = msg
	}
	for _, body := range []string{"first turn post", "same turn channel.post"} {
		msg := byBody[body]
		if msg.RunSummary == nil || msg.RunSummary.DurationMS != 1500 || msg.RunSummary.Tools["grafana.query"] != 3 {
			t.Fatalf("expected summary on %q, got %+v", body, msg.RunSummary)
		}
	}
	if msg := byBody["second run of the same session"]; msg.RunSummary != nil {
		t.Fatalf("turn-scoped writeback leaked onto another turn: %+v", msg.RunSummary)
	}
	if msg := byBody["other agent"]; msg.RunSummary != nil {
		t.Fatalf("session-scoped writeback leaked onto another agent: %+v", msg.RunSummary)
	}

	// Empty turnID matches NULL turn_id rows.
	nullTurn := &domain.ChannelMessage{
		WorkspaceID: ws.ID, ChannelID: channel.ID,
		AuthorType: domain.ChannelMemberTypeAgent, AuthorAgentID: atlas.ID,
		Body: "posted before turn discovery", SessionID: strPtr(session(atlas.ID)),
	}
	if err := s.Channels().InsertChannelMessage(ctx, nullTurn); err != nil {
		t.Fatalf("unexpected insert error: %v", err)
	}
	lateSummary := domain.RunSummary{Tools: map[string]int{}, DurationMS: 5}
	if err := s.Channels().UpdateChannelMessageRunSummary(ctx, ws.ID, channel.ID, session(atlas.ID), "", lateSummary); err != nil {
		t.Fatalf("unexpected empty-turn update error: %v", err)
	}
	feed, err = s.Channels().ListChannelMessages(ctx, store.ListChannelMessagesParams{WorkspaceID: ws.ID, ChannelID: channel.ID})
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	for _, msg := range feed {
		if msg.Body == "posted before turn discovery" && (msg.RunSummary == nil || msg.RunSummary.DurationMS != 5) {
			t.Fatalf("expected empty-turn writeback on %q, got %+v", msg.Body, msg.RunSummary)
		}
	}

	// An empty session id is invalid — without it the update is not
	// run-scoped.
	if err := s.Channels().UpdateChannelMessageRunSummary(ctx, ws.ID, channel.ID, "", turn1, summary); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for empty session, got %v", err)
	}
}

// strPtr is the integration file's nullable-string helper.
func strPtr(s string) *string { return &s }
