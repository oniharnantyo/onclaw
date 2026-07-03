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

func TestBuildConfig_Masking(t *testing.T) {
	dummyMask := func(s string) string { return s + "-masked" }

	// 1. Mask = true, maskFunc != nil -> should set MaskFunc
	cfg1 := observability.Config{
		Host:      "http://localhost",
		PublicKey: "pk-1",
		SecretKey: "sk-1",
		Mask:      true,
	}
	lfCfg1 := observability.BuildConfig(cfg1, dummyMask)
	if lfCfg1.MaskFunc == nil {
		t.Error("expected MaskFunc to be configured when Mask is true and maskFunc is provided")
	} else if lfCfg1.MaskFunc("test") != "test-masked" {
		t.Errorf("unexpected MaskFunc behavior, got %q", lfCfg1.MaskFunc("test"))
	}

	// 2. Mask = false, maskFunc != nil -> should NOT set MaskFunc
	cfg2 := observability.Config{
		Host:      "http://localhost",
		PublicKey: "pk-1",
		SecretKey: "sk-1",
		Mask:      false,
	}
	lfCfg2 := observability.BuildConfig(cfg2, dummyMask)
	if lfCfg2.MaskFunc != nil {
		t.Error("expected MaskFunc to be nil when Mask is false")
	}

	// 3. Mask = true, maskFunc = nil -> should NOT set MaskFunc (no panic)
	cfg3 := observability.Config{
		Host:      "http://localhost",
		PublicKey: "pk-1",
		SecretKey: "sk-1",
		Mask:      true,
	}
	lfCfg3 := observability.BuildConfig(cfg3, nil)
	if lfCfg3.MaskFunc != nil {
		t.Error("expected MaskFunc to be nil when maskFunc is nil")
	}
}
