package skill_test

import (
	"path/filepath"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/skill"
)

func TestTargetDir(t *testing.T) {
	home := "/home/test"

	tgtGlobal := skill.TargetDir(home, "global")
	if tgtGlobal != filepath.Join(home, "skills") {
		t.Errorf("expected target for global scope: %s, got: %s", filepath.Join(home, "skills"), tgtGlobal)
	}

	tgtAgent := skill.TargetDir(home, "myagent")
	if tgtAgent != filepath.Join(home, "workspace", "myagent", ".agents", "skills") {
		t.Errorf("expected target for agent scope: %s, got: %s", filepath.Join(home, "workspace", "myagent", ".agents", "skills"), tgtAgent)
	}
}
