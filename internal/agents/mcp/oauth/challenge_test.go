package oauth_test

import (
	"testing"

	"github.com/oniharnantyo/onclaw/internal/agents/mcp/oauth"
)

func TestParseWWWAuthenticate(t *testing.T) {
	cases := []struct {
		name       string
		values     []string
		wantURI    string
		wantCount  int
		wantScheme string
	}{
		{
			name: "gitlab single header with realm",
			values: []string{
				`Bearer realm="gitlab", resource_metadata="https://gitlab.com/.well-known/oauth-protected-resource/api/v4/mcp"`,
			},
			wantURI:    "https://gitlab.com/.well-known/oauth-protected-resource/api/v4/mcp",
			wantCount:  1,
			wantScheme: "Bearer",
		},
		{
			name: "two header values, non-bearer first",
			values: []string{
				`Basic realm="gitlab"`,
				`Bearer resource_metadata="https://rs.example/.well-known/oauth-protected-resource"`,
			},
			wantURI:   "https://rs.example/.well-known/oauth-protected-resource",
			wantCount: 2,
		},
		{
			name: "error param before resource_metadata on one line",
			values: []string{
				`Bearer error="invalid_token", resource_metadata="https://rs.example/meta"`,
			},
			wantURI:   "https://rs.example/meta",
			wantCount: 1,
		},
		{
			name: "second challenge on the same line",
			values: []string{
				`Basic realm="x", Bearer resource_metadata="https://rs.example/meta"`,
			},
			wantURI:   "https://rs.example/meta",
			wantCount: 2,
		},
		{
			name: "quoted value with comma and escaped quotes",
			values: []string{
				`Bearer realm="a, b \"quoted\"", resource_metadata="https://rs.example/meta"`,
			},
			wantURI:   "https://rs.example/meta",
			wantCount: 1,
		},
		{
			name:       "bare scheme line",
			values:     []string{"NTLM"},
			wantURI:    "",
			wantCount:  1,
			wantScheme: "NTLM",
		},
		{
			name: "non-bearer scheme with resource_metadata ignored",
			values: []string{
				`Basic resource_metadata="https://rs.example/meta"`,
			},
			wantURI:   "",
			wantCount: 1,
		},
		{
			name: "no resource_metadata param",
			values: []string{
				`Bearer realm="only"`,
			},
			wantURI:   "",
			wantCount: 1,
		},
		{
			name: "param names case-insensitive",
			values: []string{
				`bearer RESOURCE_METADATA="https://rs.example/meta"`,
			},
			wantURI:   "https://rs.example/meta",
			wantCount: 1,
		},
		{
			name: "empty and malformed values skipped",
			values: []string{
				"",
				`realm="no-scheme"`,
			},
			wantURI:   "",
			wantCount: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			challenges := oauth.ParseWWWAuthenticate(tc.values)
			if len(challenges) != tc.wantCount {
				t.Fatalf("expected %d challenges, got %d (%v)", tc.wantCount, len(challenges), challenges)
			}
			if uri := oauth.ResourceMetadataURI(challenges); uri != tc.wantURI {
				t.Errorf("expected resource_metadata %q, got %q", tc.wantURI, uri)
			}
			if tc.wantScheme != "" && challenges[0].Scheme != tc.wantScheme {
				t.Errorf("expected scheme %q, got %q", tc.wantScheme, challenges[0].Scheme)
			}
		})
	}
}

func TestParseWWWAuthenticate_QuotedValueUnescaping(t *testing.T) {
	challenges := oauth.ParseWWWAuthenticate([]string{`Bearer realm="a, b \"quoted\"", resource_metadata="https://rs.example/meta"`})
	if len(challenges) != 1 {
		t.Fatalf("expected one challenge, got %d", len(challenges))
	}
	if got := challenges[0].Params["realm"]; got != `a, b "quoted"` {
		t.Errorf("expected unescaped realm %q, got %q", `a, b "quoted"`, got)
	}
}

func TestResourceMetadataURI_FirstBearerChallengeWins(t *testing.T) {
	challenges := oauth.ParseWWWAuthenticate([]string{
		`Bearer resource_metadata="https://first.example/meta"`,
		`Bearer resource_metadata="https://second.example/meta"`,
	})
	if uri := oauth.ResourceMetadataURI(challenges); uri != "https://first.example/meta" {
		t.Errorf("expected the first challenge's URI, got %q", uri)
	}
}
