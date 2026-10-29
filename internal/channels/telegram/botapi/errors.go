package botapi

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ErrorKind classifies a Bot API failure so callers can branch on behavior
// (back off, strip formatting, stop the poller, surface an error state)
// instead of string-matching descriptions.
type ErrorKind string

const (
	// KindRateLimited is HTTP 429; RetryAfter carries the Telegram hint.
	KindRateLimited ErrorKind = "rate_limited"
	// KindForbidden is HTTP 403 (bot blocked or kicked from the chat).
	KindForbidden ErrorKind = "forbidden"
	// KindNotFound is HTTP 404 (malformed token — the path no longer
	// resolves).
	KindNotFound ErrorKind = "not_found"
	// KindConflict is HTTP 409 (another getUpdates consumer owns the token).
	KindConflict ErrorKind = "conflict"
	// KindAuthInvalid is HTTP 401 (revoked or wrong token).
	KindAuthInvalid ErrorKind = "auth_invalid"
	// KindParse is a 400 whose description indicates entity parsing failed
	// (the MarkdownV2 fallback path resends stripped plain text).
	KindParse ErrorKind = "parse"
	// KindTransient covers network errors, timeouts, 5xx and any non-2xx
	// without a known mapping; retrying (with backoff) is reasonable.
	KindTransient ErrorKind = "transient"
)

// Error is the typed failure returned by every botapi call. Match with
// errors.As: the client never returns a bare *Error for successes and always
// populates Kind (Description carries the Bot API description or the
// underlying transport message; RetryAfter is positive only for
// KindRateLimited with a retry_after hint).
type Error struct {
	Kind        ErrorKind
	Description string
	RetryAfter  time.Duration
}

// Error renders the kind, description and, when rate limited, the retry hint.
func (e *Error) Error() string {
	if e.Kind == KindRateLimited && e.RetryAfter > 0 {
		return fmt.Sprintf("botapi: %s: %s (retry after %s)", e.Kind, e.Description, e.RetryAfter)
	}
	return fmt.Sprintf("botapi: %s: %s", e.Kind, e.Description)
}

// classify maps a failed Bot API response onto an *Error. The envelope's
// error_code is authoritative; the HTTP status is the fallback for bodies
// that omit it (e.g. intermediaries answering without a Bot API envelope).
func classify(httpStatus int, env *response) *Error {
	code := env.ErrorCode
	if code == 0 {
		code = httpStatus
	}
	description := env.Description
	if description == "" {
		description = fmt.Sprintf("HTTP %d", code)
	}
	e := &Error{Kind: KindTransient, Description: description}
	switch {
	case code == http.StatusTooManyRequests:
		e.Kind = KindRateLimited
		if env.Parameters.RetryAfter > 0 {
			e.RetryAfter = time.Duration(env.Parameters.RetryAfter) * time.Second
		}
	case code == http.StatusForbidden:
		e.Kind = KindForbidden
	case code == http.StatusNotFound:
		e.Kind = KindNotFound
	case code == http.StatusConflict:
		e.Kind = KindConflict
	case code == http.StatusUnauthorized:
		e.Kind = KindAuthInvalid
	case code == http.StatusBadRequest && isParseFailure(description):
		e.Kind = KindParse
	}
	return e
}

// isParseFailure reports whether a 400 description indicates Telegram could
// not parse the message entities ("can't parse entities: ...") — the signal
// the MarkdownV2 → plain-text fallback keys off.
func isParseFailure(description string) bool {
	return strings.Contains(strings.ToLower(description), "parse")
}
