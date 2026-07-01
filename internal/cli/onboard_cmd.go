package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/llm"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/urfave/cli/v3"
)

type providerConfig struct {
	kind           string
	promptBaseURL  bool
	defaultBaseURL string
	keyless        bool
}

var providerConfigs = []providerConfig{
	{kind: "anthropic", promptBaseURL: false, defaultBaseURL: "", keyless: false},
	{kind: "openai", promptBaseURL: false, defaultBaseURL: "", keyless: false},
	{kind: "openai-compatible", promptBaseURL: true, defaultBaseURL: "", keyless: false},
	{kind: "ollama", promptBaseURL: true, defaultBaseURL: "http://localhost:11434/v1", keyless: true},
}

func runProviderSetup(ctx context.Context, mgr *llm.Service, db *sql.DB, dbPath string, in io.Reader, out io.Writer) error {
	fmt.Fprintln(out, "Starting provider setup...")

	for {
		fmt.Fprintln(out, "Please select a provider kind:")
		kinds := make([]string, len(providerConfigs))
		for i, cfg := range providerConfigs {
			kinds[i] = cfg.kind
		}

		idx, err := promptChoice("Select provider kind", kinds, in, out)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}
		cfg := providerConfigs[idx]

		var name string
		for {
			name, err = promptString("Enter profile name", cfg.kind, in, out)
			if err != nil {
				return err
			}

			// Check name collision
			_, err = mgr.GetProfile(ctx, name)
			if err == nil {
				fmt.Fprintf(out, "Provider profile %q already exists. Please enter a unique name.\n", name)
				continue
			}
			if !strings.Contains(err.Error(), "not found") {
				return err
			}
			break
		}

		var baseURL string
		if cfg.promptBaseURL {
			var err error
			baseURL, err = promptString("Enter base URL", cfg.defaultBaseURL, in, out)
			if err != nil {
				return err
			}
		}

		var apiKey string
		if !cfg.keyless {
			var err error
			apiKey, err = promptSecret("Enter API key", in, out)
			if err != nil {
				return err
			}
		}

		p := &store.Profile{
			Name:         name,
			ProviderType: cfg.kind,
			APIBase:      baseURL,
			Enabled:      1,
		}
		if err := mgr.AddProfile(ctx, p); err != nil {
			return fmt.Errorf("add profile %q: %w", name, err)
		}

		if !cfg.keyless {
			if err := mgr.SetSecret(ctx, name, apiKey); err != nil {
				return fmt.Errorf("set secret for profile %q: %w", name, err)
			}
		}

		_ = signalRunningProcess(dbPath)
		fmt.Fprintf(out, "Provider profile %q configured successfully.\n", name)

		addAnother, err := promptConfirm("Add another provider?", false, in, out)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}
		if !addAnother {
			break
		}
	}

	if err := setDefaultProvider(ctx, db, in, out); err != nil {
		return err
	}

	_ = signalRunningProcess(dbPath)
	fmt.Fprintln(out, "Provider setup complete.")
	return nil
}

func setDefaultProvider(ctx context.Context, db *sql.DB, in io.Reader, out io.Writer) error {
	var existingDefault string
	err := db.QueryRowContext(ctx, "SELECT value FROM preferences WHERE key = 'default_provider'").Scan(&existingDefault)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if existingDefault != "" {
		return nil
	}

	rows, err := db.QueryContext(ctx, "SELECT name FROM llm_providers WHERE enabled = 1 ORDER BY name ASC")
	if err != nil {
		return err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if len(names) == 0 {
		return nil
	}
	if len(names) == 1 {
		_, err = db.ExecContext(ctx, "INSERT OR REPLACE INTO preferences (key, value) VALUES ('default_provider', ?)", names[0])
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Silently set default provider to %q.\n", names[0])
		return nil
	}

	fmt.Fprintln(out, "Multiple providers configured. Please select a default provider:")
	idx, err := promptChoice("Select default provider", names, in, out)
	if err != nil {
		return err
	}
	defaultName := names[idx]
	_, err = db.ExecContext(ctx, "INSERT OR REPLACE INTO preferences (key, value) VALUES ('default_provider', ?)", defaultName)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Default provider set to %q.\n", defaultName)
	return nil
}

func providerSetupCommand(st *appState) *cli.Command {
	return &cli.Command{
		Name:  "setup",
		Usage: "Guide setup of LLM provider profiles interactively",
		Action: func(ctx context.Context, c *cli.Command) error {
			mgr, _, db, err := st.getProviderManager(c)
			if err != nil {
				return err
			}
			defer db.Close()

			return runProviderSetup(ctx, mgr, db, st.cfg.DbPath, os.Stdin, os.Stdout)
		},
	}
}
