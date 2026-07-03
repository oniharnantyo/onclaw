package web_test

import (
	"testing"

	"github.com/oniharnantyo/onclaw/internal/web"
)

func TestValidateURLNotInternal(t *testing.T) {
	tests := []struct {
		urlStr  string
		wantErr bool
	}{
		// Public URLs
		{"http://example.com", false},
		{"https://google.com/search?q=test", false},
		{"https://1.1.1.1", false},
		{"http://8.8.8.8", false},

		// Blocked URLs
		{"http://localhost", true},
		{"http://127.0.0.1", true},
		{"http://[::1]", true},
		{"http://192.168.1.1", true},
		{"http://10.0.0.1", true},
		{"http://172.16.0.1", true},
		{"http://169.254.169.254", true}, // AWS / cloud metadata
		{"http://0.0.0.0", true},         // unspecified

		// Invalid schemes
		{"ftp://example.com", true},
		{"file:///etc/passwd", true},
		{"mailto:test@example.com", true},

		// Malformed
		{"http://%20", true},
		{"", true},
	}

	for _, tt := range tests {
		err := web.ValidateURLNotInternal(tt.urlStr)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateURLNotInternal(%q) error = %v, wantErr %v", tt.urlStr, err, tt.wantErr)
		}
	}
}
