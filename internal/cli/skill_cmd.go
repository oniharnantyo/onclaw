package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/skill"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/sqlite"
	"github.com/urfave/cli/v3"
	"golang.org/x/term"
)

func skillCommand(st *appState) *cli.Command {
	return &cli.Command{
		Name:  "skill",
		Usage: "Manage agent skills",
		Commands: []*cli.Command{
			{
				Name:      "install",
				Usage:     "Install one or more skills from a source",
				ArgsUsage: "<source>",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "scope",
						Usage: "Target scope: 'global' or agent name",
						Value: "global",
					},
					&cli.StringFlag{
						Name:  "branch",
						Usage: "Branch for GitHub source",
					},
					&cli.StringFlag{
						Name:  "as",
						Usage: "Rename skill (only valid when installing a single skill)",
					},
					&cli.StringFlag{
						Name:  "path",
						Usage: "Relative path in source to restrict search/installation to",
					},
					&cli.BoolFlag{
						Name:  "all",
						Usage: "Install all discovered skills without asking",
					},
					&cli.BoolFlag{
						Name:  "dry-run",
						Usage: "Discover skills without installing them",
					},
					&cli.BoolFlag{
						Name:  "plugin",
						Usage: "Force classification of source as a Claude plugin",
					},
					&cli.BoolFlag{
						Name:  "force",
						Usage: "Force overwrite if skill already exists from different source",
					},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.Args().Len() < 1 {
						return fmt.Errorf("source argument is required")
					}
					source := c.Args().First()
					scope := c.String("scope")
					branch := c.String("branch")
					asName := c.String("as")
					pathRestrict := c.String("path")
					allFlag := c.Bool("all")
					dryRunFlag := c.Bool("dry-run")
					forcePlugin := c.Bool("plugin")
					forceFlag := c.Bool("force")

					inst, db, err := st.getSkillInstaller(c)
					if err != nil {
						return err
					}
					defer db.Close()

					pkgName, isPlugin, candidates, tempDir, err := inst.DiscoverSource(ctx, source, branch, forcePlugin)
					if err != nil {
						return err
					}
					defer os.RemoveAll(tempDir)

					if len(candidates) == 0 {
						return fmt.Errorf("no skills found in source")
					}

					// Filter by path if --path is set
					if pathRestrict != "" {
						var filtered []*skill.Candidate
						for _, cand := range candidates {
							if strings.Contains(cand.RelPath, pathRestrict) || strings.Contains(cand.Name, pathRestrict) {
								filtered = append(filtered, cand)
							}
						}
						candidates = filtered
						if len(candidates) == 0 {
							return fmt.Errorf("no skills match the --path restriction: %s", pathRestrict)
						}
					}

					// Dry run logic
					if dryRunFlag {
						fmt.Printf("Source is classified as: %s (isPlugin: %v)\n", pkgName, isPlugin)
						fmt.Printf("Discovered %d skill(s):\n", len(candidates))
						for _, cand := range candidates {
							fmt.Printf("- name: %s\n  description: %s\n  relPath: %s\n", cand.Name, cand.Description, cand.RelPath)
						}
						return nil
					}

					var selectedNames []string
					isTerminal := term.IsTerminal(int(os.Stdin.Fd()))

					if len(candidates) > 1 && !isTerminal && !allFlag && pathRestrict == "" {
						return fmt.Errorf("multiple skills found and command is running non-interactively; specify which skill to install, or use --all or --path to scope the installation")
					}

					if allFlag || len(candidates) == 1 || !isTerminal {
						// Auto-select all or the single candidate
						for _, cand := range candidates {
							selectedNames = append(selectedNames, cand.Name)
						}
					} else {
						// Interactive prompt
						fmt.Printf("Discovered %d skill(s) in %q:\n", len(candidates), pkgName)
						for i, cand := range candidates {
							fmt.Printf("[%d] %s: %s (path: %s)\n", i+1, cand.Name, cand.Description, cand.RelPath)
						}
						fmt.Print("Enter comma-separated numbers of skills to install (or press Enter for all): ")
						
						reader := bufio.NewReader(os.Stdin)
						line, _ := reader.ReadString('\n')
						line = strings.TrimSpace(line)

						if line == "" {
							for _, cand := range candidates {
								selectedNames = append(selectedNames, cand.Name)
							}
						} else {
							parts := strings.Split(line, ",")
							for _, p := range parts {
								p = strings.TrimSpace(p)
								idx, err := strconv.Atoi(p)
								if err != nil || idx < 1 || idx > len(candidates) {
									return fmt.Errorf("invalid selection: %s", p)
								}
								selectedNames = append(selectedNames, candidates[idx-1].Name)
							}
						}
					}

					if asName != "" && len(selectedNames) > 1 {
						return fmt.Errorf("cannot use --as when installing multiple skills")
					}

					// Perform installation
					installed, err := inst.Install(ctx, source, selectedNames, scope, skill.InstallOpts{
						Force:  forceFlag,
						AsName: asName,
						Branch: branch,
					})
					if err != nil {
						return err
					}

					_ = signalRunningProcess(st.cfg.DbPath)
					fmt.Printf("Successfully installed %d skill(s):\n", len(installed))
					for _, sk := range installed {
						fmt.Printf("- %s (path: %s)\n", sk.Name, sk.SkillPath)
					}

					return nil
				},
			},
			{
				Name:  "list",
				Usage: "List all installed skills",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "scope",
						Usage: "Filter list by scope ('global' or agent name)",
					},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					inst, db, err := st.getSkillInstaller(c)
					if err != nil {
						return err
					}
					defer db.Close()

					scope := c.String("scope")
					var skills []*store.Skill
					if scope != "" {
						ss := sqlite.NewSkillStore(db)
						skills, err = ss.ListSkillsByScope(ctx, scope)
					} else {
						skills, err = inst.List(ctx)
					}
					if err != nil {
						return err
					}

					if len(skills) == 0 {
						fmt.Println("No skills installed.")
						return nil
					}

					for _, sk := range skills {
						fmt.Printf("name: %s, scope: %s, source: %s, type: %s, path: %s, enabled: %d\n",
							sk.Name, sk.Scope, sk.Source, sk.SourceType, sk.SkillPath, sk.Enabled)
					}
					return nil
				},
			},
			{
				Name:      "show",
				Usage:     "Show full details and instructions of an installed skill",
				ArgsUsage: "<name>",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "scope",
						Value: "global",
						Usage: "Scope of the skill ('global' or agent name)",
					},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.Args().Len() < 1 {
						return fmt.Errorf("skill name is required")
					}
					name := c.Args().First()
					scope := c.String("scope")

					_, db, err := st.getSkillInstaller(c)
					if err != nil {
						return err
					}
					defer db.Close()

					ss := sqlite.NewSkillStore(db)
					sk, err := ss.GetSkill(ctx, name, scope)
					if err != nil {
						return fmt.Errorf("skill %q in scope %q not found in ledger: %w", name, scope, err)
					}

					// Read SKILL.md
					skillFilePath := filepath.Join(sk.SkillPath, "SKILL.md")
					contentBytes, err := os.ReadFile(skillFilePath)
					if err != nil {
						return fmt.Errorf("failed to read skill instructions at %s: %w", skillFilePath, err)
					}

					fmt.Printf("Name: %s\n", sk.Name)
					fmt.Printf("Scope: %s\n", sk.Scope)
					fmt.Printf("Source: %s (%s)\n", sk.Source, sk.SourceType)
					fmt.Printf("Path: %s\n", sk.SkillPath)
					fmt.Printf("Hash: %s\n", sk.Hash)
					fmt.Println("\n--- Instructions (SKILL.md) ---")
					fmt.Println(string(contentBytes))
					return nil
				},
			},
			{
				Name:      "remove",
				Usage:     "Uninstall an installed skill",
				ArgsUsage: "<name>",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "scope",
						Value: "global",
						Usage: "Scope of the skill ('global' or agent name)",
					},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.Args().Len() < 1 {
						return fmt.Errorf("skill name is required")
					}
					name := c.Args().First()
					scope := c.String("scope")

					inst, db, err := st.getSkillInstaller(c)
					if err != nil {
						return err
					}
					defer db.Close()

					err = inst.Remove(ctx, name, scope)
					if err != nil {
						return err
					}

					_ = signalRunningProcess(st.cfg.DbPath)
					fmt.Printf("Skill %q in scope %q successfully removed.\n", name, scope)
					return nil
				},
			},
			{
				Name:      "update",
				Usage:     "Update an installed skill from its original source",
				ArgsUsage: "[name]",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "all",
						Usage: "Update all installed skills",
					},
					&cli.StringFlag{
						Name:  "scope",
						Value: "global",
						Usage: "Scope of the skill ('global' or agent name)",
					},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					inst, db, err := st.getSkillInstaller(c)
					if err != nil {
						return err
					}
					defer db.Close()

					allFlag := c.Bool("all")
					scope := c.String("scope")

					if allFlag {
						skills, err := inst.List(ctx)
						if err != nil {
							return err
						}
						if len(skills) == 0 {
							fmt.Println("No skills installed to update.")
							return nil
						}

						for _, sk := range skills {
							fmt.Printf("Updating %s in scope %s...\n", sk.Name, sk.Scope)
							_, err := inst.Update(ctx, sk.Name, sk.Scope)
							if err != nil {
								fmt.Printf("Error updating %s in scope %s: %v\n", sk.Name, sk.Scope, err)
							} else {
								fmt.Printf("Skill %s in scope %s successfully updated.\n", sk.Name, sk.Scope)
							}
						}
						_ = signalRunningProcess(st.cfg.DbPath)
						return nil
					}

					if c.Args().Len() < 1 {
						return fmt.Errorf("skill name or --all is required")
					}
					name := c.Args().First()

					_, err = inst.Update(ctx, name, scope)
					if err != nil {
						return err
					}

					_ = signalRunningProcess(st.cfg.DbPath)
					fmt.Printf("Skill %q in scope %q successfully updated.\n", name, scope)
					return nil
				},
			},
		},
	}
}
