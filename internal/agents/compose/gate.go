package compose

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
)

// Tool name constants for built-in and middleware-registered tools.
const (
	ToolExecute    = "execute"
	ToolReadFile   = "read_file"
	ToolWriteFile  = "write_file"
	ToolEditFile   = "edit_file"
	ToolListDir    = "list_dir"
	ToolDeleteFile = "delete_file"

	ToolTaskCreate = "task_create"
	ToolTaskGet    = "task_get"
	ToolTaskUpdate = "task_update"
	ToolTaskList   = "task_list"
)

// FilesystemToolNames lists the tools provided by the filesystem middleware.
var FilesystemToolNames = []string{
	ToolReadFile,
	ToolWriteFile,
	ToolEditFile,
	ToolListDir,
	ToolDeleteFile,
}

// PlantaskToolNames lists the tools provided by the plantask middleware.
var PlantaskToolNames = []string{
	ToolTaskCreate,
	ToolTaskGet,
	ToolTaskUpdate,
	ToolTaskList,
}

// ResolvedTools is one turn's gated tool surface — the product of resolve and
// the input to compose.
type ResolvedTools struct {
	// Tools are the direct tools offered to the model through the agent's
	// ToolsConfig, in catalog order.
	Tools []tool.BaseTool
	// AgentDir is the agent's workspace directory when any workspace-rooted
	// tool (filesystem, shell, delete_file, plantask) is offered; empty
	// otherwise — no workspace, no filesystem capability.
	AgentDir string
	// Shell offers the filesystem middleware's jailed execute tool.
	Shell bool
	// FsDisabled lists the filesystem middleware tool names the gate withholds.
	FsDisabled []string
	// PlantaskDisabled lists the plantask middleware tool names the gate withholds.
	PlantaskDisabled []string
}

// Names returns every offered tool name in catalog order. It exists for
// tests and diagnostics; resolution itself never needs it.
func (r *ResolvedTools) Names(ctx context.Context, catalogOrder []string) ([]string, error) {
	offered := make(map[string]bool)
	for _, t := range r.Tools {
		info, err := t.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("resolve tool names: %w", err)
		}
		offered[info.Name] = true
	}
	withheld := make(map[string]bool, len(r.FsDisabled)+len(r.PlantaskDisabled))
	for _, name := range r.FsDisabled {
		withheld[name] = true
	}
	for _, name := range r.PlantaskDisabled {
		withheld[name] = true
	}
	// The fs and plantask middlewares' tools exist only when the jail was rooted.
	if r.AgentDir != "" {
		for _, name := range FilesystemToolNames {
			if !withheld[name] {
				offered[name] = true
			}
		}
		for _, name := range PlantaskToolNames {
			if !withheld[name] {
				offered[name] = true
			}
		}
		if r.Shell {
			offered[ToolExecute] = true
		}
	}

	names := make([]string, 0, len(offered))
	for _, entryName := range catalogOrder {
		if offered[entryName] {
			names = append(names, entryName)
		}
	}
	return names, nil
}
