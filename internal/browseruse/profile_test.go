package browseruse

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateBrowserProfile(t *testing.T) {
	for _, name := range []string{"a", "0", "work", "work-1", "a_b", "a0123456789012345678901234567890"} {
		require.NoError(t, ValidateBrowserProfile(name), name)
	}
	for _, name := range []string{"", "A", "-x", "_x", ".x", "a b", "../etc", "My Profile", "a/b", "work!", "a01234567890123456789012345678901"} {
		require.Error(t, ValidateBrowserProfile(name), name)
	}
}

func TestProfileDir(t *testing.T) {
	dir, err := ProfileDir("/home/u/.buddies/browser", "work")
	require.NoError(t, err)
	require.Equal(t, filepath.Join("/home/u/.buddies/browser", "profiles", "work"), dir)

	_, err = ProfileDir("/home/u/.buddies/browser", "../etc")
	require.Error(t, err, "invalid names must be refused before any path is returned")
}

func TestResolveProfile(t *testing.T) {
	cases := []struct {
		name       string
		arg        string
		configured string
		want       string
		wantErr    bool
	}{
		{"explicit wins", "work", "other", "work", false},
		{"configured used when arg empty", "", "work", "work", false},
		{"default fallback", "", "", DefaultBrowserProfile, false},
		{"explicit invalid", "Nope", "", "", true},
		{"configured invalid", "", "Big Work", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveProfile(tc.arg, tc.configured)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}
