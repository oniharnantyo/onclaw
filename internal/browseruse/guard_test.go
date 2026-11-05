package browseruse

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateNavigateURL(t *testing.T) {
	public := "http://93.184.216.34/" // literal public IP: no DNS in tests
	cases := []struct {
		name         string
		raw          string
		allowPrivate bool
		wantErr      string
	}{
		{"public literal ip", public, false, ""},
		{"https public literal ip", "https://93.184.216.34/x", false, ""},
		{"loopback refused", "http://127.0.0.1:9245/", false, "non-public"},
		{"private refused", "http://10.0.0.5/", false, "non-public"},
		{"ipv6 loopback refused", "http://[::1]/", false, "non-public"},
		{"link-local refused", "http://169.254.169.254/latest/meta-data", false, "non-public"},
		{"loopback allowed private", "http://127.0.0.1:9245/", true, ""},
		{"ipv6 loopback allowed private", "http://[::1]:9245/", true, ""},
		{"file refused even private", "file:///etc/passwd", true, "only http and https"},
		{"ftp refused", "ftp://example.com/x", false, "only http and https"},
		{"no host refused", "https://", false, "no host"},
		{"relative refused", "not-a-url", false, "only http and https"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u, err := validateNavigateURL(context.Background(), tc.raw, tc.allowPrivate)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, u)
		})
	}
}
