package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
)

// browserConfigStub is a BrowserConfigSource returning a fixed config.
type browserConfigStub struct {
	cfg BrowserConfig
	err error
}

func (s *browserConfigStub) BrowserConfig(_ context.Context) (BrowserConfig, error) {
	return s.cfg, s.err
}

// newBrowserToolForTest builds a browser tool on a temp dir with a fixed
// config source; no browser is ever launched because no test below reaches
// the session-acquire path.
func newBrowserToolForTest(t *testing.T, cfg BrowserConfig) (webInvoker, string) {
	t.Helper()
	dir := t.TempDir()
	tl, err := NewBrowser(
		WithBrowserDir(dir),
		WithBrowserConfigSource(&browserConfigStub{cfg: cfg}),
	)
	require.NoError(t, err)
	return tl.(webInvoker), dir
}

func TestBrowser_Info(t *testing.T) {
	tl, err := NewBrowser(
		WithBrowserDir(t.TempDir()),
		WithBrowserConfigSource(&browserConfigStub{cfg: DefaultBrowserConfig()}),
	)
	require.NoError(t, err)
	info, err := tl.Info(context.Background())
	require.NoError(t, err)
	require.Equal(t, NameBrowser, info.Name)
	require.NotEmpty(t, info.Desc)

	// ParamsOneOf's payload is only reachable through the JSON round-trip.
	b, err := json.Marshal(info)
	require.NoError(t, err)
	var decoded struct {
		Params map[string]*schema.ParameterInfo `json:"params"`
	}
	require.NoError(t, json.Unmarshal(b, &decoded))
	params := decoded.Params
	require.Len(t, params, 12)
	require.Equal(t, browserActions, params["action"].Enum)
	require.True(t, params["action"].Required, "only action is required")
	for name, p := range params {
		if name != "action" {
			require.False(t, p.Required, "param %s must be optional", name)
		}
	}
}

func TestBrowser_ArgumentValidation(t *testing.T) {
	cases := []struct {
		name    string
		args    string
		wantErr string
	}{
		{"unknown action", `{"action":"explode"}`, `unknown action "explode"`},
		{"navigate without url", `{"action":"navigate"}`, "url is required"},
		{"click without ref", `{"action":"click"}`, "ref is required"},
		{"type without ref", `{"action":"type","text":"hi"}`, "ref is required"},
		{"type without text", `{"action":"type","ref":"e1"}`, "text is required"},
		{"press_key without key", `{"action":"press_key"}`, "key is required"},
		{"press_key unsupported key", `{"action":"press_key","key":"ChannelUp"}`, "unsupported key"},
		{"scroll bad direction", `{"action":"scroll","direction":"sideways"}`, "direction must be up or down"},
		{"cookies without op", `{"action":"cookies"}`, "cookie_op must be one of"},
		{"cookies bad op", `{"action":"cookies","cookie_op":"wipe"}`, "cookie_op must be one of"},
		{"cookies set without value", `{"action":"cookies","cookie_op":"set","cookie_name":"a"}`, "cookie_name and cookie_value are required"},
		{"cookies delete without name", `{"action":"cookies","cookie_op":"delete"}`, "cookie_name is required"},
		{"invalid profile traversal", `{"action":"snapshot","profile":"../etc"}`, "invalid profile name"},
		{"invalid profile spaces", `{"action":"snapshot","profile":"My Profile"}`, "invalid profile name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tl, dir := newBrowserToolForTest(t, DefaultBrowserConfig())
			_, err := tl.InvokableRun(context.Background(), tc.args)
			require.ErrorContains(t, err, tc.wantErr)
			// Validation must fail before any filesystem access.
			_, statErr := os.Stat(filepath.Join(dir, "profiles"))
			require.True(t, os.IsNotExist(statErr), "profiles dir must not be created on validation failure")
		})
	}
}

func TestBrowser_ResolveBrowserProfile(t *testing.T) {
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
			got, err := resolveBrowserProfile(tc.arg, tc.configured)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestValidateBrowserProfile(t *testing.T) {
	for _, name := range []string{"a", "0", "work", "work-1", "a_b", "a0123456789012345678901234567890"} {
		require.NoError(t, ValidateBrowserProfile(name), name)
	}
	for _, name := range []string{"", "A", "-x", "_x", ".x", "a b", "../etc", "My Profile", "a/b", "work!", "a01234567890123456789012345678901"} {
		require.Error(t, ValidateBrowserProfile(name), name)
	}
}

func TestBrowserProfileDir(t *testing.T) {
	dir, err := BrowserProfileDir("/home/u/.buddies/browser", "work")
	require.NoError(t, err)
	require.Equal(t, filepath.Join("/home/u/.buddies/browser", "profiles", "work"), dir)

	_, err = BrowserProfileDir("/home/u/.buddies/browser", "../etc")
	require.Error(t, err, "invalid names must be refused before any path is returned")
}

func TestBrowser_NewBrowserValidation(t *testing.T) {
	src := &browserConfigStub{cfg: DefaultBrowserConfig()}

	_, err := NewBrowser()
	require.ErrorContains(t, err, "no browser directory")

	_, err = NewBrowser(WithBrowserDir(t.TempDir()))
	require.ErrorContains(t, err, "no config source")

	tl, err := NewBrowser(WithBrowserDir(t.TempDir()), WithBrowserConfigSource(src))
	require.NoError(t, err)
	info, err := tl.Info(context.Background())
	require.NoError(t, err)
	require.Equal(t, NameBrowser, info.Name)

	// A failing config source surfaces as a per-call error, not a
	// construction error.
	tl, err = NewBrowser(
		WithBrowserDir(t.TempDir()),
		WithBrowserConfigSource(&browserConfigStub{err: fmt.Errorf("store offline")}),
	)
	require.NoError(t, err)
	_, err = tl.(webInvoker).InvokableRun(context.Background(), `{"action":"snapshot"}`)
	require.ErrorContains(t, err, "store offline")
}

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

func TestBrowserEnvAllowPrivate(t *testing.T) {
	require.False(t, browserEnvAllowPrivate(), "unset env must keep the guard armed")
	for _, v := range []string{"1", "true", "TRUE", "True", " 1 "} {
		t.Setenv(EnvBrowserAllowPrivate, v)
		require.True(t, browserEnvAllowPrivate(), "env %q must allow private", v)
	}
	for _, v := range []string{"", "0", "false", "yes"} {
		t.Setenv(EnvBrowserAllowPrivate, v)
		require.False(t, browserEnvAllowPrivate(), "env %q must not allow private", v)
	}
}

// TestBrowser_Live drives a real Chromium end to end; it needs a browser
// binary (CHROME_PATH or one rod can find) and is skipped unless
// BUDDIES_BROWSER_LIVE_TEST=1, so `go test ./...` never needs a browser.
func TestBrowser_Live(t *testing.T) {
	if os.Getenv("BUDDIES_BROWSER_LIVE_TEST") != "1" {
		t.Skip("set BUDDIES_BROWSER_LIVE_TEST=1 (and CHROME_PATH if no Chromium is installed) to run the live smoke test")
	}
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/login" {
			// Session cookie: no expiry, exactly what Chrome drops on close
			// unless the profile persists session cookies.
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: "live1", Path: "/"})
			fmt.Fprint(w, `<html><head><title>Logged in</title></head><body>ok</body></html>`)
			return
		}
		fmt.Fprint(w, `<html><head><title>Live</title></head><body>`+
			`<a href="/next" id="next">Next page</a>`+
			`<input type="text" id="q" name="q" value=""/>`+
			`</body></html>`)
	}))
	defer srv.Close()

	cfg := DefaultBrowserConfig()
	cfg.AllowPrivate = true // the httptest server lives on 127.0.0.1
	tl, _ := newBrowserToolForTest(t, cfg)
	ctx := context.Background()

	out, err := tl.InvokableRun(ctx, fmt.Sprintf(`{"action":"navigate","url":%q}`, srv.URL))
	require.NoError(t, err)
	require.Contains(t, out, `"title":"Live"`)

	out, err = tl.InvokableRun(ctx, `{"action":"snapshot"}`)
	require.NoError(t, err)
	var snap browserSnapshotResult
	require.NoError(t, json.Unmarshal([]byte(out), &snap))
	var linkRef, inputRef string
	for _, el := range snap.Elements {
		switch el.Role {
		case "link":
			linkRef = el.Ref
		case "textbox":
			inputRef = el.Ref
		}
	}
	require.NotEmpty(t, linkRef, "snapshot must expose the link")
	require.NotEmpty(t, inputRef, "snapshot must expose the textbox")
	require.NotEqual(t, linkRef, inputRef, "refs must be distinct")

	out, err = tl.InvokableRun(ctx, fmt.Sprintf(`{"action":"type","ref":%q,"text":"hello"}`, inputRef))
	require.NoError(t, err)
	require.Contains(t, out, `"ok":true`)

	out, err = tl.InvokableRun(ctx, fmt.Sprintf(`{"action":"click","ref":%q}`, linkRef))
	require.NoError(t, err)
	require.Contains(t, out, "/next", "click must navigate to the link target")

	out, err = tl.InvokableRun(ctx, `{"action":"cookies","cookie_op":"set","cookie_name":"persist","cookie_value":"year1"}`)
	require.NoError(t, err)
	out, err = tl.InvokableRun(ctx, `{"action":"cookies","cookie_op":"list"}`)
	require.NoError(t, err)
	require.Contains(t, out, `"name":"persist"`)
	require.Contains(t, out, `"value":"year1"`)

	// Site sets a session cookie through a normal Set-Cookie response.
	out, err = tl.InvokableRun(ctx, fmt.Sprintf(`{"action":"navigate","url":%q}`, srv.URL+"/login"))
	require.NoError(t, err)
	out, err = tl.InvokableRun(ctx, `{"action":"cookies","cookie_op":"list"}`)
	require.NoError(t, err)
	require.Contains(t, out, `"name":"sid"`, "the login response must have set the session cookie")

	// Restart the profile's browser and prove both cookies survive: the
	// site-set session cookie via the seeded Chrome preference, the tool-set
	// one via its persistent expiry.
	require.NoError(t, ShutdownBrowserProfile(DefaultBrowserProfile))
	out, err = tl.InvokableRun(ctx, `{"action":"cookies","cookie_op":"list"}`)
	require.NoError(t, err)
	require.Contains(t, out, `"name":"sid"`, "site-set session cookie must survive a browser restart")
	require.Contains(t, out, `"value":"live1"`, "site-set session cookie must survive a browser restart")
	require.Contains(t, out, `"name":"persist"`, "tool-set persistent cookie must survive a browser restart")
	require.Contains(t, out, `"value":"year1"`, "tool-set persistent cookie must survive a browser restart")

	out, err = tl.InvokableRun(ctx, `{"action":"cookies","cookie_op":"clear"}`)
	require.NoError(t, err)
	out, err = tl.InvokableRun(ctx, `{"action":"cookies","cookie_op":"list"}`)
	require.NoError(t, err)
	require.NotContains(t, out, `"name":"sid"`, "clear must empty the jar")
	require.NotContains(t, out, `"name":"persist"`, "clear must empty the jar")

	out, err = tl.InvokableRun(ctx, `{"action":"screenshot"}`)
	require.NoError(t, err)
	var shot browserScreenshotResult
	require.NoError(t, json.Unmarshal([]byte(out), &shot))
	require.FileExists(t, shot.Path)
	require.Greater(t, shot.Width, 0)
	require.Greater(t, shot.Height, 0)

	require.Positive(t, hits.Load())
}
