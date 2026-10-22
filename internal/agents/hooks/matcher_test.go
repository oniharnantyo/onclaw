package hooks

import (
	"strings"
	"testing"
)

func mustCompile(t *testing.T, matcher string) Matcher {
	t.Helper()
	compiled, err := CompileMatcher(matcher)
	if err != nil {
		t.Fatalf("CompileMatcher(%q): %v", matcher, err)
	}
	return compiled
}

func TestCompileMatcher_Matches(t *testing.T) {
	tests := []struct {
		name    string
		matcher string
		value   string
		want    bool
	}{
		{
			name:    "empty matcher matches all",
			matcher: "",
			value:   "web_fetch",
			want:    true,
		},
		{
			name:    "empty matcher matches anything else too",
			matcher: "",
			value:   "mcp__github__create_issue",
			want:    true,
		},
		{
			name:    "bare star matches all",
			matcher: "*",
			value:   "web_fetch",
			want:    true,
		},
		{
			name:    "comma-separated exact hit",
			matcher: "web_fetch, read_file",
			value:   "read_file",
			want:    true,
		},
		{
			name:    "comma-separated exact miss (sibling tool)",
			matcher: "web_fetch, read_file",
			value:   "read_file.raw",
			want:    false,
		},
		{
			name:    "pipe-separated entries",
			matcher: "web_fetch|read_file",
			value:   "read_file",
			want:    true,
		},
		{
			name:    "whitespace-separated entries",
			matcher: "web_fetch read_file",
			value:   "read_file",
			want:    true,
		},
		{
			name:    "dotted name stays exact in the list tier",
			matcher: "web_search",
			value:   "web_search",
			want:    true,
		},
		{
			name:    "dotted exact does not imply a family",
			matcher: "web_search",
			value:   "web_search.news",
			want:    false,
		},
		{
			name:    "exact entry does not imply a family",
			matcher: "execute",
			value:   "execute_shell",
			want:    false,
		},
		{
			name:    "family prefix hit",
			matcher: "mcp__github.*",
			value:   "mcp__github__create_issue",
			want:    true,
		},
		{
			name:    "family miss on other server",
			matcher: "mcp__github.*",
			value:   "mcp__gitlab__create_issue",
			want:    false,
		},
		{
			name:    "family root itself is selected by its own family entry",
			matcher: "mcp__github.*",
			value:   "mcp__github",
			want:    true,
		},
		{
			name:    "mixed exact and family entries",
			matcher: "web_fetch, mcp__github.*",
			value:   "mcp__github__create_issue",
			want:    true,
		},
		{
			name:    "plain word is a list-tier exact entry, not an unanchored regex",
			matcher: "web",
			value:   "web_fetch",
			want:    false,
		},
		{
			name:    "plain word exact hit",
			matcher: "web",
			value:   "web",
			want:    true,
		},
		{
			name:    "an anchor character puts the whole string in the regex tier",
			matcher: "^web_",
			value:   "web_fetch",
			want:    true,
		},
		{
			name:    "anchored regex miss on prefix-inside value",
			matcher: "^web_",
			value:   "toolbox.web.reader",
			want:    false,
		},
		{
			name:    "unanchored regex matches mid-value",
			matcher: "web.*fetch",
			value:   "toolbox_web_fetcher",
			want:    true,
		},
		{
			name:    "regex miss",
			matcher: "web.*fetch",
			value:   "channel_post",
			want:    false,
		},
		{
			name:    "charset-invalid entry makes the whole string a regex",
			matcher: "a.*b",
			value:   "axxb",
			want:    true,
		},
		{
			name:    "bare star fragment falls to the regex tier and matches everything",
			matcher: ".*",
			value:   "anything.at.all",
			want:    true,
		},
		{
			name:    "status value set: only failed",
			matcher: "failed",
			value:   "failed",
			want:    true,
		},
		{
			name:    "status value set: completed not selected",
			matcher: "failed",
			value:   "completed",
			want:    false,
		},
		{
			name:    "status value set: pipe list selects cancelled",
			matcher: "completed|cancelled",
			value:   "cancelled",
			want:    true,
		},
		{
			name:    "origin value set: only scheduler",
			matcher: "scheduler",
			value:   "scheduler",
			want:    true,
		},
		{
			name:    "origin value set: user not selected",
			matcher: "scheduler",
			value:   "user",
			want:    false,
		},
		{
			name:    "anchored regex on origins hit",
			matcher: "^(user|scheduler)$",
			value:   "user",
			want:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matcher := mustCompile(t, tt.matcher)
			if got := matcher.Matches(tt.value); got != tt.want {
				t.Errorf("Matches(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestCompileMatcher_Errors(t *testing.T) {
	// The cap and the compile check apply to the regex tier only: the string
	// must carry a character outside the tool-entry charset to land there.
	overLong := "^" + strings.Repeat("a", MaxPatternLength)
	tests := []struct {
		name    string
		matcher string
	}{
		{name: "invalid regex", matcher: "("},
		{name: "invalid regex class", matcher: "[a-"},
		{name: "over-long regex", matcher: overLong},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := CompileMatcher(tt.matcher); err == nil {
				t.Errorf("CompileMatcher(%q) = nil error, want error", tt.matcher)
			}
		})
	}
}

// TestCompileMatcher_RegexCapBoundary pins that exactly-at-cap patterns
// compile and one-past-cap patterns do not (the cap applies to the regex
// tier only; list-tier matchers have no length cap).
func TestCompileMatcher_RegexCapBoundary(t *testing.T) {
	atCap := "^" + strings.Repeat("a", MaxPatternLength-1)
	if _, err := CompileMatcher(atCap); err != nil {
		t.Errorf("at-cap pattern should compile: %v", err)
	}
	over := "^" + strings.Repeat("a", MaxPatternLength)
	if _, err := CompileMatcher(over); err == nil {
		t.Error("over-cap pattern should fail")
	}

	// A list-tier matcher longer than the cap is not a regex and has no cap.
	longList := strings.Repeat("tool", MaxPatternLength/2) + ".*"
	if _, err := CompileMatcher(longList); err != nil {
		t.Errorf("over-cap list-tier matcher should compile: %v", err)
	}
}

// TestCompileMatcher_OnlySeparators pins the empty-list edge: a matcher of
// only separators is list-tier with zero entries and selects nothing.
func TestCompileMatcher_OnlySeparators(t *testing.T) {
	matcher := mustCompile(t, ",, |")
	if matcher.Matches("anything") {
		t.Error("a separator-only matcher must select nothing")
	}
}

// TestMatchedValue pins the event-aware matcher input: tool names on tool
// events, origin on run-start/prompt events, status on run_finished.
func TestMatchedValue(t *testing.T) {
	withTool := Event{Event: EventPreToolUse, Tool: &EventTool{Name: "web_fetch"}}
	if got := MatchedValue(withTool); got != "web_fetch" {
		t.Errorf("pre_tool_use matched value = %q, want web_fetch", got)
	}
	noTool := Event{Event: EventPostToolUse}
	if got := MatchedValue(noTool); got != "" {
		t.Errorf("tool-less tool event matched value = %q, want empty", got)
	}
	if got := MatchedValue(Event{Event: EventUserPromptSubmit, Origin: "scheduler"}); got != "scheduler" {
		t.Errorf("prompt matched value = %q, want scheduler", got)
	}
	if got := MatchedValue(Event{Event: EventRunFinished, Status: "failed"}); got != "failed" {
		t.Errorf("run_finished matched value = %q, want failed", got)
	}
	if got := MatchedValue(Event{Event: "wormhole_opened"}); got != "" {
		t.Errorf("unknown event matched value = %q, want empty", got)
	}
}
