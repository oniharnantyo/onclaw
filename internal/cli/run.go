package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/oniharnantyo/onclaw/internal/agent"
	"github.com/oniharnantyo/onclaw/internal/agent/tools"
	"github.com/oniharnantyo/onclaw/internal/llm"
	"github.com/oniharnantyo/onclaw/internal/observability"
	"github.com/oniharnantyo/onclaw/internal/store/sqlite"
	"github.com/oniharnantyo/onclaw/internal/workspace"
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

			mgr, db, err := st.getProviderManager(c)
			if err != nil {
				return err
			}
			defer db.Close()

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

			agentConf, err := mgr.GetAgent(ctx, agentName)
			if err != nil {
				if agentName == "master" {
					agentConf, err = st.getOrSeedMasterAgent(ctx, db, mgr)
					if err != nil {
						return fmt.Errorf("failed to auto-seed master agent: %w", err)
					}
				} else {
					return fmt.Errorf("agent %q not found: %w", agentName, err)
				}
			}

			// 5. Resolve workspace
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("get current directory: %w", err)
			}

			resolvedWorkspace, err := workspace.ResolveWorkspace(
				c.String("workspace"),
				agentConf.Workspace,
				st.cfg.Workspace,
				cwd,
			)
			if err != nil {
				return fmt.Errorf("resolve workspace: %w", err)
			}

			// 6. Build effective profile
			var defaultProvider string
			err = db.QueryRowContext(ctx, "SELECT value FROM preferences WHERE key = 'default_provider'").Scan(&defaultProvider)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}

			providerName := c.String("provider")
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

				if defaultProvider != "" {
					providerName = defaultProvider
				} else {
					providerName = agentConf.Provider
				}
			}

			if providerName == "" {
				return fmt.Errorf("no provider specified for agent %q; configure a provider or use the --provider flag", agentName)
			}

			p, err := mgr.GetProfile(ctx, providerName)
			if err != nil {
				return fmt.Errorf("provider %q not found: %w", providerName, err)
			}
			if p.Enabled == 0 {
				return fmt.Errorf("provider %q is disabled", providerName)
			}

			effModel := c.String("model")
			if effModel == "" {
				effModel = agentConf.Model
			}
			if effModel == "" {
				effModel = p.Model
			}

			effReasoning := c.String("reasoning")
			if effReasoning == "" {
				effReasoning = agentConf.ReasoningEffort
			}

			effProfile := *p
			effProfile.Model = effModel

			if effReasoning != "" {
				var settings map[string]interface{}
				if effProfile.Settings != "" {
					_ = json.Unmarshal([]byte(effProfile.Settings), &settings)
				}
				if settings == nil {
					settings = make(map[string]interface{})
				}
				settings["reasoning_effort"] = effReasoning
				settingsJSON, _ := json.Marshal(settings)
				effProfile.Settings = string(settingsJSON)
			}

			// 7. Build ChatModel and assemble agent
			chatModel, err := mgr.BuildWithProfile(ctx, &effProfile)
			if err != nil {
				return fmt.Errorf("failed to build model: %w", err)
			}

			var settings map[string]interface{}
			if effProfile.Settings != "" {
				_ = json.Unmarshal([]byte(effProfile.Settings), &settings)
			}
			var contextWindow int
			if settings != nil {
				if cwVal, ok := settings["context_window"]; ok {
					if cwFloat, ok := cwVal.(float64); ok {
						contextWindow = int(cwFloat)
					}
				}
			}
			if contextWindow <= 0 {
				if st.cfg.MaxContextTokens > 0 {
					contextWindow = st.cfg.MaxContextTokens
				} else {
					contextWindow = 64000
				}
			}

			userConfigDir := filepath.Dir(resolvedDbPath)
			assembledAgent, err := agent.AssembleAgent(
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

			st.log.Info("run invoked",
				"agent", agentName,
				"provider", providerName,
				"model", effProfile.Model,
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

			transcriptPath := filepath.Join(userConfigDir, "conversations", fmt.Sprintf("%s_transcript.jsonl", agentConf.Name))
			if err := agent.RunAgent(ctx, assembledAgent, prompt, os.Stdout, transcriptPath); err != nil {
				return fmt.Errorf("agent run execution failed: %w", err)
			}

			return nil
		},
	}
}
