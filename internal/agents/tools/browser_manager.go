package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// DefaultBrowserProfile is the profile used when neither the call nor the
// configuration names one; it is created automatically on first use and can
// never be deleted.
const DefaultBrowserProfile = "default"

// browserProfilePattern is the profile-name contract: path-traversal-proof
// and safe to join under the profiles directory.
var browserProfilePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// ValidateBrowserProfile reports whether name may be used as a browser
// profile. It performs no filesystem access.
func ValidateBrowserProfile(name string) error {
	if !browserProfilePattern.MatchString(name) {
		return fmt.Errorf("browser: invalid profile name %q: must match ^[a-z0-9][a-z0-9_-]{0,31}$", name)
	}
	return nil
}

// BrowserProfileDir maps a validated profile name to its persistent
// user-data directory under browserDir.
func BrowserProfileDir(browserDir, name string) (string, error) {
	if err := ValidateBrowserProfile(name); err != nil {
		return "", err
	}
	return filepath.Join(browserDir, "profiles", name), nil
}

// browserManagerIdleSeconds is how often the idle janitor sweeps, and the
// default idle lifetime of a browser when the configuration omits one.
const browserManagerIdleSeconds = 120

// browserManagerJanitorInterval is the janitor tick.
const browserManagerJanitorInterval = 30 * time.Second

// profileSession is one live browser for one profile: one Chromium process on
// a persistent user-data directory with a single page. Actions on the session
// are serialized by mu; acquire hands it out locked.
type profileSession struct {
	browser *rod.Browser
	page    *rod.Page
	dir     string
	// idleTTL is fixed at launch from the config; the janitor closes the
	// session once it has been idle longer.
	idleTTL time.Duration
	// lastUsed is bookkeeping for the janitor, guarded by the manager's map
	// mutex (only acquire and sweep touch it).
	lastUsed time.Time
	// closeFn tears the browser down. A field rather than a method so tests
	// can stub the launch and count closes without a real browser.
	closeFn func() error
	// mu serializes actions on this profile. Guarded invariants: refs is
	// only touched while mu is held.
	mu sync.Mutex
	// refs holds the element refs ("e<backendNodeID>") returned by the
	// latest snapshot; cleared on navigate.
	refs map[string]bool
}

// closeSession tears the browser down; safe on a stubbed session with no
// closer.
func (s *profileSession) closeSession() error {
	if s.closeFn == nil {
		return nil
	}
	return s.closeFn()
}

// saveCookieJar exports the live cookies to the profile's jar file so the
// next launch can restore them (headless Chrome does not persist session
// cookies on its own). Best-effort: Chrome keeps the non-session cookies in
// its own store, and a teardown must not fail over the cache. No-op on test
// stubs that carry no browser.
func (s *profileSession) saveCookieJar() {
	if s.browser == nil {
		return
	}
	_ = exportCookieJar(s.browser, s.dir)
}

// browserManager is the process-wide registry of live profile browsers
// (design D4). buildDirectTool runs per turn, so browsers must outlive tool
// instances to be reused across turns; the janitor reaps the idle ones.
type browserManager struct {
	mu       sync.Mutex
	sessions map[string]*profileSession
	// now and launch are seams for tests: a fake clock makes janitor
	// bookkeeping deterministic and a stubbed launch avoids a real browser.
	now    func() time.Time
	launch func(ctx context.Context, dir string, cfg BrowserConfig) (*profileSession, error)
}

func newBrowserManager() *browserManager {
	return &browserManager{
		sessions: map[string]*profileSession{},
		now:      time.Now,
		launch:   launchRodSession,
	}
}

var (
	browserMgrOnce sync.Once
	browserMgr     *browserManager
)

func browserManagerInstance() *browserManager {
	browserMgrOnce.Do(func() {
		browserMgr = newBrowserManager()
		go browserMgr.reapLoop()
	})
	return browserMgr
}

// acquire returns (creating on first use) the live session for a profile with
// the session mutex held; the returned release func unlocks it. Concurrent
// actions on one profile queue here; different profiles run concurrently.
func (m *browserManager) acquire(ctx context.Context, browserDir, name string, cfg BrowserConfig) (*profileSession, func(), error) {
	m.mu.Lock()
	if s, ok := m.sessions[name]; ok {
		s.lastUsed = m.now()
		s.mu.Lock()
		m.mu.Unlock()
		return s, s.mu.Unlock, nil
	}
	m.mu.Unlock()

	s, err := m.createSession(ctx, browserDir, name, cfg)
	if err != nil {
		return nil, nil, err
	}

	m.mu.Lock()
	if existing, ok := m.sessions[name]; ok {
		// Another goroutine launched the same profile meanwhile; discard the
		// loser and reuse the winner.
		m.mu.Unlock()
		_ = s.closeSession() // best-effort: the duplicate is discarded
		existing.mu.Lock()
		return existing, existing.mu.Unlock, nil
	}
	s.idleTTL = browserIdleTTL(cfg)
	s.lastUsed = m.now()
	m.sessions[name] = s
	m.mu.Unlock()

	s.mu.Lock()
	return s, s.mu.Unlock, nil
}

// createSession prepares the profile directory and launches the browser. Runs
// outside the map mutex: launching Chromium takes seconds.
func (m *browserManager) createSession(ctx context.Context, browserDir, name string, cfg BrowserConfig) (*profileSession, error) {
	dir, err := BrowserProfileDir(browserDir, name)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("browser: create profile dir: %w", err)
	}
	// No live session is tracked for this profile (acquire only gets here in
	// that state), so any Chrome Singleton* file in the directory is a
	// leftover from a crashed browser and must not block the relaunch.
	removeStaleSingletonLocks(dir)
	if err := seedProfilePreferences(dir); err != nil {
		return nil, err
	}
	s, err := m.launch(ctx, dir, cfg)
	if err != nil {
		return nil, fmt.Errorf("browser: launch profile %s: %w", name, err)
	}
	return s, nil
}

// removeStaleSingletonLocks deletes the Chromium single-instance lock files
// from a user-data directory. Missing files are fine.
func removeStaleSingletonLocks(dir string) {
	for _, name := range []string{"SingletonLock", "SingletonSocket", "SingletonCookie"} {
		_ = os.Remove(filepath.Join(dir, name)) // absent files are not an error
	}
}

// browserProfilePreferences is the startup preference seeded into fresh
// profiles: "continue where you left off". It makes Chrome persist session
// cookies across restarts; without it every browser close (including the
// idle reap) discards them and silently logs the agent out.
const browserProfilePreferences = `{"session":{"restore_on_startup":1}}`

// seedProfilePreferences writes the session-cookie persistence preference
// into a profile that has none yet, before Chromium first owns the
// directory. An existing Preferences file belongs to Chrome and is never
// touched.
func seedProfilePreferences(dir string) error {
	prefsPath := filepath.Join(dir, "Default", "Preferences")
	if _, err := os.Stat(prefsPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("browser: stat profile preferences: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "Default"), 0o700); err != nil {
		return fmt.Errorf("browser: create profile Default dir: %w", err)
	}
	if err := os.WriteFile(prefsPath, []byte(browserProfilePreferences), 0o600); err != nil {
		return fmt.Errorf("browser: write profile preferences: %w", err)
	}
	return nil
}

// sweep closes sessions idle for longer than their launch-time TTL. A session
// with an action in flight (mutex held) is skipped and picked up by a later
// sweep.
func (m *browserManager) sweep() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, s := range m.sessions {
		if m.now().Sub(s.lastUsed) <= s.idleTTL {
			continue
		}
		if !s.mu.TryLock() {
			continue // action in flight; not actually idle
		}
		delete(m.sessions, name)
		s.saveCookieJar()
		s.mu.Unlock()
		_ = s.closeSession() // best-effort: the session is being discarded
	}
}

// reapLoop is the janitor goroutine started once with the singleton.
func (m *browserManager) reapLoop() {
	ticker := time.NewTicker(browserManagerJanitorInterval)
	defer ticker.Stop()
	for range ticker.C {
		m.sweep()
	}
}

// ShutdownBrowserProfile closes the live browser for a profile, if any, so
// its user-data directory can be inspected or removed. An unknown profile is
// not an error.
func ShutdownBrowserProfile(name string) error {
	return browserManagerInstance().shutdown(name)
}

// shutdown implements ShutdownBrowserProfile on a manager instance so tests
// can exercise the close path with a stubbed launch.
func (m *browserManager) shutdown(name string) error {
	m.mu.Lock()
	s, ok := m.sessions[name]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	delete(m.sessions, name)
	s.mu.Lock()
	m.mu.Unlock()
	s.saveCookieJar()
	closeErr := s.closeSession()
	s.mu.Unlock()
	if closeErr != nil {
		return fmt.Errorf("browser: close profile %s: %w", name, closeErr)
	}
	return nil
}

// browserIdleTTL resolves the idle lifetime from the config; the zero value
// means the default (non-positive values would never reap or reap instantly).
func browserIdleTTL(cfg BrowserConfig) time.Duration {
	if cfg.IdleTimeoutSeconds <= 0 {
		return browserManagerIdleSeconds * time.Second
	}
	return time.Duration(cfg.IdleTimeoutSeconds) * time.Second
}

// browserActionTimeout resolves the per-action bound from the config; the
// zero value means the default.
func browserActionTimeout(cfg BrowserConfig) time.Duration {
	if cfg.ActionTimeoutSeconds <= 0 {
		return defaultBrowserActionSeconds * time.Second
	}
	return time.Duration(cfg.ActionTimeoutSeconds) * time.Second
}

// launchRodSession launches Chromium on dir as its user-data directory and
// opens the profile's single page. The browser is leakless (rod kills the
// child when the parent dies) and honors CHROME_PATH over rod's own
// lookup-and-download.
func launchRodSession(ctx context.Context, dir string, cfg BrowserConfig) (*profileSession, error) {
	l := launcher.New().
		UserDataDir(dir).
		Headless(cfg.Headless).
		Leakless(true).
		Set("no-first-run").
		Set("no-default-browser-check").
		Context(ctx)
	if bin := os.Getenv("CHROME_PATH"); bin != "" {
		l = l.Bin(bin)
	}
	u, err := l.Launch()
	if err != nil {
		return nil, fmt.Errorf("start chromium (set CHROME_PATH to skip rod's lookup/download): %w", err)
	}
	browser := rod.New().ControlURL(u)
	if err := browser.Connect(); err != nil {
		return nil, fmt.Errorf("connect to chromium: %w", err)
	}
	// Restore the cookies saved at the previous teardown before anything can
	// observe the profile; missing or corrupt jars are not an error.
	if err := injectCookieJar(browser, dir); err != nil {
		_ = browser.Close() // nothing to leak: no page was opened
		return nil, err
	}
	page, err := browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		_ = browser.Close() // nothing to leak: the page never opened
		return nil, fmt.Errorf("open page: %w", err)
	}
	return &profileSession{
		browser: browser,
		page:    page,
		dir:     dir,
		closeFn: browser.Close,
	}, nil
}
