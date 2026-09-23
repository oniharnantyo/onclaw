package oauth

import (
	"net/http"
	"time"

	"github.com/oniharnantyo/onclaw/internal/auth/oauthstate"
)

// Client is the MCP OAuth client: the client-identification strategies
// (BYO → client-id metadata document → DCR, design.md D3), the two grants
// (authorization-code + PKCE S256, RFC 8628 device flow), and the on-dial
// token refresh lifecycle (design.md D6/D7). It is HTTP-client-injectable and
// store-agnostic: the token rows and the expired-status persistence ride the
// CredentialStore and StatusSink seams the services layer implements
// (lifecycle.go), and the signed state / verifier-session machinery is the
// shared oauthstate package (design.md D5).
//
// Secrets discipline: a client secret exists in memory only for the duration
// of a token-endpoint (or device-endpoint) request — it is never placed in a
// URL, never logged, and never embedded in an error message.
type Client struct {
	discovery  *Discovery
	httpClient *http.Client
	state      *oauthstate.Sealer
	sessions   *oauthstate.Sealer
	margin     time.Duration
	// deviceIntervalFloor is an ops/test lower bound on the server-advised
	// poll interval (0 honors the server exactly).
	deviceIntervalFloor time.Duration
	// deviceSlowDownStep is the slow_down penalty; the default is the RFC
	// 8628 §3.5 value.
	deviceSlowDownStep time.Duration
}

// ClientOption configures a Client at construction. Zero/nil values are
// ignored, keeping the default behavior.
type ClientOption func(*Client)

// WithClientHTTPClient replaces the transport used for every token-endpoint,
// registration, client-metadata-document, and device request. The client must
// be non-nil; nil is ignored.
func WithClientHTTPClient(hc *http.Client) ClientOption {
	return func(c *Client) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// WithDiscovery shares an existing Discovery (and its cache) between this
// client and other consumers of the same chain (the dial path's 401-challenge
// re-discovery). Nil is ignored.
func WithDiscovery(d *Discovery) ClientOption {
	return func(c *Client) {
		if d != nil {
			c.discovery = d
		}
	}
}

// WithRefreshMargin overrides the within-margin refresh window. Zero or
// negative selects DefaultTokenRefreshMargin.
func WithRefreshMargin(d time.Duration) ClientOption {
	return func(c *Client) {
		if d > 0 {
			c.margin = d
		}
	}
}

// WithDeviceIntervalFloor sets a lower bound on the device-flow poll interval
// (a guard against providers advising zero). Zero disables the floor.
func WithDeviceIntervalFloor(d time.Duration) ClientOption {
	return func(c *Client) {
		if d > 0 {
			c.deviceIntervalFloor = d
		}
	}
}

// WithDeviceSlowDownStep overrides the RFC 8628 §3.5 slow_down penalty (the
// default, DefaultDeviceSlowDownStep, pins the RFC's five seconds). Zero or
// negative keeps the default.
func WithDeviceSlowDownStep(d time.Duration) ClientOption {
	return func(c *Client) {
		if d > 0 {
			c.deviceSlowDownStep = d
		}
	}
}

// NewClient builds the MCP OAuth client. masterKey is the instance master
// key, the same key the connections OAuth flow HMACs its states with — it
// seals the authorization states and the PKCE verifier sessions this client
// mints (the composition root resolves it; it is never nil on a real
// instance, mirroring NewConnectionsService's encKey).
func NewClient(masterKey []byte, opts ...ClientOption) *Client {
	c := &Client{
		discovery:          NewDiscovery(),
		httpClient:         &http.Client{Timeout: DefaultTokenHTTPTimeout},
		margin:             DefaultTokenRefreshMargin,
		deviceSlowDownStep: DefaultDeviceSlowDownStep,
	}
	c.state = oauthstate.NewSealer(masterKey, StateContext, DefaultStateTTL)
	c.sessions = oauthstate.NewSealer(masterKey, SessionContext, DefaultStateTTL)
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Discovery returns the client's discovery chain so callers can share one
// cache (and its ForceRefresh/Invalidate controls) across the authorize and
// dial paths.
func (c *Client) Discovery() *Discovery {
	return c.discovery
}
