package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// NameBrowser is the dotted capability name registered in the tool catalog.
const NameBrowser = "browser"

// EnvBrowserAllowPrivate opts the instance out of the navigate private-network
// guard (loopback, private, link-local, and unique-local targets become
// navigable). It exists for local development and testing.
const EnvBrowserAllowPrivate = "BUDDIES_BROWSER_ALLOW_PRIVATE"

// DefaultBrowserConfig returns the tool defaults: profile "default", headless
// on, 120 s idle timeout, 30 s action timeout, private networks blocked.
func DefaultBrowserConfig() BrowserConfig {
	return BrowserConfig{
		DefaultProfile:       DefaultBrowserProfile,
		Headless:             true,
		IdleTimeoutSeconds:   browserManagerIdleSeconds,
		ActionTimeoutSeconds: defaultBrowserActionSeconds,
	}
}

// defaultBrowserActionSeconds bounds every action.
const defaultBrowserActionSeconds = 30

// BrowserConfig is the per-tool configuration (stored by the caller; zero
// fields mean "use default", except Headless and AllowPrivate which are plain
// booleans honored as stored — the config source applies defaults for missing
// JSON fields).
type BrowserConfig struct {
	DefaultProfile       string // "" => DefaultBrowserProfile
	Headless             bool   // DefaultBrowserConfig() => true
	IdleTimeoutSeconds   int    // 0 => 120
	ActionTimeoutSeconds int    // 0 => 30
	AllowPrivate         bool   // false
}

// BrowserConfigSource resolves the configuration lazily on every call so
// settings changes apply without rebuilding the tool.
type BrowserConfigSource interface {
	BrowserConfig(ctx context.Context) (BrowserConfig, error)
}

// BrowserOption configures the browser tool.
type BrowserOption func(*browserTool)

// WithBrowserConfigSource sets the lazy configuration source (production: the
// persisted tool_settings under the browser key).
func WithBrowserConfigSource(src BrowserConfigSource) BrowserOption {
	return func(t *browserTool) { t.source = src }
}

// WithBrowserDir sets the browser root directory: profiles/ and screenshots/
// live under it.
func WithBrowserDir(dir string) BrowserOption {
	return func(t *browserTool) { t.dir = dir }
}

type browserTool struct {
	source BrowserConfigSource
	dir    string
}

// NewBrowser constructs the browser built-in: a CDP-driven Chromium with
// named persistent profiles (design D1–D8). The directory and the config
// source are both required — there is no usable implicit default for either.
func NewBrowser(opts ...BrowserOption) (tool.BaseTool, error) {
	t := &browserTool{}
	for _, opt := range opts {
		opt(t)
	}
	if t.dir == "" {
		return nil, errors.New("browser: no browser directory configured")
	}
	if t.source == nil {
		return nil, errors.New("browser: no config source configured")
	}
	return t, nil
}

// Info returns the tool schema surfaced to agentic models.
func (t *browserTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: NameBrowser,
		Desc: "Drive a real Chromium browser with named persistent profiles: " +
			"cookies, logins, and local storage survive across calls, sessions, and restarts. " +
			"Each profile is the agent's own cookie jar. Start with action navigate to open a URL, " +
			"then take a snapshot to list the page's interactive elements; click and type address " +
			"elements by the ref from the latest snapshot, and refs reset after every navigate — " +
			"take a new snapshot after navigating. Interactive actions return a fresh snapshot. " +
			"Private and local network addresses are blocked unless allow-private is enabled.",
		ParamsOneOf: schema.NewParamsOneOfByParams(browserParams()),
	}, nil
}

// browserParams builds the tool's parameter schema: only action is required;
// the rest are per-action arguments.
func browserParams() map[string]*schema.ParameterInfo {
	return map[string]*schema.ParameterInfo{
		"action": {
			Type:     schema.String,
			Desc:     "The operation to perform.",
			Enum:     []string{"navigate", "snapshot", "click", "type", "press_key", "scroll", "back", "screenshot", "cookies"},
			Required: true,
		},
		"profile": {
			Type: schema.String,
			Desc: "Named browser profile to use (its own persistent cookie jar). Defaults to the configured default profile.",
		},
		"url": {
			Type: schema.String,
			Desc: "The http(s) URL to open when action is navigate.",
		},
		"ref": {
			Type: schema.String,
			Desc: "Element ref from the latest snapshot when action is click or type.",
		},
		"text": {
			Type: schema.String,
			Desc: "The text to enter (replacing existing content) when action is type.",
		},
		"key": {
			Type: schema.String,
			Desc: "Key to press when action is press_key, e.g. Enter, Tab, Escape, ArrowDown.",
		},
		"direction": {
			Type: schema.String,
			Desc: "Scroll direction when action is scroll.",
			Enum: []string{"up", "down"},
		},
		"cookie_op": {
			Type: schema.String,
			Desc: "Cookie operation when action is cookies. Cookies set are persistent for 1 year.",
			Enum: []string{"list", "set", "delete", "clear"},
		},
		"cookie_name": {
			Type: schema.String,
			Desc: "Cookie name for cookies set/delete.",
		},
		"cookie_value": {
			Type: schema.String,
			Desc: "Cookie value for cookies set.",
		},
		"cookie_domain": {
			Type: schema.String,
			Desc: "Cookie domain for cookies set/delete; defaults to the current page.",
		},
		"cookie_path": {
			Type: schema.String,
			Desc: "Cookie path for cookies set/delete; defaults to /.",
		},
	}
}

// browserActions is the action enum, for error messages.
var browserActions = []string{"navigate", "snapshot", "click", "type", "press_key", "scroll", "back", "screenshot", "cookies"}

// InvokableRun satisfies tool.InvokableTool. Errors — refusals, missing
// arguments, stale refs, timeouts — return as Go errors so the runtime turns
// them into readable tool-error results and the turn continues.
func (t *browserTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args browserArgs
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("browser: %w", err)
	}
	cfg, err := t.source.BrowserConfig(ctx)
	if err != nil {
		return "", fmt.Errorf("browser: load config: %w", err)
	}
	profile, err := resolveBrowserProfile(args.Profile, cfg.DefaultProfile)
	if err != nil {
		return "", err
	}
	allowPrivate := cfg.AllowPrivate || browserEnvAllowPrivate()

	switch args.Action {
	case "navigate":
		return t.runNavigate(ctx, args, cfg, profile, allowPrivate)
	case "snapshot":
		return t.withSession(ctx, cfg, profile, func(actx context.Context, s *profileSession) (string, error) {
			return t.runSnapshot(actx, s)
		})
	case "click":
		return t.withRefAction(ctx, args, cfg, profile, func(actx context.Context, s *profileSession) ([]browserSnapshotElement, error) {
			return s.clickRef(actx, args.Ref)
		})
	case "type":
		if args.Text == "" {
			return "", errors.New("browser: text is required for type")
		}
		return t.withRefAction(ctx, args, cfg, profile, func(actx context.Context, s *profileSession) ([]browserSnapshotElement, error) {
			return s.typeRef(actx, args.Ref, args.Text)
		})
	case "press_key":
		return t.runPressKey(ctx, args, cfg, profile)
	case "scroll":
		return t.runScroll(ctx, args, cfg, profile)
	case "back":
		return t.withSession(ctx, cfg, profile, func(actx context.Context, s *profileSession) (string, error) {
			return t.runBack(actx, s)
		})
	case "screenshot":
		return t.withSession(ctx, cfg, profile, func(actx context.Context, s *profileSession) (string, error) {
			return t.runScreenshot(actx, s)
		})
	case "cookies":
		return t.runCookies(ctx, args, cfg, profile)
	default:
		return "", fmt.Errorf("browser: unknown action %q; must be one of %s", args.Action, strings.Join(browserActions, ", "))
	}
}

// resolveBrowserProfile applies the profile precedence: explicit argument,
// configured default, then DefaultBrowserProfile — validating before any
// filesystem access.
func resolveBrowserProfile(arg, configured string) (string, error) {
	name := arg
	if name == "" {
		name = configured
	}
	if name == "" {
		name = DefaultBrowserProfile
	}
	if err := ValidateBrowserProfile(name); err != nil {
		return "", err
	}
	return name, nil
}

// browserEnvAllowPrivate reports whether the environment opts out of the
// private-network guard.
func browserEnvAllowPrivate() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvBrowserAllowPrivate))) {
	case "1", "true":
		return true
	default:
		return false
	}
}

// withSession acquires the profile's session, bounds the action by the
// configured timeout, and runs fn under the session lock.
func (t *browserTool) withSession(ctx context.Context, cfg BrowserConfig, profile string, fn func(actx context.Context, s *profileSession) (string, error)) (string, error) {
	s, release, err := browserManagerInstance().acquire(ctx, t.dir, profile, cfg)
	if err != nil {
		return "", err
	}
	defer release()

	actx, cancel := context.WithTimeout(ctx, browserActionTimeout(cfg))
	defer cancel()
	return fn(actx, s)
}

// withRefAction runs a ref-targeted interaction and wraps its fresh element
// list in the standard action result.
func (t *browserTool) withRefAction(ctx context.Context, args browserArgs, cfg BrowserConfig, profile string, fn func(actx context.Context, s *profileSession) ([]browserSnapshotElement, error)) (string, error) {
	if args.Ref == "" {
		return "", fmt.Errorf("browser: ref is required for %s; take a snapshot first", args.Action)
	}
	return t.withSession(ctx, cfg, profile, func(actx context.Context, s *profileSession) (string, error) {
		elements, err := fn(actx, s)
		if err != nil {
			return "", err
		}
		return s.actionResult(elements)
	})
}
