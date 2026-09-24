package services_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/services"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// ---------------------------------------------------------------------------
// Connection attachment management (add-connection-edit tasks 1.1–1.4,
// design.md D1): the connection-side atomic multi-agent diff over the fake
// store — multi-attach saves, detach-keep-others, unknown-agent atomic
// rejection, http-kind attachment by connection id, idempotent re-saves, the
// tx seam's all-or-nothing behavior, and the nil-seam fallback.
// ---------------------------------------------------------------------------

// attachmentTxService builds a second service over the same fake store wired
// with the attachment tx seam the composition root adapts from
// store.Store.WithTx (design.md D1) — the production shape the attachment
// tests exercise.
func attachmentTxService(t *testing.T, env *connectionsTestEnv) *services.ConnectionsService {
	t.Helper()
	st := env.store
	return services.NewConnectionsService(
		st.Connections(),
		st.WorkspaceMCPServers(),
		st.Agents(),
		agents.NewMCPSettingsService(st.WorkspaceMCPServers(), st.AgentMCPServers(), st.Agents(), []byte(testEncKey)),
		st.OAuthApps(),
		[]byte(testEncKey),
		testPublicBaseURL,
		services.WithProber(func(ctx context.Context, workspaceID, serverID, name string, conn domain.MCPConnection) (int, error) {
			if env.probeErr != nil {
				return 0, env.probeErr
			}
			return testToolCount, nil
		}),
		services.WithAttachmentTx(func(ctx context.Context, fn func(ctx context.Context, agents store.AgentStore) error) error {
			return st.WithTx(ctx, func(tx store.Store) error {
				return fn(ctx, tx.Agents())
			})
		}),
	)
}

// seedAttachmentAgent creates one workspace agent carrying the given
// enabled_mcps entries.
func seedAttachmentAgent(t *testing.T, env *connectionsTestEnv, name, slug string, mcps ...string) *domain.Agent {
	t.Helper()
	agent := &domain.Agent{
		WorkspaceID: env.wsID,
		Slug:        slug,
		Name:        name,
		EnabledMCPS: mcps,
	}
	if err := env.store.Agents().Create(context.Background(), agent); err != nil {
		t.Fatalf("seed agent %s: %v", name, err)
	}
	return agent
}

// agentAttachments reloads the agent and returns its enabled_mcps.
func agentAttachments(t *testing.T, env *connectionsTestEnv, agentID string) []string {
	t.Helper()
	agent, err := env.store.Agents().ByID(context.Background(), env.wsID, agentID)
	if err != nil {
		t.Fatalf("reload agent %s: %v", agentID, err)
	}
	return agent.EnabledMCPS
}

// sortedNames returns the view's attached agents as a sorted copy — the
// attachment listing is a set, not an order.
func sortedNames(names []string) []string {
	out := append([]string(nil), names...)
	slices.Sort(out)
	return out
}

// The spec's "Attaching several agents in one save": two selected agents gain
// the connection and the deselected one loses it in one save; unrelated
// allowlist entries survive untouched.
func TestSetAttachedAgents_MultiAgentSave(t *testing.T) {
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	view, err := connectView(t, env, "github", "", testToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	atlas := seedAttachmentAgent(t, env, "Atlas", "atlas-edit", "unrelated-server-id")
	beacon := seedAttachmentAgent(t, env, "Beacon", "beacon-edit")
	cronus := seedAttachmentAgent(t, env, "Cronus", "cronus-edit", view.ServerID, "hand-made-server-id")

	svc := attachmentTxService(t, env)
	got, err := svc.SetAttachedAgents(ctx, env.wsID, view.ID, []string{atlas.ID, beacon.ID})
	if err != nil {
		t.Fatalf("set attached agents: %v", err)
	}
	if want := []string{"Atlas", "Beacon"}; !slices.Equal(sortedNames(got.AttachedAgents), want) {
		t.Errorf("view attached agents = %v, want %v", got.AttachedAgents, want)
	}

	if want := []string{"unrelated-server-id", view.ServerID}; !slices.Equal(agentAttachments(t, env, atlas.ID), want) {
		t.Errorf("atlas enabled_mcps = %v, want %v (append preserves existing order)", agentAttachments(t, env, atlas.ID), want)
	}
	if want := []string{view.ServerID}; !slices.Equal(agentAttachments(t, env, beacon.ID), want) {
		t.Errorf("beacon enabled_mcps = %v, want %v", agentAttachments(t, env, beacon.ID), want)
	}
	if want := []string{"hand-made-server-id"}; !slices.Equal(agentAttachments(t, env, cronus.ID), want) {
		t.Errorf("cronus enabled_mcps = %v, want %v (deselected agent detached, others kept)", agentAttachments(t, env, cronus.ID), want)
	}
}

// The spec's "Http-kind attachment stores the connection id": an http-kind
// connection has no server row, so the agent's allowlist carries the
// connection's own id.
func TestSetAttachedAgents_HttpKindStoresConnectionID(t *testing.T) {
	upstream := newStubUpstream()
	defer upstream.srv.Close()
	recipe := registerHTTPTestRecipe(t, "stubhttp-attach-edit", upstream.srv.URL, "")
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	view, err := connectHTTPStub(t, env, recipe.ID, httpTestToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	atlas := seedAttachmentAgent(t, env, "Atlas", "atlas-http-edit")

	svc := attachmentTxService(t, env)
	got, err := svc.SetAttachedAgents(ctx, env.wsID, view.ID, []string{atlas.ID})
	if err != nil {
		t.Fatalf("set attached agents: %v", err)
	}
	if !slices.Equal(got.AttachedAgents, []string{"Atlas"}) {
		t.Errorf("view attached agents = %v, want [Atlas]", got.AttachedAgents)
	}
	if want := []string{view.ID}; !slices.Equal(agentAttachments(t, env, atlas.ID), want) {
		t.Errorf("atlas enabled_mcps = %v, want the CONNECTION id %v", agentAttachments(t, env, atlas.ID), want)
	}
	// The http kind never materialized a server row to attach by.
	servers, err := env.store.WorkspaceMCPServers().List(ctx, env.wsID)
	if err != nil || len(servers) != 0 {
		t.Errorf("expected no materialized server rows, got %v (%v)", servers, err)
	}
}

// The spec's "Unknown agent rejected atomically": an unknown id rejects with
// a validation error naming it and changes nothing.
func TestSetAttachedAgents_UnknownAgentRejectedAtomically(t *testing.T) {
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	view, err := connectView(t, env, "github", "", testToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	atlas := seedAttachmentAgent(t, env, "Atlas", "atlas-unknown", view.ServerID)
	beacon := seedAttachmentAgent(t, env, "Beacon", "beacon-unknown")

	svc := attachmentTxService(t, env)
	_, err = svc.SetAttachedAgents(ctx, env.wsID, view.ID, []string{beacon.ID, "no-such-agent"})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected domain.ErrInvalid, got %v", err)
	}
	if !strings.Contains(err.Error(), "no-such-agent") {
		t.Errorf("expected the error to name the unknown agent id, got %q", err.Error())
	}

	// Nothing changed: the attached agent stays attached, the other stays
	// detached.
	if want := []string{view.ServerID}; !slices.Equal(agentAttachments(t, env, atlas.ID), want) {
		t.Errorf("atlas enabled_mcps = %v, want unchanged %v", agentAttachments(t, env, atlas.ID), want)
	}
	if got := agentAttachments(t, env, beacon.ID); len(got) != 0 {
		t.Errorf("beacon enabled_mcps = %v, want unchanged empty", got)
	}
	got, err := svc.Get(ctx, env.wsID, view.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !slices.Equal(got.AttachedAgents, []string{"Atlas"}) {
		t.Errorf("view attached agents = %v, want unchanged [Atlas]", got.AttachedAgents)
	}
}

// Re-saving the current set affects no one and stays a success.
func TestSetAttachedAgents_IdempotentResave(t *testing.T) {
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	view, err := connectView(t, env, "github", "", testToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	atlas := seedAttachmentAgent(t, env, "Atlas", "atlas-idem")
	beacon := seedAttachmentAgent(t, env, "Beacon", "beacon-idem")

	svc := attachmentTxService(t, env)
	desired := []string{atlas.ID, beacon.ID}
	if _, err := svc.SetAttachedAgents(ctx, env.wsID, view.ID, desired); err != nil {
		t.Fatalf("first save: %v", err)
	}
	first := agentAttachments(t, env, atlas.ID)
	got, err := svc.SetAttachedAgents(ctx, env.wsID, view.ID, desired)
	if err != nil {
		t.Fatalf("idempotent re-save: %v", err)
	}
	if !slices.Equal(sortedNames(got.AttachedAgents), []string{"Atlas", "Beacon"}) {
		t.Errorf("view attached agents = %v, want [Atlas Beacon]", got.AttachedAgents)
	}
	if !slices.Equal(agentAttachments(t, env, atlas.ID), first) {
		t.Errorf("atlas enabled_mcps changed on an idempotent re-save: %v -> %v", first, agentAttachments(t, env, atlas.ID))
	}
	if want := []string{view.ServerID}; !slices.Equal(agentAttachments(t, env, beacon.ID), want) {
		t.Errorf("beacon enabled_mcps = %v, want %v", agentAttachments(t, env, beacon.ID), want)
	}
}

// An empty desired set detaches everyone — a valid save.
func TestSetAttachedAgents_EmptyDesiredDetachesAll(t *testing.T) {
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	view, err := connectView(t, env, "github", "", testToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	atlas := seedAttachmentAgent(t, env, "Atlas", "atlas-empty", view.ServerID, "other-server-id")
	beacon := seedAttachmentAgent(t, env, "Beacon", "beacon-empty", view.ServerID)

	svc := attachmentTxService(t, env)
	got, err := svc.SetAttachedAgents(ctx, env.wsID, view.ID, nil)
	if err != nil {
		t.Fatalf("save empty set: %v", err)
	}
	if len(got.AttachedAgents) != 0 {
		t.Errorf("view attached agents = %v, want empty", got.AttachedAgents)
	}
	if want := []string{"other-server-id"}; !slices.Equal(agentAttachments(t, env, atlas.ID), want) {
		t.Errorf("atlas enabled_mcps = %v, want %v (only the connection's id removed)", agentAttachments(t, env, atlas.ID), want)
	}
	if got := agentAttachments(t, env, beacon.ID); len(got) != 0 {
		t.Errorf("beacon enabled_mcps = %v, want empty", got)
	}
}

// Detaching keeps every other allowlist entry, order included.
func TestSetAttachedAgents_DetachKeepsOtherAttachments(t *testing.T) {
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	view, err := connectView(t, env, "github", "", testToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	atlas := seedAttachmentAgent(t, env, "Atlas", "atlas-keep", "first-server-id", view.ServerID, "last-server-id")

	svc := attachmentTxService(t, env)
	if _, err := svc.SetAttachedAgents(ctx, env.wsID, view.ID, nil); err != nil {
		t.Fatalf("detach: %v", err)
	}
	if want := []string{"first-server-id", "last-server-id"}; !slices.Equal(agentAttachments(t, env, atlas.ID), want) {
		t.Errorf("atlas enabled_mcps = %v, want %v (relative order preserved)", agentAttachments(t, env, atlas.ID), want)
	}
}

// Unknown and cross-workspace connections are domain.ErrNotFound (the Get
// convention).
func TestSetAttachedAgents_UnknownConnection(t *testing.T) {
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	svc := attachmentTxService(t, env)
	if _, err := svc.SetAttachedAgents(ctx, env.wsID, "no-such-connection", nil); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// A degraded MCP connection whose server row is gone has no attach id to
// diff against and is refused, not silently mis-written.
func TestSetAttachedAgents_MissingLinkedServerRefused(t *testing.T) {
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	conn := &domain.Connection{
		WorkspaceID: env.wsID,
		Service:     "github",
		AccessLevel: domain.ConnectionAccessReadOnly,
	}
	if err := env.store.Connections().Create(ctx, conn); err != nil {
		t.Fatalf("seed connection: %v", err)
	}

	svc := attachmentTxService(t, env)
	_, err := svc.SetAttachedAgents(ctx, env.wsID, conn.ID, nil)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound for the missing linked server, got %v", err)
	}
}

// Without the tx seam the diff applies through the plain agent store — the
// fallback that keeps embedders honest.
func TestSetAttachedAgents_NilTxSeamFallsBackToAgentStore(t *testing.T) {
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	view, err := connectView(t, env, "github", "", testToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	atlas := seedAttachmentAgent(t, env, "Atlas", "atlas-nil-seam")

	if _, err := env.svc.SetAttachedAgents(ctx, env.wsID, view.ID, []string{atlas.ID}); err != nil {
		t.Fatalf("set attached agents without the seam: %v", err)
	}
	if want := []string{view.ServerID}; !slices.Equal(agentAttachments(t, env, atlas.ID), want) {
		t.Errorf("atlas enabled_mcps = %v, want %v", atlas, want)
	}
}

// The seam's error propagates and nothing persists: the fake's WithTx
// discards the closure's writes when the transaction fails, and the service
// surfaces the error instead of writing outside the seam.
func TestSetAttachedAgents_TxFailureAppliesNothing(t *testing.T) {
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	view, err := connectView(t, env, "github", "", testToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	atlas := seedAttachmentAgent(t, env, "Atlas", "atlas-tx-fail")
	beacon := seedAttachmentAgent(t, env, "Beacon", "beacon-tx-fail", view.ServerID)

	txErr := errors.New("the commit was lost")
	st := env.store
	svc := services.NewConnectionsService(
		st.Connections(),
		st.WorkspaceMCPServers(),
		st.Agents(),
		agents.NewMCPSettingsService(st.WorkspaceMCPServers(), st.AgentMCPServers(), st.Agents(), []byte(testEncKey)),
		st.OAuthApps(),
		[]byte(testEncKey),
		testPublicBaseURL,
		services.WithAttachmentTx(func(ctx context.Context, fn func(ctx context.Context, agents store.AgentStore) error) error {
			return st.WithTx(ctx, func(tx store.Store) error {
				if err := fn(ctx, tx.Agents()); err != nil {
					return err
				}
				// Fail AFTER the closure's writes: the transaction must
				// discard them.
				return txErr
			})
		}),
	)

	if _, err := svc.SetAttachedAgents(ctx, env.wsID, view.ID, []string{atlas.ID}); !errors.Is(err, txErr) {
		t.Fatalf("expected the seam's error, got %v", err)
	}
	if got := agentAttachments(t, env, atlas.ID); len(got) != 0 {
		t.Errorf("atlas enabled_mcps = %v, want empty (the failed tx discarded the attach)", got)
	}
	if want := []string{view.ServerID}; !slices.Equal(agentAttachments(t, env, beacon.ID), want) {
		t.Errorf("beacon enabled_mcps = %v, want unchanged %v", agentAttachments(t, env, beacon.ID), want)
	}
}
