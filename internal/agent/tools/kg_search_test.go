package tools_test

import (
	"context"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/agent/tools"
	"github.com/oniharnantyo/onclaw/internal/memory"
)

// mockKGStore is a test double for KGStore
type mockKGStore struct {
	searchGraphFunc func(ctx context.Context, query *memory.KGQuery) ([]memory.KGHit, error)
}

func (m *mockKGStore) IngestExtraction(ctx context.Context, ext *memory.Extraction) error {
	return nil
}

func (m *mockKGStore) DedupAfterExtraction(ctx context.Context, agentID string) error {
	return nil
}

func (m *mockKGStore) SearchGraph(ctx context.Context, query *memory.KGQuery) ([]memory.KGHit, error) {
	if m.searchGraphFunc != nil {
		return m.searchGraphFunc(ctx, query)
	}
	return nil, nil
}

func TestKGSearchTool_ReturnsConnectedEntities(t *testing.T) {
	tool := &tools.KGSearchTool{}

	// Create a scope with mock KGStore
	scope := &tools.Scope{
		AgentName: "test-agent",
		KGStore: &mockKGStore{
			searchGraphFunc: func(ctx context.Context, query *memory.KGQuery) ([]memory.KGHit, error) {
				// Return sample connected entities
				return []memory.KGHit{
					{
						Entity: &memory.Entity{
							ID:        1,
							Agent:     "test-agent",
							Name:      "Entity1",
							Type:      "Person",
							ValidFrom: "2025-01-01T00:00:00Z",
						},
						Path: []memory.Relation{
							{Predicate: "RELATES_TO"},
						},
						Distance: 1,
					},
					{
						Entity: &memory.Entity{
							ID:        2,
							Agent:     "test-agent",
							Name:      "Entity2",
							Type:      "Organization",
							ValidFrom: "2025-01-01T00:00:00Z",
						},
						Path: []memory.Relation{
							{Predicate: "RELATES_TO"},
							{Predicate: "WORKS_FOR"},
						},
						Distance: 2,
					},
				}, nil
			},
		},
	}

	// Build and invoke the tool
	invokable := tool.Build(scope)
	result, err := invokable.InvokableRun(context.Background(), `{"seed_entity_name":"Seed"}`)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify result contains entity information
	resultStr := result
	if !strings.Contains(resultStr, "Entity1") || !strings.Contains(resultStr, "Person") {
		t.Errorf("expected result to contain 'Entity1' and 'Person', got: %s", resultStr)
	}
	if !strings.Contains(resultStr, "Entity2") || !strings.Contains(resultStr, "Organization") {
		t.Errorf("expected result to contain 'Entity2' and 'Organization', got: %s", resultStr)
	}
	if !strings.Contains(resultStr, "RELATES_TO") || !strings.Contains(resultStr, "WORKS_FOR") {
		t.Errorf("expected result to contain relation paths, got: %s", resultStr)
	}
}

func TestKGSearchTool_RespectsDepthBound(t *testing.T) {
	tool := &tools.KGSearchTool{}

	maxDepthCaptured := 0
	scope := &tools.Scope{
		AgentName: "test-agent",
		KGStore: &mockKGStore{
			searchGraphFunc: func(ctx context.Context, query *memory.KGQuery) ([]memory.KGHit, error) {
				maxDepthCaptured = query.MaxDepth
				return []memory.KGHit{}, nil
			},
		},
	}

	invokable := tool.Build(scope)

	// Test with max_depth = 2
	_, err := invokable.InvokableRun(context.Background(), `{"seed_entity_name":"Seed","max_depth":2}`)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if maxDepthCaptured != 2 {
		t.Errorf("expected max_depth 2 to be passed, got %d", maxDepthCaptured)
	}

	// Test with default max_depth
	maxDepthCaptured = 0
	_, err = invokable.InvokableRun(context.Background(), `{"seed_entity_name":"Seed"}`)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if maxDepthCaptured != 3 { // default should be 3
		t.Errorf("expected default max_depth 3, got %d", maxDepthCaptured)
	}
}

func TestKGSearchTool_EmptyGraphReturnsEmptyResult(t *testing.T) {
	tool := &tools.KGSearchTool{}

	scope := &tools.Scope{
		AgentName: "test-agent",
		KGStore: &mockKGStore{
			searchGraphFunc: func(ctx context.Context, query *memory.KGQuery) ([]memory.KGHit, error) {
				// Return empty results
				return []memory.KGHit{}, nil
			},
		},
	}

	invokable := tool.Build(scope)
	result, err := invokable.InvokableRun(context.Background(), `{"seed_entity_name":"NonExistent"}`)

	if err != nil {
		t.Fatalf("expected no error for empty graph, got %v", err)
	}

	if !strings.Contains(result, "No connected entities found") {
		t.Errorf("expected 'No connected entities found' message, got: %s", result)
	}
}

func TestKGSearchTool_MissingSeedEntityNameReturnsObservation(t *testing.T) {
	tool := &tools.KGSearchTool{}

	scope := &tools.Scope{
		AgentName: "test-agent",
		KGStore:   &mockKGStore{},
	}

	invokable := tool.Build(scope)
	result, err := invokable.InvokableRun(context.Background(), `{}`)

	// An empty required field is an expected, recoverable condition: it must
	// surface as a tool-result observation (nil error), not a fatal error.
	if err != nil {
		t.Errorf("expected nil error (recoverable observation) for missing seed_entity_name, got: %v", err)
	}
	if !strings.Contains(result, "seed_entity_name is required") {
		t.Errorf("expected 'seed_entity_name is required' observation, got: %v", result)
	}
}

func TestKGSearchTool_NilKGStoreReturnsUnavailableMessage(t *testing.T) {
	tool := &tools.KGSearchTool{}

	scope := &tools.Scope{
		AgentName: "test-agent",
		KGStore:   nil, // Simulate unavailable KGStore
	}

	invokable := tool.Build(scope)
	result, err := invokable.InvokableRun(context.Background(), `{"seed_entity_name":"Seed"}`)

	if err != nil {
		t.Fatalf("expected no error for nil KGStore, got %v", err)
	}

	if result != "Knowledge graph is not available." {
		t.Errorf("expected 'Knowledge graph is not available.' message, got: %s", result)
	}
}
