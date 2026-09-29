package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/agents/backend"
)

// NameDeleteFile is the dotted capability name registered in the tool
// registry. Deleting files is not part of the eino fs middleware toolset, so
// delete_file is an ordinary registry tool jail-scoped by construction: the
// constructor binds the executing agent's workspace directory and every path
// resolves inside it, mirroring the fs jail backend's validation.
const NameDeleteFile = "delete_file"

// deleteFileTool permanently removes a file inside the agent's workspace jail.
type deleteFileTool struct {
	// agentDir is the resolved (symlink-free) jail root bound at construction.
	agentDir string
}

// NewDeleteFile constructs the delete tool for one agent workspace. A missing
// root is created at construction (self-heal: an absent directory must not
// kill every run for the agent) and the root resolves symlinks to its
// canonical form, exactly like the fs jail backend — only an unwritable path
// or a non-directory occupying the root still fails construction.
func NewDeleteFile(agentDir string) (tool.BaseTool, error) {
	if agentDir == "" {
		return nil, fmt.Errorf("agent workspace directory cannot be empty")
	}
	abs, err := filepath.Abs(agentDir)
	if err != nil {
		return nil, fmt.Errorf("resolve agent workspace path: %w", err)
	}
	if info, statErr := os.Stat(abs); statErr == nil && !info.IsDir() {
		return nil, fmt.Errorf("agent workspace is not a directory: %s", abs)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("create agent workspace directory: %w", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("resolve symlinks in agent workspace path: %w", err)
	}
	return &deleteFileTool{
		agentDir: strings.TrimSuffix(resolvedRoot, string(filepath.Separator)),
	}, nil
}

// Info returns the tool schema surfaced to agentic models.
func (t *deleteFileTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: NameDeleteFile,
		Desc: "Permanently delete a file inside the agent workspace. " +
			"Paths are absolute under " + backend.DefaultMountPoint + " (the agent workspace mount), e.g. " + backend.DefaultMountPoint + "/NOTES.md. " +
			"Deletion is permanent — removed files cannot be recovered, and deleting a missing file is an error.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"path": {
				Type:     schema.String,
				Desc:     "The file to delete, absolute under " + backend.DefaultMountPoint + ".",
				Required: true,
			},
		}),
	}, nil
}

// deleteArgs is the deserialized tool-call argument shape.
type deleteArgs struct {
	Path string `json:"path"`
}

// resolve mirrors the fs jail backend's writable-path validation
// (fsJailedBackend.resolveWritablePath): mount-scoped paths under
// DefaultMountPoint map into the agent directory, raw host absolute paths are
// rejected, ".." is rejected outright, and symlinks must resolve inside the
// jail. Returns the resolved on-disk path and its mount-scoped display form.
func (t *deleteFileTool) resolve(userPath string) (resolved string, display string, err error) {
	if userPath == "" {
		return "", "", fmt.Errorf("path cannot be empty")
	}
	// Reject obvious escape attempts.
	if strings.Contains(userPath, "..") {
		return "", "", fmt.Errorf("path escape attempt: %q contains \"..\"", userPath)
	}

	// Mount-scoped paths ("/workspace/...") resolve under the agent directory
	// no matter where the jail lives on the host.
	var rel string
	if userPath == backend.DefaultMountPoint {
		rel = "."
	} else if candidate := strings.TrimPrefix(userPath, backend.DefaultMountPoint+"/"); candidate != userPath && candidate != "" {
		rel = candidate
	} else if filepath.IsAbs(userPath) {
		// Host absolute paths outside the mount never reach the jail.
		return "", "", fmt.Errorf("absolute paths are not allowed: %q (delete under %s instead)", userPath, backend.DefaultMountPoint)
	} else {
		rel = userPath
	}

	if rel == "." {
		return "", "", fmt.Errorf("cannot delete the workspace root %s", backend.DefaultMountPoint)
	}

	fullPath := filepath.Join(t.agentDir, filepath.Clean(rel))
	resolvedPath, resolveErr := filepath.EvalSymlinks(fullPath)
	if resolveErr != nil {
		if os.IsNotExist(resolveErr) {
			return "", "", fmt.Errorf("no such file: %s", userPath)
		}
		return "", "", fmt.Errorf("resolve path: %w", resolveErr)
	}
	if !isWithinDeleteRoot(t.agentDir, resolvedPath) {
		return "", "", fmt.Errorf("path escapes jail: %q resolves to %q", userPath, resolvedPath)
	}
	return resolvedPath, backend.DefaultMountPoint + "/" + filepath.ToSlash(filepath.Clean(rel)), nil
}

// isWithinDeleteRoot reports whether path is the root or lies under it —
// the same containment predicate the fs jail backend applies.
func isWithinDeleteRoot(root, path string) bool {
	path = filepath.Clean(path)
	if path == root {
		return true
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
}

// InvokableRun satisfies tool.InvokableTool. Failures return errors: the
// runtime's tool-error middleware converts them into JSON error results the
// model reads, so the run continues.
func (t *deleteFileTool) InvokableRun(_ context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args deleteArgs
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("delete_file: %w", err)
	}
	resolved, display, err := t.resolve(args.Path)
	if err != nil {
		return "", fmt.Errorf("delete_file: %w", err)
	}
	if err := os.Remove(resolved); err != nil {
		return "", fmt.Errorf("delete_file: remove %s: %w", display, err)
	}
	confirmation := "Deleted " + display
	out, err := json.Marshal(map[string]string{"path": display, "result": confirmation})
	if err != nil {
		return "", fmt.Errorf("delete_file: encode result: %w", err)
	}
	return string(out), nil
}
