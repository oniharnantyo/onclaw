package skillcuration

import (
	"context"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/ingest"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

func TestClusterKey(t *testing.T) {
	ws, agent := "ws-1", "agt-1"
	shape := []string{"grafana.query", "files.write", "http.request", "http.request"}

	tests := []struct {
		name string
		a    string
		b    string
		same bool
	}{
		{"same shape converges", ClusterKey(ws, agent, shape), ClusterKey(ws, agent, shape), true},
		{"normalized names converge", ClusterKey(ws, agent, shape), ClusterKey(ws, agent, []string{" Grafana.Query ", "files.write", "HTTP.REQUEST", "http.request"}), true},
		{"call order changes the family", ClusterKey(ws, agent, shape), ClusterKey(ws, agent, []string{"files.write", "grafana.query", "http.request", "http.request"}), false},
		{"call count rides the sequence", ClusterKey(ws, agent, shape), ClusterKey(ws, agent, shape[:3]), false},
		{"agent partitions", ClusterKey(ws, agent, shape), ClusterKey(ws, "agt-2", shape), false},
		{"workspace partitions", ClusterKey(ws, agent, shape), ClusterKey("ws-2", agent, shape), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if (tt.a == tt.b) != tt.same {
				t.Errorf("keys %q and %q: equal = %v, want %v", tt.a, tt.b, tt.a == tt.b, tt.same)
			}
		})
	}
}

func TestClusterGateOpenBoundary(t *testing.T) {
	tests := []struct {
		name    string
		count   int
		minimum int
		want    bool
	}{
		{"one qualifying run of minimum two stays closed", 1, 2, false},
		{"two qualifying runs of minimum two open", 2, 2, true},
		{"three of two open", 3, 2, true},
		{"raised minimum keeps two closed", 2, 3, false},
		{"degenerate zero minimum behaves as one", 1, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClusterGateOpen(tt.count, tt.minimum); got != tt.want {
				t.Errorf("ClusterGateOpen(%d, %d) = %v, want %v", tt.count, tt.minimum, got, tt.want)
			}
		})
	}
}

func TestClusterRunStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	st := storefake.New()
	if err := st.Workspaces().Create(ctx, &domain.Workspace{ID: "ws-cl", Slug: "cl", Name: "Cl"}); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := st.Agents().Create(ctx, &domain.Agent{ID: "agt-cl", WorkspaceID: "ws-cl", Slug: "atlas", Name: "Atlas"}); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	clusters := st.SkillCandidates()

	run := &domain.SkillClusterRun{
		WorkspaceID: "ws-cl", AgentID: "agt-cl", ClusterID: "cl-abc",
		SessionID: "sess-1", TurnID: "turn-1",
		RunStatus: domain.SkillClusterRunFailed, Origin: "user",
		Qualifying: false, ToolCalls: 9, DistinctTools: 4, Recoveries: 0,
		ErrorResults: 2, TotalToolLatencyMS: 18000, WindowEndEventID: "evt-9",
	}
	if err := clusters.IndexClusterRun(ctx, run); err != nil {
		t.Fatalf("index run: %v", err)
	}
	if run.ID == "" || run.IndexedAt.IsZero() {
		t.Fatalf("store must assign identity and stamp IndexedAt: %+v", run)
	}

	// Re-indexing the same run rewrites in place — one row per run.
	again := &domain.SkillClusterRun{
		WorkspaceID: "ws-cl", AgentID: "agt-cl", ClusterID: "cl-abc",
		SessionID: "sess-1", TurnID: "turn-1",
		RunStatus: domain.SkillClusterRunCompleted, Origin: "user",
		Qualifying: true, ToolCalls: 9, DistinctTools: 4, Recoveries: 1,
	}
	if err := clusters.IndexClusterRun(ctx, again); err != nil {
		t.Fatalf("re-index run: %v", err)
	}
	runs, err := clusters.ListClusterRunsByCluster(ctx, "ws-cl", "cl-abc")
	if err != nil {
		t.Fatalf("list cluster: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("cluster holds %d rows after re-index, want exactly 1", len(runs))
	}
	if runs[0].ID != run.ID {
		t.Errorf("re-index kept a new id %q, want the store-assigned %q", runs[0].ID, run.ID)
	}
	if !runs[0].Qualifying {
		t.Errorf("re-index did not rewrite the row's qualifying flag: %+v", runs[0])
	}

	// An invalid run status is rejected through the domain contract.
	bad := &domain.SkillClusterRun{
		WorkspaceID: "ws-cl", AgentID: "agt-cl", ClusterID: "cl-abc",
		SessionID: "sess-2", TurnID: "turn-2", RunStatus: "cancelled",
	}
	if err := clusters.IndexClusterRun(ctx, bad); err == nil {
		t.Errorf("invalid run status must be rejected by the domain validation")
	}
}

func TestQualifierConfigSourceIsReadPerJob(t *testing.T) {
	// The config source resolves at job time, so a settings edit applies on
	// the next job without rebuilding the consumer (the posture-func
	// precedent) — the qualifier must call it on every Ingest.
	ctx, st, _, sink := newFixture(t, DefaultConfig())
	calls := 0
	q := NewQualifier(st.SessionEvents(), func(context.Context, string) Config {
		calls++
		return DefaultConfig()
	}, st.SkillCandidates(), discardLog(), WithCandidateChipSink(func(_ context.Context, _ ingest.Job, payload SkillCandidatePayload) {
		*sink = append(*sink, payload)
	}))

	if err := st.SessionEvents().AppendEvents(ctx, fixtureWorkspace, buildWindow(t, "cfg", "turn-1", qualifyingCalls(), true)); err != nil {
		t.Fatalf("append events: %v", err)
	}
	if err := q.Ingest(ctx, fixtureJob("completed", "turn-1")); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if calls != 1 {
		t.Errorf("config source called %d times per job, want 1", calls)
	}
	if len(*sink) != 1 {
		t.Errorf("chips = %d, want 1", len(*sink))
	}
}
