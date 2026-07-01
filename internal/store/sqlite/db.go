package sqlite

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// ResolveDbPath resolves the database path from configuration.
// If dbPath is empty, it uses $XDG_DATA_HOME/onclaw/onclaw.db.
// If $XDG_DATA_HOME is empty, it falls back to $HOME/.local/share/onclaw/onclaw.db.
func ResolveDbPath(dbPath string) (string, error) {
	if dbPath != "" {
		return dbPath, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user home dir: %w", err)
	}
	return filepath.Join(home, ".onclaw", "onclaw.db"), nil
}

// Open opens a SQLite database at dbPath.
func Open(dbPath string) (*sql.DB, error) {
	fi, err := os.Stat(dbPath)
	if err == nil {
		// File exists, check permissions
		if fi.Mode().Perm() != 0600 {
			return nil, fmt.Errorf("database file %s has invalid permissions: %04o, expected exactly 0600", dbPath, fi.Mode().Perm())
		}
	} else if os.IsNotExist(err) {
		// Ensure parent directory exists
		parentDir := filepath.Dir(dbPath)
		if err := os.MkdirAll(parentDir, 0700); err != nil {
			return nil, fmt.Errorf("create parent directory: %w", err)
		}
		// Create the database file with 0600 permissions
		f, err := os.OpenFile(dbPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			return nil, fmt.Errorf("create database file with permissions: %w", err)
		}
		f.Close()
	} else {
		return nil, fmt.Errorf("stat database file: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}

	// Enable WAL mode
	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable WAL mode: %w", err)
	}

	// Enable foreign key support
	if _, err := db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	return db, nil
}

// Migrate runs idempotent migrations for the database schema.
func Migrate(db *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS llm_providers (
			name TEXT PRIMARY KEY,
			provider_type TEXT NOT NULL,
			api_base TEXT NOT NULL DEFAULT '',
			settings TEXT NOT NULL DEFAULT '{}',
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS config_secrets (
			key TEXT PRIMARY KEY,
			encrypted_value TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS preferences (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS agents (
			name TEXT PRIMARY KEY,
			provider TEXT NOT NULL,
			model TEXT NOT NULL DEFAULT '',
			model_metadata TEXT NOT NULL DEFAULT '{}',
			reasoning_effort TEXT NOT NULL DEFAULT '',
			reasoning_budget_tokens INTEGER NOT NULL DEFAULT 0,
			system_prompt TEXT NOT NULL DEFAULT '',
			workspace TEXT NOT NULL DEFAULT '',
			tools TEXT NOT NULL DEFAULT '',
			max_iterations INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS conversations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			agent_name TEXT NOT NULL,
			summary_until_seq INTEGER NOT NULL DEFAULT 0,
			summary_message_id INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_conversations_agent ON conversations(agent_name);`,
		`CREATE TABLE IF NOT EXISTS conversation_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			conversation_id INTEGER NOT NULL,
			seq INTEGER NOT NULL,
			role TEXT NOT NULL,
			message TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY(conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_messages_conv_seq ON conversation_messages(conversation_id, seq);`,
		`CREATE INDEX IF NOT EXISTS idx_messages_role ON conversation_messages(role);`,
		`CREATE TABLE IF NOT EXISTS skills (
			name TEXT,
			scope TEXT NOT NULL,
			source_type TEXT NOT NULL,
			source TEXT NOT NULL,
			skill_path TEXT NOT NULL,
			version TEXT NOT NULL,
			hash TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			installed_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (name, scope)
		);`,
		`CREATE TABLE IF NOT EXISTS mcp_servers (
			name TEXT PRIMARY KEY,
			transport TEXT NOT NULL,
			command TEXT NOT NULL DEFAULT '',
			args TEXT NOT NULL DEFAULT '[]',
			env TEXT NOT NULL DEFAULT '{}',
			url TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
	}

	// Drop old skills table (if it had only name as PK) to migrate cleanly.
	// This is safe since the feature is not yet released.
	_, _ = db.Exec("DROP TABLE IF EXISTS skills;")

	for _, q := range queries {
		if _, err := db.Exec(q); err != nil {
			return fmt.Errorf("execute migration query: %w", err)
		}
	}

	// Guarded migrations for existing DBs
	hasModel, err := columnExists(db, "llm_providers", "model")
	if err != nil {
		return fmt.Errorf("check llm_providers model column: %w", err)
	}
	if hasModel {
		// Attempt to drop the column, swallow error if the sqlite version does not support DROP COLUMN
		if _, err := db.Exec("ALTER TABLE llm_providers DROP COLUMN model"); err != nil {
			log.Printf("Warning: failed to drop column 'model' from llm_providers (might not be supported by sqlite version): %v", err)
		}
	}

	hasMeta, err := columnExists(db, "agents", "model_metadata")
	if err != nil {
		return fmt.Errorf("check agents model_metadata column: %w", err)
	}
	if !hasMeta {
		if _, err := db.Exec("ALTER TABLE agents ADD COLUMN model_metadata TEXT NOT NULL DEFAULT '{}'"); err != nil {
			return fmt.Errorf("add model_metadata column to agents: %w", err)
		}
	}

	hasBudget, err := columnExists(db, "agents", "reasoning_budget_tokens")
	if err != nil {
		return fmt.Errorf("check agents reasoning_budget_tokens column: %w", err)
	}
	if !hasBudget {
		if _, err := db.Exec("ALTER TABLE agents ADD COLUMN reasoning_budget_tokens INTEGER NOT NULL DEFAULT 0"); err != nil {
			return fmt.Errorf("add reasoning_budget_tokens column to agents: %w", err)
		}
	}

	hasSummarySeq, err := columnExists(db, "conversations", "summary_until_seq")
	if err != nil {
		return fmt.Errorf("check conversations summary_until_seq column: %w", err)
	}
	if !hasSummarySeq {
		if _, err := db.Exec("ALTER TABLE conversations ADD COLUMN summary_until_seq INTEGER NOT NULL DEFAULT 0"); err != nil {
			return fmt.Errorf("add summary_until_seq column to conversations: %w", err)
		}
	}

	hasSummaryMsgID, err := columnExists(db, "conversations", "summary_message_id")
	if err != nil {
		return fmt.Errorf("check conversations summary_message_id column: %w", err)
	}
	if !hasSummaryMsgID {
		if _, err := db.Exec("ALTER TABLE conversations ADD COLUMN summary_message_id INTEGER NOT NULL DEFAULT 0"); err != nil {
			return fmt.Errorf("add summary_message_id column to conversations: %w", err)
		}
	}

	return nil

}

func columnExists(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typeStr string
		var notnull int
		var dfltVal sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typeStr, &notnull, &dfltVal, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, nil
}

func now() string {
	return time.Now().Format(time.RFC3339)
}
