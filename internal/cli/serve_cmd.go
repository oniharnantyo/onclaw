package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/oniharnantyo/onclaw/internal/api"
	"github.com/oniharnantyo/onclaw/internal/api/service"
	"github.com/oniharnantyo/onclaw/internal/llm"
	"github.com/oniharnantyo/onclaw/internal/mcp"
	"github.com/oniharnantyo/onclaw/internal/skill"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/sqlite"
	"github.com/urfave/cli/v3"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

func serveCommand(st *appState) *cli.Command {
	return &cli.Command{
		Name:  "serve",
		Usage: "Start the web management console and JSON API server",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "bind",
				Aliases: []string{"b"},
				Usage:   "Bind address for the web server",
			},
			&cli.IntFlag{
				Name:    "port",
				Aliases: []string{"p"},
				Usage:   "Port for the web server",
			},
			&cli.BoolFlag{
				Name:  "set-password",
				Usage: "Set or update the web console passphrase interactively",
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

			kv := sqlite.NewKVStore(db)
			convStore := sqlite.NewConversationStore(db)

			if c.Bool("set-password") {
				fd := int(os.Stdin.Fd())
				var bytePassword []byte
				var err error

				if term.IsTerminal(fd) {
					fmt.Print("Enter new web console password: ")
					bytePassword, err = term.ReadPassword(fd)
					if err != nil {
						return fmt.Errorf("read password: %w", err)
					}
					fmt.Println()

					fmt.Print("Confirm web console password: ")
					byteConfirm, err := term.ReadPassword(fd)
					if err != nil {
						return fmt.Errorf("read confirmation: %w", err)
					}
					fmt.Println()

					if string(bytePassword) != string(byteConfirm) {
						return fmt.Errorf("passwords do not match")
					}
				} else {
					var line1, line2 string
					fmt.Print("Enter new web console password: ")
					if _, err := fmt.Fscanln(os.Stdin, &line1); err != nil {
						return fmt.Errorf("read password from stdin: %w", err)
					}
					fmt.Print("Confirm web console password: ")
					if _, err := fmt.Fscanln(os.Stdin, &line2); err != nil {
						return fmt.Errorf("read confirmation from stdin: %w", err)
					}
					if line1 != line2 {
						return fmt.Errorf("passwords do not match")
					}
					bytePassword = []byte(line1)
				}

				hash, err := bcrypt.GenerateFromPassword(bytePassword, bcrypt.DefaultCost)
				if err != nil {
					return fmt.Errorf("hash password: %w", err)
				}

				if err := kv.Set(ctx, "web_password_hash", string(hash)); err != nil {
					return fmt.Errorf("save password hash: %w", err)
				}

				fmt.Println("Web console password updated successfully.")
				return nil
			}

			// Refuse to start if no password hash is set
			hash, err := kv.Get(ctx, "web_password_hash")
			if err != nil || hash == "" {
				return fmt.Errorf("web password is not set; run 'onclaw serve --set-password' to set it first")
			}

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

			// 4. Determine bind address and port
			bind := c.String("bind")
			if bind == "" {
				bind = st.cfg.Web.Bind
			}
			port := int(c.Int("port"))
			if port == 0 {
				port = st.cfg.Web.Port
			}
			addr := fmt.Sprintf("%s:%d", bind, port)

			resolveFn := func(ctx context.Context, agentName, providerName, modelName, reasoning, workspacePath string, convID int64) (service.AssembledAgent, string, error) {
				return resolveAndAssemble(ctx, st, db, mgr, agentSessionRequest{
					AgentName:    agentName,
					ProviderName: providerName,
					ModelName:    modelName,
					Reasoning:    reasoning,
					Workspace:    workspacePath,
					Channel:      "web",
				}, convStore, convID, mcpMgr)
			}

			ss := sqlite.NewSkillStore(db)
			resolvedPath, _ := sqlite.ResolveDbPath(st.cfg.DbPath)
			home := filepath.Dir(resolvedPath)
			installer := skill.NewInstaller(ss, home)

			hookStore := sqlite.NewHookStore(db)
			execStore := sqlite.NewHookExecutionStore(db)

			testMCPFn := func(ctx context.Context, srv *store.MCPServer) ([]string, error) {
				cliCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
				cli, err := mcp.NewClient(cliCtx, srv)
				cancel()
				if err != nil {
					return nil, err
				}
				defer cli.Close()

				toolsCtx, toolsCancel := context.WithTimeout(ctx, 5*time.Second)
				listResult, err := cli.ListTools(toolsCtx, mcpgo.ListToolsRequest{})
				toolsCancel()
				if err != nil {
					return nil, err
				}

				var toolNames []string
				for _, t := range listResult.Tools {
					toolNames = append(toolNames, t.Name)
				}
				return toolNames, nil
			}

			toolRegistryStore := sqlite.NewToolRegistryStore(db)
			toolGroupConfigStore := sqlite.NewToolGroupConfigStore(db)

			svc := service.New(mgr, kv, convStore, resolveFn, installer, st.log, hookStore, execStore, mcpStore, mcpMgr.Reload, testMCPFn, toolRegistryStore, toolGroupConfigStore)
			server := api.NewServer(svc, st.log)

			st.log.Info("Starting web management console", "addr", addr)
			fmt.Printf("Web console listening on http://%s\n", addr)
			if err := server.ListenAndServe(addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return fmt.Errorf("web server error: %w", err)
			}

			return nil
		},
	}
}
