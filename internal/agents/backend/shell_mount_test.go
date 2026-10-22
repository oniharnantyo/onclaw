package backend

import "testing"

// The shell must honor the same mount contract the fs tools promise the
// model: /workspace is the workspace root. Without the rewrite, any command
// the model writes against that absolute prefix dies on the host filesystem
// (the fs jail maps it only for fs-tool calls, and no chroot exists to
// resolve it for real).
func TestJailedShellTranslateMounts(t *testing.T) {
	const dir = "/tmp/buddies-agents/atlas"

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
