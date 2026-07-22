package tokens_test

import (
	"testing"

	"github.com/oniharnantyo/onclaw/internal/tokens"
)

func TestEstimate(t *testing.T) {
	tests := []struct {
		name    string
		charLen int
		want    int
	}{
		{"empty", 0, 0},
		{"exact multiple", 40, 10},
		{"rounds down on remainder", 41, 10},
		{"small input rounds to zero", 3, 0},
		{"large input", 1_000_000, 250_000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tokens.Estimate(tt.charLen); got != tt.want {
				t.Errorf("Estimate(%d) = %d, want %d", tt.charLen, got, tt.want)
			}
		})
	}
}
