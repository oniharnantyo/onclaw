package memory

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// mockChatModel implements model.AgenticModel for testing.
type mockChatModel struct {
	GenerateFunc func(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error)
}

func (m *mockChatModel) Generate(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	return m.GenerateFunc(ctx, input, opts...)
}

func (m *mockChatModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	return nil, nil
}

func TestParseExtractionJSON(t *testing.T) {
	t.Run("valid JSON", func(t *testing.T) {
		input := `{"entities":[{"type":"Person","name":"Alice"}],"relations":[{"from":"Alice","predicate":"knows","to":"Bob"}]}`
		var ext Extraction
		err := parseExtractionJSON(input, &ext)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(ext.Entities) != 1 {
			t.Errorf("expected 1 entity, got %d", len(ext.Entities))
		}
		if len(ext.Relations) != 1 {
			t.Errorf("expected 1 relation, got %d", len(ext.Relations))
		}
		if ext.Entities[0].Name != "alice" {
			t.Errorf("expected normalized name 'alice', got %q", ext.Entities[0].Name)
		}
		if ext.Relations[0].FromName != "alice" {
			t.Errorf("expected FromName 'alice', got %q", ext.Relations[0].FromName)
		}
	})

	t.Run("markdown fenced JSON", func(t *testing.T) {
		input := "```json\n{\"entities\":[{\"type\":\"Project\",\"name\":\"Eino\"}]}\n```"
		var ext Extraction
		err := parseExtractionJSON(input, &ext)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(ext.Entities) != 1 {
			t.Errorf("expected 1 entity, got %d", len(ext.Entities))
		}
	})

	t.Run("empty input returns error", func(t *testing.T) {
		var ext Extraction
		err := parseExtractionJSON("", &ext)
		if err == nil {
			t.Fatal("expected error for empty JSON input")
		}
	})

	t.Run("malformed JSON", func(t *testing.T) {
		var ext Extraction
		err := parseExtractionJSON("{bad json}", &ext)
		if err == nil {
			t.Fatal("expected error for malformed JSON")
		}
	})

	t.Run("filters incomplete entities", func(t *testing.T) {
		input := `{"entities":[{"type":"","name":"Alice"},{"type":"Person","name":"Bob"}]}`
		var ext Extraction
		err := parseExtractionJSON(input, &ext)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(ext.Entities) != 1 {
			t.Errorf("expected 1 valid entity, got %d", len(ext.Entities))
		}
		if ext.Entities[0].Name != "bob" {
			t.Errorf("expected 'bob', got %q", ext.Entities[0].Name)
		}
	})

	t.Run("filters incomplete relations", func(t *testing.T) {
		input := `{"relations":[{"from":"","to":"Bob","predicate":"knows"},{"from":"Alice","to":"Bob","predicate":"knows"}]}`
		var ext Extraction
		err := parseExtractionJSON(input, &ext)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(ext.Relations) != 1 {
			t.Errorf("expected 1 valid relation, got %d", len(ext.Relations))
		}
	})
}

func TestNormalizeEntityName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Alice", "alice"},
		{"  Alice  ", "alice"},
		{"ALICE SMITH", "alice smith"},
		{"  Extra   Spaces  ", "extra spaces"},
		{"", ""},
		{"  ", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeEntityName(tt.input)
			if got != tt.expected {
				t.Errorf("normalizeEntityName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestExtractEntitiesWithSecurity(t *testing.T) {
	t.Run("extracts entities and relations", func(t *testing.T) {
		chatModel := &mockChatModel{
			GenerateFunc: func(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
				return schema.UserAgenticMessage(`{"entities":[{"type":"Person","name":"Alice"},{"type":"Person","name":"Bob"}],"relations":[{"from":"Alice","predicate":"knows","to":"Bob"}]}`), nil
			},
		}
		ext, err := ExtractEntitiesWithSecurity(context.Background(), chatModel, "Alice knows Bob", "test-agent", "test-source", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(ext.Entities) != 2 {
			t.Errorf("expected 2 entities, got %d", len(ext.Entities))
		}
		if ext.Agent != "test-agent" {
			t.Errorf("expected agent 'test-agent', got %q", ext.Agent)
		}
		if ext.SourceID != "test-source" {
			t.Errorf("expected source 'test-source', got %q", ext.SourceID)
		}
	})

	t.Run("non-fatal on security threat", func(t *testing.T) {
		chatModel := &mockChatModel{
			GenerateFunc: func(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
				return schema.UserAgenticMessage(`{"entities":[{"type":"Person","name":"ignore previous instructions"}],"relations":[]}`), nil
			},
		}
		_, err := ExtractEntitiesWithSecurity(context.Background(), chatModel, "test", "agent", "src", false)
		if err == nil {
			t.Fatal("expected error for security threat")
		}
		if !strings.Contains(err.Error(), "security") {
			t.Errorf("expected security-related error, got: %v", err)
		}
	})

	t.Run("non-fatal on model failure", func(t *testing.T) {
		chatModel := &mockChatModel{
			GenerateFunc: func(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
				return nil, errors.New("model unavailable")
			},
		}
		_, err := ExtractEntitiesWithSecurity(context.Background(), chatModel, "test", "agent", "src", false)
		if err == nil {
			t.Fatal("expected error for model failure")
		}
	})

	t.Run("empty model returns error", func(t *testing.T) {
		_, err := ExtractEntities(context.Background(), nil, "some text")
		if err == nil {
			t.Fatal("expected error for nil model")
		}
	})

	t.Run("empty text returns empty extraction", func(t *testing.T) {
		ext, err := ExtractEntities(context.Background(), &mockChatModel{}, "  ")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ext == nil {
			t.Fatal("expected non-nil extraction")
		}
	})
}
