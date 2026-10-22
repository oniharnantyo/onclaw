package subagents

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/cloudwego/eino/adk/filesystem"
)

// DiskAppendOpener satisfies filesystem.AppendOpener over a base host directory.
type DiskAppendOpener struct {
	BaseDir string
}

func (d *DiskAppendOpener) OpenAppend(_ context.Context, req *filesystem.OpenAppendRequest) (io.WriteCloser, error) {
	if req == nil || req.FilePath == "" {
		return nil, fmt.Errorf("append opener: empty path")
	}
	rel := filepath.Clean(req.FilePath)
	if filepath.IsAbs(rel) {
		rel = stringsTrimLeadingSlash(rel)
	}
	target := filepath.Join(d.BaseDir, rel)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(target, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
}

func stringsTrimLeadingSlash(s string) string {
	for len(s) > 0 && s[0] == '/' {
		s = s[1:]
	}
	return s
}
