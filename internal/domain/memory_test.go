package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

func TestValidateMemoryContent(t *testing.T) {
	if err := domain.ValidateMemoryContent(""); err != nil {
		t.Errorf("ValidateMemoryContent(\"\") expected nil error, got %v", err)
	}
	if err := domain.ValidateMemoryContent(strings.Repeat("a", domain.MaxMemoryContentChars)); err != nil {
		t.Errorf("content at exactly the cap expected nil error, got %v", err)
	}

	err := domain.ValidateMemoryContent(strings.Repeat("a", domain.MaxMemoryContentChars+1))
	if !errors.Is(err, domain.ErrMemoryCapExceeded) {
		t.Fatalf("expected ErrMemoryCapExceeded, got %v", err)
	}
	// Error must name the current size, the cap, and the trim escape hatch.
	msg := err.Error()
	for _, want := range []string{"32001", "32000", "trim it via the memory editor"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message %q must contain %q", msg, want)
		}
	}
}

func TestValidateMemoryAppend(t *testing.T) {
	if err := domain.ValidateMemoryAppend("", "anything"); err != nil {
		t.Errorf("append into empty doc expected nil error, got %v", err)
	}
	// Resulting document exactly at the cap is allowed.
	current := strings.Repeat("a", domain.MaxMemoryContentChars-4)
	if err := domain.ValidateMemoryAppend(current, "abcd"); err != nil {
		t.Errorf("append resulting at exactly the cap expected nil error, got %v", err)
	}

	err := domain.ValidateMemoryAppend(current, "abcde")
	if !errors.Is(err, domain.ErrMemoryCapExceeded) {
		t.Fatalf("expected ErrMemoryCapExceeded, got %v", err)
	}
	msg := err.Error()
	for _, want := range []string{"32001", "32000", "trim it via the memory editor"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message %q must contain %q", msg, want)
		}
	}

	// A fragment over the cap alone is rejected even against an empty doc.
	if err := domain.ValidateMemoryAppend("", strings.Repeat("a", domain.MaxMemoryContentChars+1)); !errors.Is(err, domain.ErrMemoryCapExceeded) {
		t.Errorf("expected ErrMemoryCapExceeded for over-cap fragment, got %v", err)
	}
}
