package logging_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/logging"
)

func TestNewDefaultIsTextInfo(t *testing.T) {
	var buf bytes.Buffer
	l, err := logging.New("", "", &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	l.Debug("hidden") // below default info -> must not appear
	l.Info("hello")
	out := buf.String()
	if !strings.Contains(out, "hello") {
		t.Errorf("expected output to contain hello, got %q", out)
	}
	if strings.Contains(out, "hidden") {
		t.Errorf("debug output leaked at info level: %q", out)
	}
}

func TestNewJSONHandler(t *testing.T) {
	var buf bytes.Buffer
	l, err := logging.New("debug", "json", &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	l.Info("hi")
	if !strings.Contains(buf.String(), `"msg":"hi"`) {
		t.Errorf("expected JSON output, got %q", buf.String())
	}
}

func TestNewRejectsBadFormat(t *testing.T) {
	if _, err := logging.New("info", "xml", &bytes.Buffer{}); err == nil {
		t.Fatal("expected error for unsupported format")
	}
}

func TestNewRedactsSecrets(t *testing.T) {
	var buf bytes.Buffer
	l, err := logging.New("info", "text", &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 1. Redact key by name (api_key, token, password, etc)
	l.Info("user config", "api_key", "my-plaintext-api-key")
	out := buf.String()
	if !strings.Contains(out, "api_key=***") {
		t.Errorf("expected api_key to be redacted, got: %q", out)
	}
	if strings.Contains(out, "my-plaintext-api-key") {
		t.Errorf("leaked key by name: %q", out)
	}

	buf.Reset()
	l.Info("user config", "secret_key", "my-plaintext-secret-key")
	outSecret := buf.String()
	if !strings.Contains(outSecret, "secret_key=***") {
		t.Errorf("expected secret_key to be redacted, got: %q", outSecret)
	}
	if strings.Contains(outSecret, "my-plaintext-secret-key") {
		t.Errorf("leaked secret key by name: %q", outSecret)
	}

	// 2. Redact value by regex pattern (sk-...)
	buf.Reset()
	l.Info("calling api", "payload", "key is sk-ant-somekey12345678901234567890")
	out2 := buf.String()
	if !strings.Contains(out2, "key is [REDACTED]") {
		t.Errorf("expected value pattern to be redacted, got: %q", out2)
	}
	if strings.Contains(out2, "sk-ant-somekey") {
		t.Errorf("leaked value pattern: %q", out2)
	}
}
