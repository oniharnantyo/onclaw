package cli

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/urfave/cli/v3"

	"github.com/oniharnantyo/onclaw/internal/agent"
	"github.com/oniharnantyo/onclaw/internal/llm"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/sqlite"
	"github.com/oniharnantyo/onclaw/internal/workspace"
)

func chatCommand(st *appState) *cli.Command {
	return &cli.Command{
		Name:      "chat",
		Usage:     "Start an interactive chat session with the onclaw agent",
		ArgsUsage: "[prompt]",
		Description: "Starts an interactive REPL session with the selected agent. " +
			"Supports slash commands like /agent <name> and /reasoning <low|medium|high>. " +
			"Ctrl-C cancels the current turn execution, Ctrl-D exits.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "agent",
				Usage: "Agent name to start chat with (defaults to default_agent, fallback to master)",
			},
			&cli.StringFlag{
				Name:  "provider",
				Usage: "Override agent provider profile name",
			},
			&cli.StringFlag{
				Name:  "model",
				Usage: "Override default or agent model name",
			},
			&cli.StringFlag{
				Name:  "reasoning",
				Usage: "Override reasoning effort (low, medium, high)",
			},
			&cli.StringFlag{
				Name:  "workspace",
				Usage: "Override workspace directory path",
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			if err := st.ensure(c); err != nil {
				return err
			}

			mgr, db, err := st.getProviderManager(c)
			if err != nil {
				return err
			}
			defer db.Close()

			// Resolve DB and config path
			resolvedDbPath, err := sqlite.ResolveDbPath(st.cfg.DbPath)
			if err != nil {
				return err
			}
			userConfigDir := filepath.Dir(resolvedDbPath)

			// Setup SIGHUP handler
			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, syscall.SIGHUP)
			go func() {
				for {
					select {
					case <-sigChan:
						st.log.Info("received SIGHUP, triggering reload")
						mgr.TriggerReload()
					case <-ctx.Done():
						return
					}
				}
			}()
			defer func() {
				signal.Stop(sigChan)
			}()

			// Setup fsnotify watcher
			watcher, err := llm.StartDBWatcher(ctx, resolvedDbPath, mgr)
			if err != nil {
				return fmt.Errorf("start db watcher: %w", err)
			}
			defer watcher.Close()

			// Resolve starting agent
			var agentName string
			if c.String("agent") != "" {
				agentName = c.String("agent")
			} else {
				var defAgent string
				err := db.QueryRowContext(ctx, "SELECT value FROM preferences WHERE key = 'default_agent'").Scan(&defAgent)
				if err == nil && defAgent != "" {
					agentName = defAgent
				} else {
					agentName = "master"
				}
			}

			// Active configurations (mutable via slash commands)
			activeAgentName := agentName
			activeReasoning := c.String("reasoning")
			activeModel := c.String("model")
			activeProvider := c.String("provider")

			var assembledAgent *agent.Agent
			var transcriptPath string
			var resolvedWorkspace string

			// Helper to initialize or re-initialize the agent
			initAgent := func() error {
				agentConf, err := mgr.GetAgent(ctx, activeAgentName)
				if err != nil {
					if activeAgentName == "master" {
						agentConf, err = st.getOrSeedMasterAgent(ctx, db, mgr)
						if err != nil {
							return fmt.Errorf("failed to auto-seed master agent: %w", err)
						}
					} else {
						return fmt.Errorf("agent %q not found: %w", activeAgentName, err)
					}
				}

				// Resolve workspace
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("get current directory: %w", err)
				}
				resolvedWorkspace, err = workspace.ResolveWorkspace(
					c.String("workspace"),
					agentConf.Workspace,
					st.cfg.Workspace,
					cwd,
				)
				if err != nil {
					return fmt.Errorf("resolve workspace: %w", err)
				}

				// Resolve effective provider
				var defaultProvider string
				err = db.QueryRowContext(ctx, "SELECT value FROM preferences WHERE key = 'default_provider'").Scan(&defaultProvider)
				if err != nil && !errors.Is(err, sql.ErrNoRows) {
					return err
				}

				providerName := activeProvider
				if providerName == "" {
					profiles, err := mgr.ListProfiles(ctx)
					if err != nil {
						return err
					}

					var enabledCount int
					for _, pr := range profiles {
						if pr.Enabled != 0 {
							enabledCount++
						}
					}

					if enabledCount > 1 && defaultProvider == "" {
						return fmt.Errorf("multiple providers available but no default provider is set; use 'onclaw provider use <name>' to set one")
					}

					if agentConf.Provider != "" {
						providerName = agentConf.Provider
					} else {
						providerName = defaultProvider
					}
				}

				if providerName == "" {
					return fmt.Errorf("no provider specified for agent %q; configure a provider or use the --provider flag", activeAgentName)
				}

				p, err := mgr.GetProfile(ctx, providerName)
				if err != nil {
					return fmt.Errorf("provider %q not found: %w", providerName, err)
				}
				if p.Enabled == 0 {
					return fmt.Errorf("provider %q is disabled", providerName)
				}

				effModel := activeModel
				if effModel == "" {
					effModel = agentConf.Model
				}
				if effModel == "" {
					effModel = st.cfg.Model
				}
				if effModel == "" {
					return fmt.Errorf("no model specified for agent %q and no default model is configured", activeAgentName)
				}

				effReasoning := activeReasoning
				if effReasoning == "" {
					effReasoning = agentConf.ReasoningEffort
				}

				var contextWindow int
				if agentConf.ModelMetadata != "" {
					meta, err := store.UnmarshalModelMetadata(agentConf.ModelMetadata)
					if err == nil && meta != nil {
						contextWindow = meta.ContextWindow
					}
				}
				if contextWindow <= 0 {
					if st.cfg.MaxContextTokens > 0 {
						contextWindow = st.cfg.MaxContextTokens
					} else {
						contextWindow = 64000
					}
				}

				effProfile := *p

				var settings map[string]interface{}
				if effProfile.Settings != "" {
					_ = json.Unmarshal([]byte(effProfile.Settings), &settings)
				}
				if settings == nil {
					settings = make(map[string]interface{})
				}

				if effReasoning != "" {
					settings["reasoning_effort"] = effReasoning
				}

				settingsJSON, err := json.Marshal(settings)
				if err != nil {
					return fmt.Errorf("failed to marshal settings: %w", err)
				}
				effProfile.Settings = string(settingsJSON)

				chatModel, err := mgr.BuildWithProfile(ctx, &effProfile, effModel)
				if err != nil {
					return fmt.Errorf("failed to build model: %w", err)
				}

				assembledAgent, err = agent.AssembleAgent(
					ctx,
					agentConf,
					chatModel,
					resolvedWorkspace,
					userConfigDir,
					st.cfg.Tools.Shell.Policy,
					st.cfg.Tools.Shell.Allowlist,
					contextWindow,
				)
				if err != nil {
					return fmt.Errorf("assemble agent: %w", err)
				}

				transcriptPath = filepath.Join(userConfigDir, "conversations", fmt.Sprintf("%s_transcript.jsonl", agentConf.Name))
				return nil
			}

			// Initialize first time
			if err := initAgent(); err != nil {
				return err
			}

			fmt.Println("onclaw REPL session started.")
			fmt.Printf("Active Agent: %s, Workspace: %s\n", activeAgentName, resolvedWorkspace)
			fmt.Println("Commands: /agent <name> (switch agent), /reasoning <low|medium|high> (set reasoning), Ctrl-D to exit.")
			fmt.Println()

			// Run immediate prompt argument if provided
			firstPrompt := c.Args().First()
			if firstPrompt != "" {
				fmt.Printf("onclaw (%s) > %s\n", activeAgentName, firstPrompt)
				err = agent.RunAgent(ctx, assembledAgent, firstPrompt, os.Stdout, transcriptPath)
				if err != nil {
					fmt.Printf("Error: %v\n", err)
				}
				fmt.Println()
			}

			// REPL prompt loop
			reader := bufio.NewReader(os.Stdin)
			for {
				fmt.Printf("onclaw (%s) > ", activeAgentName)

				input, err := reader.ReadString('\n')
				if err != nil {
					if errors.Is(err, io.EOF) {
						fmt.Println("\nGoodbye!")
						break
					}
					return fmt.Errorf("read input: %w", err)
				}

				input = strings.TrimSpace(input)
				if input == "" {
					continue
				}

				// Handle slash commands
				if strings.HasPrefix(input, "/") {
					parts := strings.Fields(input)
					cmd := parts[0]
					switch cmd {
					case "/agent":
						if len(parts) < 2 {
							fmt.Println("Error: /agent requires an agent name")
							continue
						}
						targetAgent := parts[1]
						activeAgentName = targetAgent
						if err := initAgent(); err != nil {
							fmt.Printf("Error switching agent: %v\n", err)
						} else {
							fmt.Printf("Switched to agent %q.\n", activeAgentName)
							fmt.Printf("Workspace is: %s\n", resolvedWorkspace)
						}
					case "/reasoning":
						if len(parts) < 2 {
							fmt.Println("Error: /reasoning requires low, medium, or high")
							continue
						}
						level := parts[1]
						if level != "low" && level != "medium" && level != "high" {
							fmt.Println("Error: reasoning must be low, medium, or high")
							continue
						}
						activeReasoning = level
						if err := initAgent(); err != nil {
							fmt.Printf("Error updating reasoning: %v\n", err)
						} else {
							fmt.Printf("Reasoning effort set to %q.\n", activeReasoning)
						}
					default:
						fmt.Printf("Unknown command: %s. Supported: /agent <name>, /reasoning <low|medium|high>\n", cmd)
					}
					fmt.Println()
					continue
				}

				// Run agent turn with interrupt support
				turnCtx, turnCancel := context.WithCancel(ctx)
				termChan := make(chan os.Signal, 1)
				signal.Notify(termChan, os.Interrupt, syscall.SIGTERM)

				go func() {
					select {
					case <-termChan:
						turnCancel()
					case <-turnCtx.Done():
					}
					signal.Stop(termChan)
				}()

				err = agent.RunAgent(turnCtx, assembledAgent, input, os.Stdout, transcriptPath)
				turnCancel()

				if err != nil {
					if errors.Is(err, context.Canceled) {
						fmt.Println("\n[Turn Canceled]")
					} else {
						fmt.Printf("\nError: %v\n", err)
					}
				}
				fmt.Println()
			}

			return nil
		},
	}
}
