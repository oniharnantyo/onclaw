package mcp

import (
	"strings"
	"testing"
)

func TestToolNameSanitizes(t *testing.T) {
	tests := []struct {
		server, tool, want string
	}{
		{"github", "create_issue", "mcp__github__create_issue"},
		{"GitHub", "create-issue", "mcp__github__create-issue"}, // case-insensitive sanitize
		{"My Server", "do.thing", "mcp__my_server__do_thing"},   // spec scenario
		{"my.server", "do.thing", "mcp__my_server__do_thing"},   // dots illegal
		{"Ω-fleet", "go!", "mcp___-fleet__go_"},                 // non-ASCII → _ (hyphen stays legal)
	}
	for _, tt := range tests {
		if got := ToolName(tt.server, tt.tool); got != tt.want {
			t.Errorf("ToolName(%q, %q) = %q, want %q", tt.server, tt.tool, got, tt.want)
		}
	}
}

func TestNamerCollisionSuffixes(t *testing.T) {
	n := NewNamer()
	// "GH 1" and "GH_1" sanitize to the same segment.
	first := n.Name("GH 1", "do")
	second := n.Name("GH_1", "do")
	if first != "mcp__gh_1__do" {
		t.Fatalf("first = %q, want mcp__gh_1__do", first)
	}
	if second != "mcp__gh_1__do_2" {
		t.Fatalf("second = %q, want mcp__gh_1__do_2", second)
	}
	// Deterministic across passes given the same assignment order.
	again := NewNamer()
	if a, b := again.Name("GH 1", "do"), again.Name("GH_1", "do"); a != first || b != second {
		t.Fatalf("repeat pass = %q, %q; want %q, %q", a, b, first, second)
	}
	// Reversed order flips which occurrence is suffixed (order-stable, not
	// order-free): "GH_1" now arrives first and keeps the plain name.
	rev := NewNamer()
	if a, b := rev.Name("GH_1", "do"), rev.Name("GH 1", "do"); a != first || b != second {
		t.Fatalf("reversed pass = %q, %q; want %q, %q", a, b, first, second)
	}
}

func TestNamerInServerCollision(t *testing.T) {
	// Two tools within one server sanitizing to the same name.
	n := NewNamer()
	a := n.Name("srv", "do.thing")
	b := n.Name("srv", "do_thing")
	if a == b {
		t.Fatalf("expected distinct names, both %q", a)
	}
	if a != "mcp__srv__do_thing" || b != "mcp__srv__do_thing_2" {
		t.Fatalf("got %q, %q", a, b)
	}
}

func TestNamerOverLengthTruncatesWithHash(t *testing.T) {
	longServer := strings.Repeat("s", 80)
	longTool := strings.Repeat("t", 80)
	n := NewNamer()
	name := n.Name(longServer, longTool)
	if len(name) != maxToolNameLen {
		t.Fatalf("len = %d, want %d", len(name), maxToolNameLen)
	}
	for _, r := range name {
		if !isNameRune(r) {
			t.Fatalf("name %q has illegal rune %q", name, r)
		}
	}
	if !strings.HasPrefix(name, "mcp__") {
		t.Fatalf("name %q lost the mcp__ prefix", name)
	}
	// Deterministic and distinct from a different pathological input.
	n2 := NewNamer()
	if got := n2.Name(longServer, longTool); got != name {
		t.Fatalf("repeat = %q, want %q", got, name)
	}
	n3 := NewNamer()
	other := n3.Name(longServer, strings.Repeat("u", 80))
	if other == name {
		t.Fatal("distinct pathological inputs produced the same name")
	}
	if len(other) != maxToolNameLen {
		t.Fatalf("other len = %d, want %d", len(other), maxToolNameLen)
	}
}

func TestNamerSuffixAfterTruncationStaysWithinLimit(t *testing.T) {
	// Two identical pathological inputs collide after truncation; the numeric
	// suffix must keep the name unique AND within the limit.
	n := NewNamer()
	long := strings.Repeat("x", 80)
	a := n.Name("srv", long)
	b := n.Name("srv", long)
	if a == b {
		t.Fatalf("expected suffixes to separate duplicates, both %q", a)
	}
	if len(a) > maxToolNameLen || len(b) > maxToolNameLen {
		t.Fatalf("lengths %d, %d exceed %d", len(a), len(b), maxToolNameLen)
	}
}

func TestNamerNeverEmptySegments(t *testing.T) {
	n := NewNamer()
	name := n.Name("???", "!!!")
	if len(name) != len("mcp__"+"___"+"__"+"___") {
		t.Fatalf("unexpected shape %q", name)
	}
	if !strings.HasPrefix(name, "mcp__") {
		t.Fatalf("name %q lost prefix", name)
	}
}

// isNameRune matches the OpenAI function-name pattern [a-zA-Z0-9_-].
func isNameRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		return true
	}
	return false
}
