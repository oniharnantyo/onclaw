package webhooks_test

import (
	"testing"

	"github.com/oniharnantyo/onclaw/internal/webhooks"
)

func TestDeriveEventID(t *testing.T) {
	cases := []struct {
		name    string
		header  string
		payload string
		want    string
	}{
		{
			"github action payload",
			"pull_request",
			`{"action":"opened","pull_request":{"number":1}}`,
			"pull_request.opened",
		},
		{
			"gitlab object_attributes action",
			"merge_request",
			`{"object_attributes":{"action":"open","iid":7}}`,
			"merge_request.open",
		},
		{
			"bare event when the payload has no action",
			"push",
			`{"ref":"refs/heads/main"}`,
			"push",
		},
		{
			"bare event wins over a nested-only action when object_attributes is absent",
			"release",
			`{"release":{"tag_name":"v1"}}`,
			"release",
		},
		{
			"spaces fold to underscores",
			"Issues Event",
			`{"action":"assigned"}`,
			"issues_event.assigned",
		},
		{
			"header value is lowercased",
			"Pull_Request",
			`{"action":"closed"}`,
			"pull_request.closed",
		},
		{
			"action normalized",
			"issue_comment",
			`{"action":"Created"}`,
			"issue_comment.created",
		},
		{
			"malformed payload derives the bare id",
			"push",
			`{"not json`,
			"push",
		},
		{
			"empty header derives empty",
			"",
			`{"action":"opened"}`,
			"",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := webhooks.DeriveEventID(tc.header, []byte(tc.payload)); got != tc.want {
				t.Fatalf("DeriveEventID(%q) = %q, want %q", tc.header, got, tc.want)
			}
		})
	}
}

func TestEventSelected(t *testing.T) {
	events := []string{"push", "pull_request.opened"}
	if !webhooks.EventSelected(events, "push") {
		t.Error("push must be selected")
	}
	if !webhooks.EventSelected(events, "pull_request.opened") {
		t.Error("pull_request.opened must be selected")
	}
	if webhooks.EventSelected(events, "release.published") {
		t.Error("release.published must not be selected")
	}
	if webhooks.EventSelected(events, "") {
		t.Error("the empty event id must never be selected")
	}
	if webhooks.EventSelected(nil, "push") {
		t.Error("an empty selection selects nothing")
	}
}
