package agents

import (
	"context"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/agents/tools"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// TestWithoutSchedulerTools pins the strip set by name (integrate-scheduler
// D6): the schedule tool — by its registry constant, so registration and the
// strip can never drift — is filtered alongside the memory tool and the
// delete-file tool that can destroy memory documents.
func TestWithoutSchedulerTools(t *testing.T) {
	if _, excluded := schedulerExcludedTools[tools.NameSchedule]; !excluded {
		t.Fatal("tools.NameSchedule must stay in schedulerExcludedTools so a registered schedule tool can never leak into scheduler-origin runs")
	}

	in := []string{tools.NameSchedule, tools.NameMemory, tools.NameDeleteFile, tools.NameWebFetch, tools.Name}
	got := withoutSchedulerTools(in)
	want := []string{tools.NameWebFetch, tools.Name}
	if len(got) != len(want) {
		t.Fatalf("withoutSchedulerTools(%v) = %v, want %v", in, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("withoutSchedulerTools(%v) = %v, want %v", in, got, want)
		}
	}
}

// TestResolve_SchedulerToolStrip covers the runner-level strip (task 3.4): a
// scheduler-origin run resolves neither the schedule tool nor the memory
// tools even though the agent's denylist doesn't name them (denylist D2 —
// default-on), while ordinary tools survive. The user-origin control resolves
// the same default surface untouched.
func TestResolve_SchedulerToolStrip(t *testing.T) {
	_, runner, _, _, req := setupSchedulerRunner(t, "sess-toolstrip")
	ctx := context.Background()

	ws, err := runner.workspaces.ByID(ctx, req.WorkspaceID)
	if err != nil {
		t.Fatalf("load workspace: %v", err)
	}
	agent, err := runner.agents.ByID(ctx, req.WorkspaceID, req.AgentID)
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}

	namesOf := func(origin string) map[string]bool {
		t.Helper()
		r := req
		r.Origin = origin
		_, resolved, err := runner.resolve(ctx, r, ws, agent, nil)
		if err != nil {
			t.Fatalf("resolve (%s): %v", origin, err)
		}
		names := make(map[string]bool, len(resolved))
		for _, tl := range resolved {
			info, err := tl.Info(ctx)
			if err != nil {
				t.Fatalf("tool info: %v", err)
			}
			names[info.Name] = true
		}
		return names
	}

	// Scheduler origin: the strip removes schedule and the memory tools even
	// though the default surface exposes them; every other exposed tool
	// survives.
	schedulerNames := namesOf(OriginScheduler)
	for _, excluded := range []string{tools.NameSchedule, tools.NameMemory, tools.NameDeleteFile} {
		if schedulerNames[excluded] {
			t.Fatalf("scheduler run resolved excluded tool %q (resolved: %v)", excluded, schedulerNames)
		}
	}
	if !schedulerNames[tools.NameWebFetch] {
		t.Fatalf("scheduler run must keep the ordinary exposed tool %q (resolved: %v)", tools.NameWebFetch, schedulerNames)
	}

	// User origin control: the default surface resolves the memory tools
	// untouched — the strip applies only to origin scheduler. ("schedule" is
	// inert for every origin on the default registry, which does not register
	// it — see the registered-registry test below.)
	userNames := namesOf(OriginUser)
	if !userNames[tools.NameMemory] || !userNames[tools.NameDeleteFile] {
		t.Fatalf("user-origin run must keep the memory tools (resolved: %v)", userNames)
	}
	if !userNames[tools.NameWebFetch] {
		t.Fatalf("user-origin run must keep %q (resolved: %v)", tools.NameWebFetch, userNames)
	}
	if userNames[tools.NameSchedule] {
		t.Fatal("an unregistered tool name must never resolve")
	}
}

// TestResolve_SchedulerToolStripExcludesRegisteredSchedule covers the strip
// against a registry that actually registered the schedule tool (task 6.2):
// a scheduler-origin resolve still excludes it even though the agent never
// denied it, while a user-origin run on the same runner resolves it —
// registration and the strip can never drift apart.
func TestResolve_SchedulerToolStripExcludesRegisteredSchedule(t *testing.T) {
	st := fake.New()
	reg := NewDefaultToolRegistry(st.Memories(), WithSchedulerTools(st.Schedulers(), st.Channels()))
	if _, ok := reg.Lookup(tools.NameSchedule); !ok {
		t.Fatal("WithSchedulerTools must register the schedule tool")
	}
	_, runner, _, _, req := setupSchedulerRunner(t, "sess-toolstrip-sched", WithToolRegistry(reg))
	ctx := context.Background()

	ws, err := runner.workspaces.ByID(ctx, req.WorkspaceID)
	if err != nil {
		t.Fatalf("load workspace: %v", err)
	}
	agent, err := runner.agents.ByID(ctx, req.WorkspaceID, req.AgentID)
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}

	namesOf := func(origin string) map[string]bool {
		t.Helper()
		r := req
		r.Origin = origin
		_, resolved, err := runner.resolve(ctx, r, ws, agent, nil)
		if err != nil {
			t.Fatalf("resolve (%s): %v", origin, err)
		}
		names := make(map[string]bool, len(resolved))
		for _, tl := range resolved {
			info, err := tl.Info(ctx)
			if err != nil {
				t.Fatalf("tool info: %v", err)
			}
			names[info.Name] = true
		}
		return names
	}

	schedulerNames := namesOf(OriginScheduler)
	if schedulerNames[tools.NameSchedule] {
		t.Fatalf("scheduler run must strip the registered schedule tool (resolved: %v)", schedulerNames)
	}
	if !schedulerNames[tools.NameWebFetch] {
		t.Fatalf("scheduler run must keep the ordinary exposed tool (resolved: %v)", schedulerNames)
	}

	userNames := namesOf(OriginUser)
	if !userNames[tools.NameSchedule] {
		t.Fatalf("user-origin run must resolve the registered schedule tool (resolved: %v)", userNames)
	}
}
