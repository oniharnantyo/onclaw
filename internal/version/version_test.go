package version

import (
	"strings"
	"testing"
)

func TestStringContainsDefaults(t *testing.T) {
	s := String()
	// With no -ldflags injection, the defaults must all be visible.
	for _, want := range []string{Version, Commit, Date} {
		if !strings.Contains(s, want) {
			t.Errorf("String()=%q, expected to contain %q", s, want)
		}
	}
}

func TestStringFormatsThreeFields(t *testing.T) {
	// Format: "<version> (<commit>, <date>)" -> two parens, one comma.
	s := String()
	if strings.Count(s, "(") != 1 || strings.Count(s, ")") != 1 || strings.Count(s, ",") != 1 {
		t.Errorf("String()=%q does not match expected format", s)
	}
}
