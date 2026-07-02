package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/oniharnantyo/onclaw/internal/browser"
)

// LaunchRemote connects to a remote CDP server, resolves the WebSocket URL and returns it.
func LaunchRemote(ctx context.Context, cfg browser.RemoteConfig) (string, func() error, error) {
	if cfg.URL == "" {
		return "", nil, fmt.Errorf("remote URL is required")
	}

	wsURL, err := resolveWebSocketURL(ctx, cfg.URL)
	if err != nil {
		return "", nil, fmt.Errorf("failed to resolve remote wsURL: %w", err)
	}

	noopStop := func() error {
		return nil
	}

	return wsURL, noopStop, nil
}

func resolveWebSocketURL(ctx context.Context, httpURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", httpURL+"/json/version", nil)
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code from remote CDP endpoint: %d", resp.StatusCode)
	}

	var versionInfo struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&versionInfo); err != nil {
		return "", err
	}

	if versionInfo.WebSocketDebuggerURL == "" {
		return "", fmt.Errorf("webSocketDebuggerUrl not found in json response")
	}

	return versionInfo.WebSocketDebuggerURL, nil
}
