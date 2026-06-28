package secrets

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestKeyManager(t *testing.T) {
	dek, err := GenerateDEK()
	if err != nil {
		t.Fatalf("failed to generate DEK: %v", err)
	}

	km := NewKeyManager(dek)

	// 1. GetDEK
	if !bytes.Equal(km.GetDEK(), dek) {
		t.Error("GetDEK mismatch")
	}

	// 2. Encrypt/Decrypt
	pt := []byte("hello key manager")
	ct, err := km.Encrypt(pt)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}
	dec, err := km.Decrypt(ct)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}
	if !bytes.Equal(pt, dec) {
		t.Errorf("decrypted mismatch: got %s, want %s", dec, pt)
	}

	// 3. SwitchToKeyfile
	tempDir, err := os.MkdirTemp("", "secrets-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	keyfilePath := filepath.Join(tempDir, "master.key")
	newWrappedKf, err := km.SwitchToKeyfile(keyfilePath)
	if err != nil {
		t.Fatalf("SwitchToKeyfile failed: %v", err)
	}
	kfKek, err := GetOrCreateKeyfileKEK(keyfilePath)
	if err != nil {
		t.Fatalf("failed to get/create keyfile KEK: %v", err)
	}
	unwrappedKf, err := UnwrapDEK(newWrappedKf, kfKek)
	if err != nil {
		t.Fatalf("failed to unwrap keyfile DEK: %v", err)
	}
	if !bytes.Equal(dek, unwrappedKf) {
		t.Error("unwrapped DEK mismatch after SwitchToKeyfile")
	}

	// 4. Verify keyfile was created
	kekData, err := os.ReadFile(keyfilePath)
	if err != nil {
		t.Fatalf("failed to read keyfile: %v", err)
	}
	if len(kekData) != 32 {
		t.Errorf("keyfile has wrong size: got %d, want 32", len(kekData))
	}
}
