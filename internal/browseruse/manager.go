package browseruse

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/target"
)

// managerIdleSeconds is how often the idle janitor sweeps, and the default
// idle lifetime of a browser when the configuration omits one.
const managerIdleSeconds = 120

// managerJanitorInterval is the janitor tick.
const managerJanitorInterval = 30 * time.Second

// profileSession is one live browser for one profile: one Chromium process on
// a persistent user-data directory with a single page. Actions on the session
// are serialized by mu; acquire hands it out locked.
type profileSession struct {
	// browserCtx is the chromedp browser context: it owns the browser
	// lifecycle. pageCtx is the target context of the profile's single
	// about:blank tab — the page handle every seam takes. See engine.go for
	// the calling rules. proc is the owned Chromium child (launch.go); nil
	// on test stubs.
	browserCtx context.Context
	pageCtx    context.Context
	proc       *browserProc
	dir        string
	// idleTTL is fixed at launch from the config; the janitor closes the
	// session once it has been idle longer.
	idleTTL time.Duration
	// lastUsed is bookkeeping for the janitor, guarded by the manager's map
	// mutex (only acquire and sweep touch it).
	lastUsed time.Time
	// closeFn tears the browser down. A field rather than a method so tests
	// can stub the launch and count closes without a real browser.
	closeFn func() error
	// exportFn snapshots the live cookies into the profile's jar file. A
	// seam so tests can count exports without a real browser; nil on stubs
	// (saving is best-effort and must never fail a session).
	exportFn func() error
	// aliveFn reports whether the session's browser still answers CDP calls.
	// Wired by the real launcher; nil on test stubs (unknown — assumed
	// alive). The manager probes it at acquire so a browser that died
	// underneath it (a crash, a person closing the window, a headed takeover
	// swap) is relaunched instead of failing every action on stale handles.
	aliveFn func() bool
	// requestIdleFn / stableWaitFn / sleepFn are the settle and wait
	// primitives (network quiet, DOM stability, fixed delay). Seams so the
	// orchestration around them is testable without Chromium; nil falls back
	// to the CDP implementations. The page parameter is the session page
	// context (see engine.go).
	requestIdleFn func(ctx context.Context, page context.Context) error
	stableWaitFn  func(ctx context.Context, page context.Context) error
	sleepFn       func(ctx context.Context, d time.Duration) error
	// mu serializes actions on this profile. Guarded invariants: elements and
	// the handover state below are only touched while mu is held.
	mu sync.Mutex
	// elements is the latest snapshot's element list; refs carried by its
	// entries stay valid until the next navigation. Cleared on navigate and
	// rebuilt by every snapshot.
	elements []SnapshotElement
	// held marks a session handed over to a person (browser takeover): every
	// public tool is refused until __resolve_attention or the tab closes.
	held          bool
	heldRequestID string
	// protection is the active secure-submission guard: inspection tools are
	// refused from __request_secret until scrub-release or resolution. Nil
	// when no secret request is in flight on this profile.
	protection *protectionState
	// Handover seams (nil = CDP default; see attention.go, secret.go,
	// deliver.go, takeover.go). They make the prepare/fill/scrub/takeover
	// orchestration testable without Chromium, like the settle seams above.
	// The page parameter is the session page context (see engine.go).
	originFn      func(page context.Context) (string, string)
	isolationFn   func(ctx context.Context, page context.Context, host string) error
	probeFn       func(ctx context.Context, page context.Context, ref string) (secretProbe, error)
	snapshotFn    func(page context.Context) ([]SnapshotElement, error)
	fillFn        func(ctx context.Context, page context.Context, ref, text string) error
	submitClickFn func(ctx context.Context, page context.Context, ref string) error
	submitEnterFn func(ctx context.Context, page context.Context, ref string) error
	watchNavFn    func(ctx context.Context, page context.Context) *navWatch
	clearFieldsFn func(ctx context.Context, page context.Context, refs []string) error
	scanFn        func(ctx context.Context, page context.Context, value string) (bool, error)
	reloadFn      func(ctx context.Context, page context.Context) error
}

// closeSession tears the browser down; safe on a stubbed session with no
// closer.
func (s *profileSession) closeSession() error {
	if s.closeFn == nil {
		return nil
	}
	return s.closeFn()
}

// sessionProbeTimeout bounds the acquire-time liveness probe: a healthy
// Chromium answers in well under a second, and a wedged one must not stall
// the next action much longer than that.
const sessionProbeTimeout = 3 * time.Second

// probeSessionAlive pings the session's page target within the probe budget:
// Target.getTargetInfo on the session's target id. A closed tab, a dead
// browser process, and a wedged CDP connection all count as not alive — in
// every one of those states the only useful next action is a relaunch.
func probeSessionAlive(pageCtx context.Context) bool {
	tgt, ok := pageTarget(pageCtx)
	if !ok {
		return false
	}
	ctx, cancel := context.WithTimeout(pageCtx, sessionProbeTimeout)
	defer cancel()
	_, err := cdp.Call(ctx, tgt, target.GetTargetInfo, target.GetTargetInfoParams{TargetID: tgt.TargetID})
	return err == nil
}

// sessionAlive reports whether the session's browser still answers. Sessions
// without a probe (test stubs) count as alive. Caller holds s.mu.
func (s *profileSession) sessionAlive() bool {
	return s.aliveFn == nil || s.aliveFn()
}

// attentionHold reports a session parked in a handover arc — a person
// takeover or an in-flight secret protection. Its state decides the design's
// resolution (held until resolve or tab close), so the manager must never
// discard it out from under the arc. Caller holds s.mu.
func (s *profileSession) attentionHold() bool {
	return s.held || s.protection != nil
}

// saveCookieJar exports the live cookies to the profile's jar file so the
// next launch can restore them (headless Chrome does not persist session
// cookies on its own). Best-effort: Chrome keeps the non-session cookies in
// its own store, and a teardown must not fail over the cache. No-op on test
// stubs that carry no exporter.
func (s *profileSession) saveCookieJar() {
	if s.exportFn == nil {
		return
	}
	_ = s.exportFn()
}

// manager is the server's registry of live profile browsers. The server is
// the sole owner of browser state (design D3): browsers outlive individual
// tool calls so profiles are reused across turns; the janitor reaps the idle
// ones and disk keeps the durable state.
type manager struct {
	mu       sync.Mutex
	sessions map[string]*profileSession
	// attention parks the live handover requests (pending secrets, running
	// takeovers) keyed by requestID; guarded by mu.
	attention map[string]*attentionRequest
	// now and launch are seams for tests: a fake clock makes janitor
	// bookkeeping deterministic and a stubbed launch avoids a real browser.
	now    func() time.Time
	launch func(ctx context.Context, dir string, cfg Config) (*profileSession, error)
}

func newManager() *manager {
	return &manager{
		sessions:  map[string]*profileSession{},
		attention: map[string]*attentionRequest{},
		now:       time.Now,
		launch:    launchChromiumSession,
	}
}

var (
	mgrOnce sync.Once
	mgr     *manager
)

// managerInstance returns the server's single browser manager, starting the
// idle janitor with it. The server process runs exactly one manager.
func managerInstance() *manager {
	mgrOnce.Do(func() {
		mgr = newManager()
		go mgr.reapLoop()
	})
	return mgr
}

// acquire returns (creating on first use) the live session for a profile with
// the session mutex held; the returned release func unlocks it. Concurrent
// actions on one profile queue here; different profiles run concurrently.
// A cached session whose browser died underneath the manager is discarded and
// the profile relaunched: the persisted state (cookies, storage) survives the
// swap, the stale CDP handles would fail every action.
func (m *manager) acquire(ctx context.Context, browserDir, name string, cfg Config) (*profileSession, func(), error) {
	m.mu.Lock()
	if s, ok := m.sessions[name]; ok {
		s.lastUsed = m.now()
		s.mu.Lock()
		m.mu.Unlock()
		if s.attentionHold() || s.sessionAlive() {
			return s, s.mu.Unlock, nil
		}
		// Drop the stale entry and fall through to a fresh launch. The
		// identity check keeps a concurrent replace (another acquire's
		// relaunch, a headed takeover swap) from being discarded here.
		m.mu.Lock()
		if m.sessions[name] == s {
			delete(m.sessions, name)
		}
		m.mu.Unlock()
		s.saveCookieJar()
		_ = s.closeSession() // best-effort: the browser is already gone
	} else {
		m.mu.Unlock()
	}

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
	s.idleTTL = idleTTL(cfg)
	s.lastUsed = m.now()
	m.sessions[name] = s
	m.mu.Unlock()

	s.mu.Lock()
	return s, s.mu.Unlock, nil
}

// createSession prepares the profile directory and launches the browser. Runs
// outside the map mutex: launching Chromium takes seconds.
func (m *manager) createSession(ctx context.Context, browserDir, name string, cfg Config) (*profileSession, error) {
	dir, err := ProfileDir(browserDir, name)
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

// profilePreferences is the startup preference seeded into fresh profiles:
// "continue where you left off". It makes Chrome persist session cookies
// across restarts; without it every browser close (including the idle reap)
// discards them and silently logs the agent out.
const profilePreferences = `{"session":{"restore_on_startup":1}}`

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
	if err := os.WriteFile(prefsPath, []byte(profilePreferences), 0o600); err != nil {
		return fmt.Errorf("browser: write profile preferences: %w", err)
	}
	return nil
}

// sweep closes sessions idle for longer than their launch-time TTL. A session
// with an action in flight (mutex held) is skipped and picked up by a later
// sweep.
func (m *manager) sweep() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, s := range m.sessions {
		if m.now().Sub(s.lastUsed) <= s.idleTTL {
			continue
		}
		if !s.mu.TryLock() {
			continue // action in flight; not actually idle
		}
		if s.held || s.protection != nil {
			// An attention hold (person takeover, or a secret request's
			// freeze) outlives the idle TTL: the design holds the browser
			// until resolution or tab close, with no timeout (design D8).
			s.mu.Unlock()
			continue
		}
		delete(m.sessions, name)
		// The session is being discarded: delivered secret entries parked for
		// this profile must not outlive it in the registry. The attention map
		// is guarded by m.mu, which this loop already holds.
		for id, req := range m.attention {
			if req.Kind == attentionSecret && req.Profile == name && req.delivered {
				delete(m.attention, id)
			}
		}
		s.saveCookieJar()
		s.mu.Unlock()
		_ = s.closeSession() // best-effort: the session is being discarded
	}
}

// reapLoop is the janitor goroutine started once with the server's manager.
func (m *manager) reapLoop() {
	ticker := time.NewTicker(managerJanitorInterval)
	defer ticker.Stop()
	for range ticker.C {
		m.sweep()
	}
}

// ShutdownBrowserProfile closes the live browser for a profile, if any, so
// its user-data directory can be inspected or removed. An unknown profile is
// not an error.
func ShutdownBrowserProfile(name string) error {
	return managerInstance().shutdown(name)
}

// ShutdownAllProfiles closes every live profile browser: each session gets
// its cookie jar exported and its Chromium shut down. This is the server's
// graceful-shutdown path (stdin EOF, SIGINT, SIGTERM) — the last writer of
// profile state before process exit.
func ShutdownAllProfiles() error {
	return managerInstance().shutdownAll()
}

// shutdown implements ShutdownBrowserProfile on a manager instance so tests
// can exercise the close path with a stubbed launch.
func (m *manager) shutdown(name string) error {
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
	// The browser is gone: delivered secret entries parked for this profile
	// must not outlive its session in the registry.
	m.dropDeliveredAttentionForProfile(name)
	if closeErr != nil {
		return fmt.Errorf("browser: close profile %s: %w", name, closeErr)
	}
	return nil
}

// shutdownAll implements ShutdownAllProfiles on a manager instance. Errors
// are joined so one broken browser does not skip the others' teardown.
func (m *manager) shutdownAll() error {
	m.mu.Lock()
	names := make([]string, 0, len(m.sessions))
	for name := range m.sessions {
		names = append(names, name)
	}
	m.mu.Unlock()
	var err error
	for _, name := range names {
		if closeErr := m.shutdown(name); closeErr != nil {
			err = joinErr(err, closeErr)
		}
	}
	return err
}

// joinErr folds two best-effort teardown errors into one chain.
func joinErr(err, next error) error {
	if err == nil {
		return next
	}
	return fmt.Errorf("%w; %w", err, next)
}
