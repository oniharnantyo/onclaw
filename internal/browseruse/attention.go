package browseruse

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
)

// This file holds the state shared by the secure-submission and takeover
// handovers: the fixed model-facing refusal texts, the fixed reason codes the
// internal channel answers with, the per-session protection/hold state, and
// the manager-level attention registry keyed by requestID. The parent host
// parks a user's value in its own memory and drives the __-prefixed internal
// tools over stdio; the model-facing submit_secret/request_takeover are
// guarded refusals (design D2/D7/D8).

// Fixed refusal texts. Each is a constant: no page state, no exception text,
// no value ever flows into these (design D1 — error paths carry fixed reason
// codes, never page exceptions).
const (
	// submitSecretGuard is the submit_secret guarded refusal: the value is
	// delivered by the parent host through __deliver_secret, never through
	// this tool.
	submitSecretGuard = "submit_secret requires the buddies host handover channel."
	// takeoverGuard is the request_takeover guarded refusal: the parent host
	// parks the attention card and drives __begin_takeover itself.
	takeoverGuard = "request_takeover requires the buddies host handover channel."
	// heldRefusal is every tool's answer on a takeover-held session.
	heldRefusal = "A person controls this browser. Take a fresh snapshot when they finish."
	// protectionRefusal is the one fixed inspection refusal while a secret
	// request holds strong protection on the session.
	protectionRefusal = "Secure authentication is in progress: inspection of this browser is blocked until it completes."
	// evaluateHoldRefusal is the weaker post-scrub state (SPA-keep): only the
	// evaluate paths stay blocked until the next real main-frame navigation.
	evaluateHoldRefusal = "Secure authentication just completed: page evaluation stays blocked on this browser until the next navigation."
	// navGuardRefusal blocks navigations whose URL would carry the secret.
	navGuardRefusal = "browser: navigation blocked: the target URL would carry the secure value under protection."
	// inertRefusal is the fixed answer of every internal channel tool without
	// a matching live request: unknown, expired, or already-resolved IDs are
	// all equally inert, and any delivered value is discarded untouched.
	inertRefusal = "browser: internal attention channel is inert: no live attention request matches; any delivered value is discarded."
	// fingerprintChangedText is the fixed message behind the target-changed
	// delivery failure (design D2).
	fingerprintChangedText = "Authentication target changed."
)

// Fixed reason codes for the internal channel's JSON bodies. The parent maps
// them to model-facing sentences; they never embed page state.
const (
	reasonInsecureOrigin   = "insecure-origin"
	reasonAlreadyPending   = "already-pending"
	reasonSessionHeld      = "session-held"
	reasonRequestGone      = "request-gone"
	reasonFramed           = "framed-target"
	reasonIsolation        = "same-site-isolation"
	reasonTargetUnresolved = "target-unresolved"
	reasonTargetChanged    = "target-changed"
	reasonFillFailed       = "fill-failed"
	reasonSubmitFailed     = "submit-failed"
	reasonScrubFailed      = "scrub-failed"
	reasonProfileMissing   = "profile-not-found"
	reasonRelaunchFailed   = "relaunch-failed"
)

// attentionKind distinguishes the two parked handover shapes sharing the
// registry.
type attentionKind string

const (
	attentionSecret   attentionKind = "secret"
	attentionTakeover attentionKind = "takeover"
)

// attentionRequest is one parked handover: a prepared (but not yet delivered)
// secret, or a running browser takeover. Parked entries live in the manager's
// registry from request until resolution — delivery marks the secret's entry
// delivered (further deliveries are inert; secrets are not retried) but the
// entry stays resolvable so __resolve_attention can always end the freeze.
type attentionRequest struct {
	ID      string
	Kind    attentionKind
	Profile string
	// URL is the takeover's explicit landing page (the agent's url argument,
	// validated at request time): the headed relaunch navigates there before
	// focusing. Empty carries the session's current page over instead.
	URL string
	// Secret carries the prepared fill plan for kind secret; nil otherwise.
	// It never carries a value: the value arrives only inside the
	// __deliver_secret call frame.
	Secret *parkedSecret
	// delivered marks a secret whose delivery attempt happened (success or
	// failure): the channel goes inert for it until resolution.
	delivered bool
}

// parkedSecret is the prepared secret request held between __request_secret
// and __deliver_secret: the resolved fill plan plus the form fingerprint to
// re-check immediately before filling.
type parkedSecret struct {
	origin       string
	method       string
	digits       int
	submission   string
	targets      []secretField
	submitTarget string       // resolved ref; empty for enter/on_input
	identifier   *secretField // the username/email field, when the request declared one
}

// secretField is one resolved authentication field: the snapshot ref plus the
// fingerprint captured at prepare time.
type secretField struct {
	ref string
	fp  formFingerprint
}

// formFingerprint snapshots the authentication form's shape (design D2):
// re-checked immediately before fill so a page that swapped its form under
// the parked request fails safe.
type formFingerprint struct {
	LocalName    string `json:"localName"`
	Type         string `json:"type"`
	ID           string `json:"id"`
	Name         string `json:"name"`
	Autocomplete string `json:"autocomplete"`
	FormAction   string `json:"formAction"`
	FormMethod   string `json:"formMethod"`
}

// secretProbe is what the page probe reports per resolved target: the
// fingerprint plus whether the element lives inside an iframe.
type secretProbe struct {
	Framed bool            `json:"framed"`
	FP     formFingerprint `json:"fingerprint"`
}

// protectionState is the per-session secure-submission guard. The value is
// present only while a __deliver_secret call is in flight (one fill frame);
// the weak state (SPA-keep) keeps only the evaluate hold after the value is
// gone from child memory.
type protectionState struct {
	requestID string
	value     string
	weak      bool
}

// newRequestID mints a random hex request ID for the attention registry.
func newRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand does not fail on the supported platforms
	return hex.EncodeToString(b)
}

// Session-level guards. Handlers run under s.mu (withSession), so these are
// plain reads of the guarded fields.

// checkHeld refuses every public tool on a takeover-held session.
// Caller holds s.mu.
func (s *profileSession) checkHeld() error {
	if s.held {
		return errors.New(heldRefusal)
	}
	return nil
}

// guardInspection refuses the inspection tools (snapshot, screenshot,
// cookies) while strong protection is active. The weak SPA-keep state only
// holds the evaluate paths.
// Caller holds s.mu.
func (s *profileSession) guardInspection() error {
	if s.protection != nil && !s.protection.weak {
		return errors.New(protectionRefusal)
	}
	return nil
}

// guardEvaluate refuses the evaluate paths (read and anything that echoes
// page text) for the full protection arc: strong protection answers the one
// protection refusal; the weak SPA-keep state answers the evaluate hold
// until the next real main-frame navigation.
// Caller holds s.mu.
func (s *profileSession) guardEvaluate() error {
	if s.protection == nil {
		return nil
	}
	if s.protection.weak {
		return errors.New(evaluateHoldRefusal)
	}
	return errors.New(protectionRefusal)
}

// guardNavigation blocks any navigation whose URL carries the secret while
// protection is active (urlCarriesSecret guard).
// Caller holds s.mu.
func (s *profileSession) guardNavigation(rawURL string) error {
	if s.protection != nil && urlCarriesSecret(rawURL, s.protection.value) {
		return errors.New(navGuardRefusal)
	}
	return nil
}

// beginProtection arms the strong protection freeze for a prepared request.
// Caller holds s.mu.
func (s *profileSession) beginProtection(requestID string) {
	s.protection = &protectionState{requestID: requestID}
}

// sealProtectionValue brings the delivered value into the fill frame; the
// deferred unseal drops it from child memory at the call's end.
// Caller holds s.mu.
func (s *profileSession) sealProtectionValue(value string) {
	if s.protection != nil {
		s.protection.value = value
	}
}

// dropProtectionValue removes the value from child memory once the delivery
// call frame is done (design D1: one fill frame).
// Caller holds s.mu.
func (s *profileSession) dropProtectionValue() {
	if s.protection != nil {
		s.protection.value = ""
	}
}

// weakenProtection releases the freeze to the weaker SPA-keep state: the
// value is gone, only the evaluate hold remains until a real navigation.
// Caller holds s.mu.
func (s *profileSession) weakenProtection() {
	if s.protection != nil {
		s.protection.value = ""
		s.protection.weak = true
	}
}

// releaseProtection ends the protection freeze entirely.
// Caller holds s.mu.
func (s *profileSession) releaseProtection() {
	s.protection = nil
}

// releaseWeakProtection ends the SPA-keep evaluate hold at the next real
// main-frame navigation and returns the released hold's request ID so the
// caller can end the delivered request's registry entry, or "" when no weak
// hold released.
// Caller holds s.mu.
func (s *profileSession) releaseWeakProtection() string {
	if s.protection != nil && s.protection.weak {
		id := s.protection.requestID
		s.protection = nil
		return id
	}
	return ""
}

// holdSession marks the session takeover-held: every public tool refuses
// until resolution or tab close (no timeout).
// Caller holds s.mu.
func (s *profileSession) holdSession(requestID string) {
	s.held = true
	s.heldRequestID = requestID
}

// releaseSessionHold ends the takeover hold at resolution.
// Caller holds s.mu.
func (s *profileSession) releaseSessionHold() {
	s.held = false
	s.heldRequestID = ""
}

// urlCarriesSecret reports whether a URL would carry the secret value (plain
// containment covers query, fragment, and path positions). An empty value
// never matches, so the guard is structurally inert whenever the child holds
// no value.
func urlCarriesSecret(rawURL, value string) bool {
	return value != "" && strings.Contains(rawURL, value)
}

// Manager-level registry helpers. The registry mutex is the manager's mu; the
// attention map is only touched under it.

// parkAttention registers a live handover request.
func (m *manager) parkAttention(req *attentionRequest) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.attention[req.ID] = req
}

// findAttention returns the parked request for an ID.
func (m *manager) findAttention(id string) (*attentionRequest, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	req, ok := m.attention[id]
	return req, ok
}

// dropAttention removes a parked request, making every further call with that
// ID inert.
func (m *manager) dropAttention(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.attention, id)
}

// dropDeliveredAttention ends a delivered secret request's registry entry:
// its one attempt happened, so it must never block a new request nor park
// forever. A pending, takeover, or unknown ID is left untouched.
func (m *manager) dropDeliveredAttention(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if req, ok := m.attention[id]; ok && req.Kind == attentionSecret && req.delivered {
		delete(m.attention, id)
	}
}

// dropDeliveredAttentionForProfile ends every delivered secret entry parked
// for a profile: the profile's browser/session is closing and a delivered
// entry must not outlive it in the registry.
func (m *manager) dropDeliveredAttentionForProfile(profile string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, req := range m.attention {
		if req.Kind == attentionSecret && req.Profile == profile && req.delivered {
			delete(m.attention, id)
		}
	}
}

// markAttentionDelivered flags a secret request as delivered (its one attempt
// happened) and answers the entry, or nil when no live secret request matches.
func (m *manager) markAttentionDelivered(id string) *attentionRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	req, ok := m.attention[id]
	if !ok || req.Kind != attentionSecret || req.Secret == nil || req.delivered {
		return nil
	}
	req.delivered = true
	return req
}

// pendingSecretOnProfile reports whether the profile already parks a secret
// request that still awaits its delivery: one pending secret per profile,
// never two. A delivered entry no longer blocks a new request — its one
// attempt happened — and it ends at the release navigation, resolution, or
// the profile's session close.
func (m *manager) pendingSecretOnProfile(profile string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, req := range m.attention {
		if req.Kind == attentionSecret && req.Profile == profile && !req.delivered {
			return true
		}
	}
	return false
}

// resolveAttention ends a held/protection state by request ID: the parked
// entry is dropped and the live session's freeze or hold is cleared without
// filling anything. Unknown or expired IDs answer false (the caller renders
// the inert refusal).
func (m *manager) resolveAttention(id string) bool {
	m.mu.Lock()
	req, ok := m.attention[id]
	if ok {
		delete(m.attention, id)
	}
	var s *profileSession
	if req != nil {
		s = m.sessions[req.Profile]
		if s != nil {
			s.mu.Lock()
		}
	}
	m.mu.Unlock()
	if s == nil {
		return ok
	}
	s.protection = nil
	s.releaseSessionHold()
	s.mu.Unlock()
	return ok
}

// Seam dispatch helpers. Each falls through to the CDP default when the test
// seam is unset, mirroring the settle seams on this session type.

// preparePageOrigin resolves the page's current URL and title through the
// originFn seam (default pageInfo).
// Caller holds s.mu.
func (s *profileSession) preparePageOrigin(page context.Context) (string, string) {
	if s.originFn != nil {
		return s.originFn(page)
	}
	return pageInfo(page)
}

// checkSiteIsolation enforces the same-site isolation requirement through the
// isolationFn seam (default: CDP target enumeration).
// Caller holds s.mu.
func (s *profileSession) checkSiteIsolation(ctx context.Context, page context.Context, host string) error {
	if s.isolationFn != nil {
		return s.isolationFn(ctx, page, host)
	}
	return defaultSiteIsolation(ctx, page, host)
}

// probeSecretTarget probes one resolved target through the probeFn seam
// (default: CDP attribute snapshot).
// Caller holds s.mu.
func (s *profileSession) probeSecretTarget(ctx context.Context, page context.Context, ref string) (secretProbe, error) {
	if s.probeFn != nil {
		return s.probeFn(ctx, page, ref)
	}
	return defaultSecretProbe(ctx, page, ref)
}

// snapshotBasis takes the fresh snapshot targets resolve against through the
// snapshotFn seam (default: the session's AX-tree snapshot).
// Caller holds s.mu.
func (s *profileSession) snapshotBasis(page context.Context) ([]SnapshotElement, error) {
	if s.snapshotFn != nil {
		return s.snapshotFn(page)
	}
	return s.snapshotPage(page)
}

// parseOriginURL parses the page origin URL for the prepare checks.
func parseOriginURL(raw string) (*url.URL, error) {
	return url.Parse(strings.TrimSpace(raw))
}
