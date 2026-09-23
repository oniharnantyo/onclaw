package oauth_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents/mcp/oauth"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

// ---------------------------------------------------------------------------
// RFC 8628 device flow (task 4.5)
// ---------------------------------------------------------------------------

func TestDeviceBegin_ParsesGrantAndKeepsSecretOffTheWire(t *testing.T) {
	f := newASFixture(t)
	client := newTestClient(t, f)
	meta := testMeta(f.base(), nil)
	rc := byoConfidential(t, client, meta)

	grant, err := client.BeginDeviceAuthorization(context.Background(), meta, rc, []string{testScope})
	if err != nil {
		t.Fatalf("begin device authorization: %v", err)
	}
	if grant.DeviceCode != "dc-1" || grant.UserCode != "ABCD-EFGH" {
		t.Errorf("unexpected grant identity: %+v", grant)
	}
	if grant.VerificationURI != "https://example.com/activate" || grant.VerificationURIComplete != "https://example.com/activate?code=ABCDEFGH" {
		t.Errorf("unexpected paste-back payload: %+v", grant)
	}
	if grant.ExpiresIn != 600*time.Second || grant.Interval != time.Second {
		t.Errorf("unexpected poll parameters: %+v", grant)
	}
	// Confidential client: the secret rides the Basic header at the device
	// endpoint (RFC 8628 §3.1 keeps token-endpoint auth rules) — never the
	// body, never a URL.
	hits := f.deviceHits()
	if len(hits) != 1 {
		t.Fatalf("expected one device authorization request, got %d", len(hits))
	}
	if !strings.HasPrefix(hits[0].auth, "Basic ") {
		t.Errorf("expected Basic client authentication, got %q", hits[0].auth)
	}
	if hits[0].form.Get("client_secret") != "" || strings.Contains(hits[0].form.Encode(), byoClientSecret) {
		t.Error("the client secret must never ride the device request body")
	}
	if hits[0].form.Get("scope") != testScope {
		t.Errorf("scope = %q, want %q", hits[0].form.Get("scope"), testScope)
	}
}

func TestDeviceBegin_PublicClientAndDefaults(t *testing.T) {
	f := newASFixture(t)
	f.mu.Lock()
	// Omitted interval/expires_in: the RFC 8628 §3.2 defaults apply.
	f.deviceBodies = []string{`{"device_code":"dc-2","user_code":"WXYZ-1234","verification_uri":"https://example.com/activate"}`}
	f.mu.Unlock()
	client := newTestClient(t, f)
	meta := testMeta(f.base(), nil)
	rc := byoPublic(t, client, meta)

	grant, err := client.BeginDeviceAuthorization(context.Background(), meta, rc, nil)
	if err != nil {
		t.Fatalf("begin device authorization: %v", err)
	}
	if grant.Interval != oauth.DefaultDevicePollInterval {
		t.Errorf("interval = %v, want the RFC default %v", grant.Interval, oauth.DefaultDevicePollInterval)
	}
	if grant.ExpiresIn != oauth.DefaultDeviceAuthorizationExpiry {
		t.Errorf("expires_in = %v, want the fallback %v", grant.ExpiresIn, oauth.DefaultDeviceAuthorizationExpiry)
	}
	hit := f.deviceHits()[0]
	if hit.form.Get("client_id") != byoClientID || hit.auth != "" {
		t.Errorf("public clients present client_id in the body only: %v / %q", hit.form, hit.auth)
	}
	if hit.form.Get("scope") != "" {
		t.Errorf("no scopes requested, got %q", hit.form.Get("scope"))
	}
}

func TestDeviceBegin_RequiresAdvertisedEndpoint(t *testing.T) {
	f := newASFixture(t)
	client := newTestClient(t, f)
	meta := testMeta(f.base(), func(as *oauth.AuthorizationServerMetadata) {
		as.DeviceAuthorizationEndpoint = ""
	})
	rc := byoPublic(t, client, meta)
	if _, err := client.BeginDeviceAuthorization(context.Background(), meta, rc, nil); err == nil {
		t.Fatal("expected a rejection when no device endpoint is advertised")
	}
}

func TestDevicePollOutcomes(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		want       oauth.DeviceOutcome
		wantErr    bool
		wantTokens bool
	}{
		{"success", 200, `{"access_token":"at-dev","refresh_token":"rt-dev","expires_in":3600}`, oauth.DeviceSuccess, false, true},
		{"authorization_pending", 400, `{"error":"authorization_pending"}`, oauth.DeviceAuthorizationPending, false, false},
		{"slow_down", 400, `{"error":"slow_down"}`, oauth.DeviceSlowDown, false, false},
		{"expired_token", 400, `{"error":"expired_token"}`, oauth.DeviceExpiredToken, false, false},
		{"access_denied", 400, `{"error":"access_denied"}`, oauth.DeviceAccessDenied, false, false},
		{"unexpected protocol error", 400, `{"error":"invalid_grant","error_description":"device code unknown"}`, 0, true, false},
		{"provider crashed", 500, `oops`, 0, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newASFixture(t)
			f.mu.Lock()
			f.tokenHandler = func(int) (int, string) { return tc.status, tc.body }
			f.mu.Unlock()
			client := newTestClient(t, f)
			meta := testMeta(f.base(), nil)
			rc := byoPublic(t, client, meta)
			grant := &oauth.DeviceGrant{DeviceCode: "dc-1", Interval: time.Millisecond, ExpiresIn: 10 * time.Second}

			result, err := client.PollDeviceToken(context.Background(), meta, rc, grant)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %+v", result)
				}
				if !errors.Is(err, oauth.ErrTokenExchange) {
					t.Errorf("errors chain to ErrTokenExchange, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Outcome != tc.want {
				t.Errorf("outcome = %v, want %v", result.Outcome, tc.want)
			}
			if tc.wantTokens {
				if result.Tokens == nil || result.Tokens.AccessToken != "at-dev" {
					t.Errorf("expected the token set on success, got %+v", result.Tokens)
				}
			} else if result.Tokens != nil {
				t.Errorf("no tokens may ride a non-success outcome: %+v", result.Tokens)
			}
		})
	}
}

func TestDeviceAwait_HappyPathAndSlowDown(t *testing.T) {
	f := newASFixture(t)
	f.mu.Lock()
	// pending → slow_down → pending → success: the loop must grow the
	// interval on slow_down (the configured step) and keep polling.
	f.tokenHandler = func(call int) (int, string) {
		switch call {
		case 0:
			return 400, `{"error":"authorization_pending"}`
		case 1:
			return 400, `{"error":"slow_down"}`
		case 2:
			return 400, `{"error":"authorization_pending"}`
		default:
			return 200, `{"access_token":"at-dev-1","expires_in":600}`
		}
	}
	f.mu.Unlock()
	client := newTestClient(t, f,
		oauth.WithDeviceSlowDownStep(time.Millisecond),
		oauth.WithDeviceIntervalFloor(time.Millisecond),
	)
	meta := testMeta(f.base(), nil)
	rc := byoPublic(t, client, meta)
	grant := &oauth.DeviceGrant{DeviceCode: "dc-1", Interval: time.Millisecond, ExpiresIn: 10 * time.Second}

	tokens, err := client.AwaitDeviceToken(context.Background(), meta, rc, grant)
	if err != nil {
		t.Fatalf("await: %v", err)
	}
	if tokens.AccessToken != "at-dev-1" {
		t.Errorf("unexpected tokens: %+v", tokens)
	}
	// The RFC default slow_down step is pinned at five seconds even though
	// the test overrides it.
	if oauth.DefaultDeviceSlowDownStep != 5*time.Second {
		t.Errorf("DefaultDeviceSlowDownStep = %v, want the RFC 8628 5s", oauth.DefaultDeviceSlowDownStep)
	}
	if got := len(f.tokenHits()); got != 4 {
		t.Errorf("poll count = %d, want 4 (pending, slow_down, pending, success)", got)
	}
}

func TestDeviceAwait_TerminalOutcomes(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr error
	}{
		{"provider expired the code", 400, `{"error":"expired_token"}`, oauth.ErrDeviceExpired},
		{"user denied", 400, `{"error":"access_denied"}`, oauth.ErrDeviceDenied},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newASFixture(t)
			f.mu.Lock()
			f.tokenHandler = func(int) (int, string) { return tc.status, tc.body }
			f.mu.Unlock()
			client := newTestClient(t, f,
				oauth.WithDeviceSlowDownStep(time.Millisecond),
				oauth.WithDeviceIntervalFloor(time.Millisecond),
			)
			meta := testMeta(f.base(), nil)
			rc := byoPublic(t, client, meta)
			grant := &oauth.DeviceGrant{DeviceCode: "dc-1", Interval: time.Millisecond, ExpiresIn: 10 * time.Second}

			if _, err := client.AwaitDeviceToken(context.Background(), meta, rc, grant); !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestDeviceAwait_HardExpiresInBound(t *testing.T) {
	// The poll loop never runs past the device code's lifetime: a permanently
	// pending code with a 30ms window ends in ErrDeviceTimedOut.
	f := newASFixture(t)
	f.mu.Lock()
	f.tokenHandler = func(int) (int, string) { return 400, `{"error":"authorization_pending"}` }
	f.mu.Unlock()
	client := newTestClient(t, f,
		oauth.WithDeviceSlowDownStep(time.Millisecond),
		oauth.WithDeviceIntervalFloor(time.Millisecond),
	)
	meta := testMeta(f.base(), nil)
	rc := byoPublic(t, client, meta)
	grant := &oauth.DeviceGrant{DeviceCode: "dc-1", Interval: time.Millisecond, ExpiresIn: 30 * time.Millisecond}

	start := time.Now()
	if _, err := client.AwaitDeviceToken(context.Background(), meta, rc, grant); !errors.Is(err, oauth.ErrDeviceTimedOut) {
		t.Fatalf("error = %v, want ErrDeviceTimedOut", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("the hard bound did not hold: %v elapsed", elapsed)
	}
}

func TestDeviceAwait_ContextCancellationPropagates(t *testing.T) {
	f := newASFixture(t)
	f.mu.Lock()
	f.tokenHandler = func(int) (int, string) { return 400, `{"error":"authorization_pending"}` }
	f.mu.Unlock()
	client := newTestClient(t, f,
		oauth.WithDeviceSlowDownStep(time.Millisecond),
		oauth.WithDeviceIntervalFloor(time.Millisecond),
	)
	meta := testMeta(f.base(), nil)
	rc := byoPublic(t, client, meta)
	grant := &oauth.DeviceGrant{DeviceCode: "dc-1", Interval: time.Second, ExpiresIn: time.Hour}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := client.AwaitDeviceToken(ctx, meta, rc, grant); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
}

// ---------------------------------------------------------------------------
// Refresh lifecycle (task 4.6) — fail-open semantics over the seams
// ---------------------------------------------------------------------------

// fakeCredStore is the CredentialStore seam over one in-memory credential.
type fakeCredStore struct {
	mu       sync.Mutex
	cred     *oauth.Credential
	loadErr  error
	replaceN int
	replaceE error
}

func (s *fakeCredStore) Load(ctx context.Context, ref oauth.CredentialRef) (*oauth.Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	if s.cred == nil {
		return nil, oauth.ErrNoCredential
	}
	cp := *s.cred
	return &cp, nil
}

func (s *fakeCredStore) Replace(ctx context.Context, ref oauth.CredentialRef, cred *oauth.Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.replaceE != nil {
		return s.replaceE
	}
	s.replaceN++
	s.cred = cred
	return nil
}

// fakeStatusSink records the expired transitions.
type fakeStatusSink struct {
	mu     sync.Mutex
	calls  int
	detail string
	err    error
}

func (s *fakeStatusSink) MarkExpired(ctx context.Context, ref oauth.CredentialRef, detail string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.detail = detail
	return s.err
}

// inMargin returns an expiry inside the default refresh margin.
func inMargin() *time.Time {
	t := time.Now().Add(time.Minute)
	return &t
}

func TestEnsureFreshCredential_WithinMarginRefreshesAndReplaces(t *testing.T) {
	f := newASFixture(t)
	client := newTestClient(t, f)
	meta := testMeta(f.base(), nil)
	rc := byoPublic(t, client, meta)
	store := &fakeCredStore{cred: &oauth.Credential{
		AccessToken:   "at-old",
		RefreshToken:  "rt-old",
		ExpiresAt:     inMargin(),
		GrantedScopes: []string{testScope},
		Issuer:        f.base(),
	}}
	sink := &fakeStatusSink{}

	token, err := client.EnsureFreshCredential(context.Background(), oauth.EnsureCredentialParams{Meta: meta, Client: rc}, oauth.CredentialRef{WorkspaceID: "ws", ServerID: "srv"}, store, sink)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if token != "at-fresh-1" {
		t.Errorf("token = %q, want the renewed one", token)
	}
	hits := f.tokenHits()
	if len(hits) != 1 || hits[0].form.Get("grant_type") != "refresh_token" || hits[0].form.Get("refresh_token") != "rt-old" {
		t.Fatalf("expected one refresh grant, got %+v", hits)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.replaceN != 1 {
		t.Fatalf("expected exactly one atomic Replace, got %d", store.replaceN)
	}
	// Rotation: the response's refresh token replaces the stored one; expiry
	// advanced; granted scopes and issuer recorded.
	if store.cred.RefreshToken != "rt-fresh-1" || store.cred.AccessToken != "at-fresh-1" {
		t.Errorf("unexpected renewed credential: %+v", store.cred)
	}
	if store.cred.ExpiresAt == nil || time.Until(*store.cred.ExpiresAt) <= 0 {
		t.Errorf("expected an advanced expiry, got %v", store.cred.ExpiresAt)
	}
	if store.cred.Issuer != f.base() {
		t.Errorf("issuer = %q, want the metadata issuer", store.cred.Issuer)
	}
	if sink.calls != 0 {
		t.Errorf("a successful refresh must not touch the status, got %d expired writes", sink.calls)
	}
}

func TestEnsureFreshCredential_OutsideMarginIsPassthrough(t *testing.T) {
	f := newASFixture(t)
	client := newTestClient(t, f)
	meta := testMeta(f.base(), nil)
	rc := byoPublic(t, client, meta)
	fresh := time.Now().Add(30 * time.Minute)
	store := &fakeCredStore{cred: &oauth.Credential{AccessToken: "at-ok", RefreshToken: "rt-ok", ExpiresAt: &fresh}}
	sink := &fakeStatusSink{}

	token, err := client.EnsureFreshCredential(context.Background(), oauth.EnsureCredentialParams{Meta: meta, Client: rc}, oauth.CredentialRef{WorkspaceID: "ws", ServerID: "srv"}, store, sink)
	if err != nil || token != "at-ok" {
		t.Fatalf("expected the stored token passthrough, got %q / %v", token, err)
	}
	if len(f.tokenHits()) != 0 || store.replaceN != 0 {
		t.Error("outside the margin nothing may be requested or written")
	}
}

func TestEnsureFreshCredential_MarginReusesConnectionsValue(t *testing.T) {
	// One refresh convention: the MCP margin IS the connections flow's
	// default recipe margin.
	if oauth.DefaultTokenRefreshMargin != domain.DefaultRecipeRefreshMargin {
		t.Errorf("margin = %v, want the connections flow's %v", oauth.DefaultTokenRefreshMargin, domain.DefaultRecipeRefreshMargin)
	}
}

func TestEnsureFreshCredential_NoStoredCredential(t *testing.T) {
	f := newASFixture(t)
	client := newTestClient(t, f)
	meta := testMeta(f.base(), nil)
	rc := byoPublic(t, client, meta)
	store := &fakeCredStore{}
	sink := &fakeStatusSink{}

	if _, err := client.EnsureFreshCredential(context.Background(), oauth.EnsureCredentialParams{Meta: meta, Client: rc}, oauth.CredentialRef{WorkspaceID: "ws", ServerID: "srv"}, store, sink); !errors.Is(err, oauth.ErrNoCredential) {
		t.Fatalf("error = %v, want ErrNoCredential", err)
	}
}

func TestEnsureFreshCredential_FailOpenOnRefusal(t *testing.T) {
	f := newASFixture(t)
	f.mu.Lock()
	f.tokenHandler = func(int) (int, string) {
		return 400, `{"error":"invalid_grant","error_description":"refresh token is revoked"}`
	}
	f.mu.Unlock()
	client := newTestClient(t, f)
	meta := testMeta(f.base(), nil)
	rc := byoPublic(t, client, meta)
	store := &fakeCredStore{cred: &oauth.Credential{
		AccessToken:  "at-still-here",
		RefreshToken: "rt-revoked-secret",
		ExpiresAt:    inMargin(),
	}}
	sink := &fakeStatusSink{}

	// FAIL OPEN: the stored token is returned with NO error — the dial
	// proceeds — while the expired transition persists with the provider's
	// detail.
	token, err := client.EnsureFreshCredential(context.Background(), oauth.EnsureCredentialParams{Meta: meta, Client: rc}, oauth.CredentialRef{WorkspaceID: "ws", ServerID: "srv"}, store, sink)
	if err != nil {
		t.Fatalf("a refused refresh must fail open, got error %v", err)
	}
	if token != "at-still-here" {
		t.Errorf("token = %q, want the stored one", token)
	}
	if sink.calls != 1 {
		t.Fatalf("expected exactly one expired write, got %d", sink.calls)
	}
	if !strings.Contains(sink.detail, "refresh token is revoked") {
		t.Errorf("the provider's detail must persist, got %q", sink.detail)
	}
	// Secrets discipline: the error detail never carries token material.
	if strings.Contains(sink.detail, "rt-revoked-secret") || strings.Contains(sink.detail, "at-still-here") {
		t.Error("the expired detail must never carry token material")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.replaceN != 0 {
		t.Error("a refused refresh must not replace the rows")
	}
}

func TestEnsureFreshCredential_NoRefreshTokenIsExpiryOnly(t *testing.T) {
	f := newASFixture(t)
	client := newTestClient(t, f)
	meta := testMeta(f.base(), nil)
	rc := byoPublic(t, client, meta)
	store := &fakeCredStore{cred: &oauth.Credential{AccessToken: "at-only", ExpiresAt: inMargin()}}
	sink := &fakeStatusSink{}

	token, err := client.EnsureFreshCredential(context.Background(), oauth.EnsureCredentialParams{Meta: meta, Client: rc}, oauth.CredentialRef{WorkspaceID: "ws", ServerID: "srv"}, store, sink)
	if err != nil || token != "at-only" {
		t.Fatalf("expected the stored token, got %q / %v", token, err)
	}
	if len(f.tokenHits()) != 0 || sink.calls != 0 {
		t.Error("an expiry-only credential must not be refreshed or marked expired")
	}
}

func TestEnsureFreshCredential_UnreadableRefreshEnvelopeFailsOpenAndMarksExpired(t *testing.T) {
	f := newASFixture(t)
	client := newTestClient(t, f)
	meta := testMeta(f.base(), nil)
	rc := byoPublic(t, client, meta)
	store := &fakeCredStore{cred: &oauth.Credential{
		AccessToken:       "at-dials",
		ExpiresAt:         inMargin(),
		RefreshUnreadable: true,
	}}
	sink := &fakeStatusSink{}

	token, err := client.EnsureFreshCredential(context.Background(), oauth.EnsureCredentialParams{Meta: meta, Client: rc}, oauth.CredentialRef{WorkspaceID: "ws", ServerID: "srv"}, store, sink)
	if err != nil || token != "at-dials" {
		t.Fatalf("expected the stored token fail-open, got %q / %v", token, err)
	}
	if sink.calls != 1 || !strings.Contains(sink.detail, "could not be opened") {
		t.Errorf("expected one expired write with the unreadable-envelope detail, got %d / %q", sink.calls, sink.detail)
	}
	if len(f.tokenHits()) != 0 {
		t.Error("nothing may be sent to the token endpoint without a refresh token")
	}
}

func TestEnsureFreshCredential_OmittedRefreshTokenKeepsStoredOne(t *testing.T) {
	// RFC 6749 §6: an omitted refresh token keeps the stored one (replay vs
	// rotation is the provider's call).
	f := newASFixture(t)
	f.mu.Lock()
	f.tokenBody = `{"access_token":"at-new","expires_in":3600}`
	f.mu.Unlock()
	client := newTestClient(t, f)
	meta := testMeta(f.base(), nil)
	rc := byoPublic(t, client, meta)
	store := &fakeCredStore{cred: &oauth.Credential{
		AccessToken:   "at-old",
		RefreshToken:  "rt-keep",
		ExpiresAt:     inMargin(),
		GrantedScopes: []string{"old-scope"},
	}}
	sink := &fakeStatusSink{}

	if _, err := client.EnsureFreshCredential(context.Background(), oauth.EnsureCredentialParams{Meta: meta, Client: rc}, oauth.CredentialRef{WorkspaceID: "ws", ServerID: "srv"}, store, sink); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.cred.RefreshToken != "rt-keep" {
		t.Errorf("refresh token = %q, want the stored one kept", store.cred.RefreshToken)
	}
	if len(store.cred.GrantedScopes) != 1 || store.cred.GrantedScopes[0] != "old-scope" {
		t.Errorf("an omitted scope echo keeps the granted set, got %v", store.cred.GrantedScopes)
	}
}

func TestEnsureFreshCredential_ReplaceFailureSurfaces(t *testing.T) {
	f := newASFixture(t)
	client := newTestClient(t, f)
	meta := testMeta(f.base(), nil)
	rc := byoPublic(t, client, meta)
	store := &fakeCredStore{
		cred:     &oauth.Credential{AccessToken: "at-old", RefreshToken: "rt-old", ExpiresAt: inMargin()},
		replaceE: errors.New("store write failed"),
	}
	sink := &fakeStatusSink{}

	if _, err := client.EnsureFreshCredential(context.Background(), oauth.EnsureCredentialParams{Meta: meta, Client: rc}, oauth.CredentialRef{WorkspaceID: "ws", ServerID: "srv"}, store, sink); err == nil {
		t.Fatal("a store failure must surface (only refresh refusals fail open)")
	}
}
