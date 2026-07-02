package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func init() {
	Register("command", commandFactory)
}

type commandHandler struct {
	cfg CommandConfig
}

func commandFactory(cfgBytes []byte) (Handler, error) {
	var cfg CommandConfig
	if len(cfgBytes) > 0 {
		if err := json.Unmarshal(cfgBytes, &cfg); err != nil {
			return nil, fmt.Errorf("invalid command handler config: %w", err)
		}
	}
	if cfg.Command == "" {
		return nil, errors.New("command must not be empty")
	}
	return &commandHandler{cfg: cfg}, nil
}

func (ch *commandHandler) Run(ctx context.Context, payload Payload) (Decision, error) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return DecisionBlock, fmt.Errorf("marshal payload: %w", err)
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", ch.cfg.Command)
	cmd.Stdin = bytes.NewReader(payloadJSON)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if ch.cfg.Cwd != "" {
		cmd.Dir = ch.cfg.Cwd
	}

	var newEnv []string
	safeEnvKeys := map[string]bool{
		"PATH":   true,
		"HOME":   true,
		"USER":   true,
		"TMPDIR": true,
		"PWD":    true,
	}

	allowedKeys := make(map[string]bool)
	for _, k := range ch.cfg.AllowedEnvVars {
		allowedKeys[k] = true
	}

	for _, envVal := range os.Environ() {
		parts := strings.SplitN(envVal, "=", 2)
		if len(parts) == 2 {
			key := parts[0]
			if safeEnvKeys[key] || allowedKeys[key] {
				newEnv = append(newEnv, envVal)
			}
		}
	}
	cmd.Env = newEnv

	err = cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code := exitErr.ExitCode()
			if code == 2 {
				reason := strings.TrimSpace(stderr.String())
				if reason == "" {
					reason = "action blocked by command handler"
				}
				return DecisionBlock, errors.New(reason)
			}
			return DecisionBlock, fmt.Errorf("command failed with exit code %d: %s", code, strings.TrimSpace(stderr.String()))
		}
		return DecisionBlock, fmt.Errorf("command run: %w", err)
	}

	return DecisionAllow, nil
}
