package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBearerGate is table-driven over the gate's contract: with a token
// configured, missing or wrong tokens are rejected (401) and the token is
// accepted via either the Authorization header or the ?token= query
// fallback (browsers cannot set headers on WebSocket); with no token
// configured everything passes through.
func TestBearerGate(t *testing.T) {
	const token = "s3cret"

	probe := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Seen-Query", r.URL.RawQuery)
		fmt.Fprintln(w, "passed")
	})

	tests := []struct {
		name         string
		configure    string // token configured for the gate; "" = none
		authHeader   string
		queryToken   string
		wantStatus   int
		wantBody     string
		wantStripped bool // query token must not reach the downstream handler
	}{
		{
			name:       "no token configured: passthrough without credentials",
			configure:  "",
			wantStatus: http.StatusOK,
			wantBody:   "passed\n",
		},
		{
			name:       "no token configured: garbage credentials still pass",
			configure:  "",
			authHeader: "Bearer wrong",
			wantStatus: http.StatusOK,
			wantBody:   "passed\n",
		},
		{
			name:       "configured: missing token rejected",
			configure:  token,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "configured: wrong bearer token rejected",
			configure:  token,
			authHeader: "Bearer wrong",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "configured: wrong query token rejected",
			configure:  token,
			queryToken: "wrong",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "configured: valid bearer header accepted",
			configure:  token,
			authHeader: "Bearer " + token,
			wantStatus: http.StatusOK,
			wantBody:   "passed\n",
		},
		{
			name:       "configured: bearer header is case-insensitive",
			configure:  token,
			authHeader: "bearer " + token,
			wantStatus: http.StatusOK,
			wantBody:   "passed\n",
		},
		{
			name:         "configured: valid query token accepted and stripped",
			configure:    token,
			queryToken:   token,
			wantStatus:   http.StatusOK,
			wantBody:     "passed\n",
			wantStripped: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var seenQuery string
			called := false
			downstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				seenQuery = r.URL.RawQuery
				probe.ServeHTTP(w, r)
			})

			srv := httptest.NewServer(BearerGate(tt.configure)(downstream))
			t.Cleanup(srv.Close)

			req, err := http.NewRequest(http.MethodGet, srv.URL+"/anything", nil)
			require.NoError(t, err)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			if tt.queryToken != "" {
				q := req.URL.Query()
				q.Set("token", tt.queryToken)
				req.URL.RawQuery = q.Encode()
			}

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tt.wantStatus, resp.StatusCode)
			if tt.wantStatus != http.StatusOK {
				assert.False(t, called, "downstream must not see a rejected request")
				return
			}
			require.True(t, called)
			if tt.wantStripped {
				assert.NotContains(t, seenQuery, "token=")
			}
		})
	}
}
