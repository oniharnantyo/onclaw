package oauth_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents/mcp/oauth"
)

// ---------------------------------------------------------------------------
// The authorization-server fixture: one TLS httptest server recording every
// token / registration / device / metadata-document request. TLS so client-id
// metadata document URLs (HTTPS-only by the draft) can point at it.
// ---------------------------------------------------------------------------

const (
	testRedirectURI = "https://onclaw.example.com/api/v1/mcp/oauth/callback"
	byoClientID     = "byo-client-1"
	byoClientSecret = "byo-secret-9999"
	testScope       = "mcp"
	testMasterKey   = "01234567890123456789012345678901"
)

// capturedTokenCall is one recorded token/device-endpoint request.
type capturedTokenCall struct {
	path string
	form url.Values
	auth string // the Authorization header, verbatim
}

// asFixture is one authorization server: fixed routes with per-test
// handler overrides, recording every hit.
type asFixture struct {
	srv *httptest.Server
	mu  sync.Mutex

	tokenCalls  []capturedTokenCall
	regRequests []map[string]any
	regHits     int
	deviceCalls []capturedTokenCall
	docHits     int

	// tokenHandler overrides the default token response; nil → 200 tokenBody.
	tokenHandler func(call int) (int, string)
	tokenBody    string
	// regStatus/regBody are the registration endpoint's reply.
	regStatus int
	regBody   string
	// deviceBodies are consumed in order for /oauth/device replies.
	deviceBodies []string
	docStatus    int
	docBody      string
}

func newASFixture(t *testing.T) *asFixture {
	t.Helper()
	f := &asFixture{
		tokenBody:    `{"access_token":"at-fresh-1","refresh_token":"rt-fresh-1","expires_in":3600,"scope":"` + testScope + `"}`,
		regStatus:    http.StatusCreated,
		regBody:      `{"client_id":"dcr-client-1","token_endpoint_auth_method":"none","redirect_uris":["` + testRedirectURI + `"],"grant_types":["authorization_code","refresh_token"],"response_types":["code"]}`,
		deviceBodies: []string{`{"device_code":"dc-1","user_code":"ABCD-EFGH","verification_uri":"https://example.com/activate","verification_uri_complete":"https://example.com/activate?code=ABCDEFGH","expires_in":600,"interval":1}`},
		docStatus:    http.StatusOK,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		f.tokenCalls = append(f.tokenCalls, capturedTokenCall{path: r.URL.Path, form: r.PostForm, auth: r.Header.Get("Authorization")})
		n := len(f.tokenCalls) - 1
		handler := f.tokenHandler
		body := f.tokenBody
		f.mu.Unlock()
		status := http.StatusOK
		if handler != nil {
			status, body = handler(n)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	})
	mux.HandleFunc("/oauth/register", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		_ = json.Unmarshal(raw, &parsed)
		f.mu.Lock()
		f.regHits++
		f.regRequests = append(f.regRequests, parsed)
		status, body := f.regStatus, f.regBody
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	})
	mux.HandleFunc("/oauth/device", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		f.deviceCalls = append(f.deviceCalls, capturedTokenCall{path: r.URL.Path, form: r.PostForm, auth: r.Header.Get("Authorization")})
		idx := len(f.deviceCalls) - 1
		body := `{"error":"server_error"}`
		if idx < len(f.deviceBodies) {
			body = f.deviceBodies[idx]
		}
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	})
	mux.HandleFunc("/meta/client", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.docHits++
		status, body := f.docStatus, f.docBody
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	})
	f.srv = httptest.NewTLSServer(mux)
	t.Cleanup(f.srv.Close)
	// The default token response echoes the RFC 9207 iss (the fixture is the
	// issuer); the issuer check strips one trailing slash.
	base := strings.TrimRight(f.srv.URL, "/")
	f.tokenBody = `{"access_token":"at-fresh-1","refresh_token":"rt-fresh-1","expires_in":3600,"scope":"` + testScope + `","iss":"` + base + `"}`
	return f
}

// base is the fixture's issuer (trailing slash stripped — the RFC 8414 issuer
// check strips one).
func (f *asFixture) base() string {
	return strings.TrimRight(f.srv.URL, "/")
}

func (f *asFixture) tokenHits() []capturedTokenCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]capturedTokenCall(nil), f.tokenCalls...)
}

func (f *asFixture) registrationHits() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.regHits
}

func (f *asFixture) lastRegistration() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.regRequests) == 0 {
		return nil
	}
	return f.regRequests[len(f.regRequests)-1]
}

func (f *asFixture) deviceHits() []capturedTokenCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]capturedTokenCall(nil), f.deviceCalls...)
}

func (f *asFixture) documentHits() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.docHits
}

// newTestClient builds a Client wired to the fixture: its HTTP client trusts
// the fixture's TLS certificate and the discovery chain shares it.
func newTestClient(t *testing.T, f *asFixture, opts ...oauth.ClientOption) *oauth.Client {
	t.Helper()
	all := append([]oauth.ClientOption{
		oauth.WithClientHTTPClient(f.srv.Client()),
		oauth.WithDiscovery(oauth.NewDiscovery(oauth.WithHTTPClient(f.srv.Client()))),
	}, opts...)
	return oauth.NewClient([]byte(testMasterKey), all...)
}

// testMeta builds the discovery result for the fixture's authorization server.
func testMeta(base string, mutate func(*oauth.AuthorizationServerMetadata)) *oauth.Metadata {
	as := &oauth.AuthorizationServerMetadata{
		Issuer:                            base,
		AuthorizationEndpoint:             base + "/oauth/authorize",
		TokenEndpoint:                     base + "/oauth/token",
		RegistrationEndpoint:              base + "/oauth/register",
		DeviceAuthorizationEndpoint:       base + "/oauth/device",
		ScopesSupported:                   []string{testScope, "api"},
		CodeChallengeMethodsSupported:     []string{"S256"},
		TokenEndpointAuthMethodsSupported: []string{"none", "client_secret_basic"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
	}
	if mutate != nil {
		mutate(as)
	}
	return &oauth.Metadata{
		ServerURL:              base + "/mcp",
		Resource:               &oauth.ProtectedResourceMetadata{Resource: base + "/mcp", ScopesSupported: []string{testScope}},
		AuthorizationServerURL: base,
		AuthorizationServer:    as,
	}
}

// resolve is the test helper running ResolveClient with the given identity.
func resolve(t *testing.T, client *oauth.Client, meta *oauth.Metadata, id oauth.ClientIdentity) *oauth.ResolvedClient {
	t.Helper()
	rc, err := client.ResolveClient(context.Background(), meta, id)
	if err != nil {
		t.Fatalf("resolve client: %v", err)
	}
	return rc
}

// byoConfidential resolves the fixture's BYO confidential client.
func byoConfidential(t *testing.T, client *oauth.Client, meta *oauth.Metadata) *oauth.ResolvedClient {
	t.Helper()
	return resolve(t, client, meta, oauth.ClientIdentity{
		ConfiguredClientID:     byoClientID,
		ConfiguredClientSecret: byoClientSecret,
		RedirectURI:            testRedirectURI,
	})
}

// byoPublic resolves the fixture's BYO public client.
func byoPublic(t *testing.T, client *oauth.Client, meta *oauth.Metadata) *oauth.ResolvedClient {
	t.Helper()
	return resolve(t, client, meta, oauth.ClientIdentity{
		ConfiguredClientID: byoClientID,
		RedirectURI:        testRedirectURI,
	})
}

// ---------------------------------------------------------------------------
// Strategy resolution (tasks 4.1–4.3)
// ---------------------------------------------------------------------------

func TestResolveClient_BYOConfidential(t *testing.T) {
	f := newASFixture(t)
	client := newTestClient(t, f)
	base := f.base()

	cases := []struct {
		name           string
		advertised     []string
		wantAuthMethod string
		wantPKCE       bool
	}{
		{"none and basic advertised", []string{"none", oauth.AuthMethodBasic}, oauth.AuthMethodBasic, true},
		{"only post advertised", []string{oauth.AuthMethodPost}, oauth.AuthMethodPost, true},
		{"nothing advertised (RFC 8414 default)", nil, oauth.AuthMethodBasic, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			meta := testMeta(base, func(as *oauth.AuthorizationServerMetadata) {
				as.TokenEndpointAuthMethodsSupported = tc.advertised
			})
			rc := byoConfidential(t, client, meta)
			if rc.Strategy != oauth.StrategyBYO || rc.ClientID != byoClientID || rc.ClientSecret != byoClientSecret {
				t.Fatalf("unexpected resolution: %+v", rc)
			}
			if rc.AuthMethod != tc.wantAuthMethod {
				t.Errorf("auth method = %q, want %q", rc.AuthMethod, tc.wantAuthMethod)
			}
			if rc.UsePKCE != tc.wantPKCE {
				t.Errorf("UsePKCE = %v, want %v", rc.UsePKCE, tc.wantPKCE)
			}
		})
	}
	// Resolution is local: no HTTP request may fire for a BYO client.
	if hits := f.tokenHits(); len(hits) != 0 {
		t.Errorf("BYO resolution made %d token-endpoint calls; strategies must not", len(hits))
	}
	if f.registrationHits() != 0 {
		t.Error("BYO resolution registered a client")
	}
}

func TestResolveClient_BYOPublicAlwaysUsesPKCE(t *testing.T) {
	f := newASFixture(t)
	client := newTestClient(t, f)
	// The AS advertises NO S256 support — the public client still rides PKCE
	// (pinned: without a client secret, PKCE is the only interception guard).
	meta := testMeta(f.base(), func(as *oauth.AuthorizationServerMetadata) {
		as.CodeChallengeMethodsSupported = nil
	})
	rc := byoPublic(t, client, meta)
	if rc.Strategy != oauth.StrategyBYO || rc.AuthMethod != oauth.AuthMethodNone || rc.ClientSecret != "" {
		t.Fatalf("unexpected public resolution: %+v", rc)
	}
	if !rc.UsePKCE {
		t.Error("public client must always use PKCE, even unadvertised")
	}
}

func TestResolveClient_CIDM(t *testing.T) {
	f := newASFixture(t)
	client := newTestClient(t, f)
	base := f.base()
	f.mu.Lock()
	f.docBody = `{"client_id":"` + base + `/meta/client","client_name":"Self-published","redirect_uris":["` + testRedirectURI + `","https://other.example/callback"],"token_endpoint_auth_method":"none"}`
	f.mu.Unlock()

	docURL := base + "/meta/client"
	rc := resolve(t, client, testMeta(base, nil), oauth.ClientIdentity{
		ConfiguredClientID: docURL,
		RedirectURI:        testRedirectURI,
	})
	if rc.Strategy != oauth.StrategyCIDM || rc.ClientID != docURL || rc.MetadataDocumentURL != docURL {
		t.Fatalf("unexpected CIDM resolution: %+v", rc)
	}
	if rc.AuthMethod != oauth.AuthMethodNone || !rc.UsePKCE {
		t.Errorf("CIDM clients are public + PKCE, got %+v", rc)
	}
	if f.registrationHits() != 0 {
		t.Error("CIDM resolution must not register")
	}
	if f.documentHits() != 1 {
		t.Errorf("document fetched %d times, want 1", f.documentHits())
	}
}

func TestResolveClient_CIDMInvalid(t *testing.T) {
	cases := []struct {
		name    string
		docBody string
		docStat int
		wantIn  string
	}{
		{"no redirect_uris", `{"client_name":"broken"}`, http.StatusOK, "redirect_uris"},
		{"callback not registered", `{"redirect_uris":["https://other.example/callback"]}`, http.StatusOK, testRedirectURI},
		{"client_id mismatch", `{"client_id":"https://someone.else/client","redirect_uris":["` + testRedirectURI + `"]}`, http.StatusOK, "does not match"},
		{"confidential method unsupported", `{"redirect_uris":["` + testRedirectURI + `"],"token_endpoint_auth_method":"client_secret_basic"}`, http.StatusOK, "public"},
		{"grant types without authorization_code", `{"redirect_uris":["` + testRedirectURI + `"],"grant_types":["client_credentials"]}`, http.StatusOK, "authorization_code"},
		{"response types without code", `{"redirect_uris":["` + testRedirectURI + `"],"response_types":["token"]}`, http.StatusOK, "code"},
		{"not json", `<html>not a document</html>`, http.StatusOK, "not a valid JSON"},
		{"missing document", `{"whatever":true}`, http.StatusNotFound, "could not be fetched"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newASFixture(t)
			f.mu.Lock()
			f.docBody = tc.docBody
			f.docStatus = tc.docStat
			f.mu.Unlock()
			client := newTestClient(t, f)
			_, err := client.ResolveClient(context.Background(), testMeta(f.base(), nil), oauth.ClientIdentity{
				ConfiguredClientID: f.base() + "/meta/client",
				RedirectURI:        testRedirectURI,
			})
			if err == nil {
				t.Fatal("expected a resolution failure")
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Errorf("error %q does not mention %q", err.Error(), tc.wantIn)
			}
			var cre *oauth.ClientResolutionError
			if !errors.As(err, &cre) || cre.Strategy != oauth.StrategyCIDM {
				t.Errorf("expected a CIDM ClientResolutionError, got %v", err)
			} else if !strings.Contains(cre.Guidance(), "metadata document") {
				t.Errorf("guidance %q should point at the metadata document", cre.Guidance())
			}
		})
	}
}

func TestResolveClient_HTTPIDIsBYONotCIDM(t *testing.T) {
	// An http:// (non-HTTPS) client id is NOT a metadata-document reference —
	// the draft is HTTPS-only — so it routes to BYO as an opaque id.
	f := newASFixture(t)
	client := newTestClient(t, f)
	rc := resolve(t, client, testMeta(f.base(), nil), oauth.ClientIdentity{
		ConfiguredClientID: "http://legacy.example/client",
		RedirectURI:        testRedirectURI,
	})
	if rc.Strategy != oauth.StrategyBYO {
		t.Errorf("expected BYO for a non-HTTPS client id, got %q", rc.Strategy)
	}
	if f.documentHits() != 0 {
		t.Error("no metadata document may be fetched for a non-HTTPS client id")
	}
}

func TestResolveClient_DCR(t *testing.T) {
	f := newASFixture(t)
	client := newTestClient(t, f)
	rc := resolve(t, client, testMeta(f.base(), nil), oauth.ClientIdentity{RedirectURI: testRedirectURI})

	if rc.Strategy != oauth.StrategyDCR || rc.ReusedRegistration {
		t.Fatalf("unexpected DCR resolution: %+v", rc)
	}
	if rc.ClientID != "dcr-client-1" || rc.Registration == nil {
		t.Fatalf("expected the registration result handed back, got %+v", rc)
	}
	if rc.AuthMethod != oauth.AuthMethodNone || !rc.UsePKCE {
		t.Errorf("public+PKCE preferred when advertised, got %+v", rc)
	}
	reg := f.lastRegistration()
	if reg == nil {
		t.Fatal("no registration request recorded")
	}
	if got, _ := reg["token_endpoint_auth_method"].(string); got != oauth.AuthMethodNone {
		t.Errorf("registered auth method = %q, want none", got)
	}
	uris, _ := reg["redirect_uris"].([]any)
	if len(uris) != 1 || uris[0] != testRedirectURI {
		t.Errorf("registered redirect_uris = %v, want [%q]", uris, testRedirectURI)
	}
	if got, _ := reg["scope"].(string); got != testScope {
		t.Errorf("registered scope = %q, want the PRM's %q", got, testScope)
	}
	for _, key := range []string{"grant_types", "response_types", "client_name"} {
		if _, ok := reg[key]; !ok {
			t.Errorf("registration request missing %s", key)
		}
	}
}

func TestResolveClient_DCRPrefersBasicWhenNoneUnadvertised(t *testing.T) {
	f := newASFixture(t)
	client := newTestClient(t, f)
	meta := testMeta(f.base(), func(as *oauth.AuthorizationServerMetadata) {
		as.TokenEndpointAuthMethodsSupported = []string{oauth.AuthMethodBasic}
	})
	// The registration response is authoritative per RFC 7591 §3.2.1 — the
	// fixture echoes the requested method back.
	f.mu.Lock()
	f.regBody = `{"client_id":"dcr-client-1","token_endpoint_auth_method":"client_secret_basic","client_secret":"dcr-secret-1"}`
	f.mu.Unlock()
	rc := resolve(t, client, meta, oauth.ClientIdentity{RedirectURI: testRedirectURI})
	if rc.AuthMethod != oauth.AuthMethodBasic {
		t.Errorf("auth method = %q, want client_secret_basic", rc.AuthMethod)
	}
	if got, _ := f.lastRegistration()["token_endpoint_auth_method"].(string); got != oauth.AuthMethodBasic {
		t.Errorf("registered method = %q, want client_secret_basic", got)
	}
}

func TestResolveClient_DCRSkipWhenStored(t *testing.T) {
	// The register-once seam: a caller-held registration skips the
	// registration request entirely.
	f := newASFixture(t)
	client := newTestClient(t, f)
	stored := &oauth.Registration{ClientID: "stored-client-1", TokenEndpointAuthMethod: oauth.AuthMethodNone}
	rc := resolve(t, client, testMeta(f.base(), nil), oauth.ClientIdentity{
		RedirectURI:        testRedirectURI,
		StoredRegistration: stored,
	})
	if f.registrationHits() != 0 {
		t.Errorf("stored registration must skip the registration endpoint, got %d calls", f.registrationHits())
	}
	if !rc.ReusedRegistration || rc.ClientID != "stored-client-1" || rc.Strategy != oauth.StrategyDCR {
		t.Fatalf("unexpected reuse resolution: %+v", rc)
	}
	if !rc.UsePKCE {
		t.Error("reused public registration must keep PKCE")
	}
}

func TestResolveClient_DCRRejected(t *testing.T) {
	f := newASFixture(t)
	f.mu.Lock()
	f.regStatus = http.StatusForbidden
	f.regBody = `{"error":"access_denied","error_description":"dynamic client registration is disabled"}`
	f.mu.Unlock()
	client := newTestClient(t, f)

	_, err := client.ResolveClient(context.Background(), testMeta(f.base(), nil), oauth.ClientIdentity{RedirectURI: testRedirectURI})
	var cre *oauth.ClientResolutionError
	if !errors.As(err, &cre) || cre.Strategy != oauth.StrategyDCR {
		t.Fatalf("expected a DCR ClientResolutionError, got %v", err)
	}
	if !strings.Contains(err.Error(), "dynamic client registration is disabled") {
		t.Errorf("the provider's error detail must ride the failure, got %q", err.Error())
	}
	if !strings.Contains(cre.Guidance(), "pre-registered") {
		t.Errorf("guidance must point at pre-registered apps, got %q", cre.Guidance())
	}
}

func TestResolveClient_NoStrategyAvailable(t *testing.T) {
	f := newASFixture(t)
	client := newTestClient(t, f)
	meta := testMeta(f.base(), func(as *oauth.AuthorizationServerMetadata) {
		as.RegistrationEndpoint = ""
	})
	_, err := client.ResolveClient(context.Background(), meta, oauth.ClientIdentity{RedirectURI: testRedirectURI})
	var cre *oauth.ClientResolutionError
	if !errors.As(err, &cre) {
		t.Fatalf("expected a ClientResolutionError, got %v", err)
	}
	if !strings.Contains(cre.Guidance(), "pre-registered") {
		t.Errorf("guidance must name the BYO fallback, got %q", cre.Guidance())
	}
}

// ---------------------------------------------------------------------------
// Authorization-code + PKCE S256 (task 4.4)
// ---------------------------------------------------------------------------

// beginFor starts a flow with the given resolved client.
func beginFor(t *testing.T, client *oauth.Client, meta *oauth.Metadata, rc *oauth.ResolvedClient, mutate func(*oauth.StateClaims)) *oauth.AuthorizationBegin {
	t.Helper()
	claims := oauth.StateClaims{WorkspaceID: "ws-1", ServerID: "srv-1"}
	if mutate != nil {
		mutate(&claims)
	}
	begin, err := client.BeginAuthorization(oauth.BeginParams{
		Meta:        meta,
		Client:      rc,
		RedirectURI: testRedirectURI,
		Scopes:      []string{testScope},
		Claims:      claims,
	})
	if err != nil {
		t.Fatalf("begin authorization: %v", err)
	}
	return begin
}

func TestAuthorize_PKCES256Shape(t *testing.T) {
	f := newASFixture(t)
	client := newTestClient(t, f)
	meta := testMeta(f.base(), nil)
	rc := byoConfidential(t, client, meta)
	begin := beginFor(t, client, meta, rc, nil)

	parsed, err := url.Parse(begin.AuthorizeURL)
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}
	q := parsed.Query()
	if q.Get("response_type") != "code" || q.Get("client_id") != byoClientID || q.Get("redirect_uri") != testRedirectURI {
		t.Fatalf("authorize url missing the required params: %v", q)
	}
	if q.Get("state") == "" || q.Get("state") != begin.State {
		t.Fatal("the sealed state must ride the url")
	}
	if q.Get("scope") != testScope {
		t.Errorf("scope = %q, want %q", q.Get("scope"), testScope)
	}
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		t.Fatal("S256 PKCE must ride the url")
	}
	// The secret is sent ONLY to the token endpoint — never in the authorize
	// URL in any encoding.
	if strings.Contains(begin.AuthorizeURL, byoClientSecret) {
		t.Fatal("the client secret must never appear in the authorize url")
	}

	// Complete against the fixture and verify the S256 transform end to end:
	// the challenge in the URL is exactly BASE64URL(SHA256(verifier)).
	result, err := client.CompleteAuthorization(context.Background(), oauth.CompleteParams{
		Meta: meta, Client: rc, RedirectURI: testRedirectURI,
		Code: "the-code", State: begin.State, Session: begin.Session,
	})
	if err != nil {
		t.Fatalf("complete authorization: %v", err)
	}
	hits := f.tokenHits()
	if len(hits) != 1 {
		t.Fatalf("expected exactly one token request, got %d", len(hits))
	}
	verifier := hits[0].form.Get("code_verifier")
	if verifier == "" {
		t.Fatal("the token request must carry the pkce verifier")
	}
	sum := sha256.Sum256([]byte(verifier))
	if want := base64.RawURLEncoding.EncodeToString(sum[:]); q.Get("code_challenge") != want {
		t.Fatalf("challenge = %q, want BASE64URL(SHA256(verifier)) = %q", q.Get("code_challenge"), want)
	}
	if l := len(verifier); l < 43 || l > 128 {
		t.Errorf("verifier length %d outside the RFC 7636 window", l)
	}
	if hits[0].form.Get("grant_type") != "authorization_code" || hits[0].form.Get("code") != "the-code" || hits[0].form.Get("redirect_uri") != testRedirectURI {
		t.Fatalf("unexpected exchange form: %v", hits[0].form)
	}
	// Confidential client via Basic: the secret rides the header (never the
	// form body).
	if hits[0].form.Get("client_secret") != "" {
		t.Error("the client secret must not ride the form body for basic-auth clients")
	}
	if !strings.HasPrefix(hits[0].auth, "Basic ") {
		t.Fatalf("expected Basic client authentication, got %q", hits[0].auth)
	}
	if decoded, derr := base64.StdEncoding.DecodeString(strings.TrimPrefix(hits[0].auth, "Basic ")); derr != nil ||
		string(decoded) != url.QueryEscape(byoClientID)+":"+url.QueryEscape(byoClientSecret) {
		t.Errorf("unexpected Basic credentials: %q (%v)", hits[0].auth, derr)
	}
	// Claims round trip + token parse.
	if result.Claims.WorkspaceID != "ws-1" || result.Claims.ServerID != "srv-1" {
		t.Errorf("claims did not round trip: %+v", result.Claims)
	}
	if result.Tokens.AccessToken != "at-fresh-1" || result.Tokens.ExpiresIn != time.Hour || len(result.Tokens.Scopes) != 1 {
		t.Errorf("unexpected token set: %+v", result.Tokens)
	}
	if result.Tokens.Issuer != f.base() {
		t.Errorf("issuer echo = %q, want %q", result.Tokens.Issuer, f.base())
	}
}

func TestAuthorize_PublicClientTokenRequestShape(t *testing.T) {
	f := newASFixture(t)
	client := newTestClient(t, f)
	meta := testMeta(f.base(), nil)
	rc := byoPublic(t, client, meta)
	begin := beginFor(t, client, meta, rc, nil)

	if _, err := client.CompleteAuthorization(context.Background(), oauth.CompleteParams{
		Meta: meta, Client: rc, RedirectURI: testRedirectURI,
		Code: "the-code", State: begin.State, Session: begin.Session,
	}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	hit := f.tokenHits()[0]
	if hit.form.Get("client_id") != byoClientID || hit.auth != "" {
		t.Fatalf("public clients authenticate by client_id in the body only: %v / %q", hit.form, hit.auth)
	}
	if hit.form.Get("client_secret") != "" {
		t.Error("no secret may ride a public client's token request")
	}
}

func TestAuthorize_PostClientSecretRidesBody(t *testing.T) {
	f := newASFixture(t)
	client := newTestClient(t, f)
	meta := testMeta(f.base(), func(as *oauth.AuthorizationServerMetadata) {
		as.TokenEndpointAuthMethodsSupported = []string{oauth.AuthMethodPost}
	})
	rc := byoConfidential(t, client, meta)
	if rc.AuthMethod != oauth.AuthMethodPost {
		t.Fatalf("auth method = %q, want client_secret_post", rc.AuthMethod)
	}
	begin := beginFor(t, client, meta, rc, nil)
	if _, err := client.CompleteAuthorization(context.Background(), oauth.CompleteParams{
		Meta: meta, Client: rc, RedirectURI: testRedirectURI,
		Code: "c", State: begin.State, Session: begin.Session,
	}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	hit := f.tokenHits()[0]
	if hit.form.Get("client_id") != byoClientID || hit.form.Get("client_secret") != byoClientSecret || hit.auth != "" {
		t.Fatalf("post clients put both credentials in the body only: %v / %q", hit.form, hit.auth)
	}
}

func TestAuthorize_NoPKCEForConfidentialWhenUnadvertised(t *testing.T) {
	f := newASFixture(t)
	client := newTestClient(t, f)
	meta := testMeta(f.base(), func(as *oauth.AuthorizationServerMetadata) {
		as.CodeChallengeMethodsSupported = nil
	})
	rc := byoConfidential(t, client, meta)
	if rc.UsePKCE {
		t.Fatal("confidential clients only use PKCE when S256 is advertised")
	}
	begin := beginFor(t, client, meta, rc, nil)
	if q := mustParseQuery(t, begin.AuthorizeURL); q.Get("code_challenge") != "" {
		t.Error("no challenge may ride the url without S256 support")
	}
	if _, err := client.CompleteAuthorization(context.Background(), oauth.CompleteParams{
		Meta: meta, Client: rc, RedirectURI: testRedirectURI,
		Code: "c", State: begin.State, Session: begin.Session,
	}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if v := f.tokenHits()[0].form.Get("code_verifier"); v != "" {
		t.Error("no verifier may ride the token request without a challenge")
	}
}

func TestComplete_StateRejections(t *testing.T) {
	f := newASFixture(t)
	client := newTestClient(t, f)
	meta := testMeta(f.base(), nil)
	rc := byoPublic(t, client, meta)

	cases := []struct {
		name    string
		arrange func(t *testing.T) (oauth.CompleteParams, int)
		wantErr error
	}{
		{
			name: "forged state",
			arrange: func(t *testing.T) (oauth.CompleteParams, int) {
				begin := beginFor(t, client, meta, rc, nil)
				return oauth.CompleteParams{Meta: meta, Client: rc, RedirectURI: testRedirectURI, Code: "c", State: begin.State + "x", Session: begin.Session}, len(f.tokenHits())
			},
			wantErr: oauth.ErrStateInvalid,
		},
		{
			name: "replayed state (nonce consumed)",
			arrange: func(t *testing.T) (oauth.CompleteParams, int) {
				begin := beginFor(t, client, meta, rc, nil)
				params := oauth.CompleteParams{Meta: meta, Client: rc, RedirectURI: testRedirectURI, Code: "c", State: begin.State, Session: begin.Session}
				if _, err := client.CompleteAuthorization(context.Background(), params); err != nil {
					t.Fatalf("first completion should succeed: %v", err)
				}
				// The count is taken AFTER the successful first completion: the
				// replay itself must add zero token requests.
				return params, len(f.tokenHits())
			},
			wantErr: oauth.ErrStateInvalid,
		},
		{
			name: "expired state",
			arrange: func(t *testing.T) (oauth.CompleteParams, int) {
				begin := beginFor(t, client, meta, rc, func(c *oauth.StateClaims) {
					c.ExpiresAt = time.Now().Add(-time.Minute).Unix()
				})
				return oauth.CompleteParams{Meta: meta, Client: rc, RedirectURI: testRedirectURI, Code: "c", State: begin.State, Session: begin.Session}, len(f.tokenHits())
			},
			wantErr: oauth.ErrStateInvalid,
		},
		{
			name: "session from another flow",
			arrange: func(t *testing.T) (oauth.CompleteParams, int) {
				a := beginFor(t, client, meta, rc, nil)
				b := beginFor(t, client, meta, rc, nil)
				return oauth.CompleteParams{Meta: meta, Client: rc, RedirectURI: testRedirectURI, Code: "c", State: a.State, Session: b.Session}, len(f.tokenHits())
			},
			wantErr: oauth.ErrStateInvalid,
		},
		{
			name: "issuer drift since begin",
			arrange: func(t *testing.T) (oauth.CompleteParams, int) {
				begin := beginFor(t, client, meta, rc, nil)
				drifted := testMeta(f.base(), nil)
				drifted.AuthorizationServer.Issuer = "https://other-as.example"
				return oauth.CompleteParams{Meta: drifted, Client: rc, RedirectURI: testRedirectURI, Code: "c", State: begin.State, Session: begin.Session}, len(f.tokenHits())
			},
			wantErr: oauth.ErrStateInvalid,
		},
		{
			name: "missing code",
			arrange: func(t *testing.T) (oauth.CompleteParams, int) {
				begin := beginFor(t, client, meta, rc, nil)
				return oauth.CompleteParams{Meta: meta, Client: rc, RedirectURI: testRedirectURI, Code: "  ", State: begin.State, Session: begin.Session}, len(f.tokenHits())
			},
			wantErr: oauth.ErrTokenExchange,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params, hitsBefore := tc.arrange(t)
			_, err := client.CompleteAuthorization(context.Background(), params)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
			if got := len(f.tokenHits()) - hitsBefore; got != 0 {
				t.Errorf("%d token request(s) fired on a rejected callback; every rejection must precede the exchange", got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// RFC 9207 iss validation matrix (task 4.4) — pinned: an absent iss is
// tolerated ONLY when the AS metadata does not advertise
// authorization_response_iss_parameter_supported; a present iss is always
// validated against the metadata issuer.
// ---------------------------------------------------------------------------

func TestTokenISSValidation(t *testing.T) {
	cases := []struct {
		name      string
		advertise bool
		iss       string // "" omits iss; "match" echoes the issuer; "other" is foreign
		wantErr   bool
	}{
		{"advertising, iss matches", true, "match", false},
		{"advertising, iss absent", true, "", true},
		{"not advertising, iss matches", false, "match", false},
		{"not advertising, iss absent (tolerated)", false, "", false},
		{"iss from another server", false, "other", true},
		{"advertising, iss from another server", true, "other", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newASFixture(t)
			body := `{"access_token":"at-1"`
			switch tc.iss {
			case "match":
				body += `,"iss":"` + f.base() + `"`
			case "other":
				body += `,"iss":"https://other.example"`
			}
			body += `}`
			f.mu.Lock()
			f.tokenBody = body
			f.mu.Unlock()

			client := newTestClient(t, f)
			meta := testMeta(f.base(), func(as *oauth.AuthorizationServerMetadata) {
				as.AuthorizationResponseISSParameterSupported = tc.advertise
			})
			rc := byoConfidential(t, client, meta)
			tokens, err := client.RefreshTokens(context.Background(), oauth.RefreshInput{Meta: meta, Client: rc, RefreshToken: "rt-1"})
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an iss rejection, got tokens %+v", tokens)
				}
				if !errors.Is(err, oauth.ErrTokenExchange) {
					t.Errorf("iss rejections chain to ErrTokenExchange, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Discovery-composed round trip: the strategies ride a REAL discovery chain.
// ---------------------------------------------------------------------------

func TestResolveClient_AfterRealDiscovery(t *testing.T) {
	// One server plays the whole chain: the well-known documents describe
	// themselves, and the registration endpoint they name lives here too.
	var base string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource/mcp":
			_, _ = io.WriteString(w, `{"resource":"`+base+`/mcp","authorization_servers":["`+base+`"],"scopes_supported":["mcp"]}`)
		case "/.well-known/oauth-authorization-server":
			_, _ = io.WriteString(w, `{"issuer":"`+base+`","authorization_endpoint":"`+base+`/oauth/authorize","token_endpoint":"`+base+`/oauth/token","registration_endpoint":"`+base+`/oauth/register","code_challenge_methods_supported":["S256"],"token_endpoint_auth_methods_supported":["none"]}`)
		case "/oauth/register":
			_, _ = io.WriteString(w, `{"client_id":"dcr-client-1","token_endpoint_auth_method":"none"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	base = strings.TrimRight(srv.URL, "/")

	client := oauth.NewClient([]byte(testMasterKey),
		oauth.WithClientHTTPClient(srv.Client()),
		oauth.WithDiscovery(oauth.NewDiscovery(oauth.WithHTTPClient(srv.Client()))),
	)
	meta, err := client.Discovery().Discover(context.Background(), srv.URL+"/mcp")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	rc := resolve(t, client, meta, oauth.ClientIdentity{RedirectURI: testRedirectURI})
	if rc.Strategy != oauth.StrategyDCR || rc.ClientID != "dcr-client-1" || !rc.UsePKCE {
		t.Fatalf("expected DCR + PKCE after real discovery, got %+v", rc)
	}
}

func mustParseQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	return parsed.Query()
}
