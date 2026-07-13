package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/oniharnantyo/onclaw/internal/hooks"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/sqlite"
	"github.com/urfave/cli/v3"
)

func hooksCommand(st *appState) *cli.Command {
	return &cli.Command{
		Name:  "hooks",
		Usage: "Manage agent lifecycle hooks",
		Commands: []*cli.Command{
			{
				Name:  "add",
				Usage: "Add a new lifecycle hook",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "name",
						Usage:    "Unique name of the hook",
						Required: true,
					},
					&cli.StringFlag{
						Name:     "handler",
						Usage:    "Handler type ('command' or 'script')",
						Required: true,
					},
					&cli.StringFlag{
						Name:     "event",
						Usage:    "Lifecycle event ('session_start', 'user_prompt_submit', 'pre_tool_use', 'post_tool_use', 'stop')",
						Required: true,
					},
					&cli.StringFlag{
						Name:  "scope",
						Usage: "Scope of the hook ('global' or agent name)",
						Value: "global",
					},
					&cli.StringFlag{
						Name:  "matcher",
						Usage: "Regex pattern to match tool_name (pre/post tool events only)",
					},
					&cli.IntFlag{
						Name:  "timeout",
						Usage: "Timeout in milliseconds (default 5000)",
						Value: 5000,
					},
					&cli.StringFlag{
						Name:  "on-timeout",
						Usage: "Policy on timeout ('block' or 'allow', default 'block')",
						Value: "block",
					},
					&cli.IntFlag{
						Name:  "priority",
						Usage: "Priority of hook execution, higher runs first",
						Value: 0,
					},
					&cli.StringFlag{
						Name:  "command",
						Usage: "Command to execute (required for command handler)",
					},
					&cli.StringFlag{
						Name:  "cwd",
						Usage: "Working directory (command handler only)",
					},
					&cli.StringFlag{
						Name:  "env",
						Usage: "Comma-separated list of allowed environment variables (command handler only)",
					},
					&cli.StringFlag{
						Name:  "script",
						Usage: "Inline JavaScript source code (required for script handler)",
					},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					_, _, db, err := st.getProviderManager(c)
					if err != nil {
						return err
					}
					defer db.Close()

					name := c.String("name")
					handlerType := c.String("handler")
					event := c.String("event")
					scope := c.String("scope")
					matcher := c.String("matcher")
					timeout := int(c.Int("timeout"))
					onTimeout := c.String("on-timeout")
					priority := int(c.Int("priority"))

					if matcher != "" {
						if _, err := regexp.Compile(matcher); err != nil {
							return fmt.Errorf("invalid regex matcher %q: %w", matcher, err)
						}
					}

					if handlerType != "command" && handlerType != "script" {
						return fmt.Errorf("handler must be 'command' or 'script'")
					}

					var configStr string
					if handlerType == "command" {
						cmdStr := c.String("command")
						if cmdStr == "" {
							return fmt.Errorf("--command is required for command handler")
						}
						var envVars []string
						if c.String("env") != "" {
							for _, v := range strings.Split(c.String("env"), ",") {
								envVars = append(envVars, strings.TrimSpace(v))
							}
						}
						cfg := hooks.CommandConfig{
							Command:        cmdStr,
							Cwd:            c.String("cwd"),
							AllowedEnvVars: envVars,
						}
						b, _ := json.Marshal(cfg)
						configStr = string(b)
					} else {
						scriptStr := c.String("script")
						if scriptStr == "" {
							return fmt.Errorf("--script is required for script handler")
						}
						cfg := hooks.ScriptConfig{
							Script: scriptStr,
						}
						b, _ := json.Marshal(cfg)
						configStr = string(b)
					}

					hookStore := sqlite.NewHookStore(db)
					id := fmt.Sprintf("hook-%d", time.Now().UnixNano())
					hook := &store.Hook{
						ID:          id,
						Name:        name,
						Scope:       scope,
						Event:       event,
						HandlerType: handlerType,
						Config:      configStr,
						Matcher:     matcher,
						TimeoutMS:   timeout,
						OnTimeout:   onTimeout,
						Priority:    priority,
						Enabled:     1,
					}

					if err := hookStore.AddHook(ctx, hook); err != nil {
						return fmt.Errorf("add hook: %w", err)
					}

					fmt.Printf("Hook %s added successfully with ID: %s\n", name, id)
					_ = signalRunningProcess(st.cfg.DbPath)
					return nil
				},
			},
			{
				Name:  "list",
				Usage: "List all configured hooks",
				Action: func(ctx context.Context, c *cli.Command) error {
					_, _, db, err := st.getProviderManager(c)
					if err != nil {
						return err
					}
					defer db.Close()

					hookStore := sqlite.NewHookStore(db)
					list, err := hookStore.ListHooks(ctx)
					if err != nil {
						return fmt.Errorf("list hooks: %w", err)
					}

					if len(list) == 0 {
						fmt.Println("No hooks configured.")
						return nil
					}

					fmt.Printf("%-25s %-15s %-10s %-20s %-10s %-8s %-8s\n", "ID", "Name", "Scope", "Event", "Handler", "Enabled", "Priority")
					fmt.Println(strings.Repeat("-", 100))
					for _, h := range list {
						enabledStr := "yes"
						if h.Enabled == 0 {
							enabledStr = "no"
						}
						fmt.Printf("%-25s %-15s %-10s %-20s %-10s %-8s %-8d\n", h.ID, h.Name, h.Scope, h.Event, h.HandlerType, enabledStr, h.Priority)
					}
					return nil
				},
			},
			{
				Name:      "show",
				Usage:     "Show detailed configuration of a hook",
				ArgsUsage: "<id>",
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.Args().Len() < 1 {
						return fmt.Errorf("hook ID argument is required")
					}
					id := c.Args().First()

					_, _, db, err := st.getProviderManager(c)
					if err != nil {
						return err
					}
					defer db.Close()

					hookStore := sqlite.NewHookStore(db)
					h, err := hookStore.GetHook(ctx, id)
					if err != nil {
						return fmt.Errorf("get hook: %w", err)
					}

					fmt.Printf("ID:          %s\n", h.ID)
					fmt.Printf("Name:        %s\n", h.Name)
					fmt.Printf("Scope:       %s\n", h.Scope)
					fmt.Printf("Event:       %s\n", h.Event)
					fmt.Printf("Handler:     %s\n", h.HandlerType)
					fmt.Printf("Matcher:     %s\n", h.Matcher)
					fmt.Printf("Timeout:     %d ms\n", h.TimeoutMS)
					fmt.Printf("On Timeout:  %s\n", h.OnTimeout)
					fmt.Printf("Priority:    %d\n", h.Priority)
					enabledStr := "yes"
					if h.Enabled == 0 {
						enabledStr = "no"
					}
					fmt.Printf("Enabled:     %s\n", enabledStr)
					fmt.Printf("Config:      %s\n", h.Config)
					fmt.Printf("Created At:  %s\n", h.CreatedAt)
					fmt.Printf("Updated At:  %s\n", h.UpdatedAt)
					return nil
				},
			},
			{
				Name:      "remove",
				Usage:     "Remove a hook",
				ArgsUsage: "<id>",
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.Args().Len() < 1 {
						return fmt.Errorf("hook ID argument is required")
					}
					id := c.Args().First()

					_, _, db, err := st.getProviderManager(c)
					if err != nil {
						return err
					}
					defer db.Close()

					hookStore := sqlite.NewHookStore(db)
					if err := hookStore.RemoveHook(ctx, id); err != nil {
						return fmt.Errorf("remove hook: %w", err)
					}

					fmt.Printf("Hook %s removed successfully.\n", id)
					_ = signalRunningProcess(st.cfg.DbPath)
					return nil
				},
			},
			{
				Name:      "toggle",
				Usage:     "Toggle hook enabled state",
				ArgsUsage: "<id>",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "enable",
						Usage: "Enable the hook",
					},
					&cli.BoolFlag{
						Name:  "disable",
						Usage: "Disable the hook",
					},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.Args().Len() < 1 {
						return fmt.Errorf("hook ID argument is required")
					}
					id := c.Args().First()

					enableFlag := c.Bool("enable")
					disableFlag := c.Bool("disable")

					if enableFlag && disableFlag {
						return fmt.Errorf("cannot specify both --enable and --disable")
					}
					if !enableFlag && !disableFlag {
						return fmt.Errorf("must specify either --enable or --disable")
					}

					_, _, db, err := st.getProviderManager(c)
					if err != nil {
						return err
					}
					defer db.Close()

					hookStore := sqlite.NewHookStore(db)
					enabled := enableFlag

					if err := hookStore.ToggleHook(ctx, id, enabled); err != nil {
						return fmt.Errorf("toggle hook: %w", err)
					}

					stateStr := "disabled"
					if enabled {
						stateStr = "enabled"
					}
					fmt.Printf("Hook %s %s successfully.\n", id, stateStr)
					_ = signalRunningProcess(st.cfg.DbPath)
					return nil
				},
			},
			{
				Name:  "test",
				Usage: "Dry-run test a hook configuration against a sample payload",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "handler",
						Usage:    "Handler type ('command' or 'script')",
						Required: true,
					},
					&cli.StringFlag{
						Name:     "config",
						Usage:    "JSON configuration string for the handler",
						Required: true,
					},
					&cli.StringFlag{
						Name:     "event",
						Usage:    "Event type to simulate",
						Required: true,
					},
					&cli.StringFlag{
						Name:  "matcher",
						Usage: "Regex pattern for tool name matching (optional)",
					},
					&cli.IntFlag{
						Name:  "timeout",
						Usage: "Timeout in milliseconds (default 5000)",
						Value: 5000,
					},
					&cli.StringFlag{
						Name:  "on-timeout",
						Usage: "Policy on timeout ('block' or 'allow', default 'block')",
						Value: "block",
					},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					handlerType := c.String("handler")
					configStr := c.String("config")
					event := c.String("event")
					matcher := c.String("matcher")
					timeout := int(c.Int("timeout"))
					onTimeout := c.String("on-timeout")

					if matcher != "" {
						if _, err := regexp.Compile(matcher); err != nil {
							return fmt.Errorf("invalid regex matcher %q: %w", matcher, err)
						}
					}

					h := &store.Hook{
						ID:          "test-dry-run",
						Name:        "test-dry-run",
						Scope:       "global",
						Event:       event,
						HandlerType: handlerType,
						Config:      configStr,
						Matcher:     matcher,
						TimeoutMS:   timeout,
						OnTimeout:   onTimeout,
						Priority:    0,
						Enabled:     1,
					}

					// Build mock payload
					payload := hooks.Payload{
						Event:     hooks.Event(event),
						Agent:     "test-agent",
						Channel:   "cli",
						SessionID: "test-session-id",
						Prompt:    "List files in the active workspace directory",
						ToolName:  "ls",
						ToolArgs:  map[string]interface{}{"path": "."},
					}

					hs := &mockHookStore{hooks: map[string]*store.Hook{h.ID: h}}
					es := &mockExecStore{}
					dispatcher := hooks.NewDispatcher(hs, es)

					fmt.Printf("Dry-running hook '%s' for event '%s'...\n", handlerType, event)
					dec, err := dispatcher.TestHook(ctx, h, payload)
					if err != nil {
						fmt.Printf("Hook errored: %v\n", err)
					}
					fmt.Printf("Resulting Decision: %s\n", dec)

					return nil
				},
			},
		},
	}
}

type mockHookStore struct {
	hooks map[string]*store.Hook
}

func (m *mockHookStore) AddHook(ctx context.Context, h *store.Hook) error { return nil }
func (m *mockHookStore) GetHook(ctx context.Context, id string) (*store.Hook, error) {
	return m.hooks[id], nil
}
func (m *mockHookStore) ListHooks(ctx context.Context) ([]*store.Hook, error) { return nil, nil }
func (m *mockHookStore) ListHooksByScopeAndEvent(ctx context.Context, scope string, event string) ([]*store.Hook, error) {
	return nil, nil
}
func (m *mockHookStore) UpdateHook(ctx context.Context, h *store.Hook) error           { return nil }
func (m *mockHookStore) RemoveHook(ctx context.Context, id string) error               { return nil }
func (m *mockHookStore) ToggleHook(ctx context.Context, id string, enabled bool) error { return nil }

type mockExecStore struct{}

func (m *mockExecStore) AppendExecution(ctx context.Context, exec *store.HookExecution) error {
	return nil
}
func (m *mockExecStore) ListExecutions(ctx context.Context) ([]*store.HookExecution, error) {
	return nil, nil
}
