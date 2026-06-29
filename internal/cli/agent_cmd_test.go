package cli

import (
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/store"
)

func TestValidateReasoning(t *testing.T) {
	// Case 1: Empty settings (always valid)
	err := validateReasoning("", 0, store.ModelMetadata{})
	if err != nil {
		t.Errorf("expected nil error for empty settings, got %v", err)
	}

	// Case 2: Non-thinking model with reasoning settings (should error)
	err = validateReasoning("medium", 0, store.ModelMetadata{Thinking: false})
	if err == nil {
		t.Errorf("expected error for non-thinking model, got nil")
	}

	// Case 3: Thinking model with effort option
	effortMeta := store.ModelMetadata{
		Thinking: true,
		ReasoningOptions: []store.ReasoningOption{
			{
				Type:   "effort",
				Values: []string{"low", "medium", "high"},
			},
		},
	}
	// valid effort
	if err := validateReasoning("medium", 0, effortMeta); err != nil {
		t.Errorf("expected valid effort to pass, got: %v", err)
	}
	// invalid effort
	if err := validateReasoning("ultra-high", 0, effortMeta); err == nil {
		t.Errorf("expected invalid effort to fail, got nil")
	} else {
		expectedErrStr := "low, medium, high"
		if !strings.Contains(err.Error(), expectedErrStr) {
			t.Errorf("expected error message to contain %q, got %q", expectedErrStr, err.Error())
		}
	}
	// unsupported budget
	if err := validateReasoning("medium", 1000, effortMeta); err == nil {
		t.Errorf("expected unsupported budget to fail, got nil")
	}

	// Case 4: Thinking model with toggle option
	toggleMeta := store.ModelMetadata{
		Thinking: true,
		ReasoningOptions: []store.ReasoningOption{
			{
				Type: "toggle",
			},
		},
	}
	// valid toggles
	if err := validateReasoning("on", 0, toggleMeta); err != nil {
		t.Errorf("expected 'on' to pass, got: %v", err)
	}
	if err := validateReasoning("off", 0, toggleMeta); err != nil {
		t.Errorf("expected 'off' to pass, got: %v", err)
	}
	// invalid toggle value
	if err := validateReasoning("medium", 0, toggleMeta); err == nil {
		t.Errorf("expected invalid toggle value to fail, got nil")
	} else {
		expectedErrStr := "'on' or 'off'"
		if !strings.Contains(err.Error(), expectedErrStr) {
			t.Errorf("expected error message to contain %q, got %q", expectedErrStr, err.Error())
		}
	}

	// Case 5: Thinking model with budget option
	budgetMeta := store.ModelMetadata{
		Thinking: true,
		ReasoningOptions: []store.ReasoningOption{
			{
				Type: "budget_tokens",
				Min:  1024,
				Max:  4096,
			},
		},
	}
	// valid budget
	if err := validateReasoning("", 2048, budgetMeta); err != nil {
		t.Errorf("expected valid budget to pass, got: %v", err)
	}
	// budget too low
	if err := validateReasoning("", 512, budgetMeta); err == nil {
		t.Errorf("expected budget too low to fail, got nil")
	} else {
		expectedErrStr := "between 1024 and 4096 tokens"
		if !strings.Contains(err.Error(), expectedErrStr) {
			t.Errorf("expected error message to contain %q, got %q", expectedErrStr, err.Error())
		}
	}
	// budget too high
	if err := validateReasoning("", 8192, budgetMeta); err == nil {
		t.Errorf("expected budget too high to fail, got nil")
	} else {
		expectedErrStr := "between 1024 and 4096 tokens"
		if !strings.Contains(err.Error(), expectedErrStr) {
			t.Errorf("expected error message to contain %q, got %q", expectedErrStr, err.Error())
		}
	}
	// unsupported effort
	if err := validateReasoning("medium", 2048, budgetMeta); err == nil {
		t.Errorf("expected unsupported effort to fail, got nil")
	}
}
