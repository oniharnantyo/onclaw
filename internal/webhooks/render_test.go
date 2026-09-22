package webhooks_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/webhooks"
)

// githubPROpened is the builtin github recipe's pull_request.opened template
// (the registration-validated fixture the render tests interpolate against).
func githubPROpenedTemplate() domain.RecipeWebhookTemplate {
	for _, tmpl := range domain.RecipeByID("github").Webhooks.Templates {
		if tmpl.Event == "pull_request.opened" {
			return tmpl
		}
	}
	panic("github recipe lost its pull_request.opened template")
}

const githubPROpenedPayload = `{
	"action": "opened",
	"repository": {"full_name": "acme/api"},
	"pull_request": {"number": 42, "title": "Fix the login race", "html_url": "https://github.com/acme/api/pull/42"},
	"sender": {"login": "alice"}
}`

func TestRender_HappyPath(t *testing.T) {
	rendered, err := webhooks.Render(githubPROpenedTemplate(), []byte(githubPROpenedPayload))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		"Pull request #42 opened in acme/api by alice",
		`"Fix the login race"`,
		"https://github.com/acme/api/pull/42",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered output missing %q:\n%s", want, rendered)
		}
	}
}

func TestRender_MissingFieldFailsClosed(t *testing.T) {
	// A payload missing one whitelisted field must fail the whole render —
	// never produce a half-filled turn (design.md risks: fail closed).
	payload := `{
		"action": "opened",
		"repository": {"full_name": "acme/api"},
		"pull_request": {"number": 42, "title": "Fix the login race", "html_url": "https://github.com/acme/api/pull/42"}
	}`
	_, err := webhooks.Render(githubPROpenedTemplate(), []byte(payload))
	if err == nil {
		t.Fatal("expected the missing sender.login field to fail the render")
	}
	if !errors.Is(err, webhooks.ErrRenderFailed) {
		t.Fatalf("expected ErrRenderFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "sender.login") {
		t.Errorf("the error should name the missing field, got: %v", err)
	}

	// A null field is missing too.
	nullPayload := strings.Replace(githubPROpenedPayload, `"alice"`, "null", 1)
	if _, err := webhooks.Render(githubPROpenedTemplate(), []byte(nullPayload)); !errors.Is(err, webhooks.ErrRenderFailed) {
		t.Fatalf("a null field must fail closed, got %v", err)
	}

	// A non-object payload fails closed as well.
	if _, err := webhooks.Render(githubPROpenedTemplate(), []byte(`[1,2,3]`)); !errors.Is(err, webhooks.ErrRenderFailed) {
		t.Fatalf("a non-object payload must fail closed, got %v", err)
	}
	if _, err := webhooks.Render(githubPROpenedTemplate(), []byte(`not json`)); !errors.Is(err, webhooks.ErrRenderFailed) {
		t.Fatalf("malformed json must fail closed, got %v", err)
	}
}

func TestRender_ValuesRenderCanonically(t *testing.T) {
	tmpl := domain.RecipeWebhookTemplate{
		Event:  "test.values",
		Fields: []string{"a.string", "a.number", "a.bool", "a.object"},
		// Registration demands placeholders == whitelist; keep the same set.
		Template: "s={a.string} n={a.number} b={a.bool} o={a.object}",
	}
	payload := `{"a": {"string": "hi", "number": 7, "bool": true, "object": {"k": "v"}}}`
	rendered, err := webhooks.Render(tmpl, []byte(payload))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	want := `s=hi n=7 b=true o={"k":"v"}`
	if rendered != want {
		t.Fatalf("rendered %q, want %q", rendered, want)
	}
}

func TestTurnInput_LabelsEventData(t *testing.T) {
	rendered := "Pull request #42 opened in acme/api by alice."
	input := webhooks.TurnInput("github", "conn-uuid-1", "pull_request.opened", rendered)

	// The first line names the connection and the event — it doubles as the
	// exported trace name.
	firstLine := input[:strings.IndexByte(input, '\n')]
	if !strings.Contains(firstLine, "pull_request.opened") || !strings.Contains(firstLine, "github") || !strings.Contains(firstLine, "conn-uuid-1") {
		t.Errorf("the input's first line must name the event, service, and connection, got %q", firstLine)
	}
	// The event data is delimited and labeled untrusted.
	if !strings.Contains(input, "-----BEGIN GITHUB EVENT DATA (pull_request.opened)-----") {
		t.Errorf("missing the begin marker:\n%s", input)
	}
	if !strings.Contains(input, "-----END GITHUB EVENT DATA (pull_request.opened)-----") {
		t.Errorf("missing the end marker:\n%s", input)
	}
	if !strings.Contains(input, "untrusted event data") {
		t.Errorf("missing the untrusted-data label:\n%s", input)
	}
	// The rendered content appears exactly once, inside the markers.
	if strings.Count(input, rendered) != 1 {
		t.Errorf("expected the rendered content once, got:\n%s", input)
	}
}
