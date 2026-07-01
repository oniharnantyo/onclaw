package skill

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

type githubFetcher struct{}

var githubHttpClient = &http.Client{
	Timeout: 10 * time.Second,
}

// NewGithubFetcher creates a Fetcher for GitHub repository tarballs.
func NewGithubFetcher() Fetcher {
	return &githubFetcher{}
}

func (f *githubFetcher) Fetch(ctx context.Context, source string, branch string) (string, error) {
	// Parse owner and repo
	clean := source
	clean = strings.TrimPrefix(clean, "https://")
	clean = strings.TrimPrefix(clean, "http://")
	clean = strings.TrimPrefix(clean, "github.com/")
	
	// Remove trailing slashes
	clean = strings.TrimRight(clean, "/")
	
	parts := strings.Split(clean, "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid github repository identifier: %s", source)
	}

	owner := parts[0]
	repo := strings.TrimSuffix(parts[1], ".git")

	tempDir, err := os.MkdirTemp("", "onclaw-skill-github")
	if err != nil {
		return "", err
	}

	// Try specified branch, or default to main, falling back to master.
	branchesToTry := []string{"main", "master"}
	if branch != "" {
		branchesToTry = []string{branch}
	}

	var lastErr error
	success := false

	for _, b := range branchesToTry {
		url := fmt.Sprintf("https://codeload.github.com/%s/%s/tar.gz/%s", owner, repo, b)
		
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			lastErr = err
			continue
		}

		resp, err := githubHttpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			err = extractTarGz(resp.Body, tempDir)
			if err != nil {
				lastErr = err
				continue
			}
			success = true
			break
		} else {
			lastErr = fmt.Errorf("github fetch returned status: %s", resp.Status)
		}
	}

	if !success {
		os.RemoveAll(tempDir)
		return "", fmt.Errorf("failed to fetch from GitHub: %w", lastErr)
	}

	return tempDir, nil
}
