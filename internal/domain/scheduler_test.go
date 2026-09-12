package domain

import (
	"errors"
	"testing"
	"time"
)

func schedulerTestNow() time.Time {
	return time.Date(2026, time.September, 10, 9, 0, 0, 0, time.UTC)
}

func validRecurringScheduler() *Scheduler {
	return &Scheduler{
		WorkspaceID: "ws-1",
		AgentID:     "agent-1",
		Name:        "morning-digest",
		Prompt:      "Summarize overnight activity",
		Kind:        SchedulerKindRecurring,
		Expr:        "0 9 * * 1-5",
		Enabled:     true,
	}
}

func TestValidateScheduler_AcceptsValidShapes(t *testing.T) {
	now := schedulerTestNow()
	future := now.Add(time.Hour)

	tests := []struct {
		name string
		in   *Scheduler
	}{
		{
			name: "recurring",
			in:   validRecurringScheduler(),
		},
		{
			name: "once in the future",
			in: func() *Scheduler {
				s := &Scheduler{
					WorkspaceID: "ws-1",
					AgentID:     "agent-1",
					Name:        "launch-reminder",
					Prompt:      "Remind the team",
					Kind:        SchedulerKindOnce,
					RunAt:       &future,
				}
				return s
			}(),
		},
		{
			name: "delivery type defaults to thread",
			in: func() *Scheduler {
				s := validRecurringScheduler()
				s.Delivery = SchedulerDelivery{Type: ""}
				return s
			}(),
		},
		{
			name: "channel delivery with channel id",
			in: func() *Scheduler {
				s := validRecurringScheduler()
				s.Delivery = SchedulerDelivery{Type: SchedulerDeliveryChannel, ChannelID: "ch-1"}
				return s
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateScheduler(tt.in, now, true); err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}

func TestValidateScheduler_RejectsInvalidShapes(t *testing.T) {
	now := schedulerTestNow()
	past := now.Add(-time.Hour)

	tests := []struct {
		name   string
		mutate func(s *Scheduler)
	}{
		{
			name:   "nil scheduler",
			mutate: nil,
		},
		{
			name: "missing name",
			mutate: func(s *Scheduler) {
				s.Name = "   "
			},
		},
		{
			name: "missing prompt",
			mutate: func(s *Scheduler) {
				s.Prompt = ""
			},
		},
		{
			name: "whitespace prompt",
			mutate: func(s *Scheduler) {
				s.Prompt = "   \n\t"
			},
		},
		{
			name: "unknown kind",
			mutate: func(s *Scheduler) {
				s.Kind = "daily"
			},
		},
		{
			name: "empty kind",
			mutate: func(s *Scheduler) {
				s.Kind = ""
			},
		},
		{
			name: "invalid expression prose",
			mutate: func(s *Scheduler) {
				s.Expr = "at nine"
			},
		},
		{
			name: "expression with too few fields",
			mutate: func(s *Scheduler) {
				s.Expr = "0 9 *"
			},
		},
		{
			name: "empty recurring expression",
			mutate: func(s *Scheduler) {
				s.Expr = "  "
			},
		},
		{
			name: "once without run_at",
			mutate: func(s *Scheduler) {
				s.Kind = SchedulerKindOnce
				s.Expr = ""
				s.RunAt = nil
			},
		},
		{
			name: "once created in the past",
			mutate: func(s *Scheduler) {
				s.Kind = SchedulerKindOnce
				s.Expr = ""
				s.RunAt = &past
			},
		},
		{
			name: "once created exactly now",
			mutate: func(s *Scheduler) {
				s.Kind = SchedulerKindOnce
				s.Expr = ""
				s.RunAt = &now
			},
		},
		{
			name: "channel delivery without channel id",
			mutate: func(s *Scheduler) {
				s.Delivery = SchedulerDelivery{Type: SchedulerDeliveryChannel}
			},
		},
		{
			name: "unknown delivery type",
			mutate: func(s *Scheduler) {
				s.Delivery = SchedulerDelivery{Type: "email"}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s *Scheduler
			if tt.mutate == nil {
				s = nil
			} else {
				s = validRecurringScheduler()
				tt.mutate(s)
			}
			err := ValidateScheduler(s, now, true)
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("expected ErrInvalid, got %v", err)
			}
		})
	}
}

func TestValidateScheduler_NormalizesInPlace(t *testing.T) {
	now := schedulerTestNow()

	// Name is trimmed; unparsed once fields never leak into expr.
	future := now.Add(2 * time.Hour)
	s := &Scheduler{
		Name:   "  padded name  ",
		Prompt: "prompt",
		Kind:   SchedulerKindOnce,
		Expr:   "0 9 * * *", // must be cleared for once
		RunAt:  &future,
	}
	if err := ValidateScheduler(s, now, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Name != "padded name" {
		t.Fatalf("expected trimmed name, got %q", s.Name)
	}
	if s.Expr != "" {
		t.Fatalf("expected expr cleared for once, got %q", s.Expr)
	}

	// Delivery defaults to thread and clears the channel id.
	s2 := validRecurringScheduler()
	s2.Delivery = SchedulerDelivery{Type: "", ChannelID: "ch-9"}
	if err := ValidateScheduler(s2, now, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s2.Delivery.Type != SchedulerDeliveryThread || s2.Delivery.ChannelID != "" {
		t.Fatalf("expected thread delivery with cleared channel, got %+v", s2.Delivery)
	}

	// Recurring clears a stray run_at.
	s3 := validRecurringScheduler()
	s3.RunAt = &future
	if err := ValidateScheduler(s3, now, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s3.RunAt != nil {
		t.Fatalf("expected run_at cleared for recurring")
	}

	// Expression is trimmed.
	s4 := validRecurringScheduler()
	s4.Expr = "  30 8 * * *  "
	if err := ValidateScheduler(s4, now, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s4.Expr != "30 8 * * *" {
		t.Fatalf("expected trimmed expr, got %q", s4.Expr)
	}
}

// Updating must not fail on a one-shot whose instant already passed: only
// creation requires a strictly-future RunAt.
func TestValidateScheduler_UpdateAcceptsPastOnceInstant(t *testing.T) {
	now := schedulerTestNow()
	past := now.Add(-24 * time.Hour)
	s := &Scheduler{
		WorkspaceID: "ws-1",
		AgentID:     "agent-1",
		Name:        "stale-once",
		Prompt:      "Remind",
		Kind:        SchedulerKindOnce,
		RunAt:       &past,
	}
	if err := ValidateScheduler(s, now, false); err != nil {
		t.Fatalf("expected update with past run_at to pass, got %v", err)
	}

	// The same shape still fails on create.
	if err := ValidateScheduler(s, now, true); err == nil {
		t.Fatal("expected create with past run_at to fail")
	}
}

func TestNextRun_BasicRecurrences(t *testing.T) {
	utc := time.UTC
	after := time.Date(2026, time.September, 10, 9, 0, 0, 0, time.UTC) // Thursday

	tests := []struct {
		name string
		expr string
		want time.Time
	}{
		{
			name: "next minute",
			expr: "15 9 * * *",
			want: time.Date(2026, time.September, 10, 9, 15, 0, 0, time.UTC),
		},
		{
			name: "next day when today's slot passed",
			expr: "0 8 * * *",
			want: time.Date(2026, time.September, 11, 8, 0, 0, 0, time.UTC),
		},
		{
			name: "weekday range skips the weekend",
			expr: "0 9 * * 1-5",
			want: time.Date(2026, time.September, 11, 9, 0, 0, 0, time.UTC), // Friday
		},
		{
			name: "monthly on day 1",
			expr: "0 9 1 * *",
			want: time.Date(2026, time.October, 1, 9, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NextRun(tt.expr, after, utc)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.Equal(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			if !got.After(after) {
				t.Fatal("expected strictly-after result")
			}
		})
	}
}

func TestNextRun_EvaluatesInWorkspaceTimezone(t *testing.T) {
	jakarta, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}

	// 09:00 Jakarta = 02:00 UTC.
	after := time.Date(2026, time.September, 10, 2, 0, 0, 0, time.UTC) // exactly 09:00 Jakarta
	got, err := NextRun("0 9 * * *", after, jakarta)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Strictly after: the same wall-clock instant just passed, so the next
	// occurrence is tomorrow in Jakarta.
	want := time.Date(2026, time.September, 11, 2, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %v (UTC), want %v", got.UTC(), want)
	}
}

func TestNextRun_AcrossSpringForwardDST(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}

	// Before the transition the 02:30 slot is EST: 07:30 UTC.
	before, err := NextRun("30 2 * * *", time.Date(2026, time.March, 5, 12, 0, 0, 0, ny), ny)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := time.Date(2026, time.March, 6, 7, 30, 0, 0, time.UTC); !before.UTC().Equal(want) {
		t.Fatalf("pre-DST occurrence: got %v (UTC), want %v", before.UTC(), want)
	}

	// US spring forward 2026: 2026-03-08 02:00 EST -> 03:00 EDT, so the
	// 02:30 wall-clock slot does not exist that day. The schedule skips the
	// nonexistent occurrence and lands on the next day's 02:30, now in EDT
	// (06:30 UTC) — proving the fields are evaluated in the workspace zone.
	after := time.Date(2026, time.March, 7, 12, 0, 0, 0, ny)
	next, err := NextRun("30 2 * * *", after, ny)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Date(2026, time.March, 9, 6, 30, 0, 0, time.UTC)
	if !next.UTC().Equal(want) {
		t.Fatalf("spring-forward occurrence: got %v (UTC), want %v", next.UTC(), want)
	}
	if !next.After(after) {
		t.Fatal("expected strictly-after result")
	}

	// The following occurrences stay anchored to the local wall clock.
	following, err := NextRun("30 2 * * *", *next, ny)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantFollowing := time.Date(2026, time.March, 10, 6, 30, 0, 0, time.UTC)
	if !following.UTC().Equal(wantFollowing) {
		t.Fatalf("post-DST occurrence: got %v (UTC), want %v", following.UTC(), wantFollowing)
	}
}

func TestNextRun_RejectsInvalidExpressions(t *testing.T) {
	for _, expr := range []string{"at nine", "", "0 9 *", "61 * * * *", "* * * * * *", "0 9 * * 6-0"} {
		got, err := NextRun(expr, schedulerTestNow(), time.UTC)
		if err == nil {
			t.Fatalf("expr %q: expected error, got %v", expr, got)
		}
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("expr %q: expected ErrInvalid, got %v", expr, err)
		}
	}
}

func TestHumanLabel_Matrix(t *testing.T) {
	tests := []struct {
		expr string
		want string
	}{
		{"0 9 * * *", "09:00 · Daily"},
		{"30 8 * * *", "08:30 · Daily"},
		{"0 9 * * 1-5", "09:00 · Mon–Fri"},
		{"0 9 * * 1", "09:00 · Mon"},
		{"0 9 * * 1,3,5", "09:00 · Mon, Wed, Fri"},
		{"0 9 * * 0,6", "09:00 · Sun, Sat"},
		{"0 9 15 * *", "09:00 · Monthly on day 15"},
		{"0 9 1 * *", "09:00 · Monthly on day 1"},
		{"30 * * * *", "Hourly at :30"},
		{"0 * * * *", "Hourly at :00"},
		// Custom shapes keep the raw expression.
		{"0 9,17 * * *", "0 9,17 * * *"}, // twice daily
		{"*/15 * * * *", "*/15 * * * *"}, // every 15 minutes
		{"0 9 15 * 1", "0 9 15 * 1"},     // Vixie DOM/DOW OR — not a clean shape
		{"0 9 1 1 *", "0 9 1 1 *"},       // yearly
		// Wrap-around ranges are rejected by the standard parser, so the raw
		// text comes back (see TestNextRun_RejectsInvalidExpressions).
		{"0 9 * * 6-0", "0 9 * * 6-0"},
	}

	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			if got := HumanLabel(tt.expr); got != tt.want {
				t.Fatalf("HumanLabel(%q) = %q, want %q", tt.expr, got, tt.want)
			}
		})
	}
}

func TestHumanLabel_InvalidExpressionReturnsRaw(t *testing.T) {
	if got := HumanLabel("at nine"); got != "at nine" {
		t.Fatalf("got %q, want raw expression", got)
	}
}
