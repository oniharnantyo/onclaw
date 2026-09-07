//go:build integration

package postgres_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/session"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

// TestIntegration_ADKSessionAdapter_Conformance runs Eino's adk/session
// conformance suite against the real PostgreSQL implementation via the ADK
// session adapter (task 3.5). The suite exercises the full SessionEventStore
// contract: forward/reverse pagination, after-cursor resume, kind filtering,
// session isolation, empty sessions, and duplicate/empty/invalid EventID
// rejection.
//
// Each factory call provisions a fresh workspace so the sub-tests' event logs
// never collide under the shared schema. Workspaces back session_events via
// the workspace_id foreign key.
func TestIntegration_ADKSessionAdapter_Conformance(t *testing.T) {
	s, _, ctx := setupTestSchema(t)

	factory := func(tb testing.TB) adk.SessionEventStore[*schema.AgenticMessage] {
		tb.Helper()
		// Each factory call provisions a fresh workspace (unique slug) so the
		// suite's sub-tests never collide on the workspace_id-scoped event log.
		ws := &domain.Workspace{
			Slug: fmt.Sprintf("ws-conf-%s", uuid.NewString()[:8]),
			Name: "Conformance WS",
		}
		if err := s.Workspaces().Create(ctx, ws); err != nil {
			tb.Fatalf("create conformance workspace: %v", err)
		}
		return agents.NewADKSessionAdapter(s.SessionEvents(), s.SessionCheckpoints(), ws.ID)
	}

	makeMessage := func(content string) *schema.AgenticMessage {
		return schema.UserAgenticMessage(content)
	}

	session.RunConformanceTests[*schema.AgenticMessage](t, factory, makeMessage)
}

// TestIntegration_SessionCheckpointStore_CRUD exercises the checkpoint store
// contract that the ADK session adapter delegates to, against PostgreSQL.
func TestIntegration_SessionCheckpointStore_CRUD(t *testing.T) {
	s, _, ctx := setupTestSchema(t)

	cps := s.SessionCheckpoints()

	// Missing checkpoint -> not found.
	if _, ok, err := cps.Get(ctx, "missing-cp"); err != nil || ok {
		t.Fatalf("Get missing checkpoint: ok=%v err=%v (want ok=false, err=nil)", ok, err)
	}

	// Set + Get round-trip.
	payload := []byte(`{"state":"running","turn":"t1"}`)
	if err := cps.Set(ctx, "cp-1", payload); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, ok, err := cps.Get(ctx, "cp-1")
	if err != nil || !ok {
		t.Fatalf("Get after Set: ok=%v err=%v", ok, err)
	}
	if string(got) != string(payload) {
		t.Errorf("checkpoint payload mismatch: got %q, want %q", got, payload)
	}

	// Overwrite.
	if err := cps.Set(ctx, "cp-1", []byte(`{"state":"done"}`)); err != nil {
		t.Fatalf("Set overwrite: %v", err)
	}
	got2, _, err := cps.Get(ctx, "cp-1")
	if err != nil {
		t.Fatalf("Get after overwrite: %v", err)
	}
	if string(got2) != `{"state":"done"}` {
		t.Errorf("overwritten checkpoint not reflected: got %q", got2)
	}

	// Delete removes it.
	if err := cps.Delete(ctx, "cp-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok, err := cps.Get(ctx, "cp-1"); err != nil || ok {
		t.Fatalf("Get after Delete: ok=%v err=%v (want ok=false, err=nil)", ok, err)
	}

	// Delete of a missing checkpoint returns domain.ErrNotFound at the domain
	// store layer. The ADK adapter swallows this into a no-op.
	if err := cps.Delete(ctx, "never-existed"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Delete missing checkpoint: want domain.ErrNotFound, got %v", err)
	}
}
