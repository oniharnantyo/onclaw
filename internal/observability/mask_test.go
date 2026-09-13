package observability

import (
	"encoding/base64"
	"strings"
	"testing"
)

// mustEnvelope builds a realistic "v1:<b64 nonce>:<b64 ciphertext>" envelope
// (internal/secrets Encrypt shape) from arbitrary bytes.
func mustEnvelope(t *testing.T, nonce, ciphertext []byte) string {
	t.Helper()
	return "v1:" + base64.StdEncoding.EncodeToString(nonce) + ":" + base64.StdEncoding.EncodeToString(ciphertext)
}

func TestMaskSecretEnvelopes(t *testing.T) {
	// Shared storage form of every workspace credential: web-search provider
	// keys, gateway bot tokens, MCP env/header rows, hook secret payloads.
	envelope := mustEnvelope(t, make([]byte, 12), make([]byte, 48))
	short := "v1:abc:def" // not envelope-shaped: segments below the length floor

	got := Mask(`stored row: ` + envelope + ` (kept)`)
	if strings.Contains(got, envelope) {
		t.Fatalf("Mask did not redact secrets envelope: %q", got)
	}
	if !strings.Contains(got, MaskPlaceholder) {
		t.Fatalf("Mask output lacks placeholder: %q", got)
	}
	if !strings.Contains(got, "(kept)") {
		t.Fatalf("Mask dropped non-secret surroundings: %q", got)
	}
	if got := Mask(short); !strings.Contains(got, short) {
		t.Fatalf("Mask redacted non-envelope prefix text %q: %q", short, got)
	}
}

func TestMaskWebSearchProviderKey(t *testing.T) {
	// Runtime form: the tool config's decrypted api_key field, as the model
	// or a tool span could echo it.
	payload := `{"entries":[{"id":"a1b2c3d4","name":"tavily","provider":"tavily","api_key":"tvly-Ab12Cd34Ef56Gh78Ij90Kl12Mn34","base_url":""},{"name":"searx","provider":"searxng","base_url":"http://searxng:8080"}]}`
	got := Mask(payload)
	if strings.Contains(got, "tvly-Ab12Cd34Ef56Gh78Ij90Kl12Mn34") {
		t.Fatalf("Mask leaked web-search api_key: %q", got)
	}
	if !strings.Contains(got, `"name":"tavily"`) || !strings.Contains(got, `"provider":"tavily"`) {
		t.Fatalf("Mask disturbed non-secret entry fields: %q", got)
	}
	if !strings.Contains(got, `"api_key":"[REDACTED]"`) {
		t.Fatalf("Mask did not replace api_key in place: %q", got)
	}
	if !strings.Contains(got, "http://searxng:8080") {
		t.Fatalf("Mask redacted harmless base_url: %q", got)
	}
}

func TestMaskGatewayBotToken(t *testing.T) {
	// Envelope form (storage) and plaintext Telegram bot token shape.
	envelope := mustEnvelope(t, make([]byte, 12), []byte("123456789:AAECTOKENDEMO TOKEN PADDING"))
	got := Mask(`bot token ciphertext: ` + envelope)
	if strings.Contains(got, envelope) {
		t.Fatalf("Mask leaked gateway token envelope: %q", got)
	}

	plaintext := `{"platform":"telegram","bot_username":"onclaw_bot","token":"1234567890:AAEtb0tPlaintextTokenValue1234567890-_"}`
	got = Mask(plaintext)
	if strings.Contains(got, "AAEtb0tPlaintextTokenValue1234567890-_") {
		t.Fatalf("Mask leaked telegram bot token: %q", got)
	}
	if !strings.Contains(got, `"bot_username":"onclaw_bot"`) {
		t.Fatalf("Mask disturbed non-secret gateway fields: %q", got)
	}
}

func TestMaskMCPCredentials(t *testing.T) {
	// MCP connection rows: envelope at rest, plaintext header at runtime.
	plaintext := `{"command":"npx","args":["-y","server"],"env":[{"name":"API_TOKEN","value":"opaque-random-credential-9931"},{"name":"LOG_LEVEL","value":"debug"}],"headers":[{"name":"Authorization","value":"Bearer sk-live-Ab12Cd34Ef56Gh78"},{"name":"X-Trace-Id","value":"abc"}]}`
	got := Mask(plaintext)
	if strings.Contains(got, "opaque-random-credential-9931") {
		t.Fatalf("Mask leaked MCP env row value under secret-ish name: %q", got)
	}
	if strings.Contains(got, "sk-live-Ab12Cd34Ef56Gh78") {
		t.Fatalf("Mask leaked bearer credential: %q", got)
	}
	if !strings.Contains(got, `"name":"LOG_LEVEL","value":"debug"`) {
		t.Fatalf("Mask redacted harmless row value: %q", got)
	}
	if !strings.Contains(got, `"name":"X-Trace-Id","value":"abc"`) {
		t.Fatalf("Mask redacted harmless header row: %q", got)
	}
	if !strings.Contains(got, `"value":"`+MaskPlaceholder+`"`) {
		t.Fatalf("Mask did not replace secret row value in place: %q", got)
	}
}

func TestMaskHookSecretPayloads(t *testing.T) {
	envelope := mustEnvelope(t, make([]byte, 12), []byte("super secret webhook signing key"))
	hookCfg := `{"url":"https://hooks.example.com/gate","headers":[{"name":"X-Signing-Key","value":"` + envelope + `"}],"command":"verify","env":[{"name":"HOOK_SECRET","value":"plain-hunter2"}]}`
	got := Mask(hookCfg)
	if strings.Contains(got, "super secret webhook signing key") {
		t.Fatalf("Mask leaked hook header envelope: %q", got)
	}
	if strings.Contains(got, "plain-hunter2") {
		t.Fatalf("Mask leaked hook env secret: %q", got)
	}
	if !strings.Contains(got, `"url":"https://hooks.example.com/gate"`) || !strings.Contains(got, `"command":"verify"`) {
		t.Fatalf("Mask disturbed non-secret hook config: %q", got)
	}
}

func TestMaskSecretNamedJSONFields(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		secret  string
		keep    string
	}{
		{
			name:    "flat api key, mixed case field",
			payload: `{"TAVILY_API_KEY":"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","model":"gpt-4o"}`,
			secret:  "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",
			keep:    `"model":"gpt-4o"`,
		},
		{
			name:    "client secret and access token",
			payload: `{"client_secret":"cs-1234567890abcdef","access_token":"at-1234567890abcdef","expires_in":3600}`,
			secret:  "cs-1234567890abcdef",
			keep:    `"expires_in":3600`,
		},
		{
			name:    "secret field with escaped quotes in value",
			payload: `{"password":"with \"quoted\" inner","note":"safe"}`,
			secret:  "quoted",
			keep:    `"note":"safe"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Mask(tc.payload)
			if tc.secret != "" && strings.Contains(got, tc.secret) {
				t.Fatalf("Mask leaked secret field value %q: %q", tc.secret, got)
			}
			if tc.keep != "" && !strings.Contains(got, tc.keep) {
				t.Fatalf("Mask disturbed non-secret fields: %q", got)
			}
		})
	}
}

func TestMaskPreservesNonSecretContent(t *testing.T) {
	payload := `{"query":"what is our api_key rotation policy?","results":["docs: rotate keys quarterly"],"model":"claude-sonnet-4","trace_id":"550e8400-e29b-41d4-a716-446655440000","at":"12:34:56"}`
	got := Mask(payload)
	for _, keep := range []string{
		"what is our api_key rotation policy?",
		"claude-sonnet-4",
		"550e8400-e29b-41d4-a716-446655440000",
		"12:34:56",
	} {
		if !strings.Contains(got, keep) {
			t.Fatalf("Mask redacted harmless content %q: %q", keep, got)
		}
	}
}

func TestMaskKnownKeyPrefixes(t *testing.T) {
	cases := []struct {
		name   string
		key    string
		notKey string
	}{
		{name: "openai style", key: "sk-proj-Ab12Cd34Ef56Gh78Ij90Kl12Mn34Op56Qr78", notKey: "sk short"},
		{name: "github token", key: "ghp_AbCdEfGhIjKlMnOpQrStUvWx1234567890abcd", notKey: "ghp_short"},
		{name: "slack token", key: "xoxb-123456789012-1234567890123-abcdef", notKey: "xox-not-a-token"},
		{name: "aws access key", key: "AKIAIOSFODNN7EXAMPLE", notKey: "AKIAshort"},
		{name: "google api key", key: "AIzaSyA1234567890abcdefghijklmnopqrstuvwx", notKey: "AIzaShort"},
		{name: "langfuse secret key", key: "sk-lf-Ab12Cd34Ef56Gh78Ij90Kl12Mn34Op56", notKey: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Mask("credential " + tc.key + " end")
			if strings.Contains(got, tc.key) {
				t.Fatalf("Mask leaked prefixed key: %q", got)
			}
			if tc.notKey != "" {
				got := Mask("credential " + tc.notKey + " end")
				if !strings.Contains(got, tc.notKey) {
					t.Fatalf("Mask redacted short non-key %q: %q", tc.notKey, got)
				}
			}
		})
	}
}

func TestMaskBearerScheme(t *testing.T) {
	got := Mask(`Authorization: Bearer Ab12Cd34Ef56Gh78Ij90 basic deadbeefcafe1234`)
	if strings.Contains(got, "Ab12Cd34Ef56Gh78Ij90") || strings.Contains(got, "deadbeefcafe1234") {
		t.Fatalf("Mask leaked scheme credentials: %q", got)
	}
	lower := strings.ToLower(got)
	if !strings.Contains(lower, "bearer") || !strings.Contains(lower, "basic") {
		t.Fatalf("Mask dropped the scheme prefix: %q", got)
	}
}

func TestMaskIdempotent(t *testing.T) {
	composed := `{"entries":[{"name":"tavily","provider":"tavily","api_key":"` + mustEnvelope(t, make([]byte, 12), make([]byte, 32)) + `"}],"headers":[{"name":"Authorization","value":"Bearer sk-live-Ab12Cd34Ef56Gh78"}],"note":"12:34:56 and v1:two:short"}`
	once := Mask(composed)
	twice := Mask(once)
	if once != twice {
		t.Fatalf("Mask not idempotent:\nonce:  %q\ntwice: %q", once, twice)
	}
}

func TestMaskComposedPayload(t *testing.T) {
	// One turn-span-shaped payload mixing every credential family with
	// harmless content that must survive verbatim.
	envelope := mustEnvelope(t, make([]byte, 12), []byte("web search key ciphertext bytes"))
	composed := `{"tool":"web.search","arguments":{"query":"langfuse pricing"},"result":{"entries":[{"provider":"tavily","api_key":"` + envelope + `"}]},"mcp":{"env":[{"name":"GATEWAY_TOKEN","value":"1234567890:AAEtb0tPlaintextTokenValue1234567890-_"}]},"model":"claude-sonnet-4","turn":"ok"}`
	got := Mask(composed)
	for _, leaked := range []string{envelope, "AAEtb0tPlaintextTokenValue1234567890-_"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("Mask leaked %q from composed payload: %q", leaked, got)
		}
	}
	for _, keep := range []string{`"query":"langfuse pricing"`, `"model":"claude-sonnet-4"`, `"turn":"ok"`} {
		if !strings.Contains(got, keep) {
			t.Fatalf("Mask lost harmless content %q: %q", keep, got)
		}
	}
}

func TestMaskEmptyAndPlainText(t *testing.T) {
	if got := Mask(""); got != "" {
		t.Fatalf("Mask(\"\") = %q, want \"\"", got)
	}
	plain := "The agent answered in three sentences about workspace settings."
	if got := Mask(plain); got != plain {
		t.Fatalf("Mask altered plain prose: %q", got)
	}
}
