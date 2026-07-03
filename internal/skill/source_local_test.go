package skill_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/skill"
)

func createZipLocalForLocal(files map[string]string) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := zw.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := f.Write([]byte(content)); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func createTarGzLocalForLocal(files map[string]string) ([]byte, error) {
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			return nil, err
		}
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gzw.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func TestLocalFetcher_Archive(t *testing.T) {
	ctx := context.Background()
	tmpDir, err := os.MkdirTemp("", "onclaw-local-fetcher")
	if err != nil {
		t.Fatalf("failed temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	f := skill.NewLocalFetcher()

	// 1. Path does not exist
	_, err = f.Fetch(ctx, filepath.Join(tmpDir, "nonexistent"), "")
	if err == nil {
		t.Error("expected error for nonexistent local path")
	}

	// 2. Unsupported format
	unsupportedFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(unsupportedFile, []byte("hello"), 0644); err != nil {
		t.Fatalf("failed write: %v", err)
	}
	_, err = f.Fetch(ctx, unsupportedFile, "")
	if err == nil {
		t.Error("expected error for unsupported archive type")
	}

	// 3. Local zip file
	files := map[string]string{
		"SKILL.md": "---\nname: local-zip-skill\ndescription: local zip desc\n---\nbody",
	}
	zipBytes, err := createZipLocalForLocal(files)
	if err != nil {
		t.Fatalf("createZipLocalForLocal failed: %v", err)
	}
	zipFile := filepath.Join(tmpDir, "test.zip")
	if err := os.WriteFile(zipFile, zipBytes, 0644); err != nil {
		t.Fatalf("failed write zip: %v", err)
	}
	dir1, err := f.Fetch(ctx, zipFile, "")
	if err != nil {
		t.Fatalf("fetch local zip failed: %v", err)
	}
	defer os.RemoveAll(dir1)

	// 4. Local tar.gz file
	tarBytes, err := createTarGzLocalForLocal(files)
	if err != nil {
		t.Fatalf("createTarGzLocalForLocal failed: %v", err)
	}
	tarFile := filepath.Join(tmpDir, "test.tar.gz")
	if err := os.WriteFile(tarFile, tarBytes, 0644); err != nil {
		t.Fatalf("failed write tar.gz: %v", err)
	}
	dir2, err := f.Fetch(ctx, tarFile, "")
	if err != nil {
		t.Fatalf("fetch local tar.gz failed: %v", err)
	}
	defer os.RemoveAll(dir2)
}
