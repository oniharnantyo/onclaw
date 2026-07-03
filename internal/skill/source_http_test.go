package skill_test

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/skill"
)

func createZipLocal(files map[string]string) ([]byte, error) {
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

func TestHTTPFetcher_Zip(t *testing.T) {
	ctx := context.Background()
	files := map[string]string{
		"SKILL.md": "---\nname: zip-skill\ndescription: zip desc\n---\nbody",
		"sub/dir/": "", // test directories
	}
	zipBytes, err := createZipLocal(files)
	if err != nil {
		t.Fatalf("createZipLocal failed: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		w.WriteHeader(http.StatusOK)
		w.Write(zipBytes)
	}))
	defer server.Close()

	f := skill.NewHTTPFetcher()
	tempDir, err := f.Fetch(ctx, server.URL+"/archive.zip", "")
	if err != nil {
		t.Fatalf("failed to fetch HTTP zip: %v", err)
	}
	defer os.RemoveAll(tempDir)

	if _, err := os.Stat(filepath.Join(tempDir, "SKILL.md")); err != nil {
		t.Errorf("expected SKILL.md to exist in extracted ZIP: %v", err)
	}
}
