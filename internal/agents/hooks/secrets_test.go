package hooks

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/secrets"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, secrets.KeySize)
	for i := range key {
		key[i] = byte(i)
	}
	return key
}

func mapFields(t *testing.T, raw json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("decode config %s: %v", raw, err)
	}
	return fields
}

func TestSealHookConfig_SealsHeadersAndLeavesRestVerbatim(t *testing.T) {
	cfg := json.RawMessage(`{"url":"https://hooks.example/x","headers":[{"name":"Authorization","value":"Bearer sekrit"},{"name":"X-Trace","value":"abc"}]}`)

	sealed, err := SealHookConfig(testKey(t), "ws-1", cfg)
	if err != nil {
		t.Fatalf("SealHookConfig: %v", err)
	}

	fields := mapFields(t, sealed)
	var urlValue string
	if err := json.Unmarshal(fields["url"], &urlValue); err != nil || urlValue != "https://hooks.example/x" {
		t.Errorf("url not verbatim: %s", sealed)
	}
	var headers []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(fields["headers"], &headers); err != nil {
		t.Fatalf("decode headers: %v", err)
	}
	for _, h := range headers {
		if h.Value == "Bearer sekrit" || h.Value == "abc" {
			t.Errorf("header %q stored in plaintext", h.Name)
		}
		if !isHookSecretEnvelope(h.Value) {
			t.Errorf("header %q is not an envelope: %q", h.Name, h.Value)
		}
	}

	opened, err := OpenHookConfig(testKey(t), "ws-1", sealed)
	if err != nil {
		t.Fatalf("OpenHookConfig: %v", err)
	}
	if !bytes.Equal(normalizeJSON(t, opened), normalizeJSON(t, cfg)) {
		t.Errorf("round-trip mismatch:\n opened: %s\n want:   %s", opened, cfg)
	}
}

func TestSealHookConfig_SealsCommandEnv(t *testing.T) {
	cfg := json.RawMessage(`{"command":"/opt/hook","args":["run","--fast"],"env":[{"name":"HOOK_TOKEN","value":"v3ry-s3cret"},{"name":"MODE","value":"strict"}],"cwd":"/abs/dir"}`)

	sealed, err := SealHookConfig(testKey(t), "ws-1", cfg)
	if err != nil {
		t.Fatalf("SealHookConfig: %v", err)
	}

	fields := mapFields(t, sealed)
	for _, key := range []string{"command", "args", "cwd"} {
		if !bytes.Equal(bytes.TrimSpace(fields[key]), []byte(map[string]string{"command": `"/opt/hook"`, "args": `["run","--fast"]`, "cwd": `"/abs/dir"`}[key])) {
			t.Errorf("field %q not verbatim: %s", key, sealed)
		}
	}
	var env []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(fields["env"], &env); err != nil {
		t.Fatalf("decode env: %v", err)
	}
	if env[0].Value == "v3ry-s3cret" || !isHookSecretEnvelope(env[0].Value) {
		t.Errorf("env secret not sealed: %s", sealed)
	}
	if env[1].Value == "strict" || !isHookSecretEnvelope(env[1].Value) {
		t.Errorf("non-secret env value should still be sealed (known path): %s", sealed)
	}

	opened, err := OpenHookConfig(testKey(t), "ws-1", sealed)
	if err != nil {
		t.Fatalf("OpenHookConfig: %v", err)
	}
	if !bytes.Equal(normalizeJSON(t, opened), normalizeJSON(t, cfg)) {
		t.Errorf("round-trip mismatch:\n opened: %s\n want:   %s", opened, cfg)
	}
}

// TestSealHookConfig_Idempotent pins that already-sealed values pass through
// untouched: sealing twice yields byte-identical output to sealing once, so a
// keep-stored merge on update never double-encrypts.
func TestSealHookConfig_Idempotent(t *testing.T) {
	cfg := json.RawMessage(`{"url":"https://hooks.example/x","headers":[{"name":"Authorization","value":"Bearer sekrit"}]}`)

	once, err := SealHookConfig(testKey(t), "ws-1", cfg)
	if err != nil {
		t.Fatalf("first seal: %v", err)
	}
	twice, err := SealHookConfig(testKey(t), "ws-1", once)
	if err != nil {
		t.Fatalf("second seal: %v", err)
	}
	if !bytes.Equal(once, twice) {
		t.Errorf("sealing is not idempotent:\n once:  %s\n twice: %s", once, twice)
	}
}

func TestOpenHookConfig_WrongAADFails(t *testing.T) {
	cfg := json.RawMessage(`{"headers":[{"name":"Authorization","value":"Bearer sekrit"}]}`)
	sealed, err := SealHookConfig(testKey(t), "ws-1", cfg)
	if err != nil {
		t.Fatalf("SealHookConfig: %v", err)
	}
	if _, err := OpenHookConfig(testKey(t), "ws-other", sealed); !errors.Is(err, domain.ErrUndecryptable) {
		t.Errorf("wrong AAD: err = %v, want ErrUndecryptable", err)
	}
}

func TestOpenHookConfig_PlaintextPassesThrough(t *testing.T) {
	cfg := json.RawMessage(`{"command":"sh","env":[{"name":"K","value":"plain"}]}`)
	opened, err := OpenHookConfig(testKey(t), "ws-1", cfg)
	if err != nil {
		t.Fatalf("OpenHookConfig: %v", err)
	}
	if !bytes.Equal(normalizeJSON(t, opened), normalizeJSON(t, cfg)) {
		t.Errorf("plaintext config changed: %s", opened)
	}
}

func TestWalkHookConfigSecrets_EdgeInputs(t *testing.T) {
	tests := []struct {
		name string
		cfg  json.RawMessage
	}{
		{name: "empty config", cfg: nil},
		{name: "null config", cfg: json.RawMessage("null")},
		{name: "empty object", cfg: json.RawMessage(`{}`)},
		{name: "headers not a row array stays verbatim", cfg: json.RawMessage(`{"headers":"weird"}`)},
		{name: "non-string row values stay verbatim", cfg: json.RawMessage(`{"env":[{"name":"K","value":42}]}`)},
	}
	key := testKey(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sealed, err := SealHookConfig(key, "ws-1", tt.cfg)
			if err != nil {
				t.Fatalf("SealHookConfig: %v", err)
			}
			opened, err := OpenHookConfig(key, "ws-1", sealed)
			if err != nil {
				t.Fatalf("OpenHookConfig: %v", err)
			}
			if len(tt.cfg) == 0 {
				return
			}
			if !bytes.Equal(normalizeJSON(t, opened), normalizeJSON(t, tt.cfg)) {
				t.Errorf("config changed:\n opened: %s\n want:   %s", opened, tt.cfg)
			}
		})
	}
}

// normalizeJSON decodes and re-encodes so field order (which json.Marshal of
// a map sorts) does not matter in comparisons.
func normalizeJSON(t *testing.T, raw json.RawMessage) []byte {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return out
}
