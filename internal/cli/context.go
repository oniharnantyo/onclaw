package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"

	"github.com/oniharnantyo/onclaw/internal/config"
	"github.com/oniharnantyo/onclaw/internal/llm"
	"github.com/oniharnantyo/onclaw/internal/llm/adapter"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/skill"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/sqlite"
	"github.com/urfave/cli/v3"
)

type appState struct {
	cfg *config.Config
	log *slog.Logger
}

func decryptDEK(ctx context.Context, db *sql.DB, resolvedPath string) ([]byte, error) {
	var wrappedDek string
	err := db.QueryRowContext(ctx, "SELECT value FROM preferences WHERE key = 'wrapped_dek'").Scan(&wrappedDek)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve wrapped DEK: %w", err)
	}

	keyfilePath := secrets.ResolveKeyfilePath(resolvedPath)
	kek, err := secrets.GetOrCreateKeyfileKEK(keyfilePath)
	if err != nil {
		return nil, fmt.Errorf("get or create keyfile KEK: %w", err)
	}

	dek, err := secrets.UnwrapDEK(wrappedDek, kek)
	if err != nil {
		return nil, fmt.Errorf("failed to unwrap DEK: %w", err)
	}
	return dek, nil
}

func (s *appState) getProviderManager(c *cli.Command) (*llm.Service, store.MCPServerStore, *sql.DB, error) {
	if err := s.ensure(c); err != nil {
		return nil, nil, nil, err
	}

	resolvedPath, err := sqlite.ResolveDbPath(s.cfg.DbPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("resolve db path: %w", err)
	}

	db, err := sqlite.Open(resolvedPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open db: %w", err)
	}

	if err := sqlite.Migrate(db); err != nil {
		db.Close()
		return nil, nil, nil, fmt.Errorf("migrate db: %w", err)
	}

	ctx := context.Background()

	// Check if wrapped_dek exists in preferences
	var wrappedDek string
	err = db.QueryRowContext(ctx, "SELECT value FROM preferences WHERE key = 'wrapped_dek'").Scan(&wrappedDek)
	if errors.Is(err, sql.ErrNoRows) {
		// Initialize keyfile mode
		dek, err := secrets.GenerateDEK()
		if err != nil {
			db.Close()
			return nil, nil, nil, fmt.Errorf("generate DEK: %w", err)
		}

		keyfilePath := secrets.ResolveKeyfilePath(resolvedPath)
		km := secrets.NewKeyManager(dek)
		wrapped, err := km.SwitchToKeyfile(keyfilePath)
		if err != nil {
			db.Close()
			return nil, nil, nil, err
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			db.Close()
			return nil, nil, nil, err
		}
		defer tx.Rollback()

		_, err = tx.ExecContext(ctx, "INSERT OR REPLACE INTO preferences (key, value) VALUES ('wrapped_dek', ?)", wrapped)
		if err != nil {
			db.Close()
			return nil, nil, nil, err
		}

		if err != nil {
			db.Close()
			return nil, nil, nil, err
		}

		if err := tx.Commit(); err != nil {
			db.Close()
			return nil, nil, nil, err
		}

		ps := sqlite.NewProfileStore(db)
		ss := sqlite.NewSecretStore(db)
		as := sqlite.NewAgentStore(db)
		_ = sqlite.NewConversationStore(db)
		ar := adapter.NewRegistry()
		adapter.DefaultAdapters(ar)

		mgr := llm.NewService(ps, ss, km, ar, as)
		return mgr, sqlite.NewMCPServerStore(db), db, nil

	} else if err != nil {
		db.Close()
		return nil, nil, nil, fmt.Errorf("query wrapped_dek preference: %w", err)
	}

	// wrapped_dek exists
	dek, err := decryptDEK(ctx, db, resolvedPath)
	if err != nil {
		db.Close()
		return nil, nil, nil, err
	}

	ps := sqlite.NewProfileStore(db)
	ss := sqlite.NewSecretStore(db)
	as := sqlite.NewAgentStore(db)
	_ = sqlite.NewConversationStore(db)
	km := secrets.NewKeyManager(dek)
	ar := adapter.NewRegistry()
	adapter.DefaultAdapters(ar)

	mgr := llm.NewService(ps, ss, km, ar, as)
	return mgr, sqlite.NewMCPServerStore(db), db, nil
}

func writePIDFile(dbPath string) (string, error) {
	resolvedPath, err := sqlite.ResolveDbPath(dbPath)
	if err != nil {
		return "", err
	}
	pidPath := filepath.Join(filepath.Dir(resolvedPath), "onclaw.pid")
	pid := os.Getpid()
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", pid)), 0644); err != nil {
		return "", err
	}
	return pidPath, nil
}

func signalRunningProcess(dbPath string) error {
	resolvedPath, err := sqlite.ResolveDbPath(dbPath)
	if err != nil {
		return err
	}
	pidPath := filepath.Join(filepath.Dir(resolvedPath), "onclaw.pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var pid int
	if _, err := fmt.Sscanf(string(data), "%d", &pid); err != nil {
		return nil
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}

	err = process.Signal(syscall.Signal(0))
	if err != nil {
		_ = os.Remove(pidPath)
		return nil
	}

	_ = process.Signal(syscall.SIGHUP)
	return nil
}

func (s *appState) getOrSeedMasterAgent(ctx context.Context, db *sql.DB, mgr *llm.Service) (*store.Agent, error) {
	as := sqlite.NewAgentStore(db)
	agent, err := as.GetAgent(ctx, "master")
	if err == nil {
		return agent, nil
	}

	if errors.Is(err, sql.ErrNoRows) {
		// Master agent doesn't exist, let's seed it!
		var providerName string
		err := db.QueryRowContext(ctx, "SELECT value FROM preferences WHERE key = 'default_provider'").Scan(&providerName)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}

		if providerName == "" {
			profiles, err := mgr.ListProfiles(ctx)
			if err == nil && len(profiles) > 0 {
				providerName = profiles[0].Name
			}
		}

		if providerName == "" {
			return nil, fmt.Errorf("no provider profiles found; add one using 'onclaw provider add'")
		}

		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		defaultWS := filepath.Join(home, ".onclaw", "workspace", "master")
		if err := os.MkdirAll(defaultWS, 0755); err != nil {
			return nil, err
		}

		var defaultModel string
		p, err := mgr.GetProfile(ctx, providerName)
		if err == nil {
			switch p.ProviderType {
			case "anthropic":
				defaultModel = "claude-3-opus"
			case "openai":
				defaultModel = "gpt-4o"
			case "ollama":
				defaultModel = "llama3"
			default:
				defaultModel = "gpt-4o"
			}
		}

		agent = &store.Agent{
			Name:      "master",
			Provider:  providerName,
			Model:     defaultModel,
			Workspace: defaultWS,
		}
		if err := as.AddAgent(ctx, agent); err != nil {
			return nil, err
		}

		// Set default agent to master if not set
		var defAgent string
		err = db.QueryRowContext(ctx, "SELECT value FROM preferences WHERE key = 'default_agent'").Scan(&defAgent)
		if errors.Is(err, sql.ErrNoRows) || defAgent == "" {
			_, _ = db.ExecContext(ctx, "INSERT OR REPLACE INTO preferences (key, value) VALUES ('default_agent', 'master')")
		}

		return agent, nil
	}

	return nil, err
}

func (s *appState) getSkillInstaller(c *cli.Command) (*skill.Installer, *sql.DB, error) {
	if err := s.ensure(c); err != nil {
		return nil, nil, err
	}

	resolvedPath, err := sqlite.ResolveDbPath(s.cfg.DbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve db path: %w", err)
	}

	db, err := sqlite.Open(resolvedPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open db: %w", err)
	}

	if err := sqlite.Migrate(db); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("migrate db: %w", err)
	}

	ss := sqlite.NewSkillStore(db)
	home := filepath.Dir(resolvedPath)

	inst := skill.NewInstaller(ss, home)
	return inst, db, nil
}

