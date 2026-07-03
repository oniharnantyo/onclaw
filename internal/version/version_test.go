package version_test

import (
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/version"
)

func TestStringContainsDefaults(t *testing.T) {
	s := version.String()
	// With no -ldflags injection, the defaults must all be visible.
	for _, want := range []string{version.Version, version.Commit, version.Date} {
		if !strings.Contains(s, want) {
			t.Errorf("String()=%q, expected to contain %q", s, want)
		}
	}
}

func TestStringFormatsThreeFields(t *testing.T) {
	// Format: "<version> (<commit>, <date>)" -> two parens, one comma.
	s := version.String()
	if strings.Count(s, "(") != 1 || strings.Count(s, ")") != 1 || strings.Count(s, ",") != 1 {
		t.Errorf("String()=%q does not match expected format", s)
	}
}
