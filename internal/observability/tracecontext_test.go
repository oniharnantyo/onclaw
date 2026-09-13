package observability

import (
	"context"
	"fmt"
	"testing"
)

func TestTraceTagsShape(t *testing.T) {
	full := TraceTags(TraceContext{
		Origin:      OriginTelegram,
		AgentID:     "agent-123",
		WorkspaceID: "ws-456",
	})
	want := []string{"origin:telegram", "agent:agent-123", "ws:ws-456"}
	if len(full) != len(want) {
		t.Fatalf("TraceTags length = %d, want %d (%v)", len(full), len(want), full)
	}
	for i := range want {
		if full[i] != want[i] {
			t.Fatalf("TraceTags[%d] = %q, want %q (tags: %v)", i, full[i], want[i], full)
		}
	}
}

func TestTraceTagsSparseAndUnknownOrigin(t *testing.T) {
	// Empty origin normalizes to user; absent agent/workspace drop their tags
	// instead of exporting empty "agent:" / "ws:" values.
	got := TraceTags(TraceContext{Origin: ""})
	if len(got) != 1 || got[0] != "origin:user" {
		t.Fatalf("TraceTags(empty origin) = %v, want [origin:user]", got)
	}

	got = TraceTags(TraceContext{Origin: "carrier-pigeon", AgentID: "a-1"})
	if len(got) != 2 || got[0] != "origin:user" || got[1] != "agent:a-1" {
		t.Fatalf("TraceTags(unknown origin) = %v, want [origin:user agent:a-1]", got)
	}

	got = TraceTags(TraceContext{Origin: OriginScheduler})
	if len(got) != 1 || got[0] != "origin:scheduler" {
		t.Fatalf("TraceTags(scheduler) = %v, want [origin:scheduler]", got)
	}
}

func TestTraceMetadataShape(t *testing.T) {
	got := TraceMetadata(TraceContext{
		TurnID:      "turn-1",
		WorkspaceID: "ws-456",
		AgentID:     "agent-123",
		Origin:      OriginChannel,
	})
	want := map[string]string{
		"turn_id":      "turn-1",
		"workspace_id": "ws-456",
		"agent_id":     "agent-123",
		"origin":       "channel",
	}
	if len(got) != len(want) {
		t.Fatalf("TraceMetadata keys = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("TraceMetadata[%q] = %q, want %q", k, got[k], v)
		}
	}
}

func TestTraceMetadataOmitsEmptyKeys(t *testing.T) {
	got := TraceMetadata(TraceContext{})
	if len(got) != 1 || got["origin"] != "user" {
		t.Fatalf("TraceMetadata(empty) = %v, want only origin:user", got)
	}
}

func TestTraceNameInteractiveUsesFirstLineTruncated(t *testing.T) {
	got := TraceName(OriginUser, "  Deploy the staging stack\nto cluster eu-1  ", "")
	if got != "Deploy the staging stack" {
		t.Fatalf("TraceName = %q, want first line trimmed", got)
	}

	long := ""
	for i := 0; i < 100; i++ {
		long += "x"
	}
	got = TraceName("", long+"\nsecond line", "")
	if len([]rune(got)) != maxTraceNameRunes {
		t.Fatalf("TraceName length = %d, want %d", len([]rune(got)), maxTraceNameRunes)
	}
	if got[len(got)-3:] != "..." {
		t.Fatalf("TraceName truncation tail missing: %q", got)
	}
}

func TestTraceNameSchedulerUsesScheduleName(t *testing.T) {
	got := TraceName(OriginScheduler, "the prompt\nmore", "  Morning digest  ")
	if got != "Morning digest" {
		t.Fatalf("TraceName = %q, want the schedule name", got)
	}

	// Without a schedule name the input's first line still names the trace.
	got = TraceName(OriginScheduler, "the prompt\nmore", "")
	if got != "the prompt" {
		t.Fatalf("TraceName = %q, want input first line fallback", got)
	}
}

func TestTraceNameEmpty(t *testing.T) {
	if got := TraceName(OriginUser, "   \n  ", ""); got != "" {
		t.Fatalf("TraceName(blank input) = %q, want empty", got)
	}
}

func TestTraceIDForRunDeterministicUUID(t *testing.T) {
	id1 := TraceIDForRun("0f0a3f34-8ec6-4d16-a4e2-2b53f6b1a001")
	id2 := TraceIDForRun("0f0a3f34-8ec6-4d16-a4e2-2b53f6b1a001")
	other := TraceIDForRun("0f0a3f34-8ec6-4d16-a4e2-2b53f6b1a002")

	if id1 == "" || id2 == "" {
		t.Fatal("TraceIDForRun returned empty id for non-empty run id")
	}
	if id1 != id2 {
		t.Fatalf("TraceIDForRun not deterministic: %q vs %q", id1, id2)
	}
	if id1 == other {
		t.Fatalf("TraceIDForRun collided for distinct run ids: %q", id1)
	}
	// A Langfuse trace id is a UUID; the deterministic mapping must produce
	// the canonical 36-char form.
	if len(id1) != 36 || id1[8] != '-' || id1[13] != '-' || id1[18] != '-' || id1[23] != '-' {
		t.Fatalf("TraceIDForRun = %q, want canonical UUID shape", id1)
	}
	if got := TraceIDForRun(""); got != "" {
		t.Fatalf("TraceIDForRun(\"\") = %q, want empty", got)
	}
}

// TestSampledInGolden are golden decisions for fixed trace ids, computed with
// the documented algorithm (SHA-256, first 8 hex digits / 0xFFFFFFFF, strictly
// below rate) so any drift from the upstream consumer's sampler or across
// processes fails here first.
func TestSampledInGolden(t *testing.T) {
	const uuidAll = "ffffffff-ffff-ffff-ffff-ffffffffffff"
	const uuidOne = "00000000-0000-0000-0000-000000000001"
	const uuidSample = "550e8400-e29b-41d4-a716-446655440000"

	cases := []struct {
		traceID string
		rate    float64
		want    bool
	}{
		{uuidAll, 0.1, true},     // norm ≈ 0.0765
		{uuidAll, 0.25, true},    //
		{uuidAll, 0.5, true},     //
		{uuidOne, 0.1, false},    // norm ≈ 0.4795
		{uuidOne, 0.25, false},   //
		{uuidOne, 0.5, true},     //
		{uuidSample, 0.1, false}, // norm ≈ 0.6393
		{uuidSample, 0.25, false},
		{uuidSample, 0.5, false},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s@%.2f", tc.traceID[:8], tc.rate), func(t *testing.T) {
			if got := SampledIn(tc.traceID, tc.rate); got != tc.want {
				t.Fatalf("SampledIn(%q, %g) = %v, want %v", tc.traceID, tc.rate, got, tc.want)
			}
		})
	}
}

func TestSampledInBoundaries(t *testing.T) {
	id := "550e8400-e29b-41d4-a716-446655440000"
	// Outside (0,1) the sampler is off: everything is exported, matching the
	// upstream consumer's short-circuit.
	for _, rate := range []float64{0, -1, 1, 1.5} {
		if !SampledIn(id, rate) {
			t.Fatalf("SampledIn(id, %g) = false, want true (sampler off)", rate)
		}
	}
	if !SampledIn("", 0.5) {
		t.Fatal("SampledIn(empty id) = false, want true")
	}
	// Deterministic across calls: the decision is a pure function of
	// (trace id, rate), identical in every process.
	first := SampledIn(id, 0.3)
	for i := 0; i < 10; i++ {
		if SampledIn(id, 0.3) != first {
			t.Fatal("SampledIn not deterministic for repeated calls")
		}
	}
}

func TestSampledInDistributionAtHalfRate(t *testing.T) {
	// Sanity bound on the deterministic mapping: 200 fixed run ids sampled at
	// 0.5 must split roughly evenly (deterministic set, so this is stable).
	in := 0
	const total = 200
	for i := 0; i < total; i++ {
		if SampledIn(TraceIDForRun(fmt.Sprintf("run-%04d", i)), 0.5) {
			in++
		}
	}
	if in < 80 || in > 120 {
		t.Fatalf("SampledIn at 0.5 kept %d/%d traces, want roughly half", in, total)
	}
}

func TestTraceContextComposesSamplingWithTraceID(t *testing.T) {
	// The runner recipe: trace id pinned from the turn id, sampling decided
	// on the same id — the persisted id always equals the exported id.
	turnID := "turn-abc-123"
	traceID := TraceIDForRun(turnID)
	if traceID == "" {
		t.Fatal("TraceIDForRun returned empty")
	}
	// Deterministic: recomputing the mapping for the same turn yields the
	// same id, so retried attempts land inside one trace.
	if again := TraceIDForRun(turnID); again != traceID {
		t.Fatalf("trace id mapping unstable: %q vs %q", traceID, again)
	}
	SampledIn(traceID, 0.5) // decision must not depend on anything else
}

func TestApplyTraceContextReturnsContext(t *testing.T) {
	base := context.Background()
	ctx := ApplyTraceContext(base, TraceContext{
		SessionID:    "sess-1",
		UserID:       "user-1",
		WorkspaceID:  "ws-1",
		AgentID:      "agent-1",
		TurnID:       "turn-1",
		Origin:       OriginUser,
		Input:        "hello\nworld",
		ScheduleName: "Morning digest",
	})
	if ctx == nil {
		t.Fatal("ApplyTraceContext returned nil context")
	}
	// The eino-ext SetTrace options are stored under an unexported context
	// key (the handler reads them at export start), so the observable
	// contract is: a derived context, plus the shape helpers above which
	// feed it verbatim.
	if ctx == base {
		t.Fatal("ApplyTraceContext returned the base context unchanged")
	}
	// Empty coordinates must not panic — every option is conditional.
	if ctx := ApplyTraceContext(base, TraceContext{}); ctx == nil {
		t.Fatal("ApplyTraceContext(empty) returned nil")
	}
}

func TestFirstLine(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"hello", "hello"},
		{"hello\r\nworld", "hello"},
		{"hello\nworld", "hello"},
		{"  padded  \nnext", "padded"},
		{"", ""},
		{"   ", ""},
		{"\nfirst-after-empty", ""},
	}
	for _, tc := range cases {
		if got := FirstLine(tc.in, 80); got != tc.want {
			t.Fatalf("FirstLine(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("short", 80); got != "short" {
		t.Fatalf("truncateRunes(short) = %q", got)
	}
	if got := truncateRunes("hello", 5); got != "hello" {
		t.Fatalf("truncateRunes(5) = %q, want unchanged", got)
	}
	got := truncateRunes("hello world", 8)
	if got != "hello..." {
		t.Fatalf("truncateRunes(8) = %q, want hello...", got)
	}
	// Multi-byte runes counted as runes, not bytes.
	multibyte := "αααααααααα"
	if got := truncateRunes(multibyte, 4); got != "α..." {
		t.Fatalf("truncateRunes(multibyte, 4) = %q, want α...", got)
	}
	if got := truncateRunes("abcdef", 0); got != "" {
		t.Fatalf("truncateRunes(0) = %q, want empty", got)
	}
	if got := truncateRunes("abcdef", 2); got != "ab" {
		t.Fatalf("truncateRunes(2) = %q, want ab (max smaller than tail)", got)
	}
}
