package webhooks

// The StandaloneService tests (add-workspace-webhooks task 3.3): the
// management lifecycle over the fake store — reveal-once secrets, save-time
// filter and template validation, the D2 read-in list, the dry-run, and the
// delivery-log passthrough.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
	"github.com/oniharnantyo/onclaw/internal/webhookfilter"
)

// hmacHex computes hex(HMAC-SHA256(secret, body)) — the hmac_sha256 scheme's
// signature header value.
func hmacHex(t *testing.T, secret string, body []byte) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// timeNow is the test clock at an offset from the real present (negative is
// the past).
func timeNow(offset time.Duration) time.Time {
	return time.Now().Add(offset)
}

// standaloneServiceFixture wires the management service over the fake store:
// the connection lane's Service doubles as the StateForView seam, exactly as
// the composition root passes it.
type standaloneServiceFixture struct {
	st     store.Store
	ws     *domain.Workspace
	cipher *AESGCMCipher
	svc    *StandaloneService
}

func newStandaloneServiceFixture(t *testing.T) *standaloneServiceFixture {
	t.Helper()
	st := fake.New()
	ctx := context.Background()

	ws := &domain.Workspace{Slug: "manage", Name: "WS manage"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	cipher := NewAESGCMCipher(standaloneTestKey)
	connLane := NewService(st.Connections(), st.ConnectionWebhooks(), st.Agents(), st.Channels(), cipher)
	svc := NewStandaloneService(st.WorkspaceWebhooks(), cipher, st.Connections(), connLane)
	return &standaloneServiceFixture{st: st, ws: ws, cipher: cipher, svc: svc}
}

// createInput is a valid enabled endpoint create payload.
func createInput() CreateStandaloneWebhookInput {
	return CreateStandaloneWebhookInput{
		Name:             "gitlab-mr-review",
		Scheme:           domain.WorkspaceWebhookSchemeHmacSha256,
		EventSource:      domain.WorkspaceWebhookEventSource{Kind: domain.WorkspaceWebhookEventSourceHeader, Name: "X-Gitlab-Event"},
		DeliveryIDSource: "X-Gitlab-Event-UUID",
		TargetAgentID:    uuid.NewString(),
		TargetKind:       domain.ConnectionWebhookTargetChannel,
		TargetID:         "chan-code-review",
		Enabled:          true,
	}
}

// TestStandaloneService_Create_GeneratedSecret is the spec scenario: the
// secret is generated, stored encrypted, and returned exactly once.
func TestStandaloneService_Create_GeneratedSecret(t *testing.T) {
	f := newStandaloneServiceFixture(t)
	ctx := context.Background()

	created, err := f.svc.Create(ctx, f.ws.ID, createInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Secret == "" {
		t.Fatal("a generated secret must ride the create response")
	}
	if created.Webhook.SecretHint != created.Secret[len(created.Secret)-4:] {
		t.Errorf("hint = %q, want the last-4 of the secret", created.Webhook.SecretHint)
	}
	if !strings.HasPrefix(created.Webhook.SecretCiphertext, "v1:") {
		t.Errorf("stored envelope = %q, want a v1 envelope", created.Webhook.SecretCiphertext)
	}

	// The ciphertext decrypts back to the revealed plaintext, and the stored
	// entity carries it (the response is the only reveal).
	stored, err := f.svc.Get(ctx, f.ws.ID, created.Webhook.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	plaintext, err := f.cipher.DecryptSecret(f.ws.ID, stored.SecretCiphertext)
	if err != nil {
		t.Fatalf("decrypt stored envelope: %v", err)
	}
	if string(plaintext) != created.Secret {
		t.Error("the stored envelope must seal exactly the revealed secret")
	}

	// The create URL shape the pane displays for the sender's configuration.
	if want := "https://onclaw.example/api/ingest/hooks/" + f.ws.Slug + "/" + stored.ID; StandaloneIngestURL("https://onclaw.example", f.ws.Slug, stored.ID) != want {
		t.Errorf("ingest URL = %q, want %q", StandaloneIngestURL("https://onclaw.example", f.ws.Slug, stored.ID), want)
	}
}

// TestStandaloneService_Create_BYOSecret: a caller-supplied secret is stored
// and never re-revealed — the response carries no secret, the hint is set.
func TestStandaloneService_Create_BYOSecret(t *testing.T) {
	f := newStandaloneServiceFixture(t)
	ctx := context.Background()

	in := createInput()
	in.Secret = "my-own-shared-secret-from-the-sender"
	created, err := f.svc.Create(ctx, f.ws.ID, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Secret != "" {
		t.Fatalf("a BYO secret must never be re-revealed, got %q", created.Secret)
	}
	if created.Webhook.SecretHint != "nder" {
		t.Errorf("hint = %q, want the secret's last 4", created.Webhook.SecretHint)
	}
	plaintext, err := f.cipher.DecryptSecret(f.ws.ID, created.Webhook.SecretCiphertext)
	if err != nil || string(plaintext) != in.Secret {
		t.Fatalf("the BYO secret must be stored encrypted and intact: %q (%v)", plaintext, err)
	}
}

// TestStandaloneService_Create_InvalidFilterRejectedAtSave: the parser's
// message rides the ErrInvalid and nothing persists (spec: Invalid
// expression rejected at save).
func TestStandaloneService_Create_InvalidFilterRejectedAtSave(t *testing.T) {
	f := newStandaloneServiceFixture(t)
	ctx := context.Background()

	in := createInput()
	in.FilterExpression = `event == ` // unterminated: parse error
	created, err := f.svc.Create(ctx, f.ws.ID, in)
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected domain.ErrInvalid, got %v", err)
	}
	if created.Webhook != nil || created.Secret != "" {
		t.Fatal("a rejected create must return nothing")
	}
	// The parser's own message rides the error text.
	if !strings.Contains(err.Error(), "invalid filter expression") {
		t.Errorf("the rejection must carry the parser's message, got %q", err.Error())
	}
	endpoints, err := f.st.WorkspaceWebhooks().List(ctx, f.ws.ID)
	if err != nil || len(endpoints) != 0 {
		t.Fatalf("nothing may persist on a rejected create: %d endpoints (%v)", len(endpoints), err)
	}

	// A non-boolean expression is equally rejected at save.
	in.FilterExpression = `project.path_with_namespace`
	if err := createExpectInvalid(t, f, ctx, in); err != nil {
		t.Errorf("a non-boolean expression must reject: %v", err)
	}

	// A sandbox violation (function call) rejects too.
	in.FilterExpression = `len(payload) > 0`
	if err := createExpectInvalid(t, f, ctx, in); err != nil {
		t.Errorf("a function call must reject at save: %v", err)
	}
}

// createExpectInvalid asserts the create rejects with domain.ErrInvalid.
func createExpectInvalid(t *testing.T, f *standaloneServiceFixture, ctx context.Context, in CreateStandaloneWebhookInput) error {
	t.Helper()
	_, err := f.svc.Create(ctx, f.ws.ID, in)
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected domain.ErrInvalid, got %v", err)
	}
	return nil
}

// TestStandaloneService_Create_TemplateValidatedAtSave is the D7 whitelist
// discipline at the write boundary: a placeholder the sample payload cannot
// fill rejects with the renderer's reason.
func TestStandaloneService_Create_TemplateValidatedAtSave(t *testing.T) {
	f := newStandaloneServiceFixture(t)
	ctx := context.Background()

	in := createInput()
	in.TurnTemplate = "MR {object_attributes.iid}: {missing.field}"
	in.SamplePayload = gitlabMRPayload
	_, err := f.svc.Create(ctx, f.ws.ID, in)
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected domain.ErrInvalid, got %v", err)
	}
	if !strings.Contains(err.Error(), "missing.field") {
		t.Errorf("the rejection must name the unresolvable placeholder, got %q", err.Error())
	}

	// A covered template validates.
	in.TurnTemplate = "MR {object_attributes.iid} in {project.path_with_namespace}"
	if _, err := f.svc.Create(ctx, f.ws.ID, in); err != nil {
		t.Fatalf("a covered template must create: %v", err)
	}
}

// TestStandaloneService_RotateInvalidatesImmediately is the spec scenario:
// the replacement secret is shown once and the prior one stops verifying.
func TestStandaloneService_RotateInvalidatesImmediately(t *testing.T) {
	f := newStandaloneServiceFixture(t)
	ctx := context.Background()

	created, err := f.svc.Create(ctx, f.ws.ID, createInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	oldSecret := created.Secret

	rotated, err := f.svc.Rotate(ctx, f.ws.ID, created.Webhook.ID)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if rotated.Secret == "" || rotated.Secret == oldSecret {
		t.Fatalf("rotate must reveal a fresh secret, got %q", rotated.Secret)
	}

	// The stored envelope now seals the NEW secret only: the old one no
	// longer verifies against the stored credential.
	stored, err := f.svc.Get(ctx, f.ws.ID, created.Webhook.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	plaintext, err := f.cipher.DecryptSecret(f.ws.ID, stored.SecretCiphertext)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(plaintext) != rotated.Secret {
		t.Fatal("the stored envelope must seal the rotated-in secret")
	}
	body := []byte(gitlabMRPayload)
	if VerifySignature(domain.WorkspaceWebhookSchemeHmacSha256, hmacHex(t, oldSecret, body), plaintext, body) {
		t.Error("the rotated-out secret must no longer verify")
	}
	if !VerifySignature(domain.WorkspaceWebhookSchemeHmacSha256, hmacHex(t, rotated.Secret, body), plaintext, body) {
		t.Error("the rotated-in secret must verify")
	}
	if stored.SecretHint != rotated.Secret[len(rotated.Secret)-4:] {
		t.Errorf("hint = %q, want the new secret's last 4", stored.SecretHint)
	}

	if _, err := f.svc.Rotate(ctx, f.ws.ID, "no-such-endpoint"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("rotating an unknown endpoint must be ErrNotFound, got %v", err)
	}
}

// TestStandaloneService_SetEnabled covers pause/resume: disabling and
// enabling leave the rest of the state intact.
func TestStandaloneService_SetEnabled(t *testing.T) {
	f := newStandaloneServiceFixture(t)
	ctx := context.Background()

	created, err := f.svc.Create(ctx, f.ws.ID, createInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := created.Webhook.ID

	paused, err := f.svc.SetEnabled(ctx, f.ws.ID, id, false)
	if err != nil {
		t.Fatalf("disable: %v", err)
	}
	if paused.Enabled {
		t.Fatal("disable must pause the endpoint")
	}
	resumed, err := f.svc.SetEnabled(ctx, f.ws.ID, id, true)
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if !resumed.Enabled {
		t.Fatal("enable must resume the endpoint")
	}
	if _, err := f.svc.SetEnabled(ctx, f.ws.ID, "no-such-endpoint", true); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("enabling an unknown endpoint must be ErrNotFound, got %v", err)
	}
}

// TestStandaloneService_Update_InvalidFilterLeavesPriorInForce is the spec
// scenario: a bad expression on update rejects with the parse error and the
// prior expression stays in force.
func TestStandaloneService_Update_InvalidFilterLeavesPriorInForce(t *testing.T) {
	f := newStandaloneServiceFixture(t)
	ctx := context.Background()

	const goodFilter = `event == "merge_request"`
	in := createInput()
	in.FilterExpression = goodFilter
	created, err := f.svc.Create(ctx, f.ws.ID, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := created.Webhook.ID

	bad := UpdateStandaloneWebhookInput{
		Name:             "renamed-endpoint",
		Scheme:           domain.WorkspaceWebhookSchemePlainToken,
		EventSource:      domain.WorkspaceWebhookEventSource{Kind: domain.WorkspaceWebhookEventSourceHeader, Name: "X-Gitlab-Event"},
		DeliveryIDSource: "X-Gitlab-Event-UUID",
		FilterExpression: `event == `, // parse error
		TargetAgentID:    created.Webhook.TargetAgentID,
		TargetKind:       domain.ConnectionWebhookTargetChannel,
		TargetID:         "chan-code-review",
		Enabled:          true,
	}
	if _, err := f.svc.Update(ctx, f.ws.ID, id, bad); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected domain.ErrInvalid, got %v", err)
	}

	// The prior expression (and everything else) is intact — the rejection
	// happened before the write.
	stored, err := f.svc.Get(ctx, f.ws.ID, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.FilterExpression != goodFilter {
		t.Errorf("prior expression = %q, want %q", stored.FilterExpression, goodFilter)
	}
	if stored.Name != "gitlab-mr-review" {
		t.Errorf("a rejected update must change nothing, name = %q", stored.Name)
	}

	// The same edit with a valid expression lands, and the credential rides
	// the full-state write unchanged.
	bad.FilterExpression = `event == "pipeline"`
	updated, err := f.svc.Update(ctx, f.ws.ID, id, bad)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "renamed-endpoint" || updated.Scheme != domain.WorkspaceWebhookSchemePlainToken {
		t.Errorf("update not applied: %+v", updated)
	}
	plaintext, err := f.cipher.DecryptSecret(f.ws.ID, updated.SecretCiphertext)
	if err != nil || string(plaintext) != created.Secret {
		t.Error("update must not touch the credential")
	}
	if _, err := f.svc.Update(ctx, f.ws.ID, "no-such-endpoint", bad); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("updating an unknown endpoint must be ErrNotFound, got %v", err)
	}
}

// TestStandaloneService_List_FlagsConnectionRows is the D2 read-in: the list
// merges standalone endpoints with connection-bound webhooks flagged
// source "connection", sorted deterministically by name.
func TestStandaloneService_List_FlagsConnectionRows(t *testing.T) {
	f := newStandaloneServiceFixture(t)
	ctx := context.Background()

	// Two standalone endpoints, names out of insertion order to pin the sort.
	for _, name := range []string{"zebra-hook", "alpha-hook"} {
		in := createInput()
		in.Name = name
		if _, err := f.svc.Create(ctx, f.ws.ID, in); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}

	// A webhook-capable connection with an enabled webhook (the lane service
	// is the same StateForView seam the composition root passes).
	agent := &domain.Agent{WorkspaceID: f.ws.ID, Name: "Atlas", Slug: "atlas"}
	if err := f.st.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	channel := &domain.Channel{WorkspaceID: f.ws.ID, Name: "incidents", Slug: "incidents"}
	if err := f.st.Channels().CreateChannel(ctx, channel); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	conn := &domain.Connection{WorkspaceID: f.ws.ID, Service: "github", AccessLevel: domain.ConnectionAccessReadOnly}
	if err := f.st.Connections().Create(ctx, conn); err != nil {
		t.Fatalf("create connection: %v", err)
	}
	lane := NewService(f.st.Connections(), f.st.ConnectionWebhooks(), f.st.Agents(), f.st.Channels(), f.cipher)
	if _, _, err := lane.Enable(ctx, f.ws.ID, conn.ID, EnableRequest{
		AgentID:    agent.ID,
		TargetKind: domain.ConnectionWebhookTargetChannel,
		TargetID:   channel.ID,
	}); err != nil {
		t.Fatalf("enable connection webhook: %v", err)
	}

	// A non-webhook connection never surfaces in the read-in.
	figma := &domain.Connection{WorkspaceID: f.ws.ID, Service: "figma", AccessLevel: domain.ConnectionAccessReadOnly}
	if err := f.st.Connections().Create(ctx, figma); err != nil {
		t.Fatalf("create figma connection: %v", err)
	}

	rows, err := f.svc.List(ctx, f.ws.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	var standaloneNames, connectionServices []string
	var github *StandaloneWebhookListRow
	for i := range rows {
		switch rows[i].Source {
		case StandaloneWebhookSourceStandalone:
			standaloneNames = append(standaloneNames, rows[i].Webhook.Name)
		case StandaloneWebhookSourceConnection:
			connectionServices = append(connectionServices, rows[i].Connection.Service)
			if rows[i].Connection.Service == "github" {
				github = &rows[i]
			}
		default:
			t.Fatalf("unknown row source %q", rows[i].Source)
		}
	}
	if strings.Join(standaloneNames, ",") != "alpha-hook,zebra-hook" {
		t.Errorf("standalone rows = %v, want deterministic name order", standaloneNames)
	}
	if strings.Join(connectionServices, ",") != "github" {
		t.Errorf("connection rows = %v, want only the webhook-capable connection", connectionServices)
	}
	if github == nil || github.Connection.State == nil || !github.Connection.State.Enabled {
		t.Fatalf("the github read-in row must carry the live webhook state, got %+v", github)
	}
	if github.Connection.ConnectionID != conn.ID {
		t.Errorf("read-in connection id = %q, want %q", github.Connection.ConnectionID, conn.ID)
	}
}

// TestStandaloneService_DryRun covers the dry-run verdicts: match, ignored,
// eval error, unparseable sample, and the no-filter match — no side effects.
// The fixture's event source is a payload field, so the sample resolves the
// event exactly as a delivery body would.
func TestStandaloneService_DryRun(t *testing.T) {
	f := newStandaloneServiceFixture(t)
	ctx := context.Background()

	in := createInput()
	in.EventSource = domain.WorkspaceWebhookEventSource{Kind: domain.WorkspaceWebhookEventSourcePayloadField, Path: "object_kind"}
	in.FilterExpression = `event == "merge_request" && project.path_with_namespace == "team/api"`
	created, err := f.svc.Create(ctx, f.ws.ID, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := created.Webhook.ID

	t.Run("match", func(t *testing.T) {
		verdict, err := f.svc.DryRun(ctx, f.ws.ID, id, []byte(gitlabMRPayload))
		if err != nil {
			t.Fatalf("dry run: %v", err)
		}
		if verdict.Outcome != webhookfilter.VerdictMatch {
			t.Errorf("outcome = %v, want match", verdict.Outcome)
		}
	})

	t.Run("ignored", func(t *testing.T) {
		other := strings.Replace(gitlabMRPayload, "team/api", "team/frontend", 1)
		verdict, err := f.svc.DryRun(ctx, f.ws.ID, id, []byte(other))
		if err != nil {
			t.Fatalf("dry run: %v", err)
		}
		if verdict.Outcome != webhookfilter.VerdictIgnored {
			t.Errorf("outcome = %v, want ignored", verdict.Outcome)
		}
	})

	t.Run("eval error on a missing field", func(t *testing.T) {
		verdict, err := f.svc.DryRun(ctx, f.ws.ID, id, []byte(`{"object_kind":"merge_request"}`))
		if err != nil {
			t.Fatalf("dry run: %v", err)
		}
		if verdict.Outcome != webhookfilter.VerdictError {
			t.Errorf("outcome = %v, want error", verdict.Outcome)
		}
	})

	t.Run("unparseable sample", func(t *testing.T) {
		verdict, err := f.svc.DryRun(ctx, f.ws.ID, id, []byte("not json"))
		if err != nil {
			t.Fatalf("dry run: %v", err)
		}
		if verdict.Outcome != webhookfilter.VerdictError {
			t.Errorf("outcome = %v, want error", verdict.Outcome)
		}
	})

	t.Run("no filter matches everything", func(t *testing.T) {
		in := createInput()
		created, err := f.svc.Create(ctx, f.ws.ID, in)
		if err != nil {
			t.Fatalf("create unfiltered: %v", err)
		}
		verdict, err := f.svc.DryRun(ctx, f.ws.ID, created.Webhook.ID, []byte(`{"anything":true}`))
		if err != nil {
			t.Fatalf("dry run: %v", err)
		}
		if verdict.Outcome != webhookfilter.VerdictMatch {
			t.Errorf("a blank expression must match, got %v", verdict.Outcome)
		}
	})

	t.Run("header source resolves the sample's event key", func(t *testing.T) {
		in := createInput() // header source X-Gitlab-Event
		in.FilterExpression = `event == "merge_request"`
		created, err := f.svc.Create(ctx, f.ws.ID, in)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		// A header value cannot ride a JSON sample; the optional top-level
		// `event` key stands in for it.
		verdict, err := f.svc.DryRun(ctx, f.ws.ID, created.Webhook.ID, []byte(`{"event":"merge_request","x":1}`))
		if err != nil {
			t.Fatalf("dry run: %v", err)
		}
		if verdict.Outcome != webhookfilter.VerdictMatch {
			t.Errorf("outcome = %v, want match via the sample's event key", verdict.Outcome)
		}
		verdict, err = f.svc.DryRun(ctx, f.ws.ID, created.Webhook.ID, []byte(`{"event":"push"}`))
		if err != nil {
			t.Fatalf("dry run: %v", err)
		}
		if verdict.Outcome != webhookfilter.VerdictIgnored {
			t.Errorf("outcome = %v, want ignored", verdict.Outcome)
		}
	})

	// A dry run must never write: the delivery log stays empty.
	entries, err := f.svc.DeliveryLog(ctx, f.ws.ID, id, 0)
	if err != nil || len(entries) != 0 {
		t.Fatalf("a dry run must have no side effects: %d log rows (%v)", len(entries), err)
	}
}

// TestStandaloneService_DeliveryLogPassthrough: the log rows the ingress
// appended read back newest-first through the service.
func TestStandaloneService_DeliveryLogPassthrough(t *testing.T) {
	f := newStandaloneServiceFixture(t)
	ctx := context.Background()

	created, err := f.svc.Create(ctx, f.ws.ID, createInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := created.Webhook.ID
	store := f.st.WorkspaceWebhooks()
	for _, outcome := range []string{domain.WorkspaceWebhookDeliveryOutcomeSubmitted, domain.WorkspaceWebhookDeliveryOutcomeFiltered} {
		if err := store.AppendDeliveryLog(ctx, f.ws.ID, id, domain.WebhookDeliveryLogEntry{Event: "merge_request", Outcome: outcome, RunID: "run-1"}); err != nil {
			t.Fatalf("append log: %v", err)
		}
	}
	entries, err := f.svc.DeliveryLog(ctx, f.ws.ID, id, 10)
	if err != nil {
		t.Fatalf("delivery log: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("log rows = %d, want 2", len(entries))
	}
	if _, err := f.svc.DeliveryLog(ctx, f.ws.ID, "no-such-endpoint", 10); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("the log of an unknown endpoint must be ErrNotFound, got %v", err)
	}
}

// TestStandaloneService_PruneOnce exercises both store prunes through the
// service sweep.
func TestStandaloneService_PruneOnce(t *testing.T) {
	f := newStandaloneServiceFixture(t)
	ctx := context.Background()

	created, err := f.svc.Create(ctx, f.ws.ID, createInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := created.Webhook.ID
	store := f.st.WorkspaceWebhooks()

	old := timeNow(-2 * domain.WorkspaceWebhookDeliveryWindow)
	if _, err := store.RecordDelivery(ctx, f.ws.ID, id, "old-key", old); err != nil {
		t.Fatalf("record old delivery: %v", err)
	}
	if err := store.AppendDeliveryLog(ctx, f.ws.ID, id, domain.WebhookDeliveryLogEntry{Event: "e", Outcome: domain.WorkspaceWebhookDeliveryOutcomeSubmitted}); err != nil {
		t.Fatalf("append log: %v", err)
	}

	if err := f.svc.PruneOnce(ctx, timeNow(0)); err != nil {
		t.Fatalf("prune once: %v", err)
	}
	// The dedupe row recorded outside the window is gone; a fresh delivery
	// with the same key is accepted again.
	accepted, err := store.RecordDelivery(ctx, f.ws.ID, id, "old-key", timeNow(0))
	if err != nil || !accepted {
		t.Fatalf("the expired dedupe row must be pruned: accepted=%v (%v)", accepted, err)
	}
}

// TestStandaloneService_Delete covers the lifecycle's last step.
func TestStandaloneService_Delete(t *testing.T) {
	f := newStandaloneServiceFixture(t)
	ctx := context.Background()

	created, err := f.svc.Create(ctx, f.ws.ID, createInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := f.svc.Delete(ctx, f.ws.ID, created.Webhook.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := f.svc.Get(ctx, f.ws.ID, created.Webhook.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a deleted endpoint must read ErrNotFound, got %v", err)
	}
	if err := f.svc.Delete(ctx, f.ws.ID, created.Webhook.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("deleting an unknown endpoint must be ErrNotFound, got %v", err)
	}
}
