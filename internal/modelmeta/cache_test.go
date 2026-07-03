package modelmeta_test

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/modelmeta"
)

func TestCacheTTLAndChecksum(t *testing.T) {
	// Set up local temp directory for cache to avoid messing with ~/.onclaw/cache
	tmpDir, err := os.MkdirTemp("", "onclaw-cache-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Override user home dir / cache dir for the test
	// We can use a trick: we define a package level cacheDirOverride string in cache.go,
	// or we can mock/override CacheDir function using environment variables, or just override it.
	// Wait, let's check cache.go: CacheDir resolves home via os.UserHomeDir().
	// If we set HOME env variable, os.UserHomeDir() will return it on Unix systems!
	oldHome := os.Getenv("HOME")
	defer os.Setenv("HOME", oldHome)
	os.Setenv("HOME", tmpDir)

	// Let's verify CacheDir returns tmpDir
	cd, err := modelmeta.CacheDir()
	if err != nil {
		t.Fatalf("CacheDir err: %v", err)
	}
	expectedDir := filepath.Join(tmpDir, ".onclaw", "cache")
	if cd != expectedDir {
		t.Errorf("expected cache dir %s, got %s", expectedDir, cd)
	}

	catalogData := `{"openai": {"models": {"gpt-4o": {"limit": {"context": 128000}, "reasoning": false, "modalities": {"input": ["text"]}}}}}`

	// 1. Mock server
	serverCallCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCallCount++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(catalogData))
	}))
	defer server.Close()

	// 2. First call: should fetch from server and write files
	data, err := modelmeta.LoadOrRefreshCatalog(server.URL)
	if err != nil {
		t.Fatalf("LoadOrRefreshCatalog first call err: %v", err)
	}
	if string(data) != catalogData {
		t.Errorf("got data %s, expected %s", string(data), catalogData)
	}
	if serverCallCount != 1 {
		t.Errorf("expected 1 server call, got %d", serverCallCount)
	}

	// Verify files written
	apiPath := filepath.Join(expectedDir, "api.json")
	shaPath := filepath.Join(expectedDir, "api.json.sha256")
	if _, err := os.Stat(apiPath); err != nil {
		t.Errorf("api.json not written: %v", err)
	}
	if _, err := os.Stat(shaPath); err != nil {
		t.Errorf("api.json.sha256 not written: %v", err)
	}

	// Verify checksum contents
	shaBytes, _ := os.ReadFile(shaPath)
	h := sha256.New()
	h.Write([]byte(catalogData))
	expectedSha := hex.EncodeToString(h.Sum(nil))
	if string(shaBytes) != expectedSha {
		t.Errorf("expected checksum %s, got %s", expectedSha, string(shaBytes))
	}

	// 3. Second call: should hit cache directly (since within 12h) and not call server
	_, err = modelmeta.LoadOrRefreshCatalog(server.URL)
	if err != nil {
		t.Fatalf("LoadOrRefreshCatalog second call err: %v", err)
	}
	if serverCallCount != 1 {
		t.Errorf("expected server calls to remain 1, got %d", serverCallCount)
	}

	// 4. Force expiration: set modification time to 13 hours ago
	past := time.Now().Add(-13 * time.Hour)
	if err := os.Chtimes(apiPath, past, past); err != nil {
		t.Fatalf("failed to chtimes: %v", err)
	}

	// 5. Third call (expired): should fetch again
	_, err = modelmeta.LoadOrRefreshCatalog(server.URL)
	if err != nil {
		t.Fatalf("LoadOrRefreshCatalog third call err: %v", err)
	}
	if serverCallCount != 2 {
		t.Errorf("expected server calls to be 2, got %d", serverCallCount)
	}

	// 6. Network failure fallback (expired but server is down):
	// Let's set mod time to 13 hours ago again
	if err := os.Chtimes(apiPath, past, past); err != nil {
		t.Fatalf("failed to chtimes: %v", err)
	}
	// Call LoadOrRefreshCatalog with invalid URL (network failure)
	data, err = modelmeta.LoadOrRefreshCatalog("http://invalid-url-that-fails.local/api.json")
	if err != nil {
		t.Fatalf("LoadOrRefreshCatalog expected fallback to stale cache, but got err: %v", err)
	}
	if string(data) != catalogData {
		t.Errorf("expected stale data, got %s", string(data))
	}
}
