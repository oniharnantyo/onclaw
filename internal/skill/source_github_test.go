package skill_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/skill"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func createTarGzLocal(files map[string]string) ([]byte, error) {
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

func TestGithubFetcher(t *testing.T) {
	ctx := context.Background()

	// 1. Invalid repo format
	f := skill.NewGithubFetcher()
	_, err := f.Fetch(ctx, "invalid-repo", "")
	if err == nil {
		t.Error("expected error for invalid github repository identifier")
	}

	// 2. Successful fetch with mock client
	files := map[string]string{
		"SKILL.md": "---\nname: gh-skill\ndescription: gh desc\n---\nbody",
	}
	tarBytes, err := createTarGzLocal(files)
	if err != nil {
		t.Fatalf("createTarGzLocal failed: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "tar.gz") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/x-gzip")
		w.WriteHeader(http.StatusOK)
		w.Write(tarBytes)
	}))
	defer server.Close()

	// Temporarily override the package github client to point to our test server
	origClient := http.DefaultClient
	mockClient := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			// Redirect any request to our local test server
			mockReq, err := http.NewRequest(req.Method, server.URL+req.URL.Path, req.Body)
			if err != nil {
				return nil, err
			}
			return http.DefaultClient.Do(mockReq)
		}),
	}
	skill.SetGithubHTTPClient(mockClient)
	defer skill.SetGithubHTTPClient(origClient)

	tempDir, err := f.Fetch(ctx, "github.com/owner/repo", "main")
	if err != nil {
		t.Fatalf("fetch github failed: %v", err)
	}
	defer os.RemoveAll(tempDir)

	if _, err := os.Stat(filepath.Join(tempDir, "SKILL.md")); err != nil {
		t.Errorf("expected SKILL.md to be extracted: %v", err)
	}

	// Test GithubFetcher failure
	badMockClient := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("network error")
		}),
	}
	skill.SetGithubHTTPClient(badMockClient)
	_, err = f.Fetch(ctx, "github.com/owner/repo", "main")
	if err == nil {
		t.Error("expected error on network failure")
	}
}
