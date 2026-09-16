package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func heartbeatTestNow() time.Time {
	return time.Date(2026, time.September, 10, 9, 0, 0, 0, time.UTC)
}

func validHeartbeat() *Heartbeat {
	return &Heartbeat{
		WorkspaceID: "ws-1",
		AgentID:     "agent-1",
		Prompt:      "Check the overnight deploy",
		Expr:        "*/15 * * * *",
		Enabled:     true,
	}
}

func TestValidateHeartbeat_AcceptsValidShapes(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(hb *Heartbeat)
	}{
		{
			name:   "baseline",
			mutate: nil,
		},
		{
			name: "empty checklist allowed",
			mutate: func(hb *Heartbeat) {
				hb.Prompt = ""
			},
		},
		{
			name: "whitespace checklist allowed",
			mutate: func(hb *Heartbeat) {
				hb.Prompt = "   \n\t"
			},
		},
		{
			name: "delivery type defaults to creator_dm",
			mutate: func(hb *Heartbeat) {
				hb.Delivery = HeartbeatDelivery{Type: ""}
			},
		},
		{
			name: "explicit creator_dm",
			mutate: func(hb *Heartbeat) {
				hb.Delivery = HeartbeatDelivery{Type: HeartbeatDeliveryCreatorDM}
			},
		},
		{
			name: "channel delivery with channel id",
			mutate: func(hb *Heartbeat) {
				hb.Delivery = HeartbeatDelivery{Type: HeartbeatDeliveryChannel, ChannelID: "ch-1"}
			},
		},
		{
			name: "active hours window",
			mutate: func(hb *Heartbeat) {
				start, end := "08:00", "22:00"
				hb.ActiveStart, hb.ActiveEnd = &start, &end
			},
		},
		{
			name: "exactly five-minute cadence",
			mutate: func(hb *Heartbeat) {
				hb.Expr = "*/5 * * * *"
			},
		},
		{
			name: "hourly cadence",
			mutate: func(hb *Heartbeat) {
				hb.Expr = "30 * * * *"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hb := validHeartbeat()
			if tt.mutate != nil {
				tt.mutate(hb)
			}
			if err := ValidateHeartbeat(hb, heartbeatTestNow()); err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}

func TestValidateHeartbeat_RejectsInvalidShapes(t *testing.T) {
	tests := []struct {
		name     string
		nilInput bool
		mutate   func(hb *Heartbeat)
		wantSub  string // non-empty: the error must mention it
	}{
		{
			name:     "nil heartbeat",
			nilInput: true,
		},
		{
			name: "missing expression",
			mutate: func(hb *Heartbeat) {
				hb.Expr = ""
			},
		},
		{
			name: "whitespace expression",
			mutate: func(hb *Heartbeat) {
				hb.Expr = "   "
			},
		},
		{
			name: "invalid expression prose",
			mutate: func(hb *Heartbeat) {
				hb.Expr = "at nine"
			},
		},
		{
			name: "expression with too few fields",
			mutate: func(hb *Heartbeat) {
				hb.Expr = "0 9 *"
			},
		},
		{
			name: "two-minute cadence below the floor",
			mutate: func(hb *Heartbeat) {
				hb.Expr = "*/2 * * * *"
			},
			wantSub: "5m",
		},
		{
			name: "every-minute cadence below the floor",
			mutate: func(hb *Heartbeat) {
				hb.Expr = "* * * * *"
			},
			wantSub: "5m",
		},
		{
			name: "one-sided window start only",
			mutate: func(hb *Heartbeat) {
				start := "08:00"
				hb.ActiveStart = &start
			},
		},
		{
			name: "one-sided window end only",
			mutate: func(hb *Heartbeat) {
				end := "22:00"
				hb.ActiveEnd = &end
			},
		},
		{
			name: "equal zero-width window",
			mutate: func(hb *Heartbeat) {
				start, end := "09:00", "09:00"
				hb.ActiveStart, hb.ActiveEnd = &start, &end
			},
			wantSub: "active hours",
		},
		{
			name: "hour out of range",
			mutate: func(hb *Heartbeat) {
				start, end := "24:00", "23:00"
				hb.ActiveStart, hb.ActiveEnd = &start, &end
			},
		},
		{
			name: "minute out of range",
			mutate: func(hb *Heartbeat) {
				start, end := "08:60", "09:00"
				hb.ActiveStart, hb.ActiveEnd = &start, &end
			},
		},
		{
			name: "missing leading zero",
			mutate: func(hb *Heartbeat) {
				start, end := "8:00", "09:00"
				hb.ActiveStart, hb.ActiveEnd = &start, &end
			},
		},
		{
			name: "not a clock",
			mutate: func(hb *Heartbeat) {
				start, end := "0800", "09:00"
				hb.ActiveStart, hb.ActiveEnd = &start, &end
			},
		},
		{
			name: "non-numeric clock",
			mutate: func(hb *Heartbeat) {
				start, end := "ab:cd", "09:00"
				hb.ActiveStart, hb.ActiveEnd = &start, &end
			},
		},
		{
			name: "channel delivery without channel id",
			mutate: func(hb *Heartbeat) {
				hb.Delivery = HeartbeatDelivery{Type: HeartbeatDeliveryChannel}
			},
		},
		{
			name: "unknown delivery type",
			mutate: func(hb *Heartbeat) {
				hb.Delivery = HeartbeatDelivery{Type: "email"}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var hb *Heartbeat
			if !tt.nilInput {
				hb = validHeartbeat()
				if tt.mutate != nil {
					tt.mutate(hb)
				}
			}
			err := ValidateHeartbeat(hb, heartbeatTestNow())
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("expected ErrInvalid, got %v", err)
			}
			if tt.wantSub != "" && !strings.Contains(err.Error(), tt.wantSub) {
				t.Fatalf("expected error to mention %q, got %v", tt.wantSub, err)
			}
		})
	}
}

func TestValidateHeartbeat_NormalizesInPlace(t *testing.T) {
	// The prompt is trimmed; whitespace folds to the empty checklist.
	hb := validHeartbeat()
	hb.Prompt = "  padded checklist  "
	hb.Expr = "  30 8 * * *  "
	staleTick := heartbeatTestNow().Add(time.Hour)
	hb.NextTickAt = &staleTick
	if err := ValidateHeartbeat(hb, heartbeatTestNow()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hb.Prompt != "padded checklist" {
		t.Fatalf("expected trimmed prompt, got %q", hb.Prompt)
	}
	if hb.Expr != "30 8 * * *" {
		t.Fatalf("expected trimmed expr, got %q", hb.Expr)
	}
	// NextTickAt is derived state (add-agent-heartbeat D4): never accepted
	// from input, so validation zeroes whatever the caller carried in.
	if hb.NextTickAt != nil {
		t.Fatalf("expected next_tick_at zeroed, got %v", hb.NextTickAt)
	}

	// An empty delivery type defaults to creator_dm and clears a stray
	// channel id.
	hb2 := validHeartbeat()
	hb2.Delivery = HeartbeatDelivery{Type: "", ChannelID: "ch-9"}
	if err := ValidateHeartbeat(hb2, heartbeatTestNow()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hb2.Delivery.Type != HeartbeatDeliveryCreatorDM || hb2.Delivery.ChannelID != "" {
		t.Fatalf("expected creator_dm delivery with cleared channel, got %+v", hb2.Delivery)
	}

	// Channel delivery trims the channel id.
	hb3 := validHeartbeat()
	hb3.Delivery = HeartbeatDelivery{Type: HeartbeatDeliveryChannel, ChannelID: "  ch-7  "}
	if err := ValidateHeartbeat(hb3, heartbeatTestNow()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hb3.Delivery.ChannelID != "ch-7" {
		t.Fatalf("expected trimmed channel id, got %q", hb3.Delivery.ChannelID)
	}

	// Active hours are trimmed in place.
	hb4 := validHeartbeat()
	start, end := "  08:00 ", " 22:00  "
	hb4.ActiveStart, hb4.ActiveEnd = &start, &end
	if err := ValidateHeartbeat(hb4, heartbeatTestNow()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *hb4.ActiveStart != "08:00" || *hb4.ActiveEnd != "22:00" {
		t.Fatalf("expected trimmed active hours, got %s–%s", *hb4.ActiveStart, *hb4.ActiveEnd)
	}
}

func TestValidateHeartbeat_WhitespaceChecklistFoldsToEmpty(t *testing.T) {
	hb := validHeartbeat()
	hb.Prompt = "   \n\t  "
	if err := ValidateHeartbeat(hb, heartbeatTestNow()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hb.Prompt != "" {
		t.Fatalf("expected whitespace checklist folded to empty, got %q", hb.Prompt)
	}
}
