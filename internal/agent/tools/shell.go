package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

func init() {
	Register(&shellTool{})
}

type shellTool struct{}

func (s *shellTool) Name() string {
	return "shell"
}

func (s *shellTool) Desc() string {
	return "Execute a shell command inside the workspace directory"
}

func (s *shellTool) Category() string {
	return "Shell"
}

type ShellInput struct {
	Command string `json:"command" jsonschema_description:"The shell command to execute in the workspace"`
}

type CappedBuffer struct {
	Cap  int
	Buf  strings.Builder
	Size int
}

func (cb *CappedBuffer) Write(p []byte) (n int, err error) {
	if cb.Size >= cb.Cap {
		return len(p), nil
	}

	available := cb.Cap - cb.Size
	toWrite := len(p)
	if toWrite > available {
		toWrite = available
	}

	n, err = cb.Buf.Write(p[:toWrite])
	cb.Size += n
	if cb.Size >= cb.Cap && len(p) > toWrite {
		cb.Buf.WriteString("\n[Output truncated due to size limit]")
	}
	return len(p), err
}

func isAllowedCommand(command string, allowlist []string) bool {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return false
	}

	var binary string
	for _, part := range parts {
		if strings.Contains(part, "=") {
			continue // skip env variables like CGO_ENABLED=0
		}
		binary = part
		break
	}

	binary = filepath.Base(binary)

	for _, allowed := range allowlist {
		if binary == allowed {
			return true
		}
	}
	return false
}

func (s *shellTool) Build(scope *Scope) tool.InvokableTool {
	t, err := utils.InferTool(s.Name(), s.Desc(), func(ctx context.Context, input *ShellInput) (string, error) {
		policy := strings.ToLower(strings.TrimSpace(scope.ShellPolicy))
		if policy == "" {
			policy = "deny"
		}

		if policy == "deny" {
			return "Command blocked by execution policy: deny", nil
		}

		if policy == "allowlist" {
			if !isAllowedCommand(input.Command, scope.ShellAllowlist) {
				return "Command blocked: binary is not in the allowed commands list", nil
			}
		}

		if policy == "ask" {
			fmt.Printf("\n[Shell tool] Agent wants to execute: %s\nConfirm execution? (y/n): ", input.Command)
			var response string
			_, err := fmt.Fscanln(os.Stdin, &response)
			if err != nil {
				return "Command blocked: failed to get user confirmation", nil
			}
			response = strings.ToLower(strings.TrimSpace(response))
			if response != "y" && response != "yes" {
				return "Command blocked: user rejected execution", nil
			}
		}

		cmd := exec.CommandContext(ctx, "sh", "-c", input.Command)
		cmd.Dir = scope.Workspace

		var outBuf CappedBuffer
		outBuf.Cap = 32 * 1024 // 32KB output cap
		cmd.Stdout = &outBuf
		cmd.Stderr = &outBuf

		err := cmd.Run()
		outputStr := outBuf.Buf.String()

		if err != nil {
			if ctx.Err() != nil {
				return outputStr + "\nCommand interrupted (context cancelled)", nil
			}
			return fmt.Sprintf("%s\nCommand failed: %v", outputStr, err), nil
		}

		return outputStr, nil
	})
	if err != nil {
		panic(err)
	}
	return t
}
