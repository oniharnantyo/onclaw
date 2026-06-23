package logging

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewDefaultIsTextInfo(t *testing.T) {
	var buf bytes.Buffer
	l, err := New("", "", &buf)
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
	l, err := New("debug", "json", &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	l.Info("hi")
	if !strings.Contains(buf.String(), `"msg":"hi"`) {
		t.Errorf("expected JSON output, got %q", buf.String())
	}
}

func TestNewRejectsBadFormat(t *testing.T) {
	if _, err := New("info", "xml", &bytes.Buffer{}); err == nil {
		t.Fatal("expected error for unsupported format")
	}
}
