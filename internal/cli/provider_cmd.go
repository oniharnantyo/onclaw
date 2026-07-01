package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/urfave/cli/v3"
	"golang.org/x/term"
)

func providerCommand(st *appState) *cli.Command {
	return &cli.Command{
		Name:  "provider",
		Usage: "Manage provider profiles and credentials",
		Commands: []*cli.Command{
			providerSetupCommand(st),
			{
				Name:      "add",
				Usage:     "Add a new provider profile",
				ArgsUsage: "<name>",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "kind",
						Usage:    "Provider kind (e.g. openai, anthropic, ollama)",
						Required: true,
					},
					&cli.StringFlag{
						Name:  "base-url",
						Usage: "Base URL for the provider API",
					},
					&cli.IntFlag{
						Name:  "context-window",
						Usage: "Context window size in tokens",
					},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.Args().Len() < 1 {
						return fmt.Errorf("provider name is required")
					}
					name := c.Args().First()
					kind := c.String("kind")
					baseURL := c.String("base-url")
					contextWindow := c.Int("context-window")

					mgr, _, db, err := st.getProviderManager(c)
					if err != nil {
						return err
					}
					defer db.Close()

					var settingsJSON []byte
					if contextWindow > 0 {
						settings := map[string]interface{}{
							"context_window": contextWindow,
						}
						settingsJSON, _ = json.Marshal(settings)
					}

					p := &store.Profile{
						Name:         name,
						ProviderType: kind,
						APIBase:      baseURL,
						Enabled:      1,
						Settings:     string(settingsJSON),
					}
					if err := mgr.AddProfile(ctx, p); err != nil {
						return err
					}
					_ = signalRunningProcess(st.cfg.DbPath)
					fmt.Printf("Provider profile %q added successfully.\n", name)
					return nil
				},
			},
			{
				Name:      "login",
				Usage:     "Log in to a provider by setting its API key",
				ArgsUsage: "<name>",
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.Args().Len() < 1 {
						return fmt.Errorf("provider name is required")
					}
					name := c.Args().First()

					mgr, _, db, err := st.getProviderManager(c)
					if err != nil {
						return err
					}
					defer db.Close()

					if _, err := mgr.GetProfile(ctx, name); err != nil {
						return fmt.Errorf("provider profile %q not found: %w", name, err)
					}

					fmt.Printf("Enter API key for %s: ", name)
					fd := int(os.Stdin.Fd())
					var apiKey string
					if term.IsTerminal(fd) {
						byteKey, err := term.ReadPassword(fd)
						if err != nil {
							return fmt.Errorf("read API key: %w", err)
						}
						fmt.Println()
						apiKey = strings.TrimSpace(string(byteKey))
					} else {
						var line string
						if _, err := fmt.Fscanln(os.Stdin, &line); err != nil && !errors.Is(err, io.EOF) {
							return fmt.Errorf("read API key from stdin: %w", err)
						}
						apiKey = strings.TrimSpace(line)
					}

					if apiKey == "" {
						return fmt.Errorf("API key cannot be empty")
					}

					if err := mgr.SetSecret(ctx, name, apiKey); err != nil {
						return err
					}
					_ = signalRunningProcess(st.cfg.DbPath)
					fmt.Printf("API key for provider %q saved successfully.\n", name)
					return nil
				},
			},
			{
				Name:  "list",
				Usage: "List all provider profiles",
				Action: func(ctx context.Context, c *cli.Command) error {
					mgr, _, db, err := st.getProviderManager(c)
					if err != nil {
						return err
					}
					defer db.Close()

					profiles, err := mgr.ListProfiles(ctx)
					if err != nil {
						return err
					}

					for _, p := range profiles {
						secret, err := mgr.GetSecret(ctx, p.Name)
						if err != nil {
							return err
						}
						status := "not configured"
						if secret != "" {
							status = "configured"
						}

						var settings map[string]interface{}
						if p.Settings != "" {
							_ = json.Unmarshal([]byte(p.Settings), &settings)
						}
						cwStr := "(default)"
						if settings != nil {
							if cwVal, ok := settings["context_window"]; ok {
								if cwFloat, ok := cwVal.(float64); ok {
									cwStr = fmt.Sprintf("%d", int(cwFloat))
								}
							}
						}

						fmt.Printf("name: %s, kind: %s, base_url: %s, context_window: %s, api_key: [%s]\n",
							p.Name, p.ProviderType, p.APIBase, cwStr, status)
					}
					return nil
				},
			},
			{
				Name:      "use",
				Usage:     "Set the default provider profile",
				ArgsUsage: "<name>",
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.Args().Len() < 1 {
						return fmt.Errorf("provider name is required")
					}
					name := c.Args().First()

					mgr, _, db, err := st.getProviderManager(c)
					if err != nil {
						return err
					}
					defer db.Close()

					if _, err := mgr.GetProfile(ctx, name); err != nil {
						return fmt.Errorf("provider profile %q not found: %w", name, err)
					}

					_, err = db.ExecContext(ctx,
						"INSERT OR REPLACE INTO preferences (key, value) VALUES ('default_provider', ?)",
						name)
					if err != nil {
						return err
					}
					_ = signalRunningProcess(st.cfg.DbPath)
					fmt.Printf("Default provider set to %q.\n", name)
					return nil
				},
			},
			{
				Name:      "remove",
				Usage:     "Remove a provider profile",
				ArgsUsage: "<name>",
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.Args().Len() < 1 {
						return fmt.Errorf("provider name is required")
					}
					name := c.Args().First()

					mgr, _, db, err := st.getProviderManager(c)
					if err != nil {
						return err
					}
					defer db.Close()

					if _, err := mgr.GetProfile(ctx, name); err != nil {
						return fmt.Errorf("provider profile %q not found: %w", name, err)
					}
					if err := mgr.RemoveProfile(ctx, name); err != nil {
						return fmt.Errorf("failed to remove provider profile %q: %w", name, err)
					}
					_ = signalRunningProcess(st.cfg.DbPath)
					fmt.Printf("Provider profile %q removed.\n", name)
					return nil
				},
			},
		},
	}
}