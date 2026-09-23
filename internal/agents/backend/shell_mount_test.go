package backend

import "testing"

// The shell must honor the same mount contract the fs tools promise the
// model: /workspace is the workspace root, /project the channel's shared
// project space. Without the rewrite, any command the model writes against
// those absolute prefixes dies on the host filesystem (the fs jail maps them
// only for fs-tool calls, and no chroot exists to resolve them for real).
func TestJailedShellTranslateMounts(t *testing.T) {
	const dir = "/tmp/onclaw-agents/atlas"

	tests := []struct {
		name    string
		command string
		want    string
	}{
		{"bare cd", "cd /workspace", "cd " + dir},
		{"compound command", "cd /workspace && git clone http://gitlab.example.com/be/ceres.git ceres", "cd " + dir + " && git clone http://gitlab.example.com/be/ceres.git ceres"},
		{"path under the mount", "cat /workspace/notes.md", "cat " + dir + "/notes.md"},
		{"trailing target argument", "cp x /workspace", "cp x " + dir},
		{"prefix must end at a boundary", "ls /workspacefoo", "ls /workspacefoo"},
		{"URL path segment untouched", "curl http://host/workspace/y", "curl http://host/workspace/y"},
		{"double slash untouched", "ls //workspace", "ls //workspace"},
		{"quoted path", "echo \"/workspace\"", "echo \"" + dir + "\""},
		{"already relative command unchanged", "git status", "git status"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewJailedShell(dir)
			if got := s.translateMounts(tt.command); got != tt.want {
				t.Fatalf("translateMounts(%q) = %q, want %q", tt.command, got, tt.want)
			}
		})
	}
}

func TestJailedShellTranslateProjectMount(t *testing.T) {
	s := NewJailedShell("/tmp/onclaw-agents/atlas").WithMountTranslation(ProjectMountPoint, "/tmp/onclaw-projects/ops")
	if got := s.translateMounts("cat /project/notes.md"); got != "cat /tmp/onclaw-projects/ops/notes.md" {
		t.Fatalf("project mount not translated: %q", got)
	}
	if got := s.translateMounts("ls /project"); got != "ls /tmp/onclaw-projects/ops" {
		t.Fatalf("bare project mount not translated: %q", got)
	}
	// The workspace binding survives the extra mount.
	if got := s.translateMounts("cd /workspace"); got != "cd /tmp/onclaw-agents/atlas" {
		t.Fatalf("workspace binding lost: %q", got)
	}
	// An empty dir is a no-op, not a rewrite to "".
	empty := NewJailedShell("/tmp/onclaw-agents/atlas").WithMountTranslation("/project", "")
	if got := empty.translateMounts("cat /project/x"); got != "cat /project/x" {
		t.Fatalf("empty mount dir rewrote the command: %q", got)
	}
}
