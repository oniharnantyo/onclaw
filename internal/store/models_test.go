package store_test

import (
	"testing"

	"github.com/oniharnantyo/onclaw/internal/store"
)

func TestMarshalUnmarshalModelMetadata(t *testing.T) {
	meta := &store.ModelMetadata{
		ContextWindow:   16384,
		Thinking:        true,
		InputModalities: []string{"text", "image"},
		ReasoningOptions: []store.ReasoningOption{
			{
				Type:   "effort",
				Values: []string{"low", "medium", "high"},
			},
			{
				Type: "budget_tokens",
				Min:  1024,
				Max:  8192,
			},
		},
	}

	metaJSON, err := store.MarshalModelMetadata(meta)
	if err != nil {
		t.Fatalf("failed to marshal model metadata: %v", err)
	}

	unmarshaled, err := store.UnmarshalModelMetadata(metaJSON)
	if err != nil {
		t.Fatalf("failed to unmarshal model metadata: %v", err)
	}

	if unmarshaled.ContextWindow != meta.ContextWindow {
		t.Errorf("expected context window %d, got %d", meta.ContextWindow, unmarshaled.ContextWindow)
	}
	if unmarshaled.Thinking != meta.Thinking {
		t.Errorf("expected thinking %t, got %t", meta.Thinking, unmarshaled.Thinking)
	}
	if len(unmarshaled.InputModalities) != len(meta.InputModalities) {
		t.Errorf("expected %d input modalities, got %d", len(meta.InputModalities), len(unmarshaled.InputModalities))
	}
	if len(unmarshaled.ReasoningOptions) != len(meta.ReasoningOptions) {
		t.Errorf("expected %d reasoning options, got %d", len(meta.ReasoningOptions), len(unmarshaled.ReasoningOptions))
	}

	// Test unmarshalling empty JSON string
	emptyMeta, err := store.UnmarshalModelMetadata("")
	if err != nil {
		t.Fatalf("failed to unmarshal empty model metadata: %v", err)
	}
	if len(emptyMeta.InputModalities) != 1 || emptyMeta.InputModalities[0] != "text" {
		t.Errorf("expected default modality 'text', got %v", emptyMeta.InputModalities)
	}
}
