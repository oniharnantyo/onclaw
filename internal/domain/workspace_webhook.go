package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Webhook signature schemes a standalone workspace webhook endpoint can
// declare (add-workspace-webhooks tasks.md 1.1). The scheme names the
// verifier the ingress pipeline runs — the endpoint carries the credential
// declaration (design.md D1/D4), the pipeline carries the code. These exact
// values are a cross-worker contract: the migration's scheme CHECK and the
// ingress verifier catalog both match them literally.
const (
	// WorkspaceWebhookSchemePlainToken: the signature header carries the
	// shared secret verbatim (GitLab's X-Gitlab-Token); the comparison is
	// constant-time against the endpoint's secret.
	WorkspaceWebhookSchemePlainToken = "plain_token"
	// WorkspaceWebhookSchemeHmacSha256: the signature header carries the hex
	// HMAC-SHA256(raw body) keyed with the endpoint's secret; the comparison
	// is constant-time over the raw body bytes.
	WorkspaceWebhookSchemeHmacSha256 = "hmac_sha256"
	// WorkspaceWebhookSchemeTimestampedHmac: the signature header carries
	// "t=<unix>,v1=<hex(HMAC-SHA256(secret, "{ts}.{body}"))>" — replay
	// protection via the ±300s acceptance skew plus the durable
	// signature-hash dedupe (design.md D4).
	WorkspaceWebhookSchemeTimestampedHmac = "timestamped_hmac"
)

// Workspace webhook event-source kinds — the discriminator half of the
// declared event source (spec: Webhook endpoint lifecycle): where the
// pipeline resolves each delivery's event value from.
const (
	// WorkspaceWebhookEventSourceHeader resolves the event from a request
	// header name (e.g. "X-Gitlab-Event").
	WorkspaceWebhookEventSourceHeader = "header"
	// WorkspaceWebhookEventSourcePayloadField resolves the event from a
	// dotted payload field path (e.g. "object_kind").
	WorkspaceWebhookEventSourcePayloadField = "payload_field"
	// WorkspaceWebhookEventSourceFixed resolves the event to a fixed value —
	// the endpoint is dedicated to one event.
	WorkspaceWebhookEventSourceFixed = "fixed"
)

// WorkspaceWebhookTimestampSkew is the timestamped-HMAC acceptance window
// (design.md D4): a delivery's `t=` timestamp must lie within ±this of the
// server clock or the verifier rejects it as a stale capture — replay within
// the window is then the durable signature-hash dedupe's job.
const WorkspaceWebhookTimestampSkew = 300 * time.Second

// WorkspaceWebhookDeliveryWindow bounds the delivery-dedupe table
// (add-workspace-webhooks design.md D4): the hourly sweep prunes rows
// recorded further back than this, mirroring the connection-webhook window.
const WorkspaceWebhookDeliveryWindow = 7 * 24 * time.Hour

// WorkspaceWebhookDeliveryLogKeep is the bounded per-endpoint delivery-log
// retention (design.md D8): the sweep keeps the newest 100 rows per webhook
// — enough to answer "did the sender's event reach the agent, and if not,
// why not" without becoming a log-analysis product.
const WorkspaceWebhookDeliveryLogKeep = 100

// Delivery-log outcome catalog (design.md D8): one row per processed
// delivery records exactly one of these.
const (
	WorkspaceWebhookDeliveryOutcomeSubmitted = "submitted" // enqueued; RunID links the run
	WorkspaceWebhookDeliveryOutcomeFiltered  = "filtered"  // filter expression false / eval error
	WorkspaceWebhookDeliveryOutcomeDuplicate = "duplicate" // dedupe replay, acknowledged without a run
	WorkspaceWebhookDeliveryOutcomeRejected  = "rejected"  // verification failed
	WorkspaceWebhookDeliveryOutcomeFailed    = "failed"    // verified but processing dropped it
)

// WorkspaceWebhookEventSource is the endpoint's declared event source: where
// each delivery's event value resolves from. Exactly one field is
// meaningful per Kind — Name for headers, Path for payload fields, Value for
// the fixed form. Stored as jsonb (migration 000075).
type WorkspaceWebhookEventSource struct {
	// Kind is one of the WorkspaceWebhookEventSource* constants.
	Kind string `json:"kind"`
	// Name is the request header name (Kind = header).
	Name string `json:"name,omitempty"`
	// Path is the dotted payload field path (Kind = payload_field).
	Path string `json:"path,omitempty"`
	// Value is the fixed event value (Kind = fixed).
	Value string `json:"value,omitempty"`
}

// WorkspaceWebhook is one standalone webhook endpoint (add-workspace-webhooks
// tasks.md 1.1, design.md D1): its own aggregate row — credential envelope
// with display-once hint, declared event source and delivery-id source,
// save-time-validated filter expression and turn template, and the target
// binding (one agent plus a thread or channel). The plaintext secret never
// persists and the ciphertext never serializes.
type WorkspaceWebhook struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	// Name is the endpoint's display name (e.g. "gitlab-mr-review"); also
	// names the run's service attribution.
	Name string `json:"name"`
	// Scheme is one of the WorkspaceWebhookScheme* constants.
	Scheme string `json:"scheme"`
	// SecretCiphertext is the AES-256-GCM envelope of the endpoint secret
	// (the instance master key, workspace ID as AAD). Revealed exactly once
	// at create/rotate; never serialized.
	SecretCiphertext string `json:"-"`
	// SecretHint is the display-once residue — the secret's last 4
	// characters — shown after the one-time reveal.
	SecretHint string `json:"secret_hint"`
	// EventSource is the declared source of each delivery's event value.
	EventSource WorkspaceWebhookEventSource `json:"event_source"`
	// DeliveryIDSource is the request header name carrying the provider's
	// delivery id the dedupe keys on (e.g. "X-Gitlab-Event-UUID").
	DeliveryIDSource string `json:"delivery_id_source"`
	// FilterExpression is the optional expr-lang expression evaluated
	// against {payload fields, event}; blank means no filter. Compiled and
	// validated at save time by the service (design.md D3) — the domain
	// checks shape only.
	FilterExpression string `json:"filter_expression,omitempty"`
	// TurnTemplate is the optional prompt text with {field} placeholders;
	// blank means the payload rides as labeled, truncated JSON (design.md
	// D7). Placeholders validate against the sample payload at save time.
	TurnTemplate string `json:"turn_template,omitempty"`
	// SamplePayload is the admin-supplied JSON payload the template
	// placeholders validate against at save time, stored for the dry-run
	// tester and the form's round trip. Blank when no template is declared.
	SamplePayload string `json:"sample_payload,omitempty"`
	// TargetAgentID is the bound workspace agent the turn runs as.
	TargetAgentID string `json:"target_agent_id"`
	// TargetKind is one of the ConnectionWebhookTarget* constants — the
	// thread/channel discriminator is shared with connection webhooks.
	TargetKind string `json:"target_kind"`
	// TargetID is the bound thread (session) or channel id.
	TargetID string `json:"target_id"`
	// Enabled is the pause/resume toggle: a paused endpoint ignores
	// deliveries without verification side effects (spec: Paused endpoint).
	Enabled bool `json:"enabled"`
	// CreatedAt and UpdatedAt are app-managed (store invariant 4).
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Validate checks the endpoint's structural shape (the store boundary's
// check): workspace scope and name present, the scheme in the catalog, the
// event source well-formed per kind, the delivery-id source present, a
// complete target binding (required unconditionally — unlike a connection's
// webhook state, a standalone endpoint is born with its binding), an enabled
// state carrying the credential envelope with its hint, and a declared
// template shaped for save-time validation. Expression compilation and
// template-vs-sample placeholder matching are the service's save-time checks
// (design.md D3/D7) — the domain enforces shape only.
func (w *WorkspaceWebhook) Validate() error {
	if w == nil {
		return ErrInvalid
	}
	if strings.TrimSpace(w.WorkspaceID) == "" {
		return fmt.Errorf("%w: webhook workspace id cannot be empty", ErrInvalid)
	}
	if strings.TrimSpace(w.Name) == "" {
		return fmt.Errorf("%w: webhook name cannot be empty", ErrInvalid)
	}

	switch w.Scheme {
	case WorkspaceWebhookSchemePlainToken, WorkspaceWebhookSchemeHmacSha256, WorkspaceWebhookSchemeTimestampedHmac:
	default:
		return fmt.Errorf("%w: webhook scheme %q must be %s, %s, or %s", ErrInvalid, w.Scheme, WorkspaceWebhookSchemePlainToken, WorkspaceWebhookSchemeHmacSha256, WorkspaceWebhookSchemeTimestampedHmac)
	}

	switch w.EventSource.Kind {
	case WorkspaceWebhookEventSourceHeader:
		if strings.TrimSpace(w.EventSource.Name) == "" {
			return fmt.Errorf("%w: webhook event source kind %s requires a header name", ErrInvalid, WorkspaceWebhookEventSourceHeader)
		}
	case WorkspaceWebhookEventSourcePayloadField:
		if !isValidWebhookFieldPath(w.EventSource.Path) {
			return fmt.Errorf("%w: webhook event source kind %s requires a well-formed payload field path, got %q", ErrInvalid, WorkspaceWebhookEventSourcePayloadField, w.EventSource.Path)
		}
	case WorkspaceWebhookEventSourceFixed:
		if strings.TrimSpace(w.EventSource.Value) == "" {
			return fmt.Errorf("%w: webhook event source kind %s requires a fixed value", ErrInvalid, WorkspaceWebhookEventSourceFixed)
		}
	default:
		return fmt.Errorf("%w: webhook event source kind %q must be %s, %s, or %s", ErrInvalid, w.EventSource.Kind, WorkspaceWebhookEventSourceHeader, WorkspaceWebhookEventSourcePayloadField, WorkspaceWebhookEventSourceFixed)
	}

	if strings.TrimSpace(w.DeliveryIDSource) == "" {
		return fmt.Errorf("%w: webhook delivery id source is required", ErrInvalid)
	}

	switch w.TargetKind {
	case ConnectionWebhookTargetThread, ConnectionWebhookTargetChannel:
		if strings.TrimSpace(w.TargetAgentID) == "" {
			return fmt.Errorf("%w: webhook target agent id is required when a target kind is set", ErrInvalid)
		}
		if strings.TrimSpace(w.TargetID) == "" {
			return fmt.Errorf("%w: webhook target id is required when a target kind is set", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: webhook target kind %q must be %s or %s", ErrInvalid, w.TargetKind, ConnectionWebhookTargetThread, ConnectionWebhookTargetChannel)
	}
	// The target agent id must be uuid-shaped (the stores bind it as a uuid);
	// existence is the stores' check.
	if !isValidUUIDShape(w.TargetAgentID) {
		return fmt.Errorf("%w: webhook target agent id %q must be a uuid", ErrInvalid, w.TargetAgentID)
	}

	// FilterExpression is intentionally unchecked here: it may be blank (no
	// filter), and a non-blank one is compiled at save time by the filter
	// engine (design.md D3) — the parser's message is the rejection reason.

	// A declared template must be structurally renderable and must carry the
	// sample payload its placeholders validate against at save time
	// (design.md D7 — the whitelist discipline, moved to save time).
	if strings.TrimSpace(w.TurnTemplate) != "" {
		if _, ok := webhookTemplatePlaceholders(w.TurnTemplate); !ok {
			return fmt.Errorf("%w: webhook turn template has a malformed {field} placeholder", ErrInvalid)
		}
		if strings.TrimSpace(w.SamplePayload) == "" {
			return fmt.Errorf("%w: a webhook turn template requires a sample payload to validate its placeholders against", ErrInvalid)
		}
	}
	// A non-blank sample payload must be JSON — it feeds the save-time
	// placeholder check and the dry-run tester.
	if strings.TrimSpace(w.SamplePayload) != "" {
		if !json.Valid([]byte(w.SamplePayload)) {
			return fmt.Errorf("%w: webhook sample payload must be valid JSON", ErrInvalid)
		}
	}

	if w.Enabled {
		if !isV1Envelope(w.SecretCiphertext) {
			return fmt.Errorf("%w: enabling webhooks requires a v1 secret envelope", ErrInvalid)
		}
		if strings.TrimSpace(w.SecretHint) == "" {
			return fmt.Errorf("%w: a generated webhook secret requires its display hint", ErrInvalid)
		}
	}
	return nil
}

// Webhook run statuses (add-webhook-channel-visibility 4.1): the live state
// of a submitted delivery's run, recorded on the delivery-log row — the
// submitted log row doubles as the run record (design.md D5). The values
// match the scheduler runs' wire vocabulary so one run-history surface
// renders both lanes; "missed"/"blocked" are scheduler-only states a webhook
// run never takes.
const (
	WorkspaceWebhookRunStatusRunning   = "running"
	WorkspaceWebhookRunStatusCompleted = "completed"
	WorkspaceWebhookRunStatusFailed    = "failed"
	WorkspaceWebhookRunStatusCancelled = "cancelled"
)

// WebhookDeliveryLogEntry is one processed delivery's row in the bounded
// per-endpoint history (add-workspace-webhooks design.md D8): the resolved
// event, the outcome, the optional run link, and a short detail reason —
// the management surface's "did the event reach the agent, and if not, why
// not" answer.
type WebhookDeliveryLogEntry struct {
	// ID is store-assigned when empty.
	ID string `json:"id"`
	// WebhookID is the endpoint the delivery processed against.
	WebhookID string `json:"webhook_id"`
	// Event is the resolved event value; blank for a delivery rejected
	// before its event source resolved.
	Event string `json:"event"`
	// Outcome is one of the WorkspaceWebhookDeliveryOutcome* constants.
	Outcome string `json:"outcome"`
	// RunID links the submitted run (Outcome = submitted); blank otherwise.
	RunID string `json:"run_id,omitempty"`
	// Detail is the optional short reason (e.g. the filter's ignored reason).
	Detail string `json:"detail,omitempty"`
	// Status is the submitted run's live state (design.md D5): running at
	// submit, finished by the submitter's drain. Blank on non-submitted
	// outcomes and on legacy rows written before telemetry (read as
	// completed — the fanout's success-by-default).
	Status string `json:"status,omitempty"`
	// DurationMS is the run's wall-clock drain duration, recorded when the
	// run finishes; 0 while running or unknown.
	DurationMS int64 `json:"duration_ms,omitempty"`
	// TokensUsed is the run's total token usage (the last usage frame),
	// recorded when the run finishes; 0 while running or unknown.
	TokensUsed int `json:"tokens_used,omitempty"`
	// CreatedAt is the processing timestamp, store-stamped.
	CreatedAt time.Time `json:"created_at"`
}

// Validate checks the log entry's write shape: the outcome must be in the
// catalog. ID, webhook scope, and timestamp are store-stamped.
func (e *WebhookDeliveryLogEntry) Validate() error {
	if e == nil {
		return ErrInvalid
	}
	switch e.Outcome {
	case WorkspaceWebhookDeliveryOutcomeSubmitted,
		WorkspaceWebhookDeliveryOutcomeFiltered,
		WorkspaceWebhookDeliveryOutcomeDuplicate,
		WorkspaceWebhookDeliveryOutcomeRejected,
		WorkspaceWebhookDeliveryOutcomeFailed:
		return nil
	default:
		return fmt.Errorf("%w: webhook delivery log outcome %q must be submitted, filtered, duplicate, rejected, or failed", ErrInvalid, e.Outcome)
	}
}

// WorkspaceWebhookRunsLimit bounds the workspace-wide webhook run listing
// (add-webhook-channel-visibility 4.4): the run-history feed's default page,
// matching the run screen's scheduler page size.
const WorkspaceWebhookRunsLimit = 100

// WorkspaceWebhookRun is one entry of the workspace-wide webhook run
// listing (add-webhook-channel-visibility 4.4): a submitted delivery whose
// run the run-history screen lists beside scheduled runs. It is a read
// projection over the delivery log joined to the endpoint and its bound
// agent — never written directly.
type WorkspaceWebhookRun struct {
	// ID is the delivery-log row id — the listing's stable key.
	ID string `json:"id"`
	// WebhookID and WebhookName identify the originating endpoint (the
	// run-history trigger).
	WebhookID   string `json:"webhook_id"`
	WebhookName string `json:"webhook_name"`
	// Event is the resolved event value.
	Event string `json:"event"`
	// RunID is the run's transcript session id.
	RunID string `json:"run_id"`
	// AgentID and AgentName identify the invoked agent (name resolved at
	// read time; blank when the agent has since been deleted).
	AgentID   string `json:"agent_id"`
	AgentName string `json:"agent_name"`
	// Status is the run's live state (design.md D5); legacy rows with a
	// blank status read as completed.
	Status string `json:"status"`
	// DurationMS and TokensUsed are the run's finished telemetry; zero
	// while running or unknown.
	DurationMS int64 `json:"duration_ms"`
	TokensUsed int   `json:"tokens_used"`
	// StartedAt is the delivery's processing timestamp.
	StartedAt time.Time `json:"started_at"`
}
