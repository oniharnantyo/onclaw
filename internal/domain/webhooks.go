package domain

import (
	"fmt"
	"strings"
	"time"
)

// Webhook signature schemes a recipe can declare (add-connection-webhooks
// tasks.md 1.1). The scheme names the verifier the ingress pipeline runs —
// recipes carry provider plumbing (D1), the pipeline carries the code.
const (
	// RecipeWebhookSchemeHMACSHA256: the signature header carries
	// "<prefix><hex hmac-sha256(raw body)>" keyed with the connection's
	// secret (GitHub's X-Hub-Signature-256 uses the "sha256=" prefix). The
	// comparison is constant-time over the raw body bytes.
	RecipeWebhookSchemeHMACSHA256 = "hmac_sha256"
	// RecipeWebhookSchemeSecretToken: the signature header carries the shared
	// secret verbatim (GitLab's X-Gitlab-Token); the comparison is
	// constant-time against the connection's secret.
	RecipeWebhookSchemeSecretToken = "secret_token"
)

// Webhook target kinds — the discriminator half of the target binding
// (add-connection-webhooks tasks.md 1.2): a connection's deliveries render
// into a turn for the bound agent in the bound thread or channel.
const (
	ConnectionWebhookTargetThread  = "thread"
	ConnectionWebhookTargetChannel = "channel"
)

// ConnectionWebhookDeliveryWindow bounds the delivery-dedupe table (design.md
// D3): the cleanup hook prunes rows recorded further back than this, and
// provider redeliveries older than the window are accepted again (a
// redelivery that late is not a retry, it is a new event).
const ConnectionWebhookDeliveryWindow = 7 * 24 * time.Hour

// RecipeWebhookTemplate is one event's prompt rendering declaration
// (add-connection-webhooks tasks.md 1.1): the template text interpolates
// {field} placeholders, and Fields is the explicit whitelist of payload field
// paths it may interpolate — every placeholder must be whitelisted and every
// whitelisted field must appear, so recipe/payload drift fails at
// registration, not at delivery (design.md risks: fail closed).
type RecipeWebhookTemplate struct {
	// Event is the catalog event id this template renders (exactly one
	// template per catalog event).
	Event string `json:"event"`
	// Fields is the whitelist of dotted payload field paths the template may
	// interpolate (e.g. "pull_request.title", "repository.full_name").
	Fields []string `json:"fields"`
	// Template is the prompt text with {field} placeholders. Literal braces
	// are not representable — recipe authors avoid them.
	Template string `json:"template"`
}

// RecipeWebhookSetup is the provider-facing setup copy the manage surface
// shows (web tasks.md 4.3): which headers the provider sends, the ingest
// path shape to paste, and the per-provider configuration steps. The ingest
// host and the generated secret are per-connection values the UI
// interpolates at display time — they are deliberately not recipe data.
type RecipeWebhookSetup struct {
	// SignatureHeader is the provider header carrying the signature or
	// secret token (e.g. "X-Hub-Signature-256", "X-Gitlab-Token").
	SignatureHeader string `json:"signature_header"`
	// EventTypeHeader is the header naming the event type (e.g.
	// "X-GitHub-Event", "X-Gitlab-Event").
	EventTypeHeader string `json:"event_type_header"`
	// DeliveryIDHeader is the header carrying the provider's delivery id the
	// dedupe keys on (e.g. "X-GitHub-Delivery", "X-Gitlab-Event-UUID").
	DeliveryIDHeader string `json:"delivery_id_header"`
	// URLPathShape is the ingest path to configure at the provider, with
	// {workspace_slug} and {connection_id} placeholders the UI fills in.
	URLPathShape string `json:"url_path_shape"`
	// Help is the exact provider configuration steps (real copy).
	Help string `json:"help"`
}

// RecipeWebhook is a recipe's webhook declaration (add-connection-webhooks
// tasks.md 1.1): the ordered event catalog, the read-flavored default
// selection, the provider's signature scheme, the per-event prompt templates
// with their whitelisted payload fields, and the provider setup copy. Nil on
// a Recipe means the service declares no webhook support; a non-nil
// declaration is validated as part of ValidateRecipe — recipe knowledge is
// release-shippable server-side data, never user input.
type RecipeWebhook struct {
	// Events is the ordered event catalog — "<event>.<action>" ids
	// (e.g. "pull_request.opened") or bare event ids for actionless
	// deliveries (e.g. "push"). Order is the manage surface's checkbox order.
	Events []string `json:"events"`
	// DefaultEvents is the read-flavored default selection applied at
	// enablement (spec: service-authority attribution); always a non-empty
	// subset of Events.
	DefaultEvents []string `json:"default_events"`
	// SignatureScheme is one of the RecipeWebhookScheme* constants.
	SignatureScheme string `json:"signature_scheme"`
	// Templates is exactly one template per catalog event.
	Templates []RecipeWebhookTemplate `json:"templates"`
	// Setup is the provider configuration copy.
	Setup RecipeWebhookSetup `json:"setup"`
}

// ConnectionWebhook is one connection's webhook configuration state
// (add-connection-webhooks tasks.md 1.2): the enable toggle, the HMAC
// secret's encrypted envelope with its display-once hint, the target binding
// (one agent plus a thread or channel), and the selected subset of the
// recipe's event catalog. The state lives in the webhook columns of the
// connection's row; the plaintext secret never persists and the ciphertext
// never serializes.
type ConnectionWebhook struct {
	WorkspaceID  string `json:"workspace_id"`
	ConnectionID string `json:"connection_id"`
	Enabled      bool   `json:"enabled"`
	// SecretCiphertext is the AES-256-GCM envelope of the HMAC secret (the
	// instance master key, workspace ID as AAD — the gateway credential
	// derivation). Empty until the first enablement; never serialized.
	SecretCiphertext string `json:"-"`
	// SecretHint is the display-once residue — the secret's last 4
	// characters — shown after the one-time reveal.
	SecretHint string `json:"secret_hint"`
	// TargetAgentID is the bound workspace agent the turn runs as.
	TargetAgentID string `json:"target_agent_id"`
	// TargetKind is one of the ConnectionWebhookTarget* constants, or empty
	// while the binding is unset.
	TargetKind string `json:"target_kind"`
	// TargetID is the bound thread (session) or channel id.
	TargetID string `json:"target_id"`
	// Events is the selected subset of the recipe's event catalog; empty
	// until enablement. Membership is checked against the recipe catalog via
	// ValidateForRecipe.
	Events []string `json:"events"`
	// LastError is the fail-closed render-drop residue (add-connection-
	// webhooks task 2.4, design.md risks): a JSON envelope
	// {"event", "error", "at"} written by the ingress pipeline when a
	// delivery's payload missed a whitelisted template field — the operator-
	// visible "why is nothing happening" surface. Set on a fail-closed render
	// drop with the event id and timestamp; cleared on the next successful
	// delivery render. Empty (the store's NULL) means the pipeline last
	// rendered cleanly. It rides the full-state write path like every other
	// webhook column and never participates in Validate.
	LastError string `json:"last_error,omitempty"`
}

// Validate checks the webhook state's structural shape (the store boundary's
// check): workspace and connection scope present, the target kind in the
// catalog with a complete binding, the selected events unique and non-empty,
// and an enabled state carrying a complete binding, at least one selected
// event, and a generated secret envelope with its hint. Recipe-catalog
// membership of Events is ValidateForRecipe — the store adapters cannot see
// the connection's recipe.
func (w *ConnectionWebhook) Validate() error {
	if w == nil {
		return ErrInvalid
	}
	if strings.TrimSpace(w.WorkspaceID) == "" {
		return fmt.Errorf("%w: webhook state workspace id cannot be empty", ErrInvalid)
	}
	if strings.TrimSpace(w.ConnectionID) == "" {
		return fmt.Errorf("%w: webhook state connection id cannot be empty", ErrInvalid)
	}

	switch w.TargetKind {
	case "":
		// Unset binding: legal while disabled.
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
	// The target agent id must always be uuid-shaped when present (the stores
	// bind it as a uuid); existence is the stores' check.
	if w.TargetAgentID != "" && !isValidUUIDShape(w.TargetAgentID) {
		return fmt.Errorf("%w: webhook target agent id %q must be a uuid", ErrInvalid, w.TargetAgentID)
	}

	if len(w.Events) == 0 && w.Enabled {
		return fmt.Errorf("%w: enabling webhooks requires at least one selected event", ErrInvalid)
	}
	seenEvents := make(map[string]bool, len(w.Events))
	for _, e := range w.Events {
		if e == "" {
			return fmt.Errorf("%w: webhook events cannot contain empty entries", ErrInvalid)
		}
		if seenEvents[e] {
			return fmt.Errorf("%w: webhook event %q is selected twice", ErrInvalid, e)
		}
		seenEvents[e] = true
	}

	if w.Enabled {
		if w.TargetKind == "" {
			return fmt.Errorf("%w: enabling webhooks requires a target binding (agent plus thread or channel)", ErrInvalid)
		}
		if !isV1Envelope(w.SecretCiphertext) {
			return fmt.Errorf("%w: enabling webhooks requires a v1 secret envelope", ErrInvalid)
		}
		if strings.TrimSpace(w.SecretHint) == "" {
			return fmt.Errorf("%w: a generated webhook secret requires its display hint", ErrInvalid)
		}
	}
	return nil
}

// ValidateForRecipe additionally checks the selected events against the
// recipe's declared webhook catalog — the recipe-scoped check the management
// service runs before persisting an enablement or event update.
func (w *ConnectionWebhook) ValidateForRecipe(r *Recipe) error {
	if err := w.Validate(); err != nil {
		return err
	}
	if r == nil || r.Webhooks == nil {
		return fmt.Errorf("%w: recipe does not declare webhook support", ErrInvalid)
	}
	catalog := make(map[string]bool, len(r.Webhooks.Events))
	for _, e := range r.Webhooks.Events {
		catalog[e] = true
	}
	for _, e := range w.Events {
		if !catalog[e] {
			return fmt.Errorf("%w: webhook event %q is not in recipe %q's catalog", ErrInvalid, e, r.ID)
		}
	}
	return nil
}

// validateRecipeWebhook checks a recipe's webhook declaration
// (add-connection-webhooks tasks.md 1.1): scheme in the catalog, a non-empty
// event catalog with well-formed unique ids, a non-empty default selection
// within the catalog, exactly one template per event whose placeholders and
// whitelist match exactly, and complete provider setup copy. A malformed
// declaration is a registration-time programming error, like every other
// recipe-shape fault.
func validateRecipeWebhook(r *Recipe, w *RecipeWebhook) error {
	switch w.SignatureScheme {
	case RecipeWebhookSchemeHMACSHA256, RecipeWebhookSchemeSecretToken:
	default:
		return fmt.Errorf("%w: recipe %q webhook signature scheme %q must be %s or %s", ErrInvalid, r.ID, w.SignatureScheme, RecipeWebhookSchemeHMACSHA256, RecipeWebhookSchemeSecretToken)
	}

	if len(w.Events) == 0 {
		return fmt.Errorf("%w: recipe %q webhook declaration must name at least one event", ErrInvalid, r.ID)
	}
	catalog := make(map[string]bool, len(w.Events))
	for _, e := range w.Events {
		if !isValidWebhookEventID(e) {
			return fmt.Errorf("%w: recipe %q webhook event id %q must be lowercase <event> or <event>.<action> segments of letters, digits, or underscores", ErrInvalid, r.ID, e)
		}
		if catalog[e] {
			return fmt.Errorf("%w: recipe %q webhook event %q is duplicated", ErrInvalid, r.ID, e)
		}
		catalog[e] = true
	}

	if len(w.DefaultEvents) == 0 {
		return fmt.Errorf("%w: recipe %q webhook declaration must name a non-empty default event set", ErrInvalid, r.ID)
	}
	seenDefault := make(map[string]bool, len(w.DefaultEvents))
	for _, e := range w.DefaultEvents {
		if !catalog[e] {
			return fmt.Errorf("%w: recipe %q default webhook event %q is not in its catalog", ErrInvalid, r.ID, e)
		}
		if seenDefault[e] {
			return fmt.Errorf("%w: recipe %q default webhook event %q is duplicated", ErrInvalid, r.ID, e)
		}
		seenDefault[e] = true
	}

	if len(w.Templates) != len(w.Events) {
		return fmt.Errorf("%w: recipe %q webhook declaration must carry exactly one template per catalog event (%d templates for %d events)", ErrInvalid, r.ID, len(w.Templates), len(w.Events))
	}
	seenTemplate := make(map[string]bool, len(w.Templates))
	for i := range w.Templates {
		t := &w.Templates[i]
		if !catalog[t.Event] {
			return fmt.Errorf("%w: recipe %q webhook template names event %q outside its catalog", ErrInvalid, r.ID, t.Event)
		}
		if seenTemplate[t.Event] {
			return fmt.Errorf("%w: recipe %q webhook event %q has more than one template", ErrInvalid, r.ID, t.Event)
		}
		seenTemplate[t.Event] = true

		if len(t.Fields) == 0 {
			return fmt.Errorf("%w: recipe %q webhook template for %q must whitelist at least one payload field", ErrInvalid, r.ID, t.Event)
		}
		whitelist := make(map[string]bool, len(t.Fields))
		for _, f := range t.Fields {
			if !isValidWebhookFieldPath(f) {
				return fmt.Errorf("%w: recipe %q webhook template for %q whitelists malformed payload field path %q", ErrInvalid, r.ID, t.Event, f)
			}
			if whitelist[f] {
				return fmt.Errorf("%w: recipe %q webhook template for %q whitelists payload field %q twice", ErrInvalid, r.ID, t.Event, f)
			}
			whitelist[f] = true
		}
		placeholders, ok := webhookTemplatePlaceholders(t.Template)
		if !ok {
			return fmt.Errorf("%w: recipe %q webhook template for %q has a malformed {field} placeholder", ErrInvalid, r.ID, t.Event)
		}
		for _, p := range placeholders {
			if !whitelist[p] {
				return fmt.Errorf("%w: recipe %q webhook template for %q references payload field %q which is not whitelisted", ErrInvalid, r.ID, t.Event, p)
			}
			delete(whitelist, p)
		}
		for f := range whitelist {
			return fmt.Errorf("%w: recipe %q webhook template for %q whitelists payload field %q which never appears in the template", ErrInvalid, r.ID, t.Event, f)
		}
	}

	setup := w.Setup
	if strings.TrimSpace(setup.SignatureHeader) == "" {
		return fmt.Errorf("%w: recipe %q webhook setup must name the signature header", ErrInvalid, r.ID)
	}
	if strings.TrimSpace(setup.EventTypeHeader) == "" {
		return fmt.Errorf("%w: recipe %q webhook setup must name the event type header", ErrInvalid, r.ID)
	}
	if strings.TrimSpace(setup.DeliveryIDHeader) == "" {
		return fmt.Errorf("%w: recipe %q webhook setup must name the delivery id header", ErrInvalid, r.ID)
	}
	if strings.TrimSpace(setup.URLPathShape) == "" {
		return fmt.Errorf("%w: recipe %q webhook setup must carry the ingest url path shape", ErrInvalid, r.ID)
	}
	if strings.TrimSpace(setup.Help) == "" {
		return fmt.Errorf("%w: recipe %q webhook setup must carry the provider configuration help", ErrInvalid, r.ID)
	}
	return nil
}

// isValidWebhookEventID reports whether id is a well-formed event catalog id:
// non-empty dot-separated segments of lowercase letters, digits, or
// underscores — "<event>.<action>" or a bare "<event>".
func isValidWebhookEventID(id string) bool {
	if id == "" {
		return false
	}
	for _, seg := range strings.Split(id, ".") {
		if seg == "" {
			return false
		}
		for _, r := range seg {
			switch {
			case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			default:
				return false
			}
		}
	}
	return true
}

// isValidWebhookFieldPath reports whether path is a well-formed payload field
// path: non-empty dot-separated segments of letters, digits, underscores, or
// hyphens (JSON object keys are case-sensitive, so letter case is kept).
func isValidWebhookFieldPath(path string) bool {
	if path == "" {
		return false
	}
	for _, seg := range strings.Split(path, ".") {
		if seg == "" {
			return false
		}
		for _, r := range seg {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			default:
				return false
			}
		}
	}
	return true
}

// webhookTemplatePlaceholders extracts a template's {field} placeholder names
// in order of first appearance. ok is false when a brace is unbalanced or
// nested, or a placeholder is empty or malformed — the structural check
// template validation runs before whitelist matching.
func webhookTemplatePlaceholders(template string) (fields []string, ok bool) {
	depth, start := 0, 0
	for i := 0; i < len(template); i++ {
		switch template[i] {
		case '{':
			if depth > 0 {
				return nil, false
			}
			depth, start = 1, i+1
		case '}':
			if depth == 0 {
				return nil, false
			}
			name := template[start:i]
			if !isValidWebhookFieldPath(name) {
				return nil, false
			}
			fields = append(fields, name)
			depth = 0
		}
	}
	if depth != 0 {
		return nil, false
	}
	return fields, true
}

// isV1Envelope reports whether s carries the AES-256-GCM secrets envelope
// shape ("v1:<nonce>:<ciphertext>"). Matched literally because domain cannot
// import the secrets package without an import cycle (the
// ValidateGatewayConfig precedent).
func isV1Envelope(s string) bool {
	parts := strings.Split(s, ":")
	return len(parts) == 3 && parts[0] == "v1" && parts[1] != "" && parts[2] != ""
}

// isValidUUIDShape reports whether s is a canonical lowercase-or-uppercase
// textual UUID (8-4-4-4-12 hex digits). The target agent id must name a real
// workspace agent; the stores enforce existence, the domain enforces shape.
func isValidUUIDShape(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			switch {
			case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
			default:
				return false
			}
		}
	}
	return true
}
