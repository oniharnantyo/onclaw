// Package vault stores browser handover credentials
// (add-browser-login-handover design D5): metadata in the clear in the
// vault_items table, secrets sealed with AES-GCM under a 32-byte master
// key held in the OS keyring with a keyfile fallback. It owns the origin
// matching semantics and the redacted login views; the plaintext secret
// has exactly one consumer at a time (the settings reveal or the browser
// fill path) and is never logged or serialized.
package vault
