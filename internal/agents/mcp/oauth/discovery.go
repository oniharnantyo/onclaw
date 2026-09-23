// Package oauth implements the MCP OAuth client (openspec/changes/
// add-mcp-oauth-client). The files here own the discovery chain (tasks 3.1–3.3):
// WWW-Authenticate challenge parsing (RFC 9728 §5), protected-resource metadata
// resolution with path-inserted and root well-knowns (RFC 9728 §3.1), RFC 8414
// authorization-server metadata with §3.1 path insertion, and the in-memory TTL
// cache keyed by server URL (design.md D2). Client strategies (BYO, client-id
// metadata document, DCR), the authorization grants, and the token lifecycle
// build on the Metadata result resolved here. Discovery only ever reads public
// metadata documents — it never requests, holds, or logs token material, and it
// never contacts a token endpoint.
package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// DefaultCacheTTL bounds a cached discovery result (design.md D2):
	// metadata is stable, and a full chain adds RTTs to every cold dial.
	DefaultCacheTTL = time.Hour
	// DefaultHTTPTimeout bounds each discovery HTTP round trip; the caller's
	// context bounds the whole chain.
	DefaultHTTPTimeout = 10 * time.Second

	// maxMetadataBytes bounds a metadata document read (PRM and AS documents
	// are a few KiB; the cap keeps a hostile endpoint from streaming forever).
	maxMetadataBytes = 1 << 20
	// drainLimit is how much of an unused response body is read before close
	// (an MCP endpoint may answer the probe with a stream; never drain it all).
	drainLimit = 4 << 10
)

// probeUserAgent identifies OnClaw on discovery requests.
const probeUserAgent = "onclaw/1.0.0 (mcp-oauth-discovery)"

// ErrDiscoveryFailed is the parent sentinel every discovery failure chains
// to: errors.Is(err, ErrDiscoveryFailed) is the family test; errors.As to
// *DiscoveryError for the kind, detail, and Guidance.
var ErrDiscoveryFailed = errors.New("mcp oauth discovery failed")

// Failure classifies discovery failures into the three contracted shapes:
//
//   - FailureNoResourceMetadata: no usable challenge and no metadata anywhere
//     — the undiscoverable-server case; also covers unreachable hosts and
//     malformed server URLs, where the chain has nothing to work with;
//   - FailureNoAuthorizationServer: protected-resource metadata resolved but
//     it advertises no authorization server this client can use;
//   - FailureInvalidAuthorizationServerMetadata: an authorization-server
//     document was fetched but is invalid — unparseable, missing required
//     endpoints, or contradicting its issuer.
type Failure int

const (
	FailureNoResourceMetadata Failure = iota + 1
	FailureNoAuthorizationServer
	FailureInvalidAuthorizationServerMetadata
)

func (f Failure) String() string {
	switch f {
	case FailureNoResourceMetadata:
		return "no protected-resource metadata"
	case FailureNoAuthorizationServer:
		return "no usable authorization server"
	case FailureInvalidAuthorizationServerMetadata:
		return "invalid authorization-server metadata"
	default:
		return fmt.Sprintf("unknown failure %d", int(f))
	}
}

// DiscoveryError is a typed discovery failure: Kind for programmatic
// matching, Detail with what the chain observed, and Guidance pointing at the
// bring-your-own / static-header fallbacks (design.md: "discovery failure is
// a clean error with guidance to use BYO/static modes").
type DiscoveryError struct {
	Kind      Failure
	ServerURL string
	Detail    string
	// Cause is the underlying transport or parse error, when one exists.
	Cause error
}

func (e *DiscoveryError) Error() string {
	return fmt.Sprintf("%s for %s (%s): %s", ErrDiscoveryFailed.Error(), e.ServerURL, e.Kind, e.Detail)
}

// Unwrap chains both the ErrDiscoveryFailed sentinel and the underlying cause.
func (e *DiscoveryError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrDiscoveryFailed}
	}
	return []error{ErrDiscoveryFailed, e.Cause}
}

// Guidance returns the operator-facing remediation for the failure kind. It
// never contains credentials — the chain that produced the error holds none.
func (e *DiscoveryError) Guidance() string {
	const fallback = "configure a pre-registered OAuth app on the server (bring-your-own client id / secret), or fall back to auth mode none with static headers"
	switch e.Kind {
	case FailureNoResourceMetadata:
		return "this server publishes no OAuth discovery metadata (no WWW-Authenticate resource_metadata challenge and no RFC 9728 well-known protected-resource metadata was found); check the server URL, or " + fallback
	case FailureNoAuthorizationServer:
		return "the server publishes protected-resource metadata but advertises no authorization server this client can use; " + fallback
	case FailureInvalidAuthorizationServerMetadata:
		return "the authorization server's OAuth metadata is missing required endpoints, unparseable, or contradicts its issuer; verify the provider's OAuth deployment, or " + fallback
	default:
		return fallback
	}
}

// Discovery resolves the MCP OAuth discovery chain for a server URL and
// caches the result per server URL (design.md D2).
//
// Cache behavior: a successful Discover stores the resolved *Metadata under
// the normalized server URL and serves it until the TTL (DefaultCacheTTL,
// tunable with WithCacheTTL) elapses. A cache hit performs no HTTP requests
// and returns the same shared pointer — callers must treat Metadata as
// read-only. Failed discoveries are not cached: the next Discover re-runs the
// chain, so a provider fixing its metadata is picked up immediately. The
// 401-challenge dial path forces a fresh chain with ForceRefresh, optionally
// seeding it with the observed challenge via WithChallenge, or drops the
// entry first with Invalidate.
type Discovery struct {
	httpClient *http.Client
	cache      *discoveryCache
}

// Option configures a Discovery at construction.
type Option func(*Discovery)

// WithHTTPClient replaces the transport used for every discovery request. The
// client must be non-nil; use it to pin proxies or test transports.
func WithHTTPClient(hc *http.Client) Option {
	return func(d *Discovery) { d.httpClient = hc }
}

// WithCacheTTL replaces the discovery cache TTL.
func WithCacheTTL(ttl time.Duration) Option {
	return func(d *Discovery) { d.cache.ttl = ttl }
}

// NewDiscovery builds a discovery client with the default HTTP client
// (DefaultHTTPTimeout per round trip) and cache (DefaultCacheTTL).
func NewDiscovery(opts ...Option) *Discovery {
	d := &Discovery{
		httpClient: &http.Client{Timeout: DefaultHTTPTimeout},
		cache:      newDiscoveryCache(DefaultCacheTTL),
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// DiscoverOption tunes a single Discover call.
type DiscoverOption func(*discoverOptions)

type discoverOptions struct {
	forceRefresh bool
	challenge    []string
}

// ForceRefresh bypasses the cache: the full chain runs and the fresh result
// replaces the cached entry. The 401-challenge dial path uses it to re-run
// discovery when a cached entry no longer satisfies the server.
func ForceRefresh() DiscoverOption {
	return func(o *discoverOptions) { o.forceRefresh = true }
}

// WithChallenge seeds discovery with the WWW-Authenticate header value(s) of
// a live 401 response, skipping this Discover's own unauthenticated probe of
// the server URL. Values parse exactly like probe responses; when none of
// them carries a resource_metadata URI the chain still falls back to the
// RFC 9728 well-known locations.
func WithChallenge(values ...string) DiscoverOption {
	return func(o *discoverOptions) { o.challenge = append(o.challenge, values...) }
}

// Discover resolves the authorization metadata for serverURL: the challenge
// probe, RFC 9728 protected-resource metadata (challenge URL first, then the
// path-inserted and root well-knowns), and RFC 8414 metadata for the first
// usable advertised authorization server. Every failure is a *DiscoveryError
// chained to ErrDiscoveryFailed with operator Guidance; no token endpoint is
// ever contacted. See the Discovery type for cache behavior.
func (d *Discovery) Discover(ctx context.Context, serverURL string, opts ...DiscoverOption) (*Metadata, error) {
	options := discoverOptions{}
	for _, opt := range opts {
		opt(&options)
	}
	key := normalizeServerURL(serverURL)
	if !options.forceRefresh {
		if meta := d.cache.get(key); meta != nil {
			return meta, nil
		}
	}
	meta, err := d.resolve(ctx, serverURL, options.challenge)
	if err != nil {
		return nil, err
	}
	d.cache.put(key, meta)
	return meta, nil
}

// Invalidate drops the cached entry for serverURL (any spelling that
// normalizes to the same key). Absent entries are a no-op.
func (d *Discovery) Invalidate(serverURL string) {
	d.cache.delete(normalizeServerURL(serverURL))
}

// resolve runs the uncached chain: challenge probe → RFC 9728 protected-
// resource metadata → RFC 8414 authorization-server metadata.
func (d *Discovery) resolve(ctx context.Context, serverURL string, challenge []string) (*Metadata, error) {
	fail := func(kind Failure, detail string, cause error) (*Metadata, error) {
		return nil, &DiscoveryError{Kind: kind, ServerURL: serverURL, Detail: detail, Cause: cause}
	}
	if err := validateAbsoluteHTTPURL(serverURL); err != nil {
		return fail(FailureNoResourceMetadata, "server url is not an absolute http(s) url", nil)
	}

	challengeURI := ""
	if len(challenge) > 0 {
		challengeURI = ResourceMetadataURI(ParseWWWAuthenticate(challenge))
	} else {
		var err error
		challengeURI, err = d.probeChallenge(ctx, serverURL)
		if err != nil {
			return fail(FailureNoResourceMetadata, "server unreachable during the challenge probe", err)
		}
	}

	prmWellKnowns, err := wellKnownURLs(serverURL, protectedResourceWellKnown)
	if err != nil {
		return fail(FailureNoResourceMetadata, "server url is not an absolute http(s) url", err)
	}
	// Candidate order is the preference order: the challenge's
	// resource_metadata URL, then the path-inserted well-known, then root.
	var prm *ProtectedResourceMetadata
	prmURL := ""
	for _, candidate := range dedupeNonEmpty(append([]string{challengeURI}, prmWellKnowns...)) {
		doc := &ProtectedResourceMetadata{}
		result := d.fetch(ctx, candidate, doc)
		if !result.fetched {
			continue // 404 / transport miss — try the next well-known shape
		}
		if result.invalid {
			continue // a fetched-but-unparseable body is not our metadata; keep probing
		}
		if !doc.matchesResource(serverURL) {
			continue // RFC 9728 §3.3: the document describes a different resource
		}
		prm, prmURL = doc, candidate
		break
	}
	if prm == nil {
		return fail(FailureNoResourceMetadata, "no usable protected-resource metadata via the challenge or the RFC 9728 well-known locations", nil)
	}

	if len(prm.AuthorizationServers) == 0 {
		return fail(FailureNoAuthorizationServer, "protected-resource metadata at "+prmURL+" advertises no authorization server", nil)
	}
	// Authorization_servers is preference-ordered: the first entry whose
	// RFC 8414 metadata resolves wins. A fetched-but-invalid document fails
	// the chain — the resource advertised a broken authorization server, and
	// reporting that beats silently falling through to the next entry.
	unusable := make([]string, 0, len(prm.AuthorizationServers))
	for _, raw := range prm.AuthorizationServers {
		asURL := strings.TrimSpace(raw)
		asWellKnowns, err := wellKnownURLs(asURL, authorizationServerWellKnown)
		if err != nil {
			unusable = append(unusable, asURL)
			continue
		}
		fetched := false
		for _, candidate := range asWellKnowns {
			doc := &AuthorizationServerMetadata{}
			result := d.fetch(ctx, candidate, doc)
			if !result.fetched {
				continue
			}
			fetched = true
			if result.invalid {
				return fail(FailureInvalidAuthorizationServerMetadata, "authorization-server metadata at "+candidate+" is not a valid JSON document", result.err)
			}
			if err := doc.validate(asURL); err != nil {
				return fail(FailureInvalidAuthorizationServerMetadata, "authorization-server metadata at "+candidate+": "+err.Error(), nil)
			}
			return &Metadata{
				ServerURL:              serverURL,
				Resource:               prm,
				ResourceMetadataURL:    prmURL,
				AuthorizationServerURL: asURL,
				AuthorizationServer:    doc,
			}, nil
		}
		if !fetched {
			unusable = append(unusable, asURL)
		}
	}
	detail := "no usable authorization server among " + strings.Join(prm.AuthorizationServers, ", ")
	if len(unusable) > 0 {
		detail += " (unreachable or malformed: " + strings.Join(unusable, ", ") + ")"
	}
	return fail(FailureNoAuthorizationServer, detail, nil)
}

// probeChallenge requests the server URL without credentials and extracts the
// RFC 9728 §5 resource_metadata URI from the WWW-Authenticate challenge, if
// the response carries one. Only transport errors fail: any HTTP status is a
// valid probe outcome (servers that publish well-knowns without challenging
// are the normal fallback shape). The response body is discarded — discovery
// never runs the MCP handshake and never reads a token payload.
func (d *Discovery) probeChallenge(ctx context.Context, serverURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, serverURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", probeUserAgent)
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, drainLimit))
	return ResourceMetadataURI(ParseWWWAuthenticate(resp.Header.Values("WWW-Authenticate"))), nil
}

// fetchResult reports one metadata GET: fetched marks an HTTP 200 (any other
// status is an ordinary miss, not an error); invalid marks a fetched body
// that could not be read or parsed as JSON (err carries why).
type fetchResult struct {
	fetched bool
	invalid bool
	err     error
}

// fetch GETs one metadata URL into out. A broken or hostile endpoint is data,
// not a crash: non-200 and unparseable bodies surface through fetchResult and
// the caller decides whether to try the next candidate or fail the chain.
func (d *Discovery) fetch(ctx context.Context, rawURL string, out any) fetchResult {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fetchResult{err: err}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", probeUserAgent)
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return fetchResult{err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, drainLimit))
		return fetchResult{}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxMetadataBytes))
	if err != nil {
		return fetchResult{fetched: true, invalid: true, err: err}
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fetchResult{fetched: true, invalid: true, err: err}
	}
	return fetchResult{fetched: true}
}

// normalizeServerURL canonicalizes the cache key: trimmed, fragment-free,
// trailing path slash dropped. Two spellings of one server URL share an
// entry; a differing query string names a different endpoint and keeps its
// own entry.
func normalizeServerURL(raw string) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.Fragment = ""
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String()
}

// dedupeNonEmpty trims, drops empties, and keeps first occurrences in order —
// the candidate order (challenge URL, then path-inserted, then root) is the
// preference order the chain must try in.
func dedupeNonEmpty(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
