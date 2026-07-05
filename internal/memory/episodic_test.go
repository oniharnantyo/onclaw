package memory_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/memory"
)

func TestSummarizeSession_ReusesCompactionSummary(t *testing.T) {
	summary, l0Abstract, keyTopics, err := memory.SummarizeSession(context.Background(), nil, "Compacted summary of session.", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary != "Compacted summary of session." {
		t.Errorf("expected compaction summary to be reused, got %q", summary)
	}
	if l0Abstract == "" {
		t.Error("expected non-empty l0Abstract")
	}
	_ = keyTopics
}

func TestSummarizeSession_CallsLLM(t *testing.T) {
	mdl := &fakeAgenticModel{response: "The user worked on the Go backend and configured SQLite."}
	summary, l0Abstract, keyTopics, err := memory.SummarizeSession(context.Background(), mdl, "", []*schema.AgenticMessage{
		schema.UserAgenticMessage("hello"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary != "The user worked on the Go backend and configured SQLite." {
		t.Errorf("expected LLM summary, got %q", summary)
	}
	if l0Abstract == "" {
		t.Error("expected non-empty l0Abstract")
	}
	_ = keyTopics
}

func TestSummarizeSession_NoCompactionNoModel(t *testing.T) {
	_, _, _, err := memory.SummarizeSession(context.Background(), nil, "", nil)
	if err == nil {
		t.Fatal("expected error when no compaction summary and no model")
	}
}

func TestSummarizeSession_NoCompactionNoMessages(t *testing.T) {
	_, _, _, err := memory.SummarizeSession(context.Background(), &fakeAgenticModel{}, "", nil)
	if err == nil {
		t.Fatal("expected error when no compaction summary, model given but no messages")
	}
}

func TestComputeEpisodicTTL_Default(t *testing.T) {
	got := memory.ComputeEpisodicTTL(0)
	if got == "" {
		t.Fatal("expected non-empty TTL string")
	}
	parsed, err := time.Parse(time.RFC3339, got)
	if err != nil {
		t.Fatalf("expected valid RFC3339, got err: %v", err)
	}
	if parsed.Before(time.Now()) {
		t.Error("expected TTL in the future")
	}
}

func TestComputeEpisodicTTL_Custom(t *testing.T) {
	got := memory.ComputeEpisodicTTL(30)
	parsed, _ := time.Parse(time.RFC3339, got)
	expected := time.Now().AddDate(0, 0, 30)
	if parsed.Sub(expected) > time.Minute || expected.Sub(parsed) > time.Minute {
		t.Errorf("expected TTL ~30 days from now, got %v, expected ~%v", parsed, expected)
	}
}

func TestFormatDreamDigest_Empty(t *testing.T) {
	got := memory.FormatDreamDigest(nil)
	if got != "" {
		t.Errorf("expected empty for nil input, got %q", got)
	}
	got = memory.FormatDreamDigest([]*memory.EpisodicSummary{})
	if got != "" {
		t.Errorf("expected empty for empty slice, got %q", got)
	}
}

func TestFormatDreamDigest_SingleEpisode(t *testing.T) {
	eps := []*memory.EpisodicSummary{
		{
			L0Abstract: "Worked on authentication module.",
			KeyTopics:  "Auth, JWT, Go",
			Summary:    "Full summary about auth work.",
		},
	}
	got := memory.FormatDreamDigest(eps)
	if got == "" {
		t.Fatal("expected non-empty digest")
	}
	if !strings.Contains(got, "Worked on authentication module.") {
		t.Errorf("digest should contain l0_abstract")
	}
	if !strings.Contains(got, "Auth, JWT, Go") {
		t.Errorf("digest should contain key topics")
	}
	if !strings.Contains(got, "Full summary about auth work.") {
		t.Errorf("digest should contain full summary for first 3 episodes")
	}
}

func TestFormatDreamDigest_SkipsNil(t *testing.T) {
	eps := []*memory.EpisodicSummary{
		nil,
		{L0Abstract: "Second episode.", KeyTopics: "Go"},
	}
	got := memory.FormatDreamDigest(eps)
	if !strings.Contains(got, "Second episode.") {
		t.Errorf("digest should contain non-nil episode")
	}
}

func TestSummarizeSession_L0Abstract(t *testing.T) {
	mdl := &fakeAgenticModel{response: "Refactored the database layer. Updated all queries. Done."}
	_, l0Abstract, _, err := memory.SummarizeSession(context.Background(), mdl, "", []*schema.AgenticMessage{
		schema.UserAgenticMessage("refactor please"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if l0Abstract == "" {
		t.Error("expected non-empty l0Abstract")
	}
	if len(l0Abstract) > 165 {
		t.Errorf("l0Abstract too long: %d chars: %q", len(l0Abstract), l0Abstract)
	}
}
