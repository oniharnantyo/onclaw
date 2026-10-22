package backend

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	einofs "github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/adk/middlewares/plantask"
)

// plantaskBackend adapts fsJailedBackend to satisfy plantask.Backend.
type plantaskBackend struct {
	jail *fsJailedBackend
}

var _ plantask.Backend = (*plantaskBackend)(nil)

// NewPlantaskBackend constructs a plantask.Backend backed by a jailed workspace.
// All file operations (including task JSON files and .highwatermark) remain
// strictly confined within the workspace directory.
func NewPlantaskBackend(agentDir string, extraRoots ...string) (plantask.Backend, error) {
	jailBackend, err := NewFilesystemJailedWithRoots(agentDir, extraRoots...)
	if err != nil {
		return nil, err
	}
	jail, ok := jailBackend.(*fsJailedBackend)
	if !ok {
		return nil, fmt.Errorf("unexpected backend implementation type %T", jailBackend)
	}
	return &plantaskBackend{jail: jail}, nil
}

// LsInfo lists file information under the given path, prefixing FileInfo.Path with
// req.Path so downstream consumers (like plantask.listTasks) can pass file.Path directly
// back to Read. If the directory does not exist yet, an empty slice is returned.
func (p *plantaskBackend) LsInfo(ctx context.Context, req *einofs.LsInfoRequest) ([]einofs.FileInfo, error) {
	infos, err := p.jail.LsInfo(ctx, req)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err) {
			return []einofs.FileInfo{}, nil
		}
		return nil, err
	}
	if req == nil || req.Path == "" {
		return infos, nil
	}
	result := make([]einofs.FileInfo, len(infos))
	for i, info := range infos {
		result[i] = info
		result[i].Path = filepath.ToSlash(filepath.Join(req.Path, info.Path))
	}
	return result, nil
}

// Read reads file content from within the jail.
func (p *plantaskBackend) Read(ctx context.Context, req *einofs.ReadRequest) (*einofs.FileContent, error) {
	return p.jail.Read(ctx, req)
}

// Write creates or updates file content within the jail.
func (p *plantaskBackend) Write(ctx context.Context, req *einofs.WriteRequest) error {
	return p.jail.Write(ctx, req)
}

// Delete permanently removes a file within the jail. It prevents path escape via ..,
// raw host paths, symlinks outside the jail, and deletion of the workspace root.
func (p *plantaskBackend) Delete(ctx context.Context, req *plantask.DeleteRequest) error {
	if req == nil {
		return fmt.Errorf("delete request cannot be nil")
	}
	if req.FilePath == "" {
		return fmt.Errorf("file path cannot be empty")
	}

	p.jail.mu.Lock()
	defer p.jail.mu.Unlock()

	resolved, err := p.jail.resolveWritablePath(req.FilePath)
	if err != nil {
		return err
	}

	// Verify that the path is not the workspace root.
	if resolved == p.jail.rootAbs {
		return fmt.Errorf("cannot delete the workspace root %s", p.jail.mount)
	}

	if err := os.Remove(resolved); err != nil {
		return err
	}
	return nil
}
