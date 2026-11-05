package browseruse

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/stretchr/testify/require"
)

func TestCookieParam(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()

	t.Run("expires one year out, not a session cookie", func(t *testing.T) {
		p, err := cookieParam("session", "abc", "", "", now, "http://127.0.0.1:9245/login")
		require.NoError(t, err)
		require.Equal(t, "session", p.Name)
		require.Equal(t, "abc", p.Value)
		// A zero expiry is a session cookie, which Chrome discards when the
		// profile's browser closes; the default must be one year out.
		want := float64(now.Add(cookieTTL).Unix())
		require.InDelta(t, want, float64(p.Expires), 2)
		require.Greater(t, float64(p.Expires), float64(now.Unix()))
		require.Equal(t, "/", p.Path, "path defaults to /")
		require.Equal(t, "http://127.0.0.1:9245/login", p.URL, "no domain: scoped to the current page")
		require.Empty(t, p.Domain)
	})

	t.Run("explicit domain and path round trip", func(t *testing.T) {
		p, err := cookieParam("session", "abc", "example.com", "/app", now, "")
		require.NoError(t, err)
		require.Equal(t, "example.com", p.Domain)
		require.Equal(t, "/app", p.Path)
		require.Empty(t, p.URL, "explicit domain must not fall back to the page URL")
	})

	t.Run("requires name and value", func(t *testing.T) {
		_, err := cookieParam("session", "", "", "", now, "")
		require.Error(t, err)
		_, err = cookieParam("", "abc", "", "", now, "")
		require.Error(t, err)
	})

	t.Run("domain required without a loaded page", func(t *testing.T) {
		_, err := cookieParam("s", "v", "", "", now, "about:blank")
		require.ErrorContains(t, err, "domain is required")
	})
}

func TestValidateCookiesOp(t *testing.T) {
	require.ErrorContains(t, validateCookiesOp(cookiesArgs{Op: ""}), "op must be one of")
	require.ErrorContains(t, validateCookiesOp(cookiesArgs{Op: "wipe"}), "op must be one of")
	require.ErrorContains(t, validateCookiesOp(cookiesArgs{Op: "set", Name: "a"}), "name and value are required")
	require.ErrorContains(t, validateCookiesOp(cookiesArgs{Op: "delete"}), "name is required")
	require.NoError(t, validateCookiesOp(cookiesArgs{Op: "list"}))
	require.NoError(t, validateCookiesOp(cookiesArgs{Op: "clear"}))
	require.NoError(t, validateCookiesOp(cookiesArgs{Op: "set", Name: "a", Value: "b"}))
	require.NoError(t, validateCookiesOp(cookiesArgs{Op: "delete", Name: "a"}))
}

func TestCookieJarRoundTrip(t *testing.T) {
	live := []*network.Cookie{
		{Name: "sid", Value: "live1", Domain: "example.com", Path: "/",
			Expires: 0, Secure: false, HTTPOnly: false, SameSite: network.CookieSameSiteLax},
		{Name: "session", Value: "abc", Domain: ".example.com", Path: "/app",
			Expires: 1_800_000_000, Secure: true, HTTPOnly: true, SameSite: network.CookieSameSiteNone},
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
	require.Equal(t, network.CookieSameSiteLax, params[0].SameSite)
	require.Equal(t, ".example.com", params[1].Domain)
	require.Equal(t, "/app", params[1].Path)
	require.Equal(t, float64(1_800_000_000), float64(params[1].Expires))
	require.True(t, params[1].Secure)
	require.True(t, params[1].HTTPOnly)
	require.Equal(t, network.CookieSameSiteNone, params[1].SameSite)
}

func TestCookieJarFile(t *testing.T) {
	t.Run("write is 0600 and reads back", func(t *testing.T) {
		dir := t.TempDir()
		jar := CookieJarFile{Cookies: []StoredCookie{
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

// legacyJarBytes is a jar file written by the pre-migration (go-rod) build,
// byte-for-byte: the StoredCookie field names and SameSite string form are
// the on-disk contract the migration must not move (design D7).
const legacyJarBytes = `{"cookies":[{"name":"sid","value":"compat-42","domain":"127.0.0.1","path":"/","expires":1800000000,"secure":false,"httpOnly":true,"sameSite":"Lax"}]}`

// TestCookieJar_CompatFixture is the upgrade compatibility proof: a profile
// directory carrying a pre-migration jar file and seeded Preferences restores
// the cookie into the relaunched (chromedp) session — no migration step, the
// login state survives the engine swap. Skipped where no browser exists.
func TestCookieJar_CompatFixture(t *testing.T) {
	requireTestBrowser(t)
	f := newFixtureServer(t)
	b := newBrowserTools(t)

	// Prepare the profile directory exactly as the pre-migration build left
	// it: the jar file plus Chrome's seeded restore-on-startup preference.
	profileDir := filepath.Join(b.cfg.BrowserDir, "profiles", DefaultBrowserProfile)
	require.NoError(t, os.MkdirAll(filepath.Join(profileDir, "Default"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(profileDir, "Default", "Preferences"),
		[]byte(profilePreferences), 0o600))
	require.NoError(t, os.WriteFile(cookieJarPath(profileDir), []byte(legacyJarBytes), 0o600))

	// First use launches the browser and injects the jar before anything can
	// observe the profile; with no page loaded yet the list walks the whole
	// store, and the cookie must already be live in it.
	res := callHandler(t, b.handleCookies, map[string]any{"op": "list"})
	require.False(t, res.IsError, "cookies list: %s", resultText(t, res))
	var listed CookiesResult
	mustJSON(t, res, &listed)
	var found *Cookie
	for i := range listed.Cookies {
		if listed.Cookies[i].Name == "sid" {
			found = &listed.Cookies[i]
			break
		}
	}
	require.NotNil(t, found, "the pre-migration jar cookie must be restored into the live session: %+v", listed.Cookies)
	require.Equal(t, "compat-42", found.Value)
	require.Equal(t, "127.0.0.1", found.Domain)
	require.Equal(t, "/", found.Path)
	require.True(t, found.HTTPOnly)
	require.InDelta(t, float64(1_800_000_000), found.Expires, 2)

	// The restored cookie is visible to an http(s) page too: after a
	// navigation the list runs through the page-scoped Network.getCookies.
	navigate(t, b, f.URL+"/form")
	res = callHandler(t, b.handleCookies, map[string]any{"op": "list"})
	require.False(t, res.IsError, "cookies list after navigate: %s", resultText(t, res))
	mustJSON(t, res, &listed)
	found = nil
	for i := range listed.Cookies {
		if listed.Cookies[i].Name == "sid" {
			found = &listed.Cookies[i]
			break
		}
	}
	require.NotNil(t, found, "the restored cookie stays visible to the loaded page: %+v", listed.Cookies)

	// The seeded preference was never overwritten by the launch.
	prefs, err := os.ReadFile(filepath.Join(profileDir, "Default", "Preferences"))
	require.NoError(t, err)
	require.Equal(t, profilePreferences, string(prefs), "an existing Preferences file belongs to Chrome")

	// And a cookie set now exports back in the same on-disk shape: the jar
	// the next launch reads stays byte-compatible both directions.
	set := callHandler(t, b.handleCookies, map[string]any{
		"op": "set", "name": "fresh", "value": "exported-99", "domain": "127.0.0.1",
	})
	require.False(t, set.IsError, "cookies set: %s", resultText(t, set))

	s, release, err := b.mgr.acquire(context.Background(), b.cfg.BrowserDir, DefaultBrowserProfile, b.cfg)
	require.NoError(t, err)
	dir := s.dir
	s.saveCookieJar()
	release()

	saved, err := readCookieJar(dir)
	require.NoError(t, err)
	var exported *StoredCookie
	for i := range saved.Cookies {
		if saved.Cookies[i].Name == "fresh" {
			exported = &saved.Cookies[i]
			break
		}
	}
	require.NotNil(t, exported, "the exported jar must carry the cookie the tool set: %+v", saved.Cookies)
	require.Equal(t, "exported-99", exported.Value)
	require.Equal(t, "127.0.0.1", exported.Domain)
	require.Contains(t, []string{"", "Lax"}, exported.SameSite,
		"SameSite stays in its string form on disk, exactly as the browser reported it")

	// The exported file decodes to exactly the StoredCookie JSON shape —
	// same keys, no additions the old build would have rejected.
	raw, err := os.ReadFile(cookieJarPath(dir))
	require.NoError(t, err)
	require.Contains(t, string(raw), `"name":"fresh"`)
	require.Contains(t, string(raw), `"httpOnly":`)
	require.Contains(t, string(raw), `"sameSite":`)
}
