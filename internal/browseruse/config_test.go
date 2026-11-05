package browseruse

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecodeStoredConfig(t *testing.T) {
	t.Run("empty document decodes to the defaults", func(t *testing.T) {
		cfg, err := DecodeStoredConfig("")
		require.NoError(t, err)
		require.Equal(t, DefaultConfig(), cfg)
		require.Equal(t, DefaultBrowserProfile, cfg.DefaultProfile)
		require.True(t, cfg.Headless)
		require.Equal(t, 120, cfg.IdleTimeoutSeconds)
		require.Equal(t, 30, cfg.ActionTimeoutSeconds)
		require.False(t, cfg.AllowPrivate)
	})

	t.Run("explicit values win", func(t *testing.T) {
		cfg, err := DecodeStoredConfig(`{"defaultProfile":"work","headless":false,` +
			`"idleTimeoutSeconds":30,"actionTimeoutSeconds":10,"allowPrivate":true}`)
		require.NoError(t, err)
		require.Equal(t, "work", cfg.DefaultProfile)
		require.False(t, cfg.Headless, "an explicitly stored false must survive the decode")
		require.Equal(t, 30, cfg.IdleTimeoutSeconds)
		require.Equal(t, 10, cfg.ActionTimeoutSeconds)
		require.True(t, cfg.AllowPrivate, "an explicitly stored true must survive")
	})

	t.Run("missing keys keep the defaults", func(t *testing.T) {
		cfg, err := DecodeStoredConfig(`{"defaultProfile":"work"}`)
		require.NoError(t, err)
		require.Equal(t, "work", cfg.DefaultProfile)
		require.True(t, cfg.Headless)
		require.Equal(t, 120, cfg.IdleTimeoutSeconds)
		require.Equal(t, 30, cfg.ActionTimeoutSeconds)
		require.False(t, cfg.AllowPrivate)
	})

	t.Run("explicit zero timeouts win over defaults", func(t *testing.T) {
		cfg, err := DecodeStoredConfig(`{"idleTimeoutSeconds":0,"actionTimeoutSeconds":0}`)
		require.NoError(t, err)
		require.Equal(t, 0, cfg.IdleTimeoutSeconds)
		require.Equal(t, 0, cfg.ActionTimeoutSeconds)
	})

	t.Run("invalid json is an error", func(t *testing.T) {
		_, err := DecodeStoredConfig("{not json")
		require.ErrorContains(t, err, "decode stored config")
	})

	t.Run("resolvers honor the zero-means-default rule", func(t *testing.T) {
		cfg := DefaultConfig()
		require.Equal(t, 120, cfg.IdleTimeoutSeconds)
		require.Equal(t, 30, cfg.ActionTimeoutSeconds)
	})
}

func TestEnvAllowPrivate(t *testing.T) {
	require.False(t, envAllowPrivate(), "unset env must keep the guard armed")
	for _, v := range []string{"1", "true", "TRUE", "True", " 1 "} {
		t.Setenv(EnvAllowPrivate, v)
		require.True(t, envAllowPrivate(), "env %q must allow private", v)
	}
	for _, v := range []string{"", "0", "false", "yes"} {
		t.Setenv(EnvAllowPrivate, v)
		require.False(t, envAllowPrivate(), "env %q must not allow private", v)
	}
}
