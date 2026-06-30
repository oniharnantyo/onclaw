package service

import (
	"errors"
	"strings"
)

// ErrNotFound is returned when a resource is not found.
var ErrNotFound = errors.New("not found")

// classify wraps errors that indicate a missing resource (e.g. from LLM service or DB) into ErrNotFound.
// It avoids importing database/sql by checking the error message string.
func classify(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrNotFound) {
		return err
	}
	errMsg := err.Error()
	if strings.Contains(errMsg, "not found") || strings.Contains(errMsg, "no rows in result set") {
		return ErrNotFound
	}
	return err
}
