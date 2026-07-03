package service_test

import (
	"errors"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/api/service"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name     string
		input    error
		expected error
	}{
		{
			name:     "nil error",
			input:    nil,
			expected: nil,
		},
		{
			name:     "ErrNotFound directly",
			input:    service.ErrNotFound,
			expected: service.ErrNotFound,
		},
		{
			name:     "wrapped ErrNotFound",
			input:    errors.New("agent master: not found"),
			expected: service.ErrNotFound,
		},
		{
			name:     "database sql no rows error",
			input:    errors.New("sql: no rows in result set"),
			expected: service.ErrNotFound,
		},
		{
			name:     "arbitrary error",
			input:    errors.New("connection failed"),
			expected: errors.New("connection failed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := service.Classify(tt.input)
			if tt.expected == nil {
				if got != nil {
					t.Errorf("expected nil error, got: %v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected non-nil error, got nil")
			}
			if tt.expected == service.ErrNotFound {
				if !errors.Is(got, service.ErrNotFound) {
					t.Errorf("expected ErrNotFound, got: %v", got)
				}
			} else {
				if got.Error() != tt.expected.Error() {
					t.Errorf("expected error %q, got: %q", tt.expected.Error(), got.Error())
				}
			}
		})
	}
}
