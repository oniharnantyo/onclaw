package postgres

import (
	"errors"
	"fmt"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

// convertError maps Postgres driver and pgconn errors to domain sentinel errors.
func convertError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	if errors.Is(err, domain.ErrNotFound) ||
		errors.Is(err, domain.ErrConflict) ||
		errors.Is(err, domain.ErrInvalid) ||
		errors.Is(err, domain.ErrUnauthenticated) ||
		errors.Is(err, domain.ErrForbidden) ||
		errors.Is(err, domain.ErrLastOwnerProtected) {
		return err
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case pgerrcode.UniqueViolation: // 23505
			if pgErr.Detail != "" {
				return fmt.Errorf("%w: %s", domain.ErrConflict, pgErr.Detail)
			}
			return domain.ErrConflict
		case pgerrcode.ForeignKeyViolation: // 23503
			if pgErr.Detail != "" {
				return fmt.Errorf("%w: %s", domain.ErrNotFound, pgErr.Detail)
			}
			return domain.ErrNotFound
		case pgerrcode.InvalidTextRepresentation: // 22P02 (e.g. invalid UUID syntax)
			return domain.ErrNotFound
		case pgerrcode.NotNullViolation, // 23502
			pgerrcode.CheckViolation,                         // 23514
			pgerrcode.StringDataRightTruncationDataException: // 22001
			if pgErr.Message != "" {
				return fmt.Errorf("%w: %s", domain.ErrInvalid, pgErr.Message)
			}
			return domain.ErrInvalid
		}
	}

	return err
}
