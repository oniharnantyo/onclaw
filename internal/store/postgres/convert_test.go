package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

func TestConvertError(t *testing.T) {
	tests := []struct {
		name     string
		input    error
		expected error
	}{
		{
			name:     "nil error",
			input:    nil,
			expected: nil,
		},
		{
			name:     "pgx.ErrNoRows",
			input:    pgx.ErrNoRows,
			expected: domain.ErrNotFound,
		},
		{
			name:     "domain.ErrNotFound already wrapped",
			input:    domain.ErrNotFound,
			expected: domain.ErrNotFound,
		},
		{
			name:     "domain.ErrConflict already wrapped",
			input:    domain.ErrConflict,
			expected: domain.ErrConflict,
		},
		{
			name:     "domain.ErrInvalid already wrapped",
			input:    domain.ErrInvalid,
			expected: domain.ErrInvalid,
		},
		{
			name:     "pg UniqueViolation (23505)",
			input:    &pgconn.PgError{Code: pgerrcode.UniqueViolation, Detail: "Key (email)=(a@b.com) already exists."},
			expected: domain.ErrConflict,
		},
		{
			name:     "pg ForeignKeyViolation (23503)",
			input:    &pgconn.PgError{Code: pgerrcode.ForeignKeyViolation, Detail: "Key (workspace_id)=(...) is not present in table."},
			expected: domain.ErrNotFound,
		},
		{
			name:     "pg InvalidTextRepresentation (22P02)",
			input:    &pgconn.PgError{Code: pgerrcode.InvalidTextRepresentation, Message: "invalid input syntax for type uuid"},
			expected: domain.ErrNotFound,
		},
		{
			name:     "pg NotNullViolation (23502)",
			input:    &pgconn.PgError{Code: pgerrcode.NotNullViolation, Message: "null value in column violates not-null constraint"},
			expected: domain.ErrInvalid,
		},
		{
			name:     "pg CheckViolation (23514)",
			input:    &pgconn.PgError{Code: pgerrcode.CheckViolation, Message: "check constraint failed"},
			expected: domain.ErrInvalid,
		},
		{
			name:     "pg StringDataRightTruncation (22001)",
			input:    &pgconn.PgError{Code: pgerrcode.StringDataRightTruncationDataException, Message: "value too long for type"},
			expected: domain.ErrInvalid,
		},
		{
			name:     "unrelated error",
			input:    errors.New("custom error"),
			expected: nil, // custom error won't match any sentinel
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := convertError(tc.input)
			if tc.expected == nil {
				if tc.input == nil && actual != nil {
					t.Fatalf("expected nil, got %v", actual)
				}
				if tc.input != nil && !errors.Is(actual, tc.input) {
					t.Fatalf("expected error %v to be returned as-is, got %v", tc.input, actual)
				}
			} else {
				if !errors.Is(actual, tc.expected) {
					t.Fatalf("expected error wrapping %v, got %v", tc.expected, actual)
				}
			}
		})
	}
}

func TestStoreDriverRegistered(t *testing.T) {
	ctx := context.Background()
	_, err := storeport.Open(ctx, "postgres", storeport.DSNConfig{DSN: ""})
	if err == nil {
		t.Fatal("expected error with empty DSN")
	}
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for empty DSN, got %v", err)
	}
}
