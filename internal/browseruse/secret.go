package browseruse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	neturl "net/url"
	"strings"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
)

// This file is the secure-submission request surface (design D2): the public
// submit_secret tool (a guarded refusal — the value never travels through it),
// the internal __request_secret channel tool the parent host invokes, the
// argument validation, and prepareSecret's pre-consent validations
// (HTTPS-only origin, no framed targets, one pending request per profile,
// same-site isolation) plus the form fingerprint snapshot.

// valueForbiddenKeys are the argument keys the submit_secret schema rejects
// outright: the model cannot express a secret through this surface, no matter
// how lenient the argument binding is (defense in depth; design D1).
var valueForbiddenKeys = []string{"value", "password", "secret", "code", "otp"}

// secretArgs is the shared argument shape of submit_secret and
// __request_secret: field targets and submission controls only — there is no
// value field anywhere in the contract.
type secretArgs struct {
	Targets          []string `json:"targets"`
	Method           string   `json:"method"`
	Digits           *float64 `json:"digits"`
	Submission       string   `json:"submission"`
	SubmitTarget     string   `json:"submitTarget"`
	IdentifierTarget string   `json:"identifierTarget"`
	LoginID          string   `json:"loginId"`
	Username         string   `json:"username"`
	Profile          string   `json:"profile"`
}

// secretReadyResult is the internal channel's ready receipt.
type secretReadyResult struct {
	Status    string `json:"status"` // "ready"
	RequestID string `json:"requestID"`
	Origin    string `json:"origin"`
	Method    string `json:"method"`
	Digits    int    `json:"digits"`
}

// unavailableResult answers a prepare failure: fixed reason code only,
// never page exceptions (design D1).
type unavailableResult struct {
	Status     string `json:"status"` // "unavailable"
	ReasonCode string `json:"reasonCode"`
}

// deliverCompletedResult is the __deliver_secret success receipt.
type deliverCompletedResult struct {
	Status string `json:"status"` // "completed"
}

// channelFailedResult answers a delivery failure: fixed reason code, and the
// request is over — secrets are not retried (design D4).
type channelFailedResult struct {
	Status     string `json:"status"` // "failed"
	ReasonCode string `json:"reasonCode"`
}

// submitSecretTool builds the public submit_secret schema (design D2):
// targets/method/digits/submission plus the optional submitTarget, loginId,
// username, and profile. No value field exists.
func submitSecretTool() mcp.Tool {
	return secretRequestSchema("submit_secret",
		"Prepare a secure sign-in handover: name the authentication input fields (from the latest "+
			"snapshot) and how the form is submitted. The secret value itself never travels through this "+
			"tool — the buddies host asks the person and delivers it out-of-band; invoking this tool "+
			"directly is refused. Call this instead of typing passwords or one-time codes with type.")
}

// requestSecretTool builds the internal __request_secret channel tool with
// the same schema as submit_secret (design D7). It is filtered from the
// model's tool list by the parent and invoked over stdio.
func requestSecretTool() mcp.Tool {
	return secretRequestSchema("__request_secret",
		"Internal buddies host channel: prepare a secure sign-in handover for the person's "+
			"secret. Invoked only by the buddies host; never by the model.")
}

// secretRequestSchema is the one schema both secret tools share.
func secretRequestSchema(name, description string) mcp.Tool {
	return mcp.NewTool(name,
		mcp.WithDescription(description),
		mcp.WithArray("targets", mcp.Required(),
			mcp.Description("Authentication input fields to fill, in fill order — each a ref from the latest "+
				"snapshot or an accessible name, exactly like click/type accept. One field for method "+
				"password; one or digits fields for one-time codes."),
			mcp.MinItems(1), mcp.MaxItems(12), mcp.WithStringItems()),
		mcp.WithString("method", mcp.Required(),
			mcp.Description("Kind of secret being submitted."),
			mcp.Enum("password", "otp", "authenticator")),
		mcp.WithNumber("digits",
			mcp.Description("Expected one-time code length for code methods (0-12; default 6)."),
			mcp.Min(0), mcp.Max(12), mcp.DefaultNumber(6)),
		mcp.WithString("submission", mcp.Required(),
			mcp.Description("How the form is submitted after filling: click a named button, press Enter, "+
				"or fill only."),
			mcp.Enum("click", "enter", "on_input")),
		mcp.WithString("submitTarget",
			mcp.Description("Ref or accessible name of the submit button; required for submission click.")),
		mcp.WithString("identifierTarget",
			mcp.Description("Ref or accessible name of the identifier field (username or email) that sits beside "+
				"the secret. Its presence shows an identifier box on the handover card; the person types "+
				"whichever the site asks for, and it is filled before the secret.")),
		mcp.WithString("loginId",
			mcp.Description("Login identifier to record when the person opts to save the credential.")),
		mcp.WithString("username",
			mcp.Description("Username accompanying the credential when the person opts to save it.")),
		mcp.WithString("profile", mcp.Description(profileArgDesc)),
	)
}

// rejectValueArguments refuses any value-bearing argument key with one fixed
// error, regardless of what the schema declared. Lenient bindings must not
// become a smuggling channel.
func rejectValueArguments(req mcp.CallToolRequest) error {
	for key := range req.GetArguments() {
		for _, forbidden := range valueForbiddenKeys {
			if strings.EqualFold(key, forbidden) {
				return errors.New("browser: submit_secret must never carry a value field; secrets arrive only through the buddies host handover channel")
			}
		}
	}
	return nil
}

// handleSubmitSecret is the public tool's handler: a guarded refusal, always.
// The value-based rejection runs first so a smuggled value key is called out
// by the schema contract, not silently ignored by lenient binding.
func (b *browserTools) handleSubmitSecret(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := rejectValueArguments(req); err != nil {
		return errResult(err), nil
	}
	return errResult(errors.New(submitSecretGuard)), nil
}

// handleRequestSecret prepares a secret handover for the parent host: all
// validations run before any user consent is asked (design D2). Readiness is
// a JSON receipt; failures are fixed reason codes.
func (b *browserTools) handleRequestSecret(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args secretArgs
	if err := req.BindArguments(&args); err != nil {
		return errResult(fmt.Errorf("browser: bad arguments: %w", err)), nil
	}
	if err := rejectValueArguments(req); err != nil {
		return errResult(err), nil
	}
	if err := validateSecretArgs(args); err != nil {
		return errResult(err), nil
	}
	prof, err := b.resolveProfile(req, args.Profile)
	if err != nil {
		return errResult(err), nil
	}
	digits, err := resolveSecretDigits(args.Digits)
	if err != nil {
		return errResult(err), nil
	}

	// armedID captures the request ID the freeze was armed with, so the park
	// can happen after the session lock releases (never hold s.mu while
	// taking the registry's mutex). stdio serializes parent calls, so no
	// concurrent prepare can slip through the window.
	var armedID string
	res, err := b.withSessionInternal(ctx, prof.Key, func(actx context.Context, s *profileSession) (any, error) {
		if s.held {
			return secretUnavailable(reasonSessionHeld), nil
		}
		if b.mgr.pendingSecretOnProfile(prof.Key) {
			return secretUnavailable(reasonAlreadyPending), nil
		}
		prepared, perr := s.prepareSecret(actx, args, digits, b.allowPrivate())
		if perr != nil {
			return nil, perr
		}
		// The freeze starts at "ready", not at delivery (design D1's
		// inspection window).
		armedID = newRequestID()
		s.beginProtection(armedID)
		return prepared, nil
	})
	if err != nil {
		return renderResult(res, err)
	}

	prepared, ok := res.(preparedSecret)
	if !ok {
		return renderResult(res, nil) // the unavailable receipt
	}
	if armedID == "" {
		return errResult(errors.New("browser: prepare protection vanished before parking")), nil
	}
	b.mgr.parkAttention(&attentionRequest{
		ID:      armedID,
		Kind:    attentionSecret,
		Profile: prof.Key,
		Secret: &parkedSecret{
			origin:       prepared.origin,
			method:       args.Method,
			digits:       digits,
			submission:   args.Submission,
			targets:      prepared.targets,
			submitTarget: prepared.submitTarget,
			identifier:   prepared.identifier,
		},
	})

	return renderResult(secretReadyResult{
		Status:    "ready",
		RequestID: armedID,
		Origin:    prepared.origin,
		Method:    args.Method,
		Digits:    digits,
	}, nil)
}

// preparedSecret is the internal outcome of a successful prepare: the origin
// receipt plus the resolved fill plan waiting to be parked.
type preparedSecret struct {
	origin       string
	targets      []secretField
	submitTarget string
	identifier   *secretField // the identifier (username/email) field, when requested
}

// prepareSecret runs the pre-consent validations against the live page and
// snapshots the form fingerprint for every resolved target (design D2).
// Caller holds s.mu.
func (s *profileSession) prepareSecret(ctx context.Context, args secretArgs, digits int, allowPrivate bool) (any, error) {
	page, cancelPage := boundSessionPage(s, ctx)
	defer cancelPage()

	// HTTPS-only origin; the allow-private escape (dev and tests) admits
	// loopback http so the loopback fixtures can exercise the flow.
	rawURL, _ := s.preparePageOrigin(page)
	u, err := parseOriginURL(rawURL)
	if err != nil {
		return unavailableResult{Status: "unavailable", ReasonCode: reasonInsecureOrigin}, nil
	}
	if u.Scheme != "https" && !(allowPrivate && isLoopbackURL(u)) {
		return unavailableResult{Status: "unavailable", ReasonCode: reasonInsecureOrigin}, nil
	}
	host := u.Hostname()

	// No connected or opener tab may be showing the same site.
	if err := s.checkSiteIsolation(ctx, page, host); err != nil {
		return unavailableResult{Status: "unavailable", ReasonCode: reasonIsolation}, nil
	}

	// Resolve every target (and the submit target) against a fresh snapshot.
	elements, err := s.snapshotBasis(page)
	if err != nil {
		return unavailableResult{Status: "unavailable", ReasonCode: reasonTargetUnresolved}, nil
	}
	targets := make([]secretField, 0, len(args.Targets))
	for _, spec := range args.Targets {
		el, err := resolveSingleTarget(elements, spec)
		if err != nil {
			return unavailableResult{Status: "unavailable", ReasonCode: reasonTargetUnresolved}, nil
		}
		probe, err := s.probeSecretTarget(ctx, page, el.Ref)
		if err != nil {
			return unavailableResult{Status: "unavailable", ReasonCode: reasonTargetUnresolved}, nil
		}
		if probe.Framed {
			return unavailableResult{Status: "unavailable", ReasonCode: reasonFramed}, nil
		}
		targets = append(targets, secretField{ref: el.Ref, fp: probe.FP})
	}
	submitRef := ""
	if args.Submission == "click" {
		el, err := resolveSingleTarget(elements, args.SubmitTarget)
		if err != nil {
			return unavailableResult{Status: "unavailable", ReasonCode: reasonTargetUnresolved}, nil
		}
		probe, err := s.probeSecretTarget(ctx, page, el.Ref)
		if err != nil {
			return unavailableResult{Status: "unavailable", ReasonCode: reasonTargetUnresolved}, nil
		}
		if probe.Framed {
			return unavailableResult{Status: "unavailable", ReasonCode: reasonFramed}, nil
		}
		submitRef = el.Ref
	}

	// The identifier (username or email) field resolves and fingerprints like
	// every other authentication field; it is filled before the secret.
	var identifier *secretField
	if args.IdentifierTarget != "" {
		el, err := resolveSingleTarget(elements, args.IdentifierTarget)
		if err != nil {
			return unavailableResult{Status: "unavailable", ReasonCode: reasonTargetUnresolved}, nil
		}
		probe, err := s.probeSecretTarget(ctx, page, el.Ref)
		if err != nil {
			return unavailableResult{Status: "unavailable", ReasonCode: reasonTargetUnresolved}, nil
		}
		if probe.Framed {
			return unavailableResult{Status: "unavailable", ReasonCode: reasonFramed}, nil
		}
		identifier = &secretField{ref: el.Ref, fp: probe.FP}
	}

	return preparedSecret{origin: fmt.Sprintf("%s://%s", u.Scheme, host), targets: targets, submitTarget: submitRef, identifier: identifier}, nil
}

// secretUnavailable is the fixed unavailable receipt for a reason code.
func secretUnavailable(code string) unavailableResult {
	return unavailableResult{Status: "unavailable", ReasonCode: code}
}

// resolveSingleTarget resolves one `targets` entry: the same semantic union
// click/type accept — a snapshot ref first, then an accessible name.
func resolveSingleTarget(elements []SnapshotElement, spec string) (SnapshotElement, error) {
	if el, err := resolveTarget(elements, TargetQuery{Ref: spec}); err == nil {
		return el, nil
	}
	return resolveTarget(elements, TargetQuery{Name: spec})
}

// isLoopbackURL reports whether u addresses the local machine (the
// allow-private escape hatch for dev and the loopback test fixtures).
func isLoopbackURL(u *neturl.URL) bool {
	host := u.Hostname()
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// validateSecretArgs enforces the submit_secret contract (design D2):
// password = exactly 1 target; code = 1 or digits targets; click submission
// ⇔ submitTarget present; digits within 0-12.
func validateSecretArgs(args secretArgs) error {
	if len(args.Targets) == 0 || len(args.Targets) > 12 {
		return fmt.Errorf("browser: targets must carry 1 to 12 field references, got %d", len(args.Targets))
	}
	switch args.Method {
	case "password", "otp", "authenticator":
	default:
		return fmt.Errorf("browser: method must be password, otp, or authenticator, got %q", args.Method)
	}
	switch args.Submission {
	case "click", "enter", "on_input":
	default:
		return fmt.Errorf("browser: submission must be click, enter, or on_input, got %q", args.Submission)
	}
	digits, err := resolveSecretDigits(args.Digits)
	if err != nil {
		return err
	}
	if args.Submission == "click" && args.SubmitTarget == "" {
		return errors.New("browser: submission click requires submitTarget")
	}
	if args.Submission != "click" && args.SubmitTarget != "" {
		return errors.New("browser: submitTarget is only valid for submission click")
	}
	if args.Method == "password" && len(args.Targets) != 1 {
		return fmt.Errorf("browser: method password requires exactly 1 target, got %d", len(args.Targets))
	}
	if args.Method != "password" && len(args.Targets) != 1 && len(args.Targets) != digits {
		return fmt.Errorf("browser: method %s requires 1 or %d targets, got %d", args.Method, digits, len(args.Targets))
	}
	return nil
}

// resolveSecretDigits resolves the optional digits argument: absent means 6,
// values must be whole numbers within 0-12.
func resolveSecretDigits(arg *float64) (int, error) {
	if arg == nil {
		return 6, nil
	}
	if *arg != math.Trunc(*arg) {
		return 0, fmt.Errorf("browser: digits must be a whole number, got %v", *arg)
	}
	if *arg < 0 || *arg > 12 {
		return 0, fmt.Errorf("browser: digits must be within 0-12, got %v", *arg)
	}
	return int(*arg), nil
}

// defaultSiteIsolation refuses the prepare when another tab of the same
// browser shows the same host, or is opener-connected to this one: a
// neighboring same-site tab could observe the sign-in (design D2). Exact-host
// matching is the conservative subset of registrable-domain matching.
func defaultSiteIsolation(ctx context.Context, page context.Context, host string) error {
	runCtx, cancel := bindCall(page, ctx)
	defer cancel()
	return chromedp.Do(runCtx, chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		res, err := cdp.Call(ctx, t, target.GetTargets, target.GetTargetsParams{})
		if err != nil {
			return fmt.Errorf("browser: enumerate tabs: %w", err)
		}
		for _, info := range res.TargetInfos {
			if info == nil || info.Type != "page" {
				continue
			}
			if info.TargetID == t.TargetID {
				continue // this session's own tab is the sign-in itself
			}
			u, err := parseOriginURL(info.URL)
			if err != nil {
				continue
			}
			if u.Hostname() == host {
				return fmt.Errorf("browser: another tab is showing %s", host)
			}
		}
		return nil
	}))
}

// defaultSecretProbe snapshots one resolved target's form fingerprint plus
// its framing, via a function call bound to the element's own frame: inside
// that call, `window` is the element frame's window, so a framed target sees
// window != window.top.
func defaultSecretProbe(ctx context.Context, page context.Context, ref string) (secretProbe, error) {
	runCtx, cancel := bindCall(page, ctx)
	defer cancel()
	var probe secretProbe
	err := chromedp.Do(runCtx, chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		obj, err := resolveRefObject(ctx, t, ref)
		if err != nil {
			return err
		}
		res, err := cdp.Call(ctx, t, runtime.CallFunctionOn, runtime.CallFunctionOnParams{
			ObjectID: obj.ObjectID,
			// A function declaration, not an arrow: `this` must bind to the
			// element the ObjectID wraps.
			FunctionDeclaration: `function() { const f = this.form; return { framed: window !== window.top, localName: this.localName || '', type: this.type || '', id: this.id || '', name: this.name || '', autocomplete: this.getAttribute('autocomplete') || '', formAction: f ? (f.getAttribute('action') || '') : '', formMethod: f ? (f.getAttribute('method') || '') : '' }; }`,
			ReturnByValue:       boolPtr(true),
		})
		if err != nil {
			return fmt.Errorf("browser: probe authentication target: %w", err)
		}
		if res.ExceptionDetails != nil || res.Result == nil {
			return errors.New("browser: probe authentication target failed")
		}
		var payload struct {
			Framed       bool   `json:"framed"`
			LocalName    string `json:"localName"`
			Type         string `json:"type"`
			ID           string `json:"id"`
			Name         string `json:"name"`
			Autocomplete string `json:"autocomplete"`
			FormAction   string `json:"formAction"`
			FormMethod   string `json:"formMethod"`
		}
		if err := json.Unmarshal(res.Result.Value, &payload); err != nil {
			return errors.New("browser: probe authentication target failed")
		}
		probe = secretProbe{
			Framed: payload.Framed,
			FP: formFingerprint{
				LocalName:    payload.LocalName,
				Type:         payload.Type,
				ID:           payload.ID,
				Name:         payload.Name,
				Autocomplete: payload.Autocomplete,
				FormAction:   payload.FormAction,
				FormMethod:   payload.FormMethod,
			},
		}
		return nil
	}))
	if err != nil {
		return secretProbe{}, err
	}
	return probe, nil
}
