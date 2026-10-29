package mcp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/client/transport"
)

// DefaultRedirectURI is the loopback callback registered at dynamic client
// registration (design D4): a fixed port keeps the registration stable, so a
// re-authorization never re-registers.
const DefaultRedirectURI = "http://127.0.0.1:29173/callback"

// AuthorizationTimeout bounds the callback wait: the browser round-trip is
// human-timescale and must never leak into a bounded path (design D2). On
// timeout the listener shuts down and the row returns to auth_required.
const AuthorizationTimeout = 5 * time.Minute

// authorizationTimeout is the flow's callback bound, indirect only so tests
// can shorten it.
var authorizationTimeout = AuthorizationTimeout

// AuthorizeRequest configures StartAuthorization.
type AuthorizeRequest struct {
	// OAuth carries the client settings: ClientID empty means dynamic
	// registration, RedirectURI empty means DefaultRedirectURI, and Tokens is
	// required — the exchanged token is saved into it.
	OAuth OAuth
	// Discovery is the advertisement a prior Discover validated;
	// AuthServerMetadataURL, Scopes, and RegistrationEndpoint are used.
	Discovery Discovery
	// ClientName is the DCR client_name.
	ClientName string
}

// Authorization is a started browser flow.
type Authorization struct {
	// URL is the authorization page the caller opens in the system browser.
	URL string
	// ClientID and ClientSecret are the registered client — possibly newly
	// registered — for the caller to persist (design D4).
	ClientID     string
	ClientSecret string
	// RedirectURI is the redirect actually registered and listening — the
	// ephemeral-port fallback rewrites it (design D4).
	RedirectURI string
	// Done receives exactly one value: nil once the tokens are saved into
	// OAuth.Tokens, else the failure (the timeout wraps
	// context.DeadlineExceeded); then it closes.
	Done <-chan error
	// Cancel stops the listener; Done then receives a cancellation error if
	// the flow had not completed.
	Cancel func()
}

// StartAuthorization performs the synchronous part of the OAuth browser flow
// bounded by ctx — dynamic client registration when needed, binding the
// loopback callback listener, building the PKCE authorization URL — and then
// serves the redirect callback once in the background, bounded by
// AuthorizationTimeout. It returns immediately (design D2: bounded paths
// never block on a human).
func StartAuthorization(ctx context.Context, req AuthorizeRequest) (*Authorization, error) {
	if req.OAuth.Tokens == nil {
		return nil, errNoTokenStore
	}

	redirect := req.OAuth.RedirectURI
	if redirect == "" {
		redirect = DefaultRedirectURI
	}
	clientID, clientSecret := req.OAuth.ClientID, req.OAuth.ClientSecret

	listener, bound, rebind, err := bindCallbackListener(redirect)
	if err != nil {
		return nil, err
	}
	if rebind && !req.OAuth.PreRegistered {
		// The configured callback port was taken: the stored registration's
		// redirect URI no longer matches what is listening, so force a fresh
		// registration against the rewritten redirect (design D4's one-time
		// fallback). A pre-registered confidential client is exempt (design
		// D3's oauth_confidential): its ClientID/Secret are fixed by the
		// provider's console and must not be discarded — the rewritten
		// redirect simply has to match what the console registered.
		clientID, clientSecret = "", ""
	}
	return startAuthorization(ctx, req, listener, bound, clientID, clientSecret)
}

// startAuthorization registers the client when needed, builds the PKCE
// authorization URL, and hands the listener to the background callback
// server. It closes listener itself on every path that does not start
// serving.
func startAuthorization(ctx context.Context, req AuthorizeRequest, listener net.Listener, redirect, clientID, clientSecret string) (*Authorization, error) {
	handler := transport.NewOAuthHandler(transport.OAuthConfig{
		ClientID:              clientID,
		ClientSecret:          clientSecret,
		RedirectURI:           redirect,
		Scopes:                req.Discovery.Scopes,
		TokenStore:            req.OAuth.Tokens,
		AuthServerMetadataURL: req.Discovery.AuthServerMetadataURL,
		PKCEEnabled:           true,
	})

	if clientID == "" {
		if req.Discovery.RegistrationEndpoint == "" {
			listener.Close()
			return nil, errors.New("mcp oauth: dynamic registration required but the server advertises no registration endpoint")
		}
		if err := handler.RegisterClient(ctx, req.ClientName); err != nil {
			listener.Close()
			return nil, fmt.Errorf("mcp oauth register client: %w", err)
		}
		// RegisterClient mutates the handler's config; read the registration
		// back for the caller to persist (design D4).
		clientID, clientSecret = handler.GetClientID(), handler.GetClientSecret()
	}

	state, err := transport.GenerateState()
	if err != nil {
		listener.Close()
		return nil, fmt.Errorf("mcp oauth state: %w", err)
	}
	verifier, err := transport.GenerateCodeVerifier()
	if err != nil {
		listener.Close()
		return nil, fmt.Errorf("mcp oauth code verifier: %w", err)
	}
	authorizationURL, err := handler.GetAuthorizationURL(ctx, state, transport.GenerateCodeChallenge(verifier))
	if err != nil {
		listener.Close()
		return nil, fmt.Errorf("mcp oauth authorization url: %w", err)
	}
	authorizationURL, err = withExtraAuthParams(authorizationURL, req.OAuth.ExtraAuthParams)
	if err != nil {
		listener.Close()
		return nil, fmt.Errorf("mcp oauth authorization url: %w", err)
	}

	done := make(chan error, 1)
	var once sync.Once
	send := func(err error) {
		once.Do(func() { done <- err; close(done) })
	}
	callbackPath := callbackPathOf(redirect)

	var srv *http.Server
	answer := callbackHandler(handler, callbackPath, verifier, func() {
		go func() { _ = srv.Shutdown(context.Background()) }()
	}, send)
	srv = &http.Server{Handler: answer}
	timer := time.AfterFunc(authorizationTimeout, func() {
		_ = srv.Shutdown(context.Background())
		send(fmt.Errorf("mcp oauth authorization timed out after %s: %w", authorizationTimeout, context.DeadlineExceeded))
	})
	go func() {
		defer listener.Close()
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			send(fmt.Errorf("mcp oauth callback listener: %w", err))
		}
	}()

	return &Authorization{
		URL:          authorizationURL,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURI:  redirect,
		Done:         done,
		Cancel: func() {
			timer.Stop()
			go func() { _ = srv.Shutdown(context.Background()) }()
			send(errors.New("mcp oauth authorization canceled"))
		},
	}, nil
}

// withExtraAuthParams appends the request's extra authorization parameters
// onto the authorization URL (design D3): Google needs access_type=offline and
// prompt=consent to issue a refresh token. A parameter the URL already carries
// wins — the extras only fill in what mcp-go did not set.
func withExtraAuthParams(rawURL string, extras map[string]string) (string, error) {
	if len(extras) == 0 {
		return rawURL, nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse %q: %w", rawURL, err)
	}
	query := parsed.Query()
	for name, value := range extras {
		if query.Get(name) == "" {
			query.Set(name, value)
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

// callbackHandler serves the redirect URI once: it validates the OAuth
// response, exchanges the code (mcp-go saves the token into the request's
// token store), answers the browser with a static page, reports the outcome
// exactly once, and shuts the callback server down. The exchange runs under
// its own bounded context — once the browser has come back, the flow
// completes even if the caller went away (design D2).
func callbackHandler(h *transport.OAuthHandler, path, verifier string, shutdown func(), send func(error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			http.NotFound(w, r)
			return
		}
		query := r.URL.Query()
		if oauthErr := query.Get("error"); oauthErr != "" {
			writeCallbackPage(w, false)
			shutdown()
			send(fmt.Errorf("mcp oauth authorization failed: %s", oauthErr))
			return
		}
		exCtx, cancel := context.WithTimeout(context.Background(), authorizationTimeout)
		defer cancel()
		if err := h.ProcessAuthorizationResponse(exCtx, query.Get("code"), query.Get("state"), verifier); err != nil {
			writeCallbackPage(w, false)
			shutdown()
			send(fmt.Errorf("mcp oauth token exchange: %w", err))
			return
		}
		writeCallbackPage(w, true)
		shutdown()
		send(nil)
	})
}

// writeCallbackPage answers the browser with a static page only — the page
// never echoes request data back.
func writeCallbackPage(w http.ResponseWriter, ok bool) {
	message := "Authorization failed — return to buddies for details."
	if ok {
		message = "You can close this tab and return to buddies."
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<html><body><p>%s</p></body></html>", message)
}

// bindCallbackListener binds the redirect URI's host:port for the callback
// server, reporting the redirect that actually listens. When the configured
// port is taken it falls back to an ephemeral one and reports rebind — the
// caller must then register the client against the rewritten redirect
// (design D4).
func bindCallbackListener(redirect string) (listener net.Listener, bound string, rebind bool, err error) {
	parsed, err := url.Parse(redirect)
	if err != nil {
		return nil, "", false, fmt.Errorf("mcp oauth redirect uri %q: %w", redirect, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, "", false, fmt.Errorf("mcp oauth redirect uri %q: missing scheme or host", redirect)
	}
	host := parsed.Host
	if parsed.Port() == "" {
		host = net.JoinHostPort(parsed.Hostname(), "0")
	}
	listener, err = net.Listen("tcp", host)
	if err == nil {
		return listener, redirect, false, nil
	}
	listener, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", false, fmt.Errorf("mcp oauth callback listener: %w", err)
	}
	return listener, parsed.Scheme + "://" + listener.Addr().String() + parsed.Path, true, nil
}

// callbackPathOf extracts the redirect URI's path — the only route the
// callback server answers.
func callbackPathOf(redirect string) string {
	if parsed, err := url.Parse(redirect); err == nil && parsed.Path != "" {
		return parsed.Path
	}
	return "/"
}
