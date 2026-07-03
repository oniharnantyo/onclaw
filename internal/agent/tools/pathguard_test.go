package tools_test

import (
	"path/filepath"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/agent/tools"
)

func TestValidatePath(t *testing.T) {
	tmpDir := t.TempDir()
	workspaceAbs, err := filepath.Abs(tmpDir)
	if err != nil {
		t.Fatalf("failed to get absolute workspace path: %v", err)
	}

	tests := []struct {
		name      string
		pathStr   string
		expectErr bool
	}{
		{
			name:      "simple relative path inside workspace",
			pathStr:   "file.txt",
			expectErr: false,
		},
		{
			name:      "nested relative path inside workspace",
			pathStr:   "dir/subdir/file.txt",
			expectErr: false,
		},
		{
			name:      "path with dot dot resolving inside workspace",
			pathStr:   "dir/../file.txt",
			expectErr: false,
		},
		{
			name:      "absolute path inside workspace",
			pathStr:   filepath.Join(workspaceAbs, "file.txt"),
			expectErr: false,
		},
		{
			name:      "relative path escaping workspace",
			pathStr:   "../escaped.txt",
			expectErr: true,
		},
		{
			name:      "complex path escaping workspace",
			pathStr:   "dir/../../escaped.txt",
			expectErr: true,
		},
		{
			name:      "absolute path escaping workspace",
			pathStr:   filepath.Dir(workspaceAbs),
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tools.ValidatePath(workspaceAbs, tc.pathStr)
			if (err != nil) != tc.expectErr {
				t.Errorf("expected error: %v, got error: %v", tc.expectErr, err)
			}
		})
	}
}
