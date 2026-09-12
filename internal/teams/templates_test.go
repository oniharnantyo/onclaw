package teams

import (
	"strings"
	"testing"
)

// TestBuiltIn_SoftwareTeamIntegrity pins the built-in set's structural
// contract: exactly the Software Team template, exactly one facilitator slot,
// unique slot ids, and non-empty product copy throughout.
func TestBuiltIn_SoftwareTeamIntegrity(t *testing.T) {
	templates := BuiltIn()
	if len(templates) != 1 {
		t.Fatalf("expected exactly one built-in template (Software Team, design D11), got %d", len(templates))
	}

	tpl := templates[0]
	if tpl.ID != "software-team" || tpl.Name != "Software Team" {
		t.Errorf("unexpected template identity: %q / %q", tpl.ID, tpl.Name)
	}
	if len(tpl.Description) == 0 {
		t.Error("template description is empty")
	}
	// The conventions prefill embodies design D5: /project artifacts, PLAN.md
	// as the tracker, claim before write, feed discussion, human sign-off at
	// gates, facilitator close.
	for _, marker := range []string{"/project", "spec.md", "PLAN.md", "PLAN.md before", "feed", "sign-off", "facilitator"} {
		if !contains(tpl.Conventions, marker) {
			t.Errorf("conventions prefill missing %q: %q", marker, tpl.Conventions)
		}
	}

	wantSlots := map[string]bool{"pm": false, "architect": false, "scrum-master": false, "frontend": false, "backend": false, "tester": false}
	seen := map[string]bool{}
	facilitators := 0
	for _, slot := range tpl.Slots {
		if seen[slot.ID] {
			t.Errorf("duplicate slot id %q", slot.ID)
		}
		seen[slot.ID] = true
		if _, want := wantSlots[slot.ID]; !want {
			t.Errorf("unexpected slot %q", slot.ID)
		}
		if slot.Title == "" || slot.Specialization == "" || slot.RolePrompt == "" {
			t.Errorf("slot %q has empty copy: %+v", slot.ID, slot)
		}
		if slot.Facilitator {
			facilitators++
		}
	}
	for id := range wantSlots {
		if !seen[id] {
			t.Errorf("missing expected slot %q", id)
		}
	}
	if facilitators != 1 {
		t.Errorf("expected exactly one facilitator slot (design D2), got %d", facilitators)
	}

	// The facilitator slot is the scrum master, and the copy carries the
	// session.close contract.
	for _, slot := range tpl.Slots {
		if slot.ID == "scrum-master" && !slot.Facilitator {
			t.Error("scrum-master slot must be the facilitator")
		}
	}
}

func TestGet(t *testing.T) {
	tpl, ok := Get("software-team")
	if !ok {
		t.Fatal("software-team not found")
	}
	if tpl.Name != "Software Team" {
		t.Errorf("unexpected template: %+v", tpl)
	}

	if _, ok := Get("  software-team "); !ok {
		t.Error("Get must tolerate surrounding whitespace")
	}
	if _, ok := Get("nope"); ok {
		t.Error("unknown template must not resolve")
	}
	if _, ok := Get(""); ok {
		t.Error("empty id must not resolve")
	}
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
