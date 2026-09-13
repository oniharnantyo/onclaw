package observability

import (
	"regexp"
	"strconv"
	"strings"
)

// MaskPlaceholder replaces every redacted credential-shaped value.
const MaskPlaceholder = "[REDACTED]"

// Mask is the centralized, mandatory masking function applied to every
// Langfuse export (integrate-langfuse-tracing D4): the handler is always
// constructed with it, so no configuration can bypass masking. It redacts
// credential-shaped values following the repo's secret-row conventions —
//
//   - secrets envelopes ("v1:<b64 nonce>:<b64 ciphertext>", the storage form
//     shared by web-search provider keys, gateway bot tokens, MCP env/header
//     rows, and hook secret payloads — internal/secrets Encrypt format);
//   - JSON fields whose name is secret-shaped ("api_key", "Authorization",
//     "TAVILY_API_KEY", "bot_token", …) with any string value — the runtime
//     decrypts credentials to plaintext before use, so field-name redaction is
//     what stops a decrypted web-search key or MCP credential echoed into a
//     span from leaving the instance;
//   - name/value secret rows ({"name":"Authorization","value":"…"} — the MCP
//     connection and hook handler config shape) whose NAME is secret-shaped,
//     whatever the value looks like;
//   - credential value shapes in free text: Bearer/Basic scheme credentials,
//     Telegram bot tokens, and common provider key prefixes (sk-, ghp_, xox-,
//     AKIA, tvly-, nvapi-, AIza, …).
//
// Mask is a pure string→string function (the eino-ext MaskFunc signature),
// applied by the export consumer to span/generation input, output, and
// metadata — off the turn's critical path. It errs toward redaction: a
// harmless value that happens to look like a credential is masked, never the
// reverse. Masking is idempotent.
func Mask(s string) string {
	if s == "" {
		return s
	}
	out := maskSecretNameValueRows(s)
	out = maskSecretNamedJSONFields(out)
	out = envelopeRe.ReplaceAllString(out, `${1}`+MaskPlaceholder+`${3}`)
	out = telegramTokenRe.ReplaceAllString(out, `${1}`+MaskPlaceholder+`${3}`)
	out = schemeCredentialRe.ReplaceAllString(out, `${1}${2}`+MaskPlaceholder)
	out = prefixedKeyRe.ReplaceAllLiteralString(out, MaskPlaceholder)
	return out
}

// jsonFieldRe matches one JSON string-typed field: `"name" : "value"`. Fields
// with object/array values do not match as wholes, but their inner string
// fields match individually when the scan reaches them.
var jsonFieldRe = regexp.MustCompile(`"([^"\\]+)"(\s*:\s*)"(?:\\.|[^"\\])*"`)

// maskSecretNamedJSONFields redacts the value of every JSON field whose name
// is secret-shaped (isSecretFieldName), preserving the key and spacing.
func maskSecretNamedJSONFields(s string) string {
	return jsonFieldRe.ReplaceAllStringFunc(s, func(m string) string {
		parts := jsonFieldRe.FindStringSubmatch(m)
		if parts == nil || !isSecretFieldName(parts[1]) {
			return m
		}
		return `"` + parts[1] + `"` + parts[2] + `"` + MaskPlaceholder + `"`
	})
}

// nameValueRowRe matches a name/value secret row: the config shape of MCP
// connection env/header rows and hook handler headers/env
// ({"name":"Authorization","value":"…"}). Escaped names/values are carried
// through the submatches untouched.
var nameValueRowRe = regexp.MustCompile(`("name"\s*:\s*")((?:\\.|[^"\\])*)("\s*,\s*"value"\s*:\s*")((?:\\.|[^"\\])*)(")`)

// maskSecretNameValueRows redacts the value of every name/value row whose
// name is secret-shaped, whatever the value looks like — plaintext or
// envelope, an MCP credential or hook secret under a credential-ish row name
// never exports.
func maskSecretNameValueRows(s string) string {
	return nameValueRowRe.ReplaceAllStringFunc(s, func(m string) string {
		parts := nameValueRowRe.FindStringSubmatch(m)
		if parts == nil {
			return m
		}
		name, err := unquoteJSONString(parts[2])
		if err != nil || !isSecretFieldName(name) {
			return m
		}
		return parts[1] + parts[2] + parts[3] + MaskPlaceholder + parts[5]
	})
}

// envelopeRe matches a secrets envelope ("v1:<b64 nonce>:<b64 ciphertext>",
// base64 standard alphabet, realistic segment lengths) with one non-envelope
// character on each side as the boundary, so a prefix like "v1:notes:here"
// never matches. The boundary characters are preserved via capture groups.
var envelopeRe = regexp.MustCompile(`(^|[^A-Za-z0-9+/=_-])(v1:[A-Za-z0-9+/=_-]{8,}:[A-Za-z0-9+/=_-]{8,})([^A-Za-z0-9+/=_-]|$)`)

// telegramTokenRe matches a Telegram bot token ("\d{8,10}:<35 url-safe
// chars>" — the gateway bot credential's plaintext shape) with boundaries.
var telegramTokenRe = regexp.MustCompile(`(^|[^0-9A-Za-z_-])([0-9]{8,10}:[A-Za-z0-9_-]{30,})([^0-9A-Za-z_-]|$)`)

// schemeCredentialRe matches scheme-prefixed HTTP credentials ("Bearer …",
// "Basic …"), keeping the scheme.
var schemeCredentialRe = regexp.MustCompile(`(?i)\b(bearer|basic)(\s+)[A-Za-z0-9\-._~+/=]{8,}`)

// prefixedKeyRe matches credential-shaped values by their well-known provider
// prefixes: OpenAI-style sk-/pk-/rk- keys (including sk-lf-/pk-lf- Langfuse
// keys), GitHub tokens, Slack tokens, AWS access key ids, Tavily, NVIDIA,
// and Google API keys.
var prefixedKeyRe = regexp.MustCompile(`\b(?:` +
	`(?:sk|pk|rk)-[A-Za-z0-9_-]{16,}` +
	`|gh[pousr]_[A-Za-z0-9]{20,}` +
	`|github_pat_[A-Za-z0-9_]{20,}` +
	`|xox[baprs]-[A-Za-z0-9-]{10,}` +
	`|AKIA[0-9A-Z]{16}` +
	`|tvly-[A-Za-z0-9_-]{16,}` +
	`|nvapi-[A-Za-z0-9_-]{20,}` +
	`|AIza[0-9A-Za-z_\-]{30,}` +
	`)`)

// exactSecretFieldNames are field names that are secrets verbatim (compared
// normalized: lowercased, separators stripped).
var exactSecretFieldNames = map[string]bool{
	"authorization":      true,
	"proxyauthorization": true,
	"auth":               true,
	"credential":         true,
	"credentials":        true,
	"passwd":             true,
	"cookie":             true,
	"secretkey":          true,
}

// secretFieldSuffixes redact any field name ending in one of these
// (normalized): "x_api_key", "botToken", "client_secret", …. Plain "key" is
// deliberately absent — cache/trace idempotency keys are not secrets.
var secretFieldSuffixes = []string{
	"apikey",
	"apisecret",
	"apitoken",
	"accesstoken",
	"refreshtoken",
	"idtoken",
	"clientsecret",
	"privatekey",
	"bottoken",
	"sessiontoken",
	"password",
	"secret",
	"token",
	"credential",
}

// secretFieldPrefixes redact scheme-style header names and their qualified
// forms ("Authorization", "Authorization:foo").
var secretFieldPrefixes = []string{
	"authorization",
	"auth",
}

// isSecretFieldName reports whether a JSON field or secret-row name is
// credential-shaped. Normalization is case- and separator-insensitive so
// "TAVILY_API_KEY", "x-api-key", and "botToken" all match their stems.
func isSecretFieldName(name string) bool {
	norm := strings.ToLower(name)
	norm = strings.Map(func(r rune) rune {
		switch r {
		case '-', '_', ' ', '.':
			return -1
		}
		return r
	}, norm)
	if norm == "" {
		return false
	}
	if exactSecretFieldNames[norm] {
		return true
	}
	for _, prefix := range secretFieldPrefixes {
		if strings.HasPrefix(norm, prefix) {
			return true
		}
	}
	for _, suffix := range secretFieldSuffixes {
		if strings.HasSuffix(norm, suffix) {
			return true
		}
	}
	return false
}

// unquoteJSONString decodes the JSON string literal body (without the
// surrounding quotes) that row-name matching captured; a decode failure
// refuses the row rather than redacting blindly.
func unquoteJSONString(body string) (string, error) {
	return strconv.Unquote(`"` + body + `"`)
}
