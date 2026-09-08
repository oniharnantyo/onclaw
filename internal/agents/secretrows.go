package agents

import (
	"fmt"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/secrets"
)

// Shared secret helpers for workspace-scoped credentials (design.md D4/D6):
// one implementation, two consumers — ToolSettingsService's catalog config
// fields and MCPSettingsService's name-keyed env/header rows. Every secret is
// encrypted with the workspace ID as AAD (the same key derivation as workspace
// provider keys), hints are the last-4 plaintext tail recorded client-side,
// and no read path ever returns a full secret value.

// secretAAD returns the additional authenticated data binding a secret to its
// tenant: the workspace ID, for every workspace-scoped secret in the system.
func secretAAD(workspaceID string) []byte {
	return []byte(workspaceID)
}

// encryptSecretValue encrypts one plaintext secret into a "v1:..." envelope.
// A failing encryption is an error, never a silent plaintext store.
func encryptSecretValue(encKey []byte, aad []byte, plaintext string) (string, error) {
	envelope, err := secrets.Encrypt(encKey, aad, []byte(plaintext))
	if err != nil {
		return "", fmt.Errorf("encrypt secret: %w", err)
	}
	return envelope, nil
}

// decryptSecretValue decrypts a "v1:..." envelope back to plaintext.
func decryptSecretValue(encKey []byte, aad []byte, envelope string) (string, error) {
	plaintext, err := secrets.Decrypt(encKey, aad, envelope)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// lastSecretChars returns the last n characters of a secret for the hint.
func lastSecretChars(value string, n int) string {
	if len(value) > n {
		return value[len(value)-n:]
	}
	return value
}

// isSecretEnvelope reports whether a config value is already an encrypted
// envelope ("v1:...") rather than plaintext.
func isSecretEnvelope(value string) bool {
	return strings.HasPrefix(value, secrets.Version1Prefix)
}

// secretHint returns the client-safe last-4 hint for a secret as stored at
// rest: envelopes are decrypted to recover the hinted tail, undecryptable
// envelopes hint as empty rather than leaking, and plaintext rows (tests or
// legacy flows) hint from their own tail without the value ever being
// returned whole.
func secretHint(encKey []byte, aad []byte, value string) string {
	if value == "" {
		return ""
	}
	if isSecretEnvelope(value) {
		plaintext, err := decryptSecretValue(encKey, aad, value)
		if err != nil {
			return ""
		}
		return lastSecretChars(plaintext, 4)
	}
	return lastSecretChars(value, 4)
}

// mergeSecretRows reconciles incoming name-keyed secret rows against the
// stored ones (design.md D4): rows merge on NAME (unique within a server's
// list), an incoming row with an empty value keeps the stored secret for that
// name, and a non-empty value replaces it. A nil incoming list omits the rows
// entirely (replace semantics); an empty list clears them.
func mergeSecretRows(incoming, stored []domain.EnvRow) []domain.EnvRow {
	if incoming == nil {
		return nil
	}
	storedByName := make(map[string]string, len(stored))
	for _, row := range stored {
		storedByName[row.Name] = row.Value
	}
	out := make([]domain.EnvRow, len(incoming))
	for i, row := range incoming {
		if row.Value == "" {
			if kept, ok := storedByName[row.Name]; ok {
				row.Value = kept
			}
		}
		out[i] = row
	}
	return out
}

// encryptSecretRows encrypts every plaintext secret row value in place —
// values that are already envelopes (stored ciphertext merged back in, or
// supplied pre-encrypted) pass through untouched.
func encryptSecretRows(encKey []byte, aad []byte, rows []domain.EnvRow) error {
	for i := range rows {
		value := rows[i].Value
		if value == "" || isSecretEnvelope(value) {
			continue
		}
		envelope, err := encryptSecretValue(encKey, aad, value)
		if err != nil {
			return fmt.Errorf("encrypt secret row %q: %w", rows[i].Name, err)
		}
		rows[i].Value = envelope
	}
	return nil
}

// decryptSecretRows decrypts every envelope row value in place for runtime
// use. Rows that fail to decrypt are left as-is so plaintext values written
// by tests or older flows keep working.
func decryptSecretRows(encKey []byte, aad []byte, rows []domain.EnvRow) {
	for i := range rows {
		if !isSecretEnvelope(rows[i].Value) {
			continue
		}
		if plaintext, err := decryptSecretValue(encKey, aad, rows[i].Value); err == nil {
			rows[i].Value = plaintext
		}
	}
}

// hintSecretRows replaces every row value with its last-4 hint in place — the
// read-view shape where no plaintext or ciphertext crosses to clients.
func hintSecretRows(encKey []byte, aad []byte, rows []domain.EnvRow) {
	for i := range rows {
		rows[i].Value = secretHint(encKey, aad, rows[i].Value)
	}
}
