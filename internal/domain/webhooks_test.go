package domain

import (
	"strings"
	"testing"
)

func webhookTestRecipe() *Recipe {
	return &Recipe{
		ID:           "github",
		Service:      "GitHub",
		AuthKind:     RecipeAuthPAT,
		Availability: RecipeAvailable,
		Transport:    MCPTransportStreamableHTTP,
		Endpoint:     "https://api.githubcopilot.com/mcp/",
		TokenHeader:  "Authorization",
		TokenScheme:  "Bearer",
		AccessLevels: []string{ConnectionAccessReadOnly},
		Steps:        []RecipeStep{{Title: "Create a token"}},
		Probe:        RecipeProbe{Tool: "list_repositories"},
		Webhooks:     validWebhookDeclaration(),
	}
}

func validWebhookDeclaration() *RecipeWebhook {
	return &RecipeWebhook{
		SignatureScheme: RecipeWebhookSchemeHMACSHA256,
		Events:          []string{"push", "pull_request.opened"},
		DefaultEvents:   []string{"pull_request.opened"},
		Templates: []RecipeWebhookTemplate{
			{
				Event:    "push",
				Fields:   []string{"ref", "repository.full_name"},
				Template: "Push to {repository.full_name} on {ref}.",
			},
			{
				Event:    "pull_request.opened",
				Fields:   []string{"pull_request.title", "sender.login"},
				Template: "PR by {sender.login}: {pull_request.title}",
			},
		},
		Setup: RecipeWebhookSetup{
			SignatureHeader:  "X-Hub-Signature-256",
			EventTypeHeader:  "X-GitHub-Event",
			DeliveryIDHeader: "X-GitHub-Delivery",
			URLPathShape:     "/api/ingest/webhooks/{workspace_slug}/{connection_id}",
			Help:             "Configure the webhook at the provider with the URL and secret.",
		},
	}
}

// Recipes with a valid webhook declaration register; the builtins carry the
// real declarations (tasks.md 1.1).
func TestRecipeWebhook_DeclarationAccepted(t *testing.T) {
	if err := ValidateRecipe(webhookTestRecipe()); err != nil {
		t.Fatalf("expected valid webhook declaration to pass, got %v", err)
	}

	gh := RecipeByID("github")
	if gh == nil || gh.Webhooks == nil {
		t.Fatal("expected the github recipe to declare webhook support")
	}
	if gh.Webhooks.SignatureScheme != RecipeWebhookSchemeHMACSHA256 {
		t.Fatalf("expected github scheme hmac_sha256, got %q", gh.Webhooks.SignatureScheme)
	}
	if len(gh.Webhooks.DefaultEvents) == 0 || len(gh.Webhooks.DefaultEvents) != len(gh.Webhooks.Events) {
		t.Fatalf("expected github default events to equal the catalog, got %v of %v", gh.Webhooks.DefaultEvents, gh.Webhooks.Events)
	}
	for _, want := range []string{"pull_request.opened", "issues.assigned"} {
		found := false
		for _, e := range gh.Webhooks.Events {
			if e == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected github catalog to contain %q, got %v", want, gh.Webhooks.Events)
		}
	}
	if gh.Webhooks.Setup.SignatureHeader != "X-Hub-Signature-256" ||
		gh.Webhooks.Setup.EventTypeHeader != "X-GitHub-Event" ||
		gh.Webhooks.Setup.DeliveryIDHeader != "X-GitHub-Delivery" {
		t.Fatalf("unexpected github setup headers: %+v", gh.Webhooks.Setup)
	}

	gl := RecipeByID("gitlab")
	if gl == nil || gl.Webhooks == nil {
		t.Fatal("expected the gitlab recipe to declare webhook support")
	}
	if gl.Webhooks.SignatureScheme != RecipeWebhookSchemeSecretToken {
		t.Fatalf("expected gitlab scheme secret_token, got %q", gl.Webhooks.SignatureScheme)
	}
	if len(gl.Webhooks.DefaultEvents) >= len(gl.Webhooks.Events) {
		t.Fatalf("expected gitlab default events to be a strict subset, got %v of %v", gl.Webhooks.DefaultEvents, gl.Webhooks.Events)
	}
	if gl.Webhooks.Setup.SignatureHeader != "X-Gitlab-Token" {
		t.Fatalf("expected X-Gitlab-Token, got %q", gl.Webhooks.Setup.SignatureHeader)
	}

	// The non-webhook recipes stay declaration-free — Figma included
	// (design.md scopes webhook support to the providers that declare it).
	for _, id := range []string{"figma", "atlassian", "slack", "linear"} {
		if r := RecipeByID(id); r == nil || r.Webhooks != nil {
			t.Fatalf("expected %s to declare no webhook support", id)
		}
	}
}

func TestRecipeWebhook_ValidationTable(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*RecipeWebhook)
	}{
		{"missing scheme", func(w *RecipeWebhook) { w.SignatureScheme = "" }},
		{"unknown scheme", func(w *RecipeWebhook) { w.SignatureScheme = "md5" }},
		{"empty catalog", func(w *RecipeWebhook) { w.Events = nil }},
		{"bad event id case", func(w *RecipeWebhook) {
			w.Events = []string{"push", "Pull_Request.opened"}
			w.Templates[1].Event = "Pull_Request.opened"
		}},
		{"event id empty segment", func(w *RecipeWebhook) {
			w.Events = []string{"push", "pull_request..opened"}
			w.Templates[1].Event = "pull_request..opened"
		}},
		{"duplicate event", func(w *RecipeWebhook) {
			w.Events = []string{"push", "push"}
		}},
		{"empty default set", func(w *RecipeWebhook) { w.DefaultEvents = nil }},
		{"default outside catalog", func(w *RecipeWebhook) { w.DefaultEvents = []string{"issues.assigned"} }},
		{"duplicate default", func(w *RecipeWebhook) {
			w.DefaultEvents = []string{"pull_request.opened", "pull_request.opened"}
		}},
		{"missing template for event", func(w *RecipeWebhook) { w.Templates = w.Templates[:1] }},
		{"extra template", func(w *RecipeWebhook) {
			w.Templates = append(w.Templates, RecipeWebhookTemplate{Event: "issues.assigned", Fields: []string{"a"}, Template: "{a}"})
		}},
		{"template outside catalog", func(w *RecipeWebhook) {
			w.Templates[1].Event = "issues.assigned"
		}},
		{"duplicate template", func(w *RecipeWebhook) {
			w.Templates[1].Event = "push"
		}},
		{"template without fields", func(w *RecipeWebhook) {
			w.Templates[0].Fields = nil
			w.Templates[0].Template = "static text"
		}},
		{"malformed whitelist path", func(w *RecipeWebhook) {
			w.Templates[0].Fields = []string{"ref", "repository..full_name"}
		}},
		{"duplicate whitelist path", func(w *RecipeWebhook) {
			w.Templates[0].Fields = []string{"ref", "ref"}
		}},
		{"unbalanced placeholder", func(w *RecipeWebhook) {
			w.Templates[0].Template = "Push to {repository.full_name on {ref}."
		}},
		{"empty placeholder", func(w *RecipeWebhook) {
			w.Templates[0].Template = "Push to {}."
		}},
		{"placeholder not whitelisted", func(w *RecipeWebhook) {
			w.Templates[0].Template = "Push to {sender.login} on {ref}."
		}},
		{"whitelisted field unused", func(w *RecipeWebhook) {
			w.Templates[0].Template = "Push to {ref}."
		}},
		{"missing signature header", func(w *RecipeWebhook) { w.Setup.SignatureHeader = "" }},
		{"missing event type header", func(w *RecipeWebhook) { w.Setup.EventTypeHeader = " " }},
		{"missing delivery id header", func(w *RecipeWebhook) { w.Setup.DeliveryIDHeader = "" }},
		{"missing url path shape", func(w *RecipeWebhook) { w.Setup.URLPathShape = "" }},
		{"missing help", func(w *RecipeWebhook) { w.Setup.Help = "" }},
	}
	for _, tc := range cases {
		r := webhookTestRecipe()
		tc.mutate(r.Webhooks)
		err := ValidateRecipe(r)
		if err == nil {
			t.Errorf("[%s] expected validation failure, got nil", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), "recipe \"github\"") {
			t.Errorf("[%s] expected the error to name the recipe, got %v", tc.name, err)
		}
	}
}

// Registry reads hand out deep copies: mutating a read recipe's webhook
// declaration must not reach the registry (the Recipes-copies guarantee,
// extended to the declaration pointer).
func TestRecipeWebhook_DeclarationCopied(t *testing.T) {
	gh := RecipeByID("github")
	if gh == nil || gh.Webhooks == nil {
		t.Fatal("expected the github recipe to declare webhook support")
	}
	gh.Webhooks.Events[0] = "mutated.event"
	gh.Webhooks.SignatureScheme = "mutated"

	again := RecipeByID("github")
	if again.Webhooks.SignatureScheme != RecipeWebhookSchemeHMACSHA256 {
		t.Fatalf("expected registry scheme untouched, got %q", again.Webhooks.SignatureScheme)
	}
	for _, e := range again.Webhooks.Events {
		if e == "mutated.event" {
			t.Fatalf("expected registry catalog untouched, got %v", again.Webhooks.Events)
		}
	}
}

func TestConnectionWebhook_Validate(t *testing.T) {
	valid := func() *ConnectionWebhook {
		return &ConnectionWebhook{
			WorkspaceID:      "11111111-1111-1111-1111-111111111111",
			ConnectionID:     "22222222-2222-2222-2222-222222222222",
			Enabled:          true,
			SecretCiphertext: "v1:bm9uY2U:Y2lwaGVydGV4dA",
			SecretHint:       "bGd9",
			TargetAgentID:    "33333333-3333-3333-3333-333333333333",
			TargetKind:       ConnectionWebhookTargetChannel,
			TargetID:         "chan-1",
			Events:           []string{"pull_request.opened", "push"},
		}
	}
	if err := valid().Validate(); err != nil {
		t.Fatalf("expected valid state to pass, got %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*ConnectionWebhook)
	}{
		{"nil state", func(w *ConnectionWebhook) {}}, // handled separately below
		{"missing workspace", func(w *ConnectionWebhook) { w.WorkspaceID = "" }},
		{"missing connection", func(w *ConnectionWebhook) { w.ConnectionID = " " }},
		{"bad target kind", func(w *ConnectionWebhook) { w.TargetKind = "dm" }},
		{"target kind without agent", func(w *ConnectionWebhook) { w.TargetAgentID = "" }},
		{"target kind without target id", func(w *ConnectionWebhook) { w.TargetID = "" }},
		{"malformed agent uuid", func(w *ConnectionWebhook) { w.TargetAgentID = "atlas" }},
		{"enabled without binding", func(w *ConnectionWebhook) { w.TargetKind = "" }},
		{"enabled without events", func(w *ConnectionWebhook) { w.Events = nil }},
		{"enabled without secret", func(w *ConnectionWebhook) { w.SecretCiphertext = "" }},
		{"secret not v1 envelope", func(w *ConnectionWebhook) { w.SecretCiphertext = "raw-secret" }},
		{"enabled without hint", func(w *ConnectionWebhook) { w.SecretHint = "" }},
		{"duplicate events", func(w *ConnectionWebhook) { w.Events = []string{"push", "push"} }},
		{"empty event entry", func(w *ConnectionWebhook) { w.Events = []string{"push", ""} }},
	}
	for _, tc := range cases {
		w := valid()
		tc.mutate(w)
		if tc.name == "nil state" {
			continue
		}
		if err := w.Validate(); err == nil {
			t.Errorf("[%s] expected validation failure, got nil", tc.name)
		}
	}
	if err := (*ConnectionWebhook)(nil).Validate(); err == nil {
		t.Error("expected nil state to be invalid")
	}

	// Disabled states may keep the binding and selection, and the inert
	// default (all empty) is structurally legal.
	disabled := valid()
	disabled.Enabled = false
	disabled.SecretCiphertext = ""
	disabled.SecretHint = ""
	if err := disabled.Validate(); err != nil {
		t.Fatalf("expected disabled state with binding to pass, got %v", err)
	}
	inert := &ConnectionWebhook{
		WorkspaceID:  "11111111-1111-1111-1111-111111111111",
		ConnectionID: "22222222-2222-2222-2222-222222222222",
	}
	if err := inert.Validate(); err != nil {
		t.Fatalf("expected inert default to pass, got %v", err)
	}
}

func TestConnectionWebhook_ValidateForRecipe(t *testing.T) {
	state := &ConnectionWebhook{
		WorkspaceID:      "11111111-1111-1111-1111-111111111111",
		ConnectionID:     "22222222-2222-2222-2222-222222222222",
		Enabled:          true,
		SecretCiphertext: "v1:bm9uY2U:Y2lwaGVydGV4dA",
		SecretHint:       "bGd9",
		TargetAgentID:    "33333333-3333-3333-3333-333333333333",
		TargetKind:       ConnectionWebhookTargetThread,
		TargetID:         "sess-1",
		Events:           []string{"pull_request.opened", "push"},
	}
	if err := state.ValidateForRecipe(webhookTestRecipe()); err != nil {
		t.Fatalf("expected in-catalog selection to pass, got %v", err)
	}

	state.Events = append(state.Events, "issues.assigned")
	if err := state.ValidateForRecipe(webhookTestRecipe()); err == nil {
		t.Fatal("expected selection outside the catalog to fail")
	}

	if err := state.ValidateForRecipe(RecipeByID("figma")); err == nil {
		t.Fatal("expected a recipe without webhook support to fail the recipe check")
	}
	if err := state.ValidateForRecipe(nil); err == nil {
		t.Fatal("expected a nil recipe to fail the recipe check")
	}
}

func TestWebhookIDAndFieldShapes(t *testing.T) {
	for _, ok := range []string{"push", "pull_request.opened", "issue_comment.created", "x"} {
		if !isValidWebhookEventID(ok) {
			t.Errorf("expected %q to be a valid event id", ok)
		}
	}
	for _, bad := range []string{"", "Push", "pull_request..opened", ".push", "push.", "pull request", "pr#1"} {
		if isValidWebhookEventID(bad) {
			t.Errorf("expected %q to be an invalid event id", bad)
		}
	}

	for _, ok := range []string{"ref", "pull_request.title", "repository.full_name", "Object.Attributes.URL"} {
		if !isValidWebhookFieldPath(ok) {
			t.Errorf("expected %q to be a valid field path", ok)
		}
	}
	for _, bad := range []string{"", "a..b", ".a", "a b", "a[0]"} {
		if isValidWebhookFieldPath(bad) {
			t.Errorf("expected %q to be an invalid field path", bad)
		}
	}

	fields, ok := webhookTemplatePlaceholders("PR {pull_request.title} by {sender.login}")
	if !ok || len(fields) != 2 || fields[0] != "pull_request.title" || fields[1] != "sender.login" {
		t.Fatalf("unexpected placeholder extraction: %v ok=%v", fields, ok)
	}
	for _, bad := range []string{"{a", "a}", "{}", "{a}{b", "{a{b}}", "{a b}"} {
		if _, ok := webhookTemplatePlaceholders(bad); ok {
			t.Errorf("expected %q to be a malformed template", bad)
		}
	}
}
