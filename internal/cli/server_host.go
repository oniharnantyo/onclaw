//go:build server

package cli

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/oniharnantyo/buddies/internal/services"
)

// ServerHostOptions configures the public front of server mode.
type ServerHostOptions struct {
	// Addr is the public listen address. Empty means 127.0.0.1:8080 (the
	// spec's loopback default; remote access must be explicitly configured).
	Addr string
	// Token is the optional bearer token. Empty disables the gate.
	Token string
}

// wailsEmitter adapts services.Emitter onto the Wails event bus in server
// mode (mirror of the desktop main.go adapter; the app handle is installed
// by RunServerHost right after application.New).
type wailsEmitter struct {
	app atomic.Pointer[application.App]
}

func (e *wailsEmitter) Emit(name string, data ...any) {
	if a := e.app.Load(); a != nil {
		a.Event.Emit(name, data...)
	}
}

// NewWailsEmitter returns the server-mode services.Emitter backed by the
// Wails event bus (events reach browsers over /wails/events).
func NewWailsEmitter() services.Emitter {
	return &wailsEmitter{}
}

// ParseServerOptions resolves the public address and bearer token:
//
//   - env BUDDIES_ADDR / BUDDIES_TOKEN;
//   - then --addr/--token flags. urfave/cli owns flag parsing and the serve
//     subcommand defines no flags, so the entrypoint accepts them after the
//     standard "--" separator: buddies serve -- --addr 127.0.0.1:9000
//     --token secret (both "--k v" and "--k=v" forms).
func ParseServerOptions() ServerHostOptions {
	opts := ServerHostOptions{
		Addr:  os.Getenv("BUDDIES_ADDR"),
		Token: os.Getenv("BUDDIES_TOKEN"),
	}
	rest := os.Args
	for i, a := range rest {
		if a != "--" {
			continue
		}
		flags := rest[i+1:]
		for j := 0; j < len(flags); j++ {
			name, val, hasVal := flags[j], "", false
			if idx := strings.Index(name, "="); idx >= 0 {
				name, val, hasVal = name[:idx], name[idx+1:], true
			} else if j+1 < len(flags) && (name == "--addr" || name == "--token") {
				j++
				val, hasVal = flags[j], true
			}
			switch name {
			case "--addr":
				if hasVal {
					opts.Addr = val
				}
			case "--token":
				if hasVal {
					opts.Token = val
				}
			}
		}
		break
	}
	return opts
}

// RunServerHost is the server-mode host (design D1/D4): it starts the Wails
// application bound to a loopback-internal port and fronts it with our own
// authenticating HTTP server on the public address. The front server gates
// every request — plain HTTP and both WebSocket endpoints
// (/wails/events, /wails/stream/ws) — through the bearer-token gate and
// reverse-proxies to the Wails handler, closing the auth gap noted in
// spike-server-mode.md (AssetOptions.Middleware does not cover the WS
// endpoints). It returns when the public server shuts down (signal or
// error); the caller's defer closes the assembled app.
func RunServerHost(ctx context.Context, host *App, emitter services.Emitter, assets http.Handler, opts ServerHostOptions) error {
	if opts.Addr == "" {
		opts.Addr = "127.0.0.1:8080"
	}

	// Grab an ephemeral loopback port for the internal Wails server. Wails
	// reads WAILS_SERVER_HOST/PORT with priority over Options.Server, so pin
	// them: the Wails listener must never be the public surface.
	internal, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("serve: pick internal port: %w", err)
	}
	internalPort := internal.Addr().(*net.TCPAddr).Port
	internal.Close()
	os.Setenv("WAILS_SERVER_HOST", "127.0.0.1")
	os.Setenv("WAILS_SERVER_PORT", strconv.Itoa(internalPort))

	wailsApp := application.New(application.Options{
		Name:        services.AppName,
		Description: "A local agent gateway that keeps running in the background",
		Assets: application.AssetOptions{
			Handler: assets,
		},
		Services: []application.Service{
			application.NewService(host.Services.AppInfo),
			application.NewService(host.Services.Agents),
			application.NewService(host.Services.Chat),
			application.NewService(host.Services.Settings),
			application.NewService(host.Services.Gateway),
			application.NewService(host.Services.Schedule),
			application.NewService(host.Services.Knowledge),
			application.NewService(host.Services.Channels),
			application.NewService(services.NewClipboardService()),
		},
		Server: application.ServerOptions{
			Host: "127.0.0.1",
			Port: internalPort,
		},
	})
	if we, ok := emitter.(*wailsEmitter); ok {
		we.app.Store(wailsApp)
	}

	// The gateway starts with the app and serves over both hosts (spec:
	// service parity across hosts).
	if err := host.Services.Gateway.Start(ctx); err != nil {
		log.Printf("serve: start gateway: %v", err)
	}

	target := &url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(internalPort))}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("serve: proxy %s %s: %v", r.Method, r.URL.Path, err)
		http.Error(w, "backend unavailable", http.StatusBadGateway)
	}
	public := &http.Server{
		Addr:              opts.Addr,
		Handler:           BearerGate(opts.Token)(proxy),
		ReadHeaderTimeout: 10 * time.Second,
	}

	listener, err := net.Listen("tcp", opts.Addr)
	if err != nil {
		return fmt.Errorf("serve: listen on %s: %w", opts.Addr, err)
	}
	if opts.Token != "" {
		log.Printf("buddies serving %s (bearer token required)", opts.Addr)
	} else {
		log.Printf("buddies serving %s (no token: unauthenticated local access)", opts.Addr)
	}

	// The Wails server mode blocks in Run until shutdown; run it alongside
	// the public server. It handles SIGINT/SIGTERM itself; on a signal we
	// drain the public server too.
	go func() {
		if err := wailsApp.Run(); err != nil {
			log.Printf("serve: wails host: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = public.Shutdown(shutdownCtx)
		wailsApp.Quit()
	}()

	if err := public.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}
