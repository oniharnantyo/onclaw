package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/oniharnantyo/onclaw/internal/memory"
	"github.com/oniharnantyo/onclaw/internal/store/sqlite"
	"github.com/oniharnantyo/onclaw/internal/workspace"
	"github.com/urfave/cli/v3"
)

func memoryCommand(st *appState) *cli.Command {
	return &cli.Command{
		Name:  "memory",
		Usage: "Manage agent memory (staged writes, approval workflow)",
		Commands: []*cli.Command{
			memoryPendingCommand(st),
			memoryApproveCommand(st),
			memoryRejectCommand(st),
		},
	}
}

func memoryPendingCommand(st *appState) *cli.Command {
	return &cli.Command{
		Name:  "pending",
		Usage: "List all pending staged memory writes awaiting approval",
		Action: func(ctx context.Context, c *cli.Command) error {
			if err := st.ensure(c); err != nil {
				return err
			}

			_, _, db, err := st.getProviderManager(c)
			if err != nil {
				return err
			}
			defer db.Close()

			rows, err := db.QueryContext(ctx, `
				SELECT id, agent, operation, target, content, created_at
				FROM staged_memory_writes
				WHERE status = 'pending'
				ORDER BY created_at ASC`)
			if err != nil {
				return fmt.Errorf("list pending writes: %w", err)
			}
			defer rows.Close()

			type pendingWrite struct {
				ID        int64
				Agent     string
				Operation string
				Target    string
				Content   string
				CreatedAt string
			}
			var writes []pendingWrite
			for rows.Next() {
				var w pendingWrite
				if err := rows.Scan(&w.ID, &w.Agent, &w.Operation, &w.Target, &w.Content, &w.CreatedAt); err != nil {
					return fmt.Errorf("scan pending write: %w", err)
				}
				writes = append(writes, w)
			}
			if err := rows.Err(); err != nil {
				return fmt.Errorf("iterate pending writes: %w", err)
			}

			if len(writes) == 0 {
				fmt.Println("No pending memory writes.")
				return nil
			}

			fmt.Printf("Found %d pending memory write(s):\n\n", len(writes))
			for _, w := range writes {
				fmt.Printf("  ID:        %d\n", w.ID)
				fmt.Printf("  Agent:     %s\n", w.Agent)
				fmt.Printf("  Operation: %s\n", w.Operation)
				if w.Target != "" {
					fmt.Printf("  Target:    %q\n", w.Target)
				}
				fmt.Printf("  Content:   %q\n", w.Content)
				fmt.Printf("  Staged:    %s\n", w.CreatedAt)
				fmt.Println()
			}
			return nil
		},
	}
}

func memoryApproveCommand(st *appState) *cli.Command {
	return &cli.Command{
		Name:      "approve",
		Usage:     "Approve and apply a staged memory write to MEMORY.md",
		ArgsUsage: "<id>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "workspace",
				Usage: "Override workspace directory path",
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			if err := st.ensure(c); err != nil {
				return err
			}
			if c.Args().Len() < 1 {
				return fmt.Errorf("staged write ID is required")
			}

			var writeID int64
			if _, err := fmt.Sscanf(c.Args().First(), "%d", &writeID); err != nil {
				return fmt.Errorf("invalid staged write ID %q: %w", c.Args().First(), err)
			}

			mgr, _, db, err := st.getProviderManager(c)
			if err != nil {
				return err
			}
			defer db.Close()

			stagedStore := sqlite.NewStagedWriteStore(db)

			sw, err := stagedStore.GetStagedWrite(ctx, writeID)
			if err != nil {
				return fmt.Errorf("read staged write: %w", err)
			}
			if sw.Status != "pending" {
				return fmt.Errorf("staged write %d is not pending (status: %s)", writeID, sw.Status)
			}

			workspaceFlag := c.String("workspace")
			var resolvedWorkspace string
			if workspaceFlag != "" {
				resolvedWorkspace = workspaceFlag
			} else {
				agentConf, err := mgr.GetAgent(ctx, sw.Agent)
				if err == nil && agentConf != nil {
					cwd, _ := os.Getwd()
					resolvedWorkspace, err = workspace.ResolveWorkspace("", agentConf.Workspace, st.cfg.Workspace, cwd)
					if err != nil {
						return fmt.Errorf("resolve workspace: %w", err)
					}
				} else {
					cwd, err := os.Getwd()
					if err != nil {
						return fmt.Errorf("get current directory: %w", err)
					}
					resolvedWorkspace = cwd
				}
			}

			charLimit := st.cfg.Memory.CharLimit
			if charLimit <= 0 {
				charLimit = 3200
			}
			coreStore := memory.NewFileCoreStore(charLimit)

			newContent, err := coreStore.WriteCore(ctx, resolvedWorkspace, sw.Operation, sw.Target, sw.Content)
			if err != nil {
				return fmt.Errorf("apply to MEMORY.md: %w", err)
			}

			if err := stagedStore.ApproveWrite(ctx, writeID); err != nil {
				return fmt.Errorf("approve write %d: %w", writeID, err)
			}

			fmt.Printf("Memory write %d approved and applied to %s/MEMORY.md.\n", writeID, resolvedWorkspace)
			fmt.Printf("New memory state:\n%s\n", newContent)
			return nil
		},
	}
}

func memoryRejectCommand(st *appState) *cli.Command {
	return &cli.Command{
		Name:      "reject",
		Usage:     "Reject a staged memory write without applying it",
		ArgsUsage: "<id>",
		Action: func(ctx context.Context, c *cli.Command) error {
			if err := st.ensure(c); err != nil {
				return err
			}
			if c.Args().Len() < 1 {
				return fmt.Errorf("staged write ID is required")
			}

			var writeID int64
			if _, err := fmt.Sscanf(c.Args().First(), "%d", &writeID); err != nil {
				return fmt.Errorf("invalid staged write ID %q: %w", c.Args().First(), err)
			}

			_, _, db, err := st.getProviderManager(c)
			if err != nil {
				return err
			}
			defer db.Close()

			stagedStore := sqlite.NewStagedWriteStore(db)

			if _, err := stagedStore.GetStagedWrite(ctx, writeID); err != nil {
				return fmt.Errorf("read staged write: %w", err)
			}

			if err := stagedStore.RejectWrite(ctx, writeID); err != nil {
				return fmt.Errorf("reject write %d: %w", writeID, err)
			}

			fmt.Printf("Memory write %d rejected.\n", writeID)
			return nil
		},
	}
}
