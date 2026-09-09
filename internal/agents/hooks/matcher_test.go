package hooks

import (
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
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
			value:   "web.fetch",
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
			value:   "shell.run",
			want:    true,
		},
		{
			name:    "comma-separated exact hit",
			matcher: "shell, read_file",
			value:   "shell",
			want:    true,
		},
		{
			name:    "comma-separated exact miss (sibling tool)",
			matcher: "shell, read_file",
			value:   "shell.exec",
			want:    false,
		},
		{
			name:    "pipe-separated entries",
			matcher: "shell|read_file",
			value:   "read_file",
			want:    true,
		},
		{
			name:    "whitespace-separated entries",
			matcher: "shell read_file",
			value:   "read_file",
			want:    true,
		},
		{
			name:    "dotted name stays exact in the list tier",
			matcher: "web.search",
			value:   "web.search",
			want:    true,
		},
		{
			name:    "dotted exact does not imply a family",
			matcher: "web.search",
			value:   "web.search.news",
			want:    false,
		},
		{
			name:    "exact entry does not imply a family",
			matcher: "browser",
			value:   "browser.snapshot",
			want:    false,
		},
		{
			name:    "browser family prefix hit",
			matcher: "browser.*",
			value:   "browser.snapshot",
			want:    true,
		},
		{
			name:    "family prefix is a plain prefix (spec wording), selecting the dotted family",
			matcher: "browser.*",
			value:   "browser.click.deep",
			want:    true,
		},
		{
			name:    "family root itself is selected by its own family entry",
			matcher: "browser.*",
			value:   "browser",
			want:    true,
		},
		{
			name:    "mcp server family hit (double-underscore names)",
			matcher: "mcp__github.*",
			value:   "mcp__github__create_issue",
			want:    true,
		},
		{
			name:    "mcp server family miss on other server",
			matcher: "mcp__github.*",
			value:   "mcp__gitlab__create_issue",
			want:    false,
		},
		{
			name:    "mixed exact and family entries",
			matcher: "shell.run, browser.*",
			value:   "browser.click",
			want:    true,
		},
		{
			name:    "plain word is a list-tier exact entry, not an unanchored regex (the D19 adaptation)",
			matcher: "web",
			value:   "web.fetch",
			want:    false,
		},
		{
			name:    "plain word exact hit",
			matcher: "web",
			value:   "web",
			want:    true,
		},
		{
			name:    "regex tier entry with an anchor character puts the whole string in the regex tier",
			matcher: "^web\\.",
			value:   "web.fetch",
			want:    true,
		},
		{
			name:    "anchored regex miss on prefix-inside value",
			matcher: "^web\\.",
			value:   "toolbox.web.reader",
			want:    false,
		},
		{
			name:    "unanchored regex matches mid-value",
			matcher: "web\\.fetch",
			value:   "toolbox.web.fetcher",
			want:    true,
		},
		{
			name:    "regex miss",
			matcher: "web\\.fetch",
			value:   "shell.run",
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
			name:    "origin value set: only cron",
			matcher: "cron",
			value:   "cron",
			want:    true,
		},
		{
			name:    "origin value set: user not selected",
			matcher: "cron",
			value:   "user",
			want:    false,
		},
		{
			name:    "anchored regex on origins",
			matcher: "^(user|cron)$",
			value:   "channel",
			want:    false,
		},
		{
			name:    "anchored regex on origins hit",
			matcher: "^(user|cron)$",
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
	overLong := "^" + strings.Repeat("a", domain.MaxHookPatternLength)
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
	atCap := "^" + strings.Repeat("a", domain.MaxHookPatternLength-1)
	if _, err := CompileMatcher(atCap); err != nil {
		t.Errorf("at-cap pattern should compile: %v", err)
	}
	over := "^" + strings.Repeat("a", domain.MaxHookPatternLength)
	if _, err := CompileMatcher(over); err == nil {
		t.Error("over-cap pattern should fail")
	}

	// A list-tier matcher longer than the cap is not a regex and has no cap.
	longList := strings.Repeat("tool", domain.MaxHookPatternLength/2) + ".*"
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
