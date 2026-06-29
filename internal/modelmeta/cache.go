package modelmeta

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// CacheDir returns the absolute path to the onclaw cache directory.
func CacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get user home dir: %w", err)
	}
	return filepath.Join(home, ".onclaw", "cache"), nil
}

// LoadOrRefreshCatalog loads the catalog from cache, or fetches/refreshes it if expired.
func LoadOrRefreshCatalog(url string) ([]byte, error) {
	dir, err := CacheDir()
	if err != nil {
		return nil, err
	}
	apiPath := filepath.Join(dir, "api.json")
	shaPath := filepath.Join(dir, "api.json.sha256")

	// 1. Check if cached file exists and is younger than 12 hours
	info, err := os.Stat(apiPath)
	if err == nil {
		if time.Since(info.ModTime()) < 12*time.Hour {
			data, err := os.ReadFile(apiPath)
			if err == nil {
				return data, nil
			}
		}
	}

	// 2. Expired or doesn't exist: fetch from url
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	data, fetchErr := FetchCatalog(ctx, url)
	if fetchErr != nil {
		// Network/fetch failure: fall back to stale cache if present
		if _, err := os.Stat(apiPath); err == nil {
			staleData, readErr := os.ReadFile(apiPath)
			if readErr == nil {
				return staleData, nil
			}
		}
		return nil, fmt.Errorf("failed to fetch models.dev catalog: %w", fetchErr)
	}

	// 3. Ensure directory exists
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	// 4. Compute checksum
	h := sha256.New()
	h.Write(data)
	newSha := hex.EncodeToString(h.Sum(nil))

	// 5. Compare with stored checksum
	var oldSha string
	shaBytes, err := os.ReadFile(shaPath)
	if err == nil {
		oldSha = string(bytes.TrimSpace(shaBytes))
	}

	if oldSha == newSha {
		// Unchanged: refresh mod time only
		now := time.Now()
		if err := os.Chtimes(apiPath, now, now); err != nil {
			return nil, fmt.Errorf("failed to refresh cache file mod time: %w", err)
		}
	} else {
		// Changed: write atomically to api.json via temp file
		tmpPath := apiPath + ".tmp"
		if err := os.WriteFile(tmpPath, data, 0600); err != nil {
			return nil, fmt.Errorf("failed to write temp cache file: %w", err)
		}
		if err := os.Rename(tmpPath, apiPath); err != nil {
			os.Remove(tmpPath)
			return nil, fmt.Errorf("failed to rename temp cache file: %w", err)
		}
		// Write checksum
		if err := os.WriteFile(shaPath, []byte(newSha), 0600); err != nil {
			return nil, fmt.Errorf("failed to write checksum file: %w", err)
		}
	}

	return data, nil
}
