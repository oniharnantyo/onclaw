package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/agent"
	"github.com/oniharnantyo/onclaw/internal/modelmeta"
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
						Usage: "Optional reasoning effort override (low, medium, high, minimal, xhigh, max, none, or on/off toggle)",
					},
					&cli.IntFlag{
						Name:  "reasoning-budget",
						Usage: "Optional reasoning budget override in tokens",
					},
					&cli.StringFlag{
						Name:  "workspace",
						Usage: "Optional custom workspace path",
					},
					&cli.StringFlag{
						Name:  "system-prompt",
						Usage: "Optional extra system instructions or '-' to read from stdin",
					},
					&cli.IntFlag{
						Name:  "max-context",
						Usage: "Optional max context tokens override (0 = use global default)",
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
					reasoningBudget := int(c.Int("reasoning-budget"))
					workspace := c.String("workspace")
					systemPrompt := c.String("system-prompt")
					maxContext := int(c.Int("max-context"))
					if maxContext < 0 {
						return fmt.Errorf("max-context must be >= 0")
					}

					if systemPrompt == "-" {
						fmt.Println("Reading system prompt from stdin... (Ctrl+D to finish)")
						data, err := io.ReadAll(os.Stdin)
						if err != nil {
							return fmt.Errorf("failed to read system prompt from stdin: %w", err)
						}
						systemPrompt = string(data)
					}

					mgr, _, db, err := st.getProviderManager(c)
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

					agentWS := workspace
					if agentWS == "" {
						agentWS = defaultAgentWS
					}

					// Seed workspace files and global USER.md from templates
					if err := os.MkdirAll(agentWS, 0755); err != nil {
						return fmt.Errorf("failed to create agent workspace: %w", err)
					}
					if err := agent.SeedWorkspace(agentWS); err != nil {
						return fmt.Errorf("failed to seed agent workspace: %w", err)
					}
					if err := agent.SeedGlobalUser(filepath.Join(home, ".onclaw")); err != nil {
						return fmt.Errorf("failed to seed global user facts: %w", err)
					}

					var modelMetadataJSON string
					if model == "" {
						mID, meta, pickedEffort, pickedBudget, err := pickModel(ctx, mgr, provider, os.Stdin, os.Stdout)
						if err != nil {
							return fmt.Errorf("failed to pick model: %w", err)
						}
						model = mID
						jsonBytes, err := json.Marshal(meta)
						if err != nil {
							return fmt.Errorf("failed to marshal model metadata: %w", err)
						}
						modelMetadataJSON = string(jsonBytes)
						reasoning = pickedEffort
						reasoningBudget = pickedBudget
					} else {
						p, err := mgr.GetProfile(ctx, provider)
						if err != nil {
							return fmt.Errorf("referenced provider %q not found or disabled: %w", provider, err)
						}
						apiKey, _ := mgr.ResolveSecret(ctx, provider)
						catalog, _ := modelmeta.GetCatalog()
						meta := modelmeta.Resolve(ctx, model, p.ProviderType, p.APIBase, apiKey, catalog)
						jsonBytes, err := json.Marshal(meta)
						if err != nil {
							return fmt.Errorf("failed to marshal model metadata: %w", err)
						}
						modelMetadataJSON = string(jsonBytes)

						if err := validateReasoning(reasoning, reasoningBudget, meta); err != nil {
							return err
						}
					}

					a := &store.Agent{
						Name:                  name,
						Provider:              provider,
						Model:                 model,
						ModelMetadata:         modelMetadataJSON,
						ReasoningEffort:       reasoning,
						ReasoningBudgetTokens: reasoningBudget,
						SystemPrompt:          systemPrompt,
						Workspace:             agentWS,
						MaxContextTokens:      maxContext,
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

					mgr, _, db, err := st.getProviderManager(c)
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
					mgr, _, db, err := st.getProviderManager(c)
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
						meta, _ := store.UnmarshalModelMetadata(a.ModelMetadata)
						metaStr := "unknown"
						if meta != nil {
							metaStr = fmt.Sprintf("context: %d, thinking: %t, modalities: %s",
								meta.ContextWindow, meta.Thinking, strings.Join(meta.InputModalities, ","))
						}
						fmt.Printf("%s %s (provider: %s, model: %s (%s), reasoning: %s, workspace: %s)\n",
							marker, a.Name, a.Provider, a.Model, metaStr, a.ReasoningEffort, a.Workspace)
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

					mgr, _, db, err := st.getProviderManager(c)
					if err != nil {
						return err
					}
					defer db.Close()

					a, err := mgr.GetAgent(ctx, name)
					if err != nil {
						return fmt.Errorf("agent %q not found: %w", name, err)
					}

					meta, _ := store.UnmarshalModelMetadata(a.ModelMetadata)
					var metaStr string
					if meta != nil {
						metaStr = fmt.Sprintf("context window: %d, thinking: %t, modalities: %s",
							meta.ContextWindow, meta.Thinking, strings.Join(meta.InputModalities, ","))
					} else {
						metaStr = "unknown"
					}

					fmt.Printf("Name:             %s\n", a.Name)
					fmt.Printf("Provider:         %s\n", a.Provider)
					fmt.Printf("Model Override:   %s\n", a.Model)
					fmt.Printf("Model Metadata:   %s\n", metaStr)
					fmt.Printf("Reasoning Effort: %s\n", a.ReasoningEffort)
					if a.ReasoningBudgetTokens > 0 {
						fmt.Printf("Reasoning Budget: %d tokens\n", a.ReasoningBudgetTokens)
					}
					fmt.Printf("Workspace:        %s\n", a.Workspace)
					fmt.Printf("Tools Allowed:    %s\n", a.Tools)
					if a.MaxContextTokens > 0 {
						fmt.Printf("Max Context Override: %d tokens\n", a.MaxContextTokens)
					}
					fmt.Printf("Max Iterations:   %d\n", a.MaxIterations)
					fmt.Printf("System Prompt:\n%s\n", a.SystemPrompt)
					return nil
				},
			},
			{
				Name:      "edit",
				Usage:     "Edit an agent profile's model, reasoning effort, or budget",
				ArgsUsage: "<name>",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "model",
						Usage: "Override model name, or leave empty to trigger interactive re-picker",
					},
					&cli.StringFlag{
						Name:  "reasoning",
						Usage: "Override reasoning effort (low, medium, high, minimal, xhigh, max, none, on/off toggle, or empty)",
					},
					&cli.IntFlag{
						Name:  "reasoning-budget",
						Usage: "Override reasoning budget override in tokens",
					},
					&cli.StringFlag{
						Name:  "workspace",
						Usage: "Override workspace path",
					},
					&cli.IntFlag{
						Name:  "max-context",
						Usage: "Override max context tokens (0 = use global default)",
					},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.Args().Len() < 1 {
						return fmt.Errorf("agent name is required")
					}
					name := c.Args().First()

					mgr, _, db, err := st.getProviderManager(c)
					if err != nil {
						return err
					}
					defer db.Close()

					a, err := mgr.GetAgent(ctx, name)
					if err != nil {
						return fmt.Errorf("agent %q not found: %w", name, err)
					}

					hasModel := c.IsSet("model")
					hasReasoning := c.IsSet("reasoning")
					hasReasoningBudget := c.IsSet("reasoning-budget")
					hasWorkspace := c.IsSet("workspace")
					hasMaxContext := c.IsSet("max-context")

					if hasMaxContext && c.Int("max-context") < 0 {
						return fmt.Errorf("max-context must be >= 0")
					}

					triggerPicker := (!hasModel && !hasReasoning && !hasReasoningBudget && !hasWorkspace && !hasMaxContext) || (hasModel && c.String("model") == "")

					if triggerPicker {
						mID, meta, effort, budget, err := pickModel(ctx, mgr, a.Provider, os.Stdin, os.Stdout)
						if err != nil {
							return fmt.Errorf("failed to pick model: %w", err)
						}
						a.Model = mID
						jsonBytes, err := json.Marshal(meta)
						if err != nil {
							return fmt.Errorf("failed to marshal model metadata: %w", err)
						}
						a.ModelMetadata = string(jsonBytes)
						a.ReasoningEffort = effort
						a.ReasoningBudgetTokens = budget
					} else {
						if hasModel {
							modelVal := c.String("model")
							a.Model = modelVal
							p, err := mgr.GetProfile(ctx, a.Provider)
							if err != nil {
								return fmt.Errorf("provider %q not found: %w", a.Provider, err)
							}
							apiKey, _ := mgr.ResolveSecret(ctx, a.Provider)
							catalog, _ := modelmeta.GetCatalog()
							meta := modelmeta.Resolve(ctx, modelVal, p.ProviderType, p.APIBase, apiKey, catalog)
							jsonBytes, err := json.Marshal(meta)
							if err != nil {
								return fmt.Errorf("failed to marshal model metadata: %w", err)
							}
							a.ModelMetadata = string(jsonBytes)
						}

						meta, err := store.UnmarshalModelMetadata(a.ModelMetadata)
						if err != nil {
							return fmt.Errorf("failed to unmarshal model metadata: %w", err)
						}

						effVal := a.ReasoningEffort
						if hasReasoning {
							effVal = c.String("reasoning")
						}
						budgetVal := a.ReasoningBudgetTokens
						if hasReasoningBudget {
							budgetVal = int(c.Int("reasoning-budget"))
						}

						if err := validateReasoning(effVal, budgetVal, *meta); err != nil {
							return err
						}

						a.ReasoningEffort = effVal
						a.ReasoningBudgetTokens = budgetVal

						if hasWorkspace {
							a.Workspace = c.String("workspace")
						}
					}

					if hasMaxContext {
						a.MaxContextTokens = int(c.Int("max-context"))
					}

					if err := mgr.UpdateAgent(ctx, a); err != nil {
						return fmt.Errorf("failed to update agent: %w", err)
					}

					_ = signalRunningProcess(st.cfg.DbPath)
					fmt.Printf("Agent %q updated successfully.\n", name)
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

					mgr, _, db, err := st.getProviderManager(c)
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

func validateReasoning(effort string, budget int, meta store.ModelMetadata) error {
	// If neither effort nor budget is provided, it's valid
	if effort == "" && budget == 0 {
		return nil
	}

	// If the model is not a reasoning model or has no reasoning options
	if !meta.Thinking || len(meta.ReasoningOptions) == 0 {
		return fmt.Errorf("model is not a reasoning model or its options are unknown, but reasoning settings were provided")
	}

	// Map to verify options
	var hasEffort, hasBudget, hasToggle bool
	var allowedEfforts []string
	var budgetMin, budgetMax int

	for i := range meta.ReasoningOptions {
		opt := &meta.ReasoningOptions[i]
		switch opt.Type {
		case "effort":
			hasEffort = true
			allowedEfforts = opt.Values
		case "budget_tokens":
			hasBudget = true
			budgetMin = opt.Min
			budgetMax = opt.Max
		case "toggle":
			hasToggle = true
		}
	}

	// Validate effort
	if effort != "" {
		if hasEffort {
			found := false
			for _, v := range allowedEfforts {
				if v == effort {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("invalid reasoning effort %q: must be one of %s", effort, strings.Join(allowedEfforts, ", "))
			}
		} else if hasToggle {
			if effort != "on" && effort != "off" {
				return fmt.Errorf("invalid reasoning effort %q: model only supports reasoning toggle ('on' or 'off')", effort)
			}
		} else {
			return fmt.Errorf("reasoning effort %q is not supported by this model", effort)
		}
	}

	// Validate budget
	if budget != 0 {
		if hasBudget {
			if budget < budgetMin || budget > budgetMax {
				return fmt.Errorf("invalid reasoning budget %d: must be between %d and %d tokens", budget, budgetMin, budgetMax)
			}
		} else {
			return fmt.Errorf("reasoning budget %d is not supported by this model", budget)
		}
	}

	return nil
}
