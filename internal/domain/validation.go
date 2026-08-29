package domain

import (
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata"
)

var (
	slugRegex  = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
	emailRegex = regexp.MustCompile(`^[a-zA-Z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)+$`)
)

// ReservedSlugs contains slugs reserved for system and routing purposes.
var ReservedSlugs = map[string]bool{
	"api":        true,
	"auth":       true,
	"new":        true,
	"settings":   true,
	"workspaces": true,
	"login":      true,
	"logout":     true,
	"master":     true,
}

// IsReservedSlug reports whether a slug is reserved.
func IsReservedSlug(slug string) bool {
	return ReservedSlugs[strings.ToLower(strings.TrimSpace(slug))]
}

// ValidateSlug validates that a workspace slug is a valid DNS label and not reserved.
// Slugs must be between 1 and 63 characters, match ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$,
// and not be in the reserved slug list.
func ValidateSlug(slug string) error {
	if len(slug) < 1 || len(slug) > 63 {
		return fmt.Errorf("%w: slug must be between 1 and 63 characters", ErrInvalid)
	}

	if !slugRegex.MatchString(slug) {
		return fmt.Errorf("%w: slug must match DNS label format (lowercase alphanumeric with interior hyphens)", ErrInvalid)
	}

	if IsReservedSlug(slug) {
		return fmt.Errorf("%w: slug %q is reserved", ErrInvalid, slug)
	}

	return nil
}

// NormalizeEmail converts an email address to lowercase with leading and trailing whitespace removed.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ValidateEmail checks that an email address is syntactically valid.
func ValidateEmail(email string) error {
	email = strings.TrimSpace(email)
	if email == "" {
		return fmt.Errorf("%w: email cannot be empty", ErrInvalid)
	}

	if len(email) > 254 {
		return fmt.Errorf("%w: email exceeds maximum length of 254 characters", ErrInvalid)
	}

	// Must parse with net/mail and match strict email pattern with domain dot
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return fmt.Errorf("%w: invalid email address format", ErrInvalid)
	}

	if !emailRegex.MatchString(email) {
		return fmt.Errorf("%w: invalid email address format", ErrInvalid)
	}

	return nil
}

// ValidateTimezone validates that a timezone identifier is a valid IANA timezone name.
func ValidateTimezone(tz string) error {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		return fmt.Errorf("%w: timezone cannot be empty", ErrInvalid)
	}

	if _, err := time.LoadLocation(tz); err != nil {
		return fmt.Errorf("%w: unknown timezone identifier %q", ErrInvalid, tz)
	}

	return nil
}
