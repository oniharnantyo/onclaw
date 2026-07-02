package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/oniharnantyo/onclaw/internal/agent/tools"
	"github.com/oniharnantyo/onclaw/internal/llm"
	"github.com/oniharnantyo/onclaw/internal/mcp"
	"github.com/oniharnantyo/onclaw/internal/observability"
	"github.com/oniharnantyo/onclaw/internal/render"
	"github.com/oniharnantyo/onclaw/internal/store/sqlite"
	"github.com/urfave/cli/v3"
)

func runCommand(st *appState) *cli.Command {
	return &cli.Command{
		Name:      "run",
		Usage:     "Run a prompt through the onclaw agent",
		ArgsUsage: "[prompt]",
		Description: "Executes a single ReAct turn with the selected agent. " +
			"Streams output incrementally and appends to transcripts.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "agent",
				Usage: "Agent name to run (defaults to default_agent, fallback to master)",
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

			prompt := c.Args().First()
			if prompt == "" {
				return fmt.Errorf("prompt argument is required for 'run' command")
			}

			mgr, mcpStore, db, err := st.getProviderManager(c)
			if err != nil {
				return err
			}
			defer db.Close()

			mcpMgr := mcp.NewManager(mcpStore)
			defer mcpMgr.Close()

			// 1. Write PID file
			pidPath, err := writePIDFile(st.cfg.DbPath)
			if err != nil {
				return fmt.Errorf("write pidfile: %w", err)
			}
			defer os.Remove(pidPath)

			// 2. Setup SIGHUP handler
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

			// 3. Setup fsnotify watcher
			resolvedDbPath, err := sqlite.ResolveDbPath(st.cfg.DbPath)
			if err != nil {
				return err
			}
			watcher, err := llm.StartDBWatcher(ctx, resolvedDbPath, mgr)
			if err != nil {
				return fmt.Errorf("start db watcher: %w", err)
			}
			defer watcher.Close()

			// 4. Resolve agent configuration
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
			convID, err := convStore.CreateConversation(ctx, agentName)
			if err != nil {
				return fmt.Errorf("create conversation: %w", err)
			}

			assembledAgent, resolvedWorkspace, err := resolveAndAssemble(ctx, st, db, mgr, agentSessionRequest{
				AgentName:    agentName,
				ProviderName: c.String("provider"),
				ModelName:    c.String("model"),
				Reasoning:    c.String("reasoning"),
				Workspace:    c.String("workspace"),
				Channel:      "cli",
			}, convStore, convID, mcpMgr)
			if err != nil {
				return err
			}

			st.log.Info("run invoked",
				"agent", agentName,
				"provider", assembledAgent.Config.Provider,
				"model", assembledAgent.Config.Model,
				"workspace", resolvedWorkspace,
			)

			// 8. Run the agent turn
			obsCfg := observability.Config{
				Host:      st.cfg.Langfuse.Host,
				PublicKey: st.cfg.Langfuse.PublicKey,
				SecretKey: st.cfg.Langfuse.SecretKey,
				SessionID: st.cfg.Langfuse.SessionID,
				Release:   st.cfg.Langfuse.Release,
				Mask:      st.cfg.Langfuse.Mask,
			}
			flush, err := observability.Setup(ctx, obsCfg, tools.Redact)
			if err != nil {
				return fmt.Errorf("observability setup: %w", err)
			}
			if flush != nil {
				st.log.Info("langfuse_flush_deferred")
				defer func() {
					st.log.Info("langfuse_flush_starting")
					flush()
					st.log.Info("langfuse_flush_completed")
				}()
			}

			it := assembledAgent.Run(ctx, prompt)
			tr := render.Text(os.Stdout)
			for {
				msg, ok := it.Next()
				if !ok {
					break
				}
				if err := tr.Render(msg); err != nil {
					return fmt.Errorf("render message failed: %w", err)
				}
			}
			if err := tr.Flush(); err != nil {
				return fmt.Errorf("flush renderer failed: %w", err)
			}
			if err := it.Err(); err != nil {
				return fmt.Errorf("agent run execution failed: %w", err)
			}

			return nil
		},
	}
}
