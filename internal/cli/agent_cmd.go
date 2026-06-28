package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/oniharnantyo/onclaw/internal/agent"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/urfave/cli/v3"
)

func agentCommand(st *appState) *cli.Command {
	return &cli.Command{
		Name:  "agent",
		Usage: "Manage agent profiles and configurations",
		Commands: []*cli.Command{
			{
				Name:      "add",
				Usage:     "Add a new agent profile",
				ArgsUsage: "<name>",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "provider",
						Usage:    "Referenced provider profile name",
						Required: true,
					},
					&cli.StringFlag{
						Name:  "model",
						Usage: "Optional model name override",
					},
					&cli.StringFlag{
						Name:  "reasoning",
						Usage: "Optional reasoning effort override (low, medium, high)",
					},
					&cli.StringFlag{
						Name:  "workspace",
						Usage: "Optional custom workspace path",
					},
					&cli.StringFlag{
						Name:  "system-prompt",
						Usage: "Optional extra system instructions or '-' to read from stdin",
					},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.Args().Len() < 1 {
						return fmt.Errorf("agent name is required")
					}
					name := c.Args().First()
					provider := c.String("provider")
					model := c.String("model")
					reasoning := c.String("reasoning")
					workspace := c.String("workspace")
					systemPrompt := c.String("system-prompt")

					if systemPrompt == "-" {
						fmt.Println("Reading system prompt from stdin... (Ctrl+D to finish)")
						data, err := io.ReadAll(os.Stdin)
						if err != nil {
							return fmt.Errorf("failed to read system prompt from stdin: %w", err)
						}
						systemPrompt = string(data)
					}

					mgr, db, err := st.getProviderManager(c)
					if err != nil {
						return err
					}
					defer db.Close()

					// Validate referenced provider exists
					if _, err := mgr.GetProfile(ctx, provider); err != nil {
						return fmt.Errorf("referenced provider %q not found or disabled: %w", provider, err)
					}

					// Build default agent workspace path: ~/.onclaw/workspace/<name>/
					home, err := os.UserHomeDir()
					if err != nil {
						return fmt.Errorf("failed to get user home dir: %w", err)
					}
					defaultAgentWS := filepath.Join(home, ".onclaw", "workspace", name)
					if err := os.MkdirAll(defaultAgentWS, 0755); err != nil {
						return fmt.Errorf("failed to create agent workspace: %w", err)
					}

					agentWS := workspace
					if agentWS == "" {
						agentWS = defaultAgentWS
					}

					// Seed workspace files and global USER.md from templates
					if err := agent.SeedWorkspace(agentWS); err != nil {
						return fmt.Errorf("failed to seed agent workspace: %w", err)
					}
					if err := agent.SeedGlobalUser(filepath.Join(home, ".onclaw")); err != nil {
						return fmt.Errorf("failed to seed global user facts: %w", err)
					}

					a := &store.Agent{
						Name:            name,
						Provider:        provider,
						Model:           model,
						ReasoningEffort: reasoning,
						SystemPrompt:    systemPrompt,
						Workspace:       agentWS,
					}

					if err := mgr.AddAgent(ctx, a); err != nil {
						return err
					}
					_ = signalRunningProcess(st.cfg.DbPath)

					fmt.Printf("Agent %q added successfully. Workspace initialized at: %s\n", name, agentWS)
					return nil
				},
			},
			{
				Name:      "use",
				Usage:     "Set the default agent",
				ArgsUsage: "<name>",
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.Args().Len() < 1 {
						return fmt.Errorf("agent name is required")
					}
					name := c.Args().First()

					mgr, db, err := st.getProviderManager(c)
					if err != nil {
						return err
					}
					defer db.Close()

					// Validate agent exists
					if _, err := mgr.GetAgent(ctx, name); err != nil {
						return fmt.Errorf("agent %q not found: %w", name, err)
					}

					_, err = db.ExecContext(ctx,
						"INSERT OR REPLACE INTO preferences (key, value) VALUES ('default_agent', ?)",
						name)
					if err != nil {
						return err
					}
					_ = signalRunningProcess(st.cfg.DbPath)
					fmt.Printf("Default agent set to %q.\n", name)
					return nil
				},
			},
			{
				Name:  "list",
				Usage: "List all agent profiles",
				Action: func(ctx context.Context, c *cli.Command) error {
					mgr, db, err := st.getProviderManager(c)
					if err != nil {
						return err
					}
					defer db.Close()

					agents, err := mgr.ListAgents(ctx)
					if err != nil {
						return err
					}

					var defaultAgent string
					_ = db.QueryRowContext(ctx, "SELECT value FROM preferences WHERE key = 'default_agent'").Scan(&defaultAgent)

					for _, a := range agents {
						marker := " "
						if a.Name == defaultAgent {
							marker = "*"
						}
						fmt.Printf("%s %s (provider: %s, model: %s, reasoning: %s, workspace: %s)\n",
							marker, a.Name, a.Provider, a.Model, a.ReasoningEffort, a.Workspace)
					}
					return nil
				},
			},
			{
				Name:      "show",
				Usage:     "Show details of an agent profile",
				ArgsUsage: "<name>",
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.Args().Len() < 1 {
						return fmt.Errorf("agent name is required")
					}
					name := c.Args().First()

					mgr, db, err := st.getProviderManager(c)
					if err != nil {
						return err
					}
					defer db.Close()

					a, err := mgr.GetAgent(ctx, name)
					if err != nil {
						return fmt.Errorf("agent %q not found: %w", name, err)
					}

					fmt.Printf("Name:             %s\n", a.Name)
					fmt.Printf("Provider:         %s\n", a.Provider)
					fmt.Printf("Model Override:   %s\n", a.Model)
					fmt.Printf("Reasoning Effort: %s\n", a.ReasoningEffort)
					fmt.Printf("Workspace:        %s\n", a.Workspace)
					fmt.Printf("Tools Allowed:    %s\n", a.Tools)
					fmt.Printf("Max Iterations:   %d\n", a.MaxIterations)
					fmt.Printf("System Prompt:\n%s\n", a.SystemPrompt)
					return nil
				},
			},
			{
				Name:      "remove",
				Usage:     "Remove an agent profile",
				ArgsUsage: "<name>",
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.Args().Len() < 1 {
						return fmt.Errorf("agent name is required")
					}
					name := c.Args().First()

					mgr, db, err := st.getProviderManager(c)
					if err != nil {
						return err
					}
					defer db.Close()

					if err := mgr.RemoveAgent(ctx, name); err != nil {
						return err
					}
					_ = signalRunningProcess(st.cfg.DbPath)
					fmt.Printf("Agent profile %q removed.\n", name)
					return nil
				},
			},
		},
	}
}
