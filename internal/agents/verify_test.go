package agents

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// probeRecorder captures the request a verification probe sent.
type probeRecorder struct {
	path       string
	query      string
	header     http.Header
	statusCode int
	body       string
	delay      time.Duration
}

func (p *probeRecorder) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.path = r.URL.Path
		p.query = r.URL.RawQuery
		p.header = r.Header.Clone()
		if p.delay > 0 {
			time.Sleep(p.delay)
		}
		if p.statusCode != 0 {
			w.WriteHeader(p.statusCode)
		}
		_, _ = w.Write([]byte(p.body))
	})
}

func TestVerifyCredentials_Success(t *testing.T) {
	const apiKey = "secret-key-1234"

	tests := []struct {
		name         string
		providerType string
		wantPath     string
		wantQuery    string
		wantAuth     func(t *testing.T, header http.Header)
	}{
		{
			name:         "anthropic appends /v1 and uses x-api-key",
			providerType: ProviderAnthropic,
			wantPath:     "/v1/models",
			wantAuth: func(t *testing.T, header http.Header) {
				require.Equal(t, apiKey, header.Get("x-api-key"))
				require.Equal(t, "2023-06-01", header.Get("anthropic-version"))
			},
		},
		{
			name:         "anthropic-compatible probes the user base",
			providerType: ProviderAnthropicCompatible,
			wantPath:     "/v1/models",
			wantAuth: func(t *testing.T, header http.Header) {
				require.Equal(t, apiKey, header.Get("x-api-key"))
				require.Equal(t, "2023-06-01", header.Get("anthropic-version"))
			},
		},
		{
			name:         "openai uses bearer on /models",
			providerType: ProviderOpenAI,
			wantPath:     "/models",
			wantAuth: func(t *testing.T, header http.Header) {
				require.Equal(t, "Bearer "+apiKey, header.Get("Authorization"))
			},
		},
		{
			name:         "openrouter uses bearer on /models",
			providerType: ProviderOpenRouter,
			wantPath:     "/models",
			wantAuth: func(t *testing.T, header http.Header) {
				require.Equal(t, "Bearer "+apiKey, header.Get("Authorization"))
			},
		},
		{
			name:         "openai-compatible uses bearer on the user base",
			providerType: ProviderOpenAICompatible,
			wantPath:     "/models",
			wantAuth: func(t *testing.T, header http.Header) {
				require.Equal(t, "Bearer "+apiKey, header.Get("Authorization"))
			},
		},
		{
			name:         "gemini passes the key as a query parameter",
			providerType: ProviderGemini,
			wantPath:     "/v1beta/models",
			wantQuery:    "key=" + apiKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe := &probeRecorder{statusCode: http.StatusOK, body: `{"data":[]}`}
			server := httptest.NewServer(probe.handler())
			defer server.Close()

			// A trailing slash on the base must not double up in the path.
			ok, detail, err := VerifyCredentials(context.Background(), tt.providerType, server.URL+"/", apiKey)
			require.NoError(t, err)
			require.True(t, ok)
			require.Empty(t, detail)
			require.Equal(t, tt.wantPath, probe.path)
			require.Equal(t, tt.wantQuery, probe.query)
			if tt.wantAuth != nil {
				tt.wantAuth(t, probe.header)
			}
		})
	}
}

func TestVerifyCredentials_AuthFailureCarriesEndpointDetail(t *testing.T) {
	probe := &probeRecorder{
		statusCode: http.StatusUnauthorized,
		body:       `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`,
	}
	server := httptest.NewServer(probe.handler())
	defer server.Close()

	ok, detail, err := VerifyCredentials(context.Background(), ProviderAnthropic, server.URL, "wrong-key")
	require.NoError(t, err)
	require.False(t, ok)
	require.Contains(t, detail, "401")
	require.Contains(t, detail, "invalid x-api-key")
}

func TestVerifyCredentials_DetailIsTruncated(t *testing.T) {
	probe := &probeRecorder{
		statusCode: http.StatusBadRequest,
		body:       strings.Repeat("x", 1000),
	}
	server := httptest.NewServer(probe.handler())
	defer server.Close()

	_, detail, err := VerifyCredentials(context.Background(), ProviderOpenAI, server.URL, "k")
	require.NoError(t, err)
	require.LessOrEqual(t, len([]rune(detail)), maxVerifyDetail+1)
	require.True(t, strings.HasSuffix(detail, "…"))
}

func TestVerifyCredentials_EmptyErrorBodyFallsBackToStatus(t *testing.T) {
	probe := &probeRecorder{statusCode: http.StatusInternalServerError}
	server := httptest.NewServer(probe.handler())
	defer server.Close()

	ok, detail, err := VerifyCredentials(context.Background(), ProviderOpenAI, server.URL, "k")
	require.NoError(t, err)
	require.False(t, ok)
	require.Equal(t, "500 Internal Server Error: ", detail)
}

func TestVerifyCredentials_Timeout(t *testing.T) {
	probe := &probeRecorder{statusCode: http.StatusOK, delay: 500 * time.Millisecond}
	server := httptest.NewServer(probe.handler())
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	_, _, err := VerifyCredentials(ctx, ProviderOpenAI, server.URL, "k")
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestVerifyCredentials_UnknownTypeIsError(t *testing.T) {
	_, _, err := VerifyCredentials(context.Background(), "mystery", "https://example.invalid", "k")
	require.ErrorIs(t, err, ErrProviderUnsupported)
}

func TestVerifyCredentials_CompatibleBlankBaseFailsWithDetail(t *testing.T) {
	for _, providerType := range []string{ProviderOpenAICompatible, ProviderAnthropicCompatible} {
		t.Run(providerType, func(t *testing.T) {
			ok, detail, err := VerifyCredentials(context.Background(), providerType, "", "k")
			require.NoError(t, err)
			require.False(t, ok)
			require.Contains(t, detail, "base URL is required")
		})
	}
}
