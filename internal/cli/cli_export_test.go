package cli

import (
	"context"
	"database/sql"
	"io"

	"github.com/oniharnantyo/onclaw/internal/config"
	"github.com/oniharnantyo/onclaw/internal/llm"
	"github.com/urfave/cli/v3"
)

// AppState aliases the unexported appState type.
type AppState = appState

// GetProviderManager is a test helper wrapper around getProviderManager.
func (st *AppState) GetProviderManager(c *cli.Command) (*llm.Service, *sql.DB, error) {
	mgr, _, db, err := st.getProviderManager(c)
	return mgr, db, err
}

// Ensure is a test helper wrapper around ensure.
func (st *AppState) Ensure(c *cli.Command) error {
	return st.ensure(c)
}

// GetConfig is a test helper to get the unexported cfg field.
func (st *AppState) GetConfig() *config.Config {
	return st.cfg
}

// SetConfig is a test helper to set the unexported cfg field.
func (st *AppState) SetConfig(cfg *config.Config) {
	st.cfg = cfg
}

// RunProviderSetup is a test helper wrapper around runProviderSetup.
func RunProviderSetup(ctx context.Context, mgr *llm.Service, db *sql.DB, dbPath string, in io.Reader, out io.Writer) error {
	return runProviderSetup(ctx, mgr, db, dbPath, in, out)
}

// WritePIDFile is a test helper wrapper around writePIDFile.
func WritePIDFile(dbPath string) (string, error) {
	return writePIDFile(dbPath)
}

// SignalRunningProcess is a test helper wrapper around signalRunningProcess.
func SignalRunningProcess(dbPath string) error {
	return signalRunningProcess(dbPath)
}

// Exported unexported helper variables
var ValidateReasoning = validateReasoning
var ParseString = parseString
var ParseChoice = parseChoice
var ParseConfirm = parseConfirm
var PromptString = promptString
var PromptSecret = promptSecret
var PromptChoice = promptChoice
var PromptConfirm = promptConfirm
