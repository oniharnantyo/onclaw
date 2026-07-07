package observability_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/observability"
)

func TestSetup_Empty(t *testing.T) {
	ctx := context.Background()
	cfg := observability.Config{}
	flush, err := observability.Setup(ctx, cfg, nil)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if flush != nil {
		t.Errorf("expected nil flush, got non-nil")
	}
}

func TestSetup_Incomplete(t *testing.T) {
	ctx := context.Background()

	// Missing public and secret key
	cfg1 := observability.Config{Host: "http://localhost"}
	_, err := observability.Setup(ctx, cfg1, nil)
	if err == nil {
		t.Error("expected error for incomplete config")
	} else if !strings.Contains(err.Error(), "public_key") || !strings.Contains(err.Error(), "secret_key") {
		t.Errorf("unexpected error message: %v", err)
	}

	// Missing secret key
	cfg2 := observability.Config{Host: "http://localhost", PublicKey: "pk-123"}
	_, err = observability.Setup(ctx, cfg2, nil)
	if err == nil {
		t.Error("expected error for incomplete config")
	} else if !strings.Contains(err.Error(), "secret_key") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSetup_SuccessAndMasking(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := observability.Config{
		Host:      server.URL,
		PublicKey: "pk-test",
		SecretKey: "sk-test",
		Mask:      true,
	}

	redactedStr := ""
	maskFunc := func(s string) string {
		redactedStr = s + "-masked"
		return redactedStr
	}

	flush, err := observability.Setup(ctx, cfg, maskFunc)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if flush == nil {
		t.Fatal("expected non-nil flush")
	}

	// Run flush to ensure it doesn't panic
	flush()
}
