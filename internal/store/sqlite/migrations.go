package sqlite

import (
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// migrationsFS embeds the forward-only SQL migrations. File names follow the
// NNNNNN_name.sql convention; the numeric prefix is the schema version.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

const migrationsDir = "migrations"

// migration is one forward-only schema step applied in its own transaction.
type migration struct {
	Version int
	Name    string
	SQL     string
}

// embeddedMigrations loads and parses the embedded migration files ordered
// by version.
func embeddedMigrations() ([]migration, error) {
	entries, err := migrationsFS.ReadDir(migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	migs := make([]migration, 0, len(names))
	for _, name := range names {
		version, err := parseVersion(name)
		if err != nil {
			return nil, fmt.Errorf("embedded migration %s: %w", name, err)
		}
		raw, err := migrationsFS.ReadFile(migrationsDir + "/" + name)
		if err != nil {
			return nil, fmt.Errorf("read embedded migration %s: %w", name, err)
		}
		migs = append(migs, migration{
			Version: version,
			Name:    strings.TrimSuffix(name, ".sql"),
			SQL:     string(raw),
		})
	}
	return migs, nil
}

// parseVersion extracts the numeric prefix of a migration file name
// (NNNNNN_name.sql).
func parseVersion(name string) (int, error) {
	prefix, _, found := strings.Cut(name, "_")
	if !found {
		return 0, fmt.Errorf("missing version prefix")
	}
	version, err := strconv.Atoi(prefix)
	if err != nil {
		return 0, fmt.Errorf("invalid version prefix %q: %w", prefix, err)
	}
	return version, nil
}
