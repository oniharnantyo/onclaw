package hooks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/secrets"
)

// Handler config shapes. These are the storage shapes of the `config` JSONB
// column per handler type — the REST layer builds its dialogs from exactly
// these fields, and the secret sealing below walks only the row-array keys.
// Every handler receives the DECRYPTED form at execution time.
//
//	http:    {"url":"https://…","headers":[{"name":"Authorization","value":"…"}]}
//	command: {"command":"…","args":["…"],"env":[{"name":"K","value":"…"}],"cwd":"/abs/optional"}
//
// Secret values (http headers[].value, command env[].value) are stored as
// inline AES-GCM envelopes ("v1:<nonce>:<ciphertext>") inside the config —
// the web-search-stacks precedent (internal/agents/secretrows.go) — with the
// workspace ID as AAD. Read endpoints must never return full values; updates
// keep stored envelopes unless a row is replaced with a new plaintext value.
const (
	secretPathHeaders = "headers" // http handler: [{"name","value"}]
	secretPathEnv     = "env"     // command handler: [{"name","value"}]
)

// SealHookConfig returns cfg with every plaintext secret value at the known
// secret paths (headers[].value, env[].value) replaced by an AES-GCM envelope
// keyed by encKey and bound to aadWorkspaceID. All other fields — and every
// non-string row value — are carried through verbatim. Sealing is
// idempotent: values that are already envelopes pass through untouched.
func SealHookConfig(encKey []byte, aadWorkspaceID string, cfg json.RawMessage) (json.RawMessage, error) {
	return walkHookConfigSecrets(cfg, func(value string) (string, error) {
		if value == "" || isHookSecretEnvelope(value) {
			return value, nil
		}
		envelope, err := secrets.Encrypt(encKey, []byte(aadWorkspaceID), []byte(value))
		if err != nil {
			return "", fmt.Errorf("seal hook secret: %w", err)
		}
		return envelope, nil
	})
}

// OpenHookConfig returns cfg with every envelope at the known secret paths
// decrypted back to plaintext. Plaintext values (no envelope prefix) pass
// through untouched so test-authored configs work unchanged; an envelope that
// fails to decrypt is an error rather than a silent ciphertext hand-off to a
// webhook or child process.
func OpenHookConfig(encKey []byte, aadWorkspaceID string, cfg json.RawMessage) (json.RawMessage, error) {
	return walkHookConfigSecrets(cfg, func(value string) (string, error) {
		if !isHookSecretEnvelope(value) {
			return value, nil
		}
		plaintext, err := secrets.Decrypt(encKey, []byte(aadWorkspaceID), value)
		if err != nil {
			return "", fmt.Errorf("open hook secret: %w", err)
		}
		return string(plaintext), nil
	})
}

// isHookSecretEnvelope reports whether a config value is already an encrypted
// envelope ("v1:...") rather than plaintext — same convention as the MCP and
// tool-settings secret rows.
func isHookSecretEnvelope(value string) bool {
	return strings.HasPrefix(value, secrets.Version1Prefix+":")
}

// hookConfigSecretRow is one name-keyed row inside a secret path. Value stays
// raw so non-string values are re-emitted byte-for-byte.
type hookConfigSecretRow struct {
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
}

// walkHookConfigSecrets decodes cfg as a JSON object, applies transform to
// every string value at the known secret paths, and re-marshals. Fields other
// than the secret paths keep their exact raw bytes; an empty or null config
// passes through unchanged.
func walkHookConfigSecrets(cfg json.RawMessage, transform func(string) (string, error)) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(cfg)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return cfg, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return nil, fmt.Errorf("%w: config: must be a JSON object", domain.ErrInvalid)
	}
	changed := false
	for _, path := range []string{secretPathHeaders, secretPathEnv} {
		raw, ok := fields[path]
		if !ok {
			continue
		}
		var rows []hookConfigSecretRow
		if err := json.Unmarshal(bytes.TrimSpace(raw), &rows); err != nil {
			// Not a row array (wrong handler shape or absent): leave verbatim;
			// handler-type validation owns the shape error.
			continue
		}
		rowsChanged := false
		for i, row := range rows {
			var value string
			if err := json.Unmarshal(bytes.TrimSpace(row.Value), &value); err != nil {
				continue // non-string value: verbatim
			}
			transformed, err := transform(value)
			if err != nil {
				return nil, err
			}
			if transformed == value {
				continue
			}
			encoded, err := json.Marshal(transformed)
			if err != nil {
				return nil, fmt.Errorf("encode hook secret: %w", err)
			}
			rows[i].Value = encoded
			rowsChanged = true
		}
		if rowsChanged {
			encoded, err := json.Marshal(rows)
			if err != nil {
				return nil, fmt.Errorf("encode hook secret rows: %w", err)
			}
			fields[path] = encoded
			changed = true
		}
	}
	if !changed {
		return cfg, nil
	}
	out, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("encode hook config: %w", err)
	}
	return out, nil
}
