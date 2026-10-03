package driver

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveProvisionAndTurn is the tasks.md 2.1 verification hook: against a
// live local onclaw server it provisions the fixture workspace and agent via
// the public API only, then drives one trivial chat turn through the real
// runner and asserts a completed run with a non-empty answer and an
// extractable transcript.
//
// It is skipped unless ONCLAW_BENCH_LIVE == "1" (live runs cost real model
// tokens and need a running server). Required env:
//
//	ONCLAW_BENCH_LIVE=1
//	ONCLAW_BENCH_ADMIN_EMAIL / ONCLAW_BENCH_ADMIN_PASSWORD   (superadmin or admin identity)
//	ONCLAW_BENCH_PROVIDER_KEY                                (model key, when the workspace has no provider yet)
//	ONCLAW_BENCH_BASE_URL                                    (default http://localhost:8080)
//	ONCLAW_BENCH_MODEL                                       (default glm-5.3-flash — the protocol pin)
func TestLiveProvisionAndTurn(t *testing.T) {
	if os.Getenv("ONCLAW_BENCH_LIVE") != "1" {
		t.Skip("live test disabled: set ONCLAW_BENCH_LIVE=1 (requires a running onclaw server and live model keys)")
	}
	baseURL := os.Getenv("ONCLAW_BENCH_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	adminEmail := os.Getenv("ONCLAW_BENCH_ADMIN_EMAIL")
	adminPassword := os.Getenv("ONCLAW_BENCH_ADMIN_PASSWORD")
	if adminEmail == "" || adminPassword == "" {
		t.Skip("live test disabled: set ONCLAW_BENCH_ADMIN_EMAIL and ONCLAW_BENCH_ADMIN_PASSWORD")
	}
	model := os.Getenv("ONCLAW_BENCH_MODEL")
	if model == "" {
		model = "glm-5.3-flash"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	h, err := Provision(ctx, Options{
		BaseURL:       baseURL,
		AdminEmail:    adminEmail,
		AdminPassword: adminPassword,
		WorkspaceSlug: "bench-live-it",
		AgentSlug:     "bench-live-agent",
		Model:         model,
	})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	t.Logf("provisioned: workspace=%s agent=%s model=%s reused=%v warnings=%v",
		h.Workspace.Slug, h.Agent.Slug, h.Agent.Model, h.Reused, h.Warnings)

	sessionID := fmt.Sprintf("bench-live-it-%d", time.Now().UnixNano())
	resp, err := NewClient(baseURL, 0).ChatTurn(ctx, h.ChatKey, h.Agent.Slug, "Reply with the single word: ready", sessionID)
	if err != nil {
		t.Fatalf("ChatTurn: %v", err)
	}

	turn := ExtractTurn(resp)
	if turn.Status != "completed" {
		t.Fatalf("run status = %q (need completed), response=%v", turn.Status, resp)
	}
	if strings.TrimSpace(turn.AnswerText) == "" {
		t.Fatalf("empty answer for the ready prompt: %v", resp)
	}
	t.Logf("answer: %s (%d tool calls in transcript)", turn.AnswerText, len(turn.ToolCalls))
}
