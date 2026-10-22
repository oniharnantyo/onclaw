package tools

import (
	"os"
	"testing"
	"time"

	"github.com/go-rod/rod/lib/proto"
	"github.com/stretchr/testify/require"
)

func TestCookieParamFromArgs(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()

	t.Run("expires one year out, not a session cookie", func(t *testing.T) {
		p, err := cookieParamFromArgs(browserArgs{
			CookieOp: "set", CookieName: "session", CookieValue: "abc",
		}, now, "http://127.0.0.1:9245/login")
		require.NoError(t, err)
		require.Equal(t, "session", p.Name)
		require.Equal(t, "abc", p.Value)
		// A zero expiry is a session cookie, which Chrome discards when the
		// profile's browser closes; the default must be one year out.
		want := float64(now.Add(browserCookieTTL).Unix())
		require.InDelta(t, want, float64(p.Expires), 2)
		require.Greater(t, float64(p.Expires), float64(now.Unix()))
		require.Equal(t, "/", p.Path, "path defaults to /")
		require.Equal(t, "http://127.0.0.1:9245/login", p.URL, "no domain: scoped to the current page")
		require.Empty(t, p.Domain)
	})

	t.Run("explicit domain and path round trip", func(t *testing.T) {
		p, err := cookieParamFromArgs(browserArgs{
			CookieOp: "set", CookieName: "session", CookieValue: "abc",
			CookieDomain: "example.com", CookiePath: "/app",
		}, now, "")
		require.NoError(t, err)
		require.Equal(t, "example.com", p.Domain)
		require.Equal(t, "/app", p.Path)
		require.Empty(t, p.URL, "explicit domain must not fall back to the page URL")
	})

	t.Run("requires name and value", func(t *testing.T) {
		_, err := cookieParamFromArgs(browserArgs{CookieOp: "set", CookieName: "session"}, now, "")
		require.Error(t, err)
		_, err = cookieParamFromArgs(browserArgs{CookieOp: "set", CookieValue: "abc"}, now, "")
		require.Error(t, err)
	})

	t.Run("domain required without a loaded page", func(t *testing.T) {
		_, err := cookieParamFromArgs(browserArgs{CookieOp: "set", CookieName: "s", CookieValue: "v"}, now, "about:blank")
		require.ErrorContains(t, err, "cookie_domain is required")
	})
}

func TestCookieJarRoundTrip(t *testing.T) {
	live := []*proto.NetworkCookie{
		{Name: "sid", Value: "live1", Domain: "example.com", Path: "/",
			Expires: 0, Secure: false, HTTPOnly: false, SameSite: proto.NetworkCookieSameSiteLax},
		{Name: "session", Value: "abc", Domain: ".example.com", Path: "/app",
			Expires: 1_800_000_000, Secure: true, HTTPOnly: true, SameSite: proto.NetworkCookieSameSiteNone},
	}

	jar := cookiesToJar(live)
	require.Len(t, jar.Cookies, 2)
	require.Equal(t, "sid", jar.Cookies[0].Name)
	require.Equal(t, "live1", jar.Cookies[0].Value)
	require.Equal(t, float64(0), jar.Cookies[0].Expires, "session cookie keeps its zero expiry")

	params := jarToParams(jar)
	require.Len(t, params, 2)
	require.Equal(t, "sid", params[0].Name)
	require.Equal(t, "live1", params[0].Value)
	require.Equal(t, "example.com", params[0].Domain)
	require.Equal(t, "/", params[0].Path)
	require.Equal(t, float64(0), float64(params[0].Expires), "session cookie re-enters as a session cookie")
	require.Equal(t, proto.NetworkCookieSameSiteLax, params[0].SameSite)
	require.Equal(t, ".example.com", params[1].Domain)
	require.Equal(t, "/app", params[1].Path)
	require.Equal(t, float64(1_800_000_000), float64(params[1].Expires))
	require.True(t, params[1].Secure)
	require.True(t, params[1].HTTPOnly)
	require.Equal(t, proto.NetworkCookieSameSiteNone, params[1].SameSite)
}

func TestCookieJarFile(t *testing.T) {
	t.Run("write is 0600 and reads back", func(t *testing.T) {
		dir := t.TempDir()
		jar := browserCookieJarFile{Cookies: []browserStoredCookie{
			{Name: "sid", Value: "live1", Domain: "example.com", Path: "/"},
		}}
		require.NoError(t, writeCookieJar(dir, jar))

		info, err := os.Stat(cookieJarPath(dir))
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0), info.Mode().Perm()&0o077, "the jar carries login state: 0600")

		got, err := readCookieJar(dir)
		require.NoError(t, err)
		require.Equal(t, jar, got)
	})

	t.Run("missing jar is empty", func(t *testing.T) {
		got, err := readCookieJar(t.TempDir())
		require.NoError(t, err)
		require.Empty(t, got.Cookies)
	})

	t.Run("corrupt jar is ignored", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(cookieJarPath(dir), []byte("{not json"), 0o600))
		got, err := readCookieJar(dir)
		require.NoError(t, err, "a corrupt cache must not wedge the profile")
		require.Empty(t, got.Cookies)
	})
}
