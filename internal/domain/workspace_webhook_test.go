package domain

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func validWorkspaceWebhook() *WorkspaceWebhook {
	return &WorkspaceWebhook{
		WorkspaceID:      uuid.NewString(),
		Name:             "gitlab-mr-review",
		Scheme:           WorkspaceWebhookSchemeTimestampedHmac,
		SecretCiphertext: "v1:bm9uY2U:Y2lwaGVydGV4dA",
		SecretHint:       "bG9s",
		EventSource:      WorkspaceWebhookEventSource{Kind: WorkspaceWebhookEventSourceHeader, Name: "X-Gitlab-Event"},
		DeliveryIDSource: "X-Gitlab-Event-UUID",
		TargetAgentID:    uuid.NewString(),
		TargetKind:       ConnectionWebhookTargetChannel,
		TargetID:         "chan-code-review",
		Enabled:          true,
	}
}

// A fully-declared enabled endpoint validates (tasks.md 1.1).
func TestWorkspaceWebhook_ValidAccepted(t *testing.T) {
	if err := validWorkspaceWebhook().Validate(); err != nil {
		t.Fatalf("expected valid webhook to pass, got %v", err)
	}
}

func TestWorkspaceWebhook_SchemeCatalog(t *testing.T) {
	for _, scheme := range []string{WorkspaceWebhookSchemePlainToken, WorkspaceWebhookSchemeHmacSha256, WorkspaceWebhookSchemeTimestampedHmac} {
		w := validWorkspaceWebhook()
		w.Scheme = scheme
		if err := w.Validate(); err != nil {
			t.Fatalf("expected scheme %q to pass, got %v", scheme, err)
		}
	}
	w := validWorkspaceWebhook()
	w.Scheme = "hmac_md5"
	err := w.Validate()
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for bad scheme, got %v", err)
	}
	if !strings.Contains(err.Error(), "hmac_md5") {
		t.Fatalf("expected the bad scheme named in the message, got %q", err.Error())
	}
}

func TestWorkspaceWebhook_RequiredScopeAndName(t *testing.T) {
	w := validWorkspaceWebhook()
	w.WorkspaceID = ""
	if err := w.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for empty workspace id, got %v", err)
	}
	w = validWorkspaceWebhook()
	w.Name = "   "
	if err := w.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for blank name, got %v", err)
	}
	if err := (*WorkspaceWebhook)(nil).Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for nil webhook, got %v", err)
	}
}

func TestWorkspaceWebhook_EventSourceShapes(t *testing.T) {
	// Well-formed per kind.
	header := validWorkspaceWebhook()
	header.EventSource = WorkspaceWebhookEventSource{Kind: WorkspaceWebhookEventSourceHeader, Name: "X-Event"}
	if err := header.Validate(); err != nil {
		t.Fatalf("expected header source to pass, got %v", err)
	}
	field := validWorkspaceWebhook()
	field.EventSource = WorkspaceWebhookEventSource{Kind: WorkspaceWebhookEventSourcePayloadField, Path: "project.path_with_namespace"}
	if err := field.Validate(); err != nil {
		t.Fatalf("expected payload field source to pass, got %v", err)
	}
	fixed := validWorkspaceWebhook()
	fixed.EventSource = WorkspaceWebhookEventSource{Kind: WorkspaceWebhookEventSourceFixed, Value: "merge_request"}
	if err := fixed.Validate(); err != nil {
		t.Fatalf("expected fixed source to pass, got %v", err)
	}

	// Malformed per kind.
	bad := validWorkspaceWebhook()
	bad.EventSource = WorkspaceWebhookEventSource{Kind: WorkspaceWebhookEventSourceHeader, Name: " "}
	if err := bad.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for header source without a name, got %v", err)
	}
	bad = validWorkspaceWebhook()
	bad.EventSource = WorkspaceWebhookEventSource{Kind: WorkspaceWebhookEventSourcePayloadField, Path: "project..path"}
	if err := bad.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for malformed payload field path, got %v", err)
	}
	bad = validWorkspaceWebhook()
	bad.EventSource = WorkspaceWebhookEventSource{Kind: WorkspaceWebhookEventSourceFixed, Value: ""}
	if err := bad.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for fixed source without a value, got %v", err)
	}
	bad = validWorkspaceWebhook()
	bad.EventSource.Kind = "oracle"
	if err := bad.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for unknown event source kind, got %v", err)
	}
}

func TestWorkspaceWebhook_DeliveryIDSourceRequired(t *testing.T) {
	w := validWorkspaceWebhook()
	w.DeliveryIDSource = ""
	if err := w.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for blank delivery id source, got %v", err)
	}
}

func TestWorkspaceWebhook_EmptyTargetRejected(t *testing.T) {
	// The target binding is required unconditionally — a standalone endpoint
	// is born with its binding (tasks.md 1.1).
	w := validWorkspaceWebhook()
	w.TargetKind = ""
	if err := w.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for empty target kind, got %v", err)
	}
	w = validWorkspaceWebhook()
	w.TargetKind = "dm"
	if err := w.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for out-of-catalog target kind, got %v", err)
	}
	w = validWorkspaceWebhook()
	w.TargetID = ""
	if err := w.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for empty target id, got %v", err)
	}
	w = validWorkspaceWebhook()
	w.TargetAgentID = ""
	if err := w.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for empty target agent id, got %v", err)
	}
	w = validWorkspaceWebhook()
	w.TargetAgentID = "not-a-uuid"
	if err := w.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for non-uuid target agent id, got %v", err)
	}
}

func TestWorkspaceWebhook_EnabledRequiresCredential(t *testing.T) {
	w := validWorkspaceWebhook()
	w.SecretCiphertext = ""
	if err := w.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for enabled-without-envelope, got %v", err)
	}
	w = validWorkspaceWebhook()
	w.SecretCiphertext = "raw-plaintext-secret"
	if err := w.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for non-envelope ciphertext, got %v", err)
	}
	w = validWorkspaceWebhook()
	w.SecretHint = ""
	if err := w.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for enabled-without-hint, got %v", err)
	}

	// A disabled endpoint may lack the credential (e.g. pre-rotation pause
	// keeps state; creation-before-enablement stores no secret yet).
	w = validWorkspaceWebhook()
	w.Enabled = false
	w.SecretCiphertext = ""
	w.SecretHint = ""
	if err := w.Validate(); err != nil {
		t.Fatalf("expected disabled endpoint without credential to pass, got %v", err)
	}
}

func TestWorkspaceWebhook_ExpressionMayBeBlankOrNot(t *testing.T) {
	// Blank expression: no filter — legal.
	blank := validWorkspaceWebhook()
	blank.FilterExpression = ""
	if err := blank.Validate(); err != nil {
		t.Fatalf("expected blank expression to pass, got %v", err)
	}
	// Non-blank expression rides as given; compilation is the save-time
	// filter engine's check (design.md D3), not the domain's.
	nonBlank := validWorkspaceWebhook()
	nonBlank.FilterExpression = `event == "merge_request" && project.path_with_namespace == "team/api"`
	if err := nonBlank.Validate(); err != nil {
		t.Fatalf("expected non-blank expression to pass, got %v", err)
	}
	silly := validWorkspaceWebhook()
	silly.FilterExpression = "this is not (valid expr"
	if err := silly.Validate(); err != nil {
		t.Fatalf("expected syntactically-broken expression to pass domain shape checks, got %v", err)
	}
}

func TestWorkspaceWebhook_TemplateShape(t *testing.T) {
	sample := `{"project":{"path_with_namespace":"team/api"},"user":{"name":"kim"}}`

	// A template with well-formed placeholders and its sample payload passes.
	ok := validWorkspaceWebhook()
	ok.TurnTemplate = "MR on {project.path_with_namespace} by {user.name}"
	ok.SamplePayload = sample
	if err := ok.Validate(); err != nil {
		t.Fatalf("expected well-formed template to pass, got %v", err)
	}

	// Malformed placeholder shape: nested braces.
	nested := validWorkspaceWebhook()
	nested.TurnTemplate = "bad {a{b}} template"
	nested.SamplePayload = sample
	if err := nested.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for malformed placeholder, got %v", err)
	}

	// Malformed placeholder shape: empty braces.
	empty := validWorkspaceWebhook()
	empty.TurnTemplate = "bad {} template"
	empty.SamplePayload = sample
	if err := empty.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for empty placeholder, got %v", err)
	}

	// A template without its sample payload cannot be save-validated —
	// rejected (design.md D7 fail-closed).
	noSample := validWorkspaceWebhook()
	noSample.TurnTemplate = "MR on {project.path_with_namespace}"
	if err := noSample.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for template without sample payload, got %v", err)
	}

	// A non-blank sample payload must be valid JSON.
	badSample := validWorkspaceWebhook()
	badSample.SamplePayload = "{not json"
	if err := badSample.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for non-JSON sample payload, got %v", err)
	}
}

// Delivery-log write shape: the outcome must be in the catalog; id, scope,
// and timestamp are store-stamped (tasks.md 1.1).
func TestWebhookDeliveryLogEntry_OutcomeCatalog(t *testing.T) {
	for _, outcome := range []string{
		WorkspaceWebhookDeliveryOutcomeSubmitted,
		WorkspaceWebhookDeliveryOutcomeFiltered,
		WorkspaceWebhookDeliveryOutcomeDuplicate,
		WorkspaceWebhookDeliveryOutcomeRejected,
		WorkspaceWebhookDeliveryOutcomeFailed,
	} {
		e := &WebhookDeliveryLogEntry{Outcome: outcome}
		if err := e.Validate(); err != nil {
			t.Fatalf("expected outcome %q to pass, got %v", outcome, err)
		}
	}
	e := &WebhookDeliveryLogEntry{Outcome: "exploded"}
	if err := e.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for unknown outcome, got %v", err)
	}
	if err := (*WebhookDeliveryLogEntry)(nil).Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for nil entry, got %v", err)
	}
}

// The exact scheme constant values are a cross-worker contract (migration
// CHECK + ingress verifier catalog) — pin them.
func TestWorkspaceWebhook_SchemeConstantValues(t *testing.T) {
	if WorkspaceWebhookSchemePlainToken != "plain_token" {
		t.Fatalf("plain_token constant drifted: %q", WorkspaceWebhookSchemePlainToken)
	}
	if WorkspaceWebhookSchemeHmacSha256 != "hmac_sha256" {
		t.Fatalf("hmac_sha256 constant drifted: %q", WorkspaceWebhookSchemeHmacSha256)
	}
	if WorkspaceWebhookSchemeTimestampedHmac != "timestamped_hmac" {
		t.Fatalf("timestamped_hmac constant drifted: %q", WorkspaceWebhookSchemeTimestampedHmac)
	}
}
