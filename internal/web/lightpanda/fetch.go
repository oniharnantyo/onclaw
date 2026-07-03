package lightpanda

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/web"
)

func init() {
	web.RegisterFetcher("lightpanda", func(cfg web.Config, resolver secrets.SecretResolver) (web.Fetcher, error) {
		return &lightpandaFetcher{cfg: cfg}, nil
	})
}

type lightpandaFetcher struct {
	cfg         web.Config
	execCommand func(ctx context.Context, name string, arg ...string) *exec.Cmd
}

func (f *lightpandaFetcher) Fetch(ctx context.Context, urlStr string, headers map[string]string) (web.FetchResult, error) {
	// SSRF check first
	if err := web.ValidateURLNotInternal(urlStr); err != nil {
		return web.FetchResult{}, fmt.Errorf("SSRF protection blocked URL: %w", err)
	}

	binPath := f.cfg.LightpandaBinPath
	if binPath == "" {
		binPath = "lightpanda"
	}

	// Build command args
	args := []string{"fetch", "--dump", "markdown", urlStr}

	timeout := time.Duration(f.cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var cmd *exec.Cmd
	if f.execCommand != nil {
		cmd = f.execCommand(cmdCtx, binPath, args...)
	} else {
		cmd = exec.CommandContext(cmdCtx, binPath, args...)
	}

	output, err := cmd.Output()
	if err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound) {
			return web.FetchResult{}, fmt.Errorf("lightpanda binary %q not found: %w", binPath, err)
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return web.FetchResult{}, fmt.Errorf("lightpanda command exited with error (code %d): %s: %w", exitErr.ExitCode(), string(exitErr.Stderr), err)
		}
		return web.FetchResult{}, fmt.Errorf("failed to run lightpanda: %w", err)
	}

	// Limit byte reading
	content := string(output)
	if int64(len(content)) > f.cfg.MaxBytes {
		content = content[:f.cfg.MaxBytes]
	}

	return web.FetchResult{Content: content}, nil
}
