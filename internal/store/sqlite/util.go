package sqlite

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/oniharnantyo/buddies/internal/store"
)

// Timestamps and JSON columns are stored as TEXT (RFC3339Nano / compact
// JSON); these helpers keep the encoding in one place.

func formatTime(t time.Time) string { return t.Format(time.RFC3339Nano) }

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse time %q: %w", s, err)
	}
	return t, nil
}

func jsonMarshal(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal json column: %w", err)
	}
	return string(raw), nil
}

func jsonUnmarshal(raw string, v any) error {
	if err := json.Unmarshal([]byte(raw), v); err != nil {
		return fmt.Errorf("unmarshal json column: %w", err)
	}
	return nil
}

// newID returns a random 128-bit hex ID. Store-assigned identity, mirroring
// the donor's uuid columns without adding a dependency.
func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// nowUTC is the single clock the store stamps app-managed timestamps from.
func nowUTC() time.Time { return time.Now().UTC() }

// boolInt encodes a Go bool as a SQLite 0/1 INTEGER column value.
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// stampCreated fills both timestamps on insert when the caller left them
// zero, keeping CreatedAt <= UpdatedAt.
func stampCreated(createdAt, updatedAt *time.Time) {
	now := nowUTC()
	if createdAt.IsZero() {
		*createdAt = now
	}
	if updatedAt.IsZero() {
		*updatedAt = *createdAt
	}
}

// nullString encodes a Go string as a nullable column value: the empty
// string is stored as NULL (an agent without a slug yet), anything else
// verbatim.
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// scanner is the common single-row scan surface of *sql.Row and *sql.Rows,
// letting one scan helper serve both single- and multi-row reads.
type scanner interface {
	Scan(dest ...any) error
}

// readErr maps driver no-rows onto the store sentinel and wraps everything
// else with the operation name for context.
func readErr(op string, err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s: %w", op, store.ErrNotFound)
	}
	return fmt.Errorf("%s: %w", op, err)
}
