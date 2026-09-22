package webhooks_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/webhooks"
)

// The management service tests (tasks 2.6, 5.1): enablement, display-once,
// rotation invalidation, target/event updates, disable-keeps-state, and the
// recipe-declaration guards.

func TestService_EnableGeneratesSecretAndBinds(t *testing.T) {
	sd := seedWorkspace(t, "wh-enable")
	svc := sd.newService(t)

	secret := sd.enable(t, svc, []string{"pull_request.opened", "issues.assigned"})

	// The secret is 43 chars of base64url; the stored hint is its last 4.
	if len(secret) != 43 || strings.ContainsAny(secret, "+/=") {
		t.Fatalf("expected a 43-char base64url secret, got %q", secret)
	}
	state, err := sd.webhooks.GetState(context.Background(), sd.WS.ID, sd.Connection.ID)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if state.SecretHint != webhooks.SecretHint(secret) {
		t.Fatalf("hint %q must be the secret's last 4 (%q)", state.SecretHint, webhooks.SecretHint(secret))
	}
	// The store carries the encrypted envelope, never the plaintext.
	if state.SecretCiphertext == "" || strings.Contains(state.SecretCiphertext, secret) {
		t.Fatalf("the envelope must not contain the plaintext secret: %q", state.SecretCiphertext)
	}
	// The cipher's workspace AAD opens it.
	cipher := webhooks.NewAESGCMCipher(testKey)
	pt, err := cipher.DecryptSecret(sd.WS.ID, state.SecretCiphertext)
	if err != nil || string(pt) != secret {
		t.Fatalf("the envelope must decrypt to the revealed secret (workspace AAD): %v", err)
	}
	// The selection persisted.
	if len(state.Events) != 2 || state.TargetKind != domain.ConnectionWebhookTargetChannel || state.TargetID != sd.Channel.ID || state.TargetAgentID != sd.Agent.ID {
		t.Fatalf("unexpected persisted state: %+v", state)
	}
}

func TestService_EnableDefaultsAndEmptyEventsRejectedWhenEnabled(t *testing.T) {
	sd := seedWorkspace(t, "wh-defaults")
	svc := sd.newService(t)

	// An empty selection starts from the recipe's read-flavored default set.
	state, _, err := svc.Enable(context.Background(), sd.WS.ID, sd.Connection.ID, webhooks.EnableRequest{
		AgentID:    sd.Agent.ID,
		TargetKind: domain.ConnectionWebhookTargetChannel,
		TargetID:   sd.Channel.ID,
	})
	if err != nil {
		t.Fatalf("enable with defaults: %v", err)
	}
	defaults := domain.RecipeByID("github").Webhooks.DefaultEvents
	if len(state.Events) != len(defaults) {
		t.Fatalf("expected the recipe default events %v, got %v", defaults, state.Events)
	}

	// Clearing every event while enabled is refused (spec: enabling
	// requires at least one selected event).
	if _, err := svc.UpdateEvents(context.Background(), sd.WS.ID, sd.Connection.ID, webhooks.EventsRequest{Events: []string{}}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("empty events on a live connection must be invalid, got %v", err)
	}
}

func TestService_DisplayOnce(t *testing.T) {
	sd := seedWorkspace(t, "wh-display-once")
	svc := sd.newService(t)
	secret := sd.enable(t, svc, nil)

	// Every later read carries only the hint — never the plaintext.
	state, err := svc.Get(context.Background(), sd.WS.ID, sd.Connection.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if strings.Contains(state.SecretCiphertext, secret) || state.SecretCiphertext == "" {
		t.Fatal("the stored state must not carry the plaintext secret")
	}
}

func TestService_RotateInvalidatesPriorSecret(t *testing.T) {
	sd := seedWorkspace(t, "wh-rotate")
	svc := sd.newService(t)
	oldSecret := sd.enable(t, svc, []string{"push"})
	oldState, _ := sd.webhooks.GetState(context.Background(), sd.WS.ID, sd.Connection.ID)

	_, newSecret, err := svc.Rotate(context.Background(), sd.WS.ID, sd.Connection.ID)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if newSecret == "" {
		t.Fatal("rotate must return the new plaintext exactly once")
	}
	_ = oldSecret

	// A single write swapped the envelope; the binding and selection stay.
	newState, _ := sd.webhooks.GetState(context.Background(), sd.WS.ID, sd.Connection.ID)
	if newState.SecretCiphertext == oldState.SecretCiphertext {
		t.Fatal("rotation must store a fresh envelope (the prior secret is invalid immediately)")
	}
	if newState.SecretHint == oldState.SecretHint {
		t.Log("hint unchanged — same last-4 by chance; the envelope is the invalidation")
	}
	if newState.TargetAgentID != oldState.TargetAgentID || newState.TargetID != oldState.TargetID || len(newState.Events) != len(oldState.Events) {
		t.Fatalf("rotation must preserve the binding and selection: %+v vs %+v", newState, oldState)
	}

	// Rotation on a never-enabled connection is refused.
	sd2 := seedWorkspace(t, "wh-rotate-off")
	svc2 := sd2.newService(t)
	if _, _, err := svc2.Rotate(context.Background(), sd2.WS.ID, sd2.Connection.ID); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("rotating a disabled connection must be invalid, got %v", err)
	}
}

func TestService_DisableKeepsStateAndStopsNothingElse(t *testing.T) {
	sd := seedWorkspace(t, "wh-disable")
	svc := sd.newService(t)
	sd.enable(t, svc, []string{"push"})

	state, err := svc.Disable(context.Background(), sd.WS.ID, sd.Connection.ID)
	if err != nil {
		t.Fatalf("disable: %v", err)
	}
	if state.Enabled {
		t.Fatal("disable must clear the toggle")
	}
	// Disabling keeps binding, selection, and secret (spec).
	if state.TargetID != sd.Channel.ID || state.TargetKind != domain.ConnectionWebhookTargetChannel || len(state.Events) != 1 || state.SecretHint == "" || state.SecretCiphertext == "" {
		t.Fatalf("disable must keep the configuration, got %+v", state)
	}

	// Re-enable with a fresh secret: the old envelope is replaced.
	second := sd.enable(t, svc, []string{"push"})
	if second == "" {
		t.Fatal("re-enable must reveal a fresh secret")
	}
}

func TestService_TargetAndEventUpdates(t *testing.T) {
	sd := seedWorkspace(t, "wh-updates")
	svc := sd.newService(t)
	sd.enable(t, svc, []string{"push"})

	// Rebind to the thread target.
	state, err := svc.UpdateTarget(context.Background(), sd.WS.ID, sd.Connection.ID, webhooks.TargetRequest{
		AgentID:    sd.Agent.ID,
		TargetKind: domain.ConnectionWebhookTargetThread,
		TargetID:   "tg_dm_123_agent",
	})
	if err != nil {
		t.Fatalf("update target: %v", err)
	}
	if state.TargetKind != domain.ConnectionWebhookTargetThread || state.TargetID != "tg_dm_123_agent" {
		t.Fatalf("target not rebound: %+v", state)
	}

	// Replace the selection.
	state, err = svc.UpdateEvents(context.Background(), sd.WS.ID, sd.Connection.ID, webhooks.EventsRequest{Events: []string{"release.published", "issues.opened"}})
	if err != nil {
		t.Fatalf("update events: %v", err)
	}
	if len(state.Events) != 2 || state.Events[0] != "release.published" {
		t.Fatalf("events not replaced: %+v", state)
	}
}

func TestService_TargetValidation(t *testing.T) {
	sd := seedWorkspace(t, "wh-targets")
	svc := sd.newService(t)
	ctx := context.Background()

	cases := []struct {
		name string
		req  webhooks.EnableRequest
	}{
		{"unknown agent", webhooks.EnableRequest{AgentID: "no-such-agent", TargetKind: domain.ConnectionWebhookTargetChannel, TargetID: sd.Channel.ID}},
		{"unknown channel", webhooks.EnableRequest{AgentID: sd.Agent.ID, TargetKind: domain.ConnectionWebhookTargetChannel, TargetID: "no-such-channel"}},
		{"bad kind", webhooks.EnableRequest{AgentID: sd.Agent.ID, TargetKind: "email", TargetID: "x"}},
		{"empty kind", webhooks.EnableRequest{AgentID: sd.Agent.ID, TargetKind: "", TargetID: "x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := svc.Enable(ctx, sd.WS.ID, sd.Connection.ID, tc.req); !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("expected ErrInvalid, got %v", err)
			}
		})
	}
}

func TestService_RecipeGuards(t *testing.T) {
	sd := seedWorkspace(t, "wh-recipe-guard")
	svc := sd.newService(t)
	ctx := context.Background()

	// A figma connection's recipe declares no webhook support.
	figma := &domain.Connection{WorkspaceID: sd.WS.ID, Service: "figma", AccessLevel: domain.ConnectionAccessReadOnly}
	if err := sd.connections.Create(ctx, figma); err != nil {
		t.Fatalf("create figma connection: %v", err)
	}
	if _, _, err := svc.Enable(ctx, sd.WS.ID, figma.ID, webhooks.EnableRequest{AgentID: sd.Agent.ID}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("enabling a non-webhook recipe must be invalid, got %v", err)
	}

	// Events outside the catalog are refused — recipe-catalog membership is
	// the service's ValidateForRecipe check.
	if _, _, err := svc.Enable(ctx, sd.WS.ID, sd.Connection.ID, webhooks.EnableRequest{
		AgentID:    sd.Agent.ID,
		TargetKind: domain.ConnectionWebhookTargetChannel,
		TargetID:   sd.Channel.ID,
		Events:     []string{"pull_request.labeled"},
	}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("out-of-catalog events must be invalid, got %v", err)
	}

	// Unknown connection: the store's ErrNotFound (no existence leak).
	if _, _, err := svc.Enable(ctx, sd.WS.ID, "no-such-connection", webhooks.EnableRequest{}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown connection must 404, got %v", err)
	}
}

func TestService_BuildView(t *testing.T) {
	sd := seedWorkspace(t, "wh-view")
	svc := sd.newService(t)
	sd.enable(t, svc, []string{"pull_request.opened"})
	state, _ := sd.webhooks.GetState(context.Background(), sd.WS.ID, sd.Connection.ID)

	view := webhooks.BuildView(state, sd.WS.Slug, "https://onclaw.example.com")
	if view.IngestURL != "https://onclaw.example.com/api/ingest/webhooks/"+sd.WS.Slug+"/"+sd.Connection.ID {
		t.Fatalf("derived ingest URL wrong: %q", view.IngestURL)
	}
	if !view.Enabled || view.Target == nil || view.Target.TargetKind != domain.ConnectionWebhookTargetChannel {
		t.Fatalf("view shape wrong: %+v", view)
	}
	// The view never carries secret material beyond the hint.
	if strings.Contains(view.SecretHint, "v1:") {
		t.Fatalf("the view must never carry the envelope: %+v", view)
	}

	// An unset binding omits the target key.
	empty := webhooks.BuildView(&domain.ConnectionWebhook{WorkspaceID: sd.WS.ID, ConnectionID: sd.Connection.ID, Events: nil}, sd.WS.Slug, "")
	if empty.Target != nil || empty.Enabled {
		t.Fatalf("inert view wrong: %+v", empty)
	}
	if empty.Events == nil {
		t.Fatal("events must serialize as an array, never null")
	}
}

func TestService_PruneOnce(t *testing.T) {
	sd := seedWorkspace(t, "wh-prune")
	svc := sd.newService(t)
	sd.enable(t, svc, []string{"push"})
	ctx := context.Background()

	now := time.Now()
	if _, err := sd.webhooks.RecordDelivery(ctx, sd.WS.ID, sd.Connection.ID, "old-delivery", now.Add(-8*24*time.Hour)); err != nil {
		t.Fatalf("record: %v", err)
	}
	if _, err := sd.webhooks.RecordDelivery(ctx, sd.WS.ID, sd.Connection.ID, "fresh-delivery", now); err != nil {
		t.Fatalf("record: %v", err)
	}

	pruned, err := svc.PruneOnce(ctx, now)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if pruned != 1 {
		t.Fatalf("expected to prune exactly the window-expired row, got %d", pruned)
	}
	// The pruned id is accepted again (a redelivery that late is a new
	// event).
	if accepted, err := sd.webhooks.RecordDelivery(ctx, sd.WS.ID, sd.Connection.ID, "old-delivery", now); err != nil || !accepted {
		t.Fatalf("the pruned delivery id must be acceptable again: %v %v", accepted, err)
	}
}
