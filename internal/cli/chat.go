package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/urfave/cli/v3"

	"github.com/oniharnantyo/onclaw/internal/agent"
	"github.com/oniharnantyo/onclaw/internal/llm"
	"github.com/oniharnantyo/onclaw/internal/mcp"
	"github.com/oniharnantyo/onclaw/internal/render"
	"github.com/oniharnantyo/onclaw/internal/store/sqlite"
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

			mgr, mcpStore, db, err := st.getProviderManager(c)
			if err != nil {
				return err
			}
			defer db.Close()

			mcpMgr := mcp.NewManager(mcpStore)
			defer mcpMgr.Close()

			resolvedDbPath, err := sqlite.ResolveDbPath(st.cfg.DbPath)
			if err != nil {
				return err
			}

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

			convStore := sqlite.NewConversationStore(db)
			convIDs := make(map[string]int64)

			// Active configurations (mutable via slash commands)
			activeAgentName := agentName
			activeReasoning := c.String("reasoning")
			activeModel := c.String("model")
			activeProvider := c.String("provider")

			var assembledAgent *agent.Agent
			var resolvedWorkspace string

			// Helper to initialize or re-initialize the agent
			initAgent := func() error {
				convID, ok := convIDs[activeAgentName]
				if !ok {
					var err error
					convID, err = convStore.CreateConversation(ctx, activeAgentName)
					if err != nil {
						return fmt.Errorf("create conversation: %w", err)
					}
					convIDs[activeAgentName] = convID
				}

				var err error
				assembledAgent, resolvedWorkspace, err = resolveAndAssemble(ctx, st, db, mgr, agentSessionRequest{
					AgentName:    activeAgentName,
					ProviderName: activeProvider,
					ModelName:    activeModel,
					Reasoning:    activeReasoning,
					Workspace:    c.String("workspace"),
					Channel:      "cli",
				}, convStore, convID, mcpMgr)
				if err != nil {
					return err
				}

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
				it := assembledAgent.Run(ctx, firstPrompt)
				tr := render.Text(os.Stdout)
				for {
					msg, ok := it.Next()
					if !ok {
						break
					}
					if err := tr.Render(msg); err != nil {
						fmt.Printf("Error rendering: %v\n", err)
						break
					}
				}
				if err := tr.Flush(); err != nil {
					fmt.Printf("Error flushing: %v\n", err)
				}
				if err := it.Err(); err != nil {
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

				it := assembledAgent.Run(turnCtx, input)
				tr := render.Text(os.Stdout)
				for {
					msg, ok := it.Next()
					if !ok {
						break
					}
					if err := tr.Render(msg); err != nil {
						fmt.Printf("\nError rendering: %v\n", err)
						break
					}
				}
				if err := tr.Flush(); err != nil {
					fmt.Printf("\nError flushing: %v\n", err)
				}
				err = it.Err()
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
