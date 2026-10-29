package botapi

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClassifyTable covers every error kind, the retry_after extraction and
// the envelope-error_code-over-http-status precedence.
func TestClassifyTable(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		env       response
		wantKind  ErrorKind
		wantRetry time.Duration
	}{
		{
			name:      "429 rate limited with retry_after",
			status:    429,
			env:       response{OK: false, ErrorCode: 429, Description: "Too Many Requests: retry after 30", Parameters: responseParameters{RetryAfter: 30}},
			wantKind:  KindRateLimited,
			wantRetry: 30 * time.Second,
		},
		{
			name:     "429 rate limited without retry_after",
			status:   429,
			env:      response{OK: false, ErrorCode: 429, Description: "Too Many Requests"},
			wantKind: KindRateLimited,
		},
		{
			name:     "403 forbidden",
			status:   403,
			env:      response{OK: false, ErrorCode: 403, Description: "Forbidden: bot was kicked from the group chat"},
			wantKind: KindForbidden,
		},
		{
			name:     "404 not found",
			status:   404,
			env:      response{OK: false, ErrorCode: 404, Description: "Not Found"},
			wantKind: KindNotFound,
		},
		{
			name:     "409 conflict",
			status:   409,
			env:      response{OK: false, ErrorCode: 409, Description: "Conflict: terminated by other getUpdates request"},
			wantKind: KindConflict,
		},
		{
			name:     "401 auth invalid",
			status:   401,
			env:      response{OK: false, ErrorCode: 401, Description: "Unauthorized"},
			wantKind: KindAuthInvalid,
		},
		{
			name:     "400 parse failure",
			status:   400,
			env:      response{OK: false, ErrorCode: 400, Description: "Bad Request: can't parse entities: Can't parse entities: character '{' is reserved"},
			wantKind: KindParse,
		},
		{
			name:     "400 without parse in description is transient",
			status:   400,
			env:      response{OK: false, ErrorCode: 400, Description: "Bad Request: message text is empty"},
			wantKind: KindTransient,
		},
		{
			name:     "502 without envelope error_code falls back to http status",
			status:   502,
			env:      response{OK: false, Description: "Bad Gateway"},
			wantKind: KindTransient,
		},
		{
			name:     "empty description falls back to the status code",
			status:   500,
			env:      response{OK: false},
			wantKind: KindTransient,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := classify(tt.status, &tt.env)
			require.Error(t, err)
			assert.Equal(t, tt.wantKind, err.Kind)
			assert.Equal(t, tt.wantRetry, err.RetryAfter)

			// Callers classify through errors.As, not a type assertion.
			var target *Error
			require.True(t, errors.As(error(err), &target))
			assert.Equal(t, tt.wantKind, target.Kind)
		})
	}
}

func TestIsParseFailure(t *testing.T) {
	tests := []struct {
		name        string
		description string
		want        bool
	}{
		{name: "entity parse failure", description: "Bad Request: can't parse entities: Unsupported start tag", want: true},
		{name: "parse failure, capitalised", description: "Bad Request: Can't parse entities: character '_' is reserved", want: true},
		{name: "reply keyboard parse failure", description: "Bad Request: can't parse reply keyboard markup json object", want: true},
		{name: "unrelated bad request", description: "Bad Request: chat not found", want: false},
		{name: "empty description", description: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isParseFailure(tt.description))
		})
	}
}

func TestErrorMessage(t *testing.T) {
	t.Run("rate limited with retry hint", func(t *testing.T) {
		err := &Error{Kind: KindRateLimited, Description: "Too Many Requests: retry after 17", RetryAfter: 17 * time.Second}
		assert.Equal(t, "botapi: rate_limited: Too Many Requests: retry after 17 (retry after 17s)", err.Error())
	})
	t.Run("plain kind and description", func(t *testing.T) {
		err := &Error{Kind: KindConflict, Description: "Conflict: terminated by other getUpdates request"}
		assert.Equal(t, "botapi: conflict: Conflict: terminated by other getUpdates request", err.Error())
	})
}
