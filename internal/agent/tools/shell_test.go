package tools_test

import (
	"context"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/agent/tools"
)

func TestShellToolPolicy(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name       string
		policy     string
		allowlist  []string
		command    string
		wantResult string
	}{
		{
			name:       "deny blocks execution",
			policy:     "deny",
			command:    "echo hello",
			wantResult: "Command blocked by execution policy: deny",
		},
		{
			name:       "allowlist blocks unlisted command",
			policy:     "allowlist",
			allowlist:  []string{"git"},
			command:    "echo hello",
			wantResult: "Command blocked: binary is not in the allowed commands list",
		},
		{
			name:       "allowlist allows listed command",
			policy:     "allowlist",
			allowlist:  []string{"echo"},
			command:    "echo hello",
			wantResult: "hello",
		},
		{
			name:       "allowlist ignores env variables to identify binary name",
			policy:     "allowlist",
			allowlist:  []string{"echo"},
			command:    "CGO_ENABLED=0 echo hello",
			wantResult: "hello",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scope := &tools.Scope{
				Workspace:      tmpDir,
				ShellPolicy:    tc.policy,
				ShellAllowlist: tc.allowlist,
			}

			toolObj := getTool("shell")
			invokable := toolObj.Build(scope)

			ctx := context.Background()
			res, err := invokable.InvokableRun(ctx, `{"command": "`+tc.command+`"}`)
			if err != nil {
				t.Fatalf("shell tool run failed: %v", err)
			}

			if !strings.Contains(strings.TrimSpace(res), tc.wantResult) {
				t.Errorf("shell tool output %q does not contain expected %q", res, tc.wantResult)
			}
		})
	}
}
