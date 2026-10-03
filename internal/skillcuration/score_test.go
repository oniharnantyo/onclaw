package skillcuration

import (
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/ingest"
)

// scoredRun builds a membership row with the given tally knobs.
func scoredRun(cluster string, recoveries, distinct int, latencyMS int64, origin string, qualifying bool) domain.SkillClusterRun {
	return domain.SkillClusterRun{
		ClusterID: cluster, Origin: origin, Qualifying: qualifying,
		ToolCalls: 9, DistinctTools: distinct, Recoveries: recoveries,
		TotalToolLatencyMS: latencyMS, IndexedAt: fixtureBase,
	}
}

func TestScoreRunOrdering(t *testing.T) {
	tests := []struct {
		name      string
		high      domain.SkillClusterRun
		low       domain.SkillClusterRun
		highCount int
		lowCount  int
	}{
		{
			name:      "sweet-spot recoveries outrank the gate minimum",
			high:      scoredRun("c", 2, 4, 20000, ingest.OriginUser, true),
			low:       scoredRun("c", 1, 4, 20000, ingest.OriginUser, true),
			highCount: 1, lowCount: 1,
		},
		{
			name:      "recoveries past the sweet spot decay",
			high:      scoredRun("c", 3, 4, 20000, ingest.OriginUser, true),
			low:       scoredRun("c", 7, 4, 20000, ingest.OriginUser, true),
			highCount: 1, lowCount: 1,
		},
		{
			name:      "greater latency effort outranks a drive-by",
			high:      scoredRun("c", 2, 4, 60000, ingest.OriginUser, true),
			low:       scoredRun("c", 2, 4, 4000, ingest.OriginUser, true),
			highCount: 1, lowCount: 1,
		},
		{
			name:      "wider tool diversity outranks the two-tool minimum",
			high:      scoredRun("c", 2, 6, 20000, ingest.OriginUser, true),
			low:       scoredRun("c", 2, 2, 20000, ingest.OriginUser, true),
			highCount: 1, lowCount: 1,
		},
		{
			name:      "an existing family outranks a first-of-cluster run",
			high:      scoredRun("c", 2, 4, 20000, ingest.OriginUser, true),
			low:       scoredRun("c", 2, 4, 20000, ingest.OriginUser, true),
			highCount: 3, lowCount: 1,
		},
		{
			name:      "attended origins outrank unattended ones",
			high:      scoredRun("c", 2, 4, 20000, ingest.OriginUser, true),
			low:       scoredRun("c", 2, 4, 20000, ingest.OriginScheduler, true),
			highCount: 1, lowCount: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			high, low := ScoreRun(tt.high, tt.highCount), ScoreRun(tt.low, tt.lowCount)
			if high <= low {
				t.Errorf("ScoreRun high = %v, low = %v: expected strict ordering", high, low)
			}
			if high < 0 || high > 1 || low < 0 || low > 1 {
				t.Errorf("scores must stay in [0, 1], got %v and %v", high, low)
			}
		})
	}
}

func TestScoreRunOriginFloorAndSymmetry(t *testing.T) {
	scheduler := ScoreRun(scoredRun("c", 2, 4, 20000, ingest.OriginScheduler, true), 1)
	heartbeat := ScoreRun(scoredRun("c", 2, 4, 20000, ingest.OriginHeartbeat, true), 1)
	if scheduler != heartbeat {
		t.Errorf("scheduler and heartbeat must share the down-weight, got %v vs %v", scheduler, heartbeat)
	}
	// Down-weighted, never zeroed out.
	if scheduler <= 0 {
		t.Errorf("unattended origins rank low but never to zero, got %v", scheduler)
	}
}

func TestScoreRunDeterministic(t *testing.T) {
	run := scoredRun("c", 2, 4, 20000, ingest.OriginUser, true)
	if ScoreRun(run, 2) != ScoreRun(run, 2) {
		t.Errorf("the score must be a pure function of its inputs")
	}
}

func TestSelectForCycleBudgetCap(t *testing.T) {
	// 25 qualifying runs across 5 clusters; every cluster holds 5 members.
	qualified := make([]domain.SkillClusterRun, 0, 25)
	for i := 0; i < 25; i++ {
		qualified = append(qualified, domain.SkillClusterRun{
			ID:         "run-" + string(rune('a'+i)),
			ClusterID:  "cl-" + string(rune('a'+i%5)),
			Qualifying: true,
			ToolCalls:  9, DistinctTools: 4, Recoveries: 1 + i%4,
			IndexedAt: fixtureBase.Add(time.Duration(i) * time.Minute),
		})
	}

	tests := []struct {
		name   string
		budget int
		want   int
	}{
		{"budget zero selects nothing", 0, 0},
		{"negative budget selects nothing", -3, 0},
		{"cap below the pool", 10, 10},
		{"cap at the pool size", 25, 25},
		{"cap above the pool returns the pool", 100, 25},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SelectForCycle(qualified, tt.budget)
			if len(got) != tt.want {
				t.Fatalf("SelectForCycle(budget=%d) returned %d runs, want %d", tt.budget, len(got), tt.want)
			}
			if got == nil {
				t.Fatalf("SelectForCycle must never return nil")
			}
			// Ordered by score, descending.
			for i := 1; i < len(got); i++ {
				if got[i-1].Score < got[i].Score {
					t.Errorf("selection not score-ordered at %d: %v < %v", i, got[i-1].Score, got[i].Score)
				}
			}
		})
	}
}

func TestSelectForCycleDeterministic(t *testing.T) {
	qualified := []domain.SkillClusterRun{
		{ID: "run-b", ClusterID: "cl-1", Qualifying: true, ToolCalls: 9, DistinctTools: 3, Recoveries: 2, IndexedAt: fixtureBase.Add(time.Minute)},
		{ID: "run-a", ClusterID: "cl-1", Qualifying: true, ToolCalls: 9, DistinctTools: 3, Recoveries: 2, IndexedAt: fixtureBase},
		{ID: "run-c", ClusterID: "cl-2", Qualifying: true, ToolCalls: 9, DistinctTools: 3, Recoveries: 2, IndexedAt: fixtureBase.Add(2 * time.Minute)},
	}

	first := SelectForCycle(qualified, 3)
	second := SelectForCycle(qualified, 3)
	if len(first) != 3 {
		t.Fatalf("expected all three runs selected, got %d", len(first))
	}
	for i := range first {
		if first[i].Run.ID != second[i].Run.ID {
			t.Fatalf("selection is not deterministic: %v vs %v", first, second)
		}
	}

	// Identical tallies tie-break oldest-evidence-first: run-a predates
	// run-b and run-c.
	if first[0].Run.ID != "run-a" {
		t.Errorf("tie-break should rank the oldest evidence first, got %q", first[0].Run.ID)
	}

	// The growth signal reflects the input set: cl-1 holds two qualifying
	// members, cl-2 holds one.
	for _, qr := range first {
		want := 1
		if qr.ClusterID == "cl-1" {
			want = 2
		}
		if qr.ClusterQualifying != want {
			t.Errorf("run %s: ClusterQualifying = %d, want %d", qr.Run.ID, qr.ClusterQualifying, want)
		}
	}
}

func TestSelectForCycleSkipsNonQualifying(t *testing.T) {
	qualified := []domain.SkillClusterRun{
		{ID: "failed-1", ClusterID: "cl-1", Qualifying: false, ToolCalls: 9, DistinctTools: 4, Recoveries: 0, IndexedAt: fixtureBase},
		{ID: "ok-1", ClusterID: "cl-2", Qualifying: true, ToolCalls: 9, DistinctTools: 4, Recoveries: 2, IndexedAt: fixtureBase},
	}
	got := SelectForCycle(qualified, 10)
	if len(got) != 1 || got[0].Run.ID != "ok-1" {
		t.Errorf("non-qualifying rows must never enter the budget: %+v", got)
	}
}

func TestSelectForCycleEmptyInput(t *testing.T) {
	got := SelectForCycle(nil, 10)
	if got == nil || len(got) != 0 {
		t.Errorf("empty input must yield an empty non-nil slice, got %+v", got)
	}
}
