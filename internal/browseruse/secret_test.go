package browseruse

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

// The stub-level fixtures follow the package's established seams: sessions
// are registered directly on a test manager and every CDP touchpoint is a
// seam override, so no test here ever starts a real browser. The loopback
// browser fixtures live in secret_browser_test.go.

// callHandler drives one handler with JSON-style arguments and returns the
// tool result.
func callHandler(t *testing.T, h func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error), args map[string]any) mcp.CallToolResult {
	t.Helper()
	res, err := h(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Name: "test", Arguments: args},
	})
	require.NoError(t, err, "domain failures are tool errors, never JSON-RPC errors")
	require.NotNil(t, res)
	require.True(t, res.IsError || len(res.Content) == 1, "every result carries exactly its message")
	return *res
}

// stubLaunchPanicking fails the test if anything tries to launch a real
// browser from a path the test did not plan for.
func stubLaunchPanicking(t *testing.T) func(context.Context, string, Config) (*profileSession, error) {
	return func(context.Context, string, Config) (*profileSession, error) {
		t.Helper()
		t.Error("unexpected real browser launch")
		return nil, errors.New("no browser in this test")
	}
}

// newTestTools returns browserTools over a test manager whose launch fails
// the test, plus a stub session registered for the named profiles.
func newTestTools(t *testing.T, profiles ...string) (*browserTools, *manager) {
	t.Helper()
	m := newManager()
	m.launch = stubLaunchPanicking(t)
	for _, name := range profiles {
		s := &profileSession{dir: name}
		m.sessions[name] = s
	}
	return &browserTools{cfg: DefaultConfig(), mgr: m}, m
}

// armedProtection seals a canary value into a session's strong protection, as
// a live __deliver_secret call frame would.
func armedProtection(s *profileSession, requestID, value string) {
	s.beginProtection(requestID)
	s.sealProtectionValue(value)
}

func TestRejectValueArguments(t *testing.T) {
	ok := map[string]any{"targets": []string{"e1"}, "method": "password", "submission": "enter"}
	require.NoError(t, rejectValueArguments(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: ok}}),
		"the schema-shaped arguments carry no value field")

	for _, key := range []string{"value", "password", "secret", "code", "otp", "Value", "PASSWORD", "Secret", "Code", "OTP"} {
		args := map[string]any{"targets": []string{"e1"}, key: "canary-value"}
		err := rejectValueArguments(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: args}})
		require.Error(t, err, "key %q must be rejected", key)
		require.Equal(t,
			"browser: submit_secret must never carry a value field; secrets arrive only through the buddies host handover channel",
			err.Error(), "the value-field rejection is one fixed error, key %q", key)
	}
}

func TestValidateSecretArgs(t *testing.T) {
	d := func(v float64) *float64 { return &v }
	cases := []struct {
		name    string
		args    secretArgs
		wantErr string // empty means valid
	}{
		{
			name: "password with click submission",
			args: secretArgs{Targets: []string{"e1"}, Method: "password", Submission: "click", SubmitTarget: "e2"},
		},
		{
			name: "password with enter submission",
			args: secretArgs{Targets: []string{"e1"}, Method: "password", Submission: "enter"},
		},
		{
			name: "one-time code as one pasted target",
			args: secretArgs{Targets: []string{"e1"}, Method: "otp", Submission: "on_input", Digits: d(6)},
		},
		{
			name: "one-time code as digits boxes",
			args: secretArgs{Targets: []string{"e1", "e2", "e3", "e4", "e5", "e6"}, Method: "authenticator", Submission: "enter", Digits: d(6)},
		},
		{name: "no targets", args: secretArgs{Method: "password", Submission: "enter"},
			wantErr: "1 to 12 field references"},
		{name: "thirteen targets", args: secretArgs{Targets: make([]string, 13), Method: "password", Submission: "enter"},
			wantErr: "1 to 12 field references"},
		{name: "bad method", args: secretArgs{Targets: []string{"e1"}, Method: "passphrase", Submission: "enter"},
			wantErr: "method must be password, otp, or authenticator"},
		{name: "bad submission", args: secretArgs{Targets: []string{"e1"}, Method: "password", Submission: "paste"},
			wantErr: "submission must be click, enter, or on_input"},
		{name: "click without submitTarget", args: secretArgs{Targets: []string{"e1"}, Method: "password", Submission: "click"},
			wantErr: "click requires submitTarget"},
		{name: "submitTarget outside click", args: secretArgs{Targets: []string{"e1"}, Method: "password", Submission: "enter", SubmitTarget: "e2"},
			wantErr: "submitTarget is only valid for submission click"},
		{name: "password with two targets", args: secretArgs{Targets: []string{"e1", "e2"}, Method: "password", Submission: "enter"},
			wantErr: "exactly 1 target"},
		{name: "code with neither one nor digits targets", args: secretArgs{Targets: []string{"e1", "e2", "e3"}, Method: "otp", Submission: "enter", Digits: d(6)},
			wantErr: "1 or 6 targets"},
		{name: "fractional digits", args: secretArgs{Targets: []string{"e1"}, Method: "otp", Submission: "enter", Digits: d(6.5)},
			wantErr: "whole number"},
		{name: "digits above the cap", args: secretArgs{Targets: []string{"e1"}, Method: "otp", Submission: "enter", Digits: d(13)},
			wantErr: "within 0-12"},
		{name: "negative digits", args: secretArgs{Targets: []string{"e1"}, Method: "otp", Submission: "enter", Digits: d(-1)},
			wantErr: "within 0-12"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSecretArgs(tc.args)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestResolveSecretDigits(t *testing.T) {
	got, err := resolveSecretDigits(nil)
	require.NoError(t, err)
	require.Equal(t, 6, got, "absent digits default to 6")
	got, err = resolveSecretDigits(floatPtr(0))
	require.NoError(t, err)
	require.Equal(t, 0, got)
	got, err = resolveSecretDigits(floatPtr(12))
	require.NoError(t, err)
	require.Equal(t, 12, got)
	_, err = resolveSecretDigits(floatPtr(6.5))
	require.Error(t, err)
	_, err = resolveSecretDigits(floatPtr(13))
	require.Error(t, err)
	_, err = resolveSecretDigits(floatPtr(-1))
	require.Error(t, err)
}

func floatPtr(v float64) *float64 { return &v }

func TestURLCarriesSecret(t *testing.T) {
	require.True(t, urlCarriesSecret("https://site.test/next?pw=canary-123", "canary-123"))
	require.True(t, urlCarriesSecret("https://site.test/canary-123/x", "canary-123"))
	require.True(t, urlCarriesSecret("https://site.test/next#canary-123", "canary-123"))
	require.False(t, urlCarriesSecret("https://site.test/next?pw=other", "canary-123"))
	require.False(t, urlCarriesSecret("https://site.test/next", ""), "an empty value never matches: the guard is inert while the child holds no value")
}

// TestHandleSubmitSecret_GuardedRefusal pins the public surface: the handler
// never reaches the browser, whatever the arguments say.
func TestHandleSubmitSecret_GuardedRefusal(t *testing.T) {
	b, _ := newTestTools(t, "work")
	t.Run("schema-shaped call answers the fixed guard", func(t *testing.T) {
		res := callHandler(t, b.handleSubmitSecret, map[string]any{
			"targets": []string{"e1"}, "method": "password", "submission": "enter",
		})
		require.True(t, res.IsError)
		require.Equal(t, "submit_secret requires the buddies host handover channel.", resultText(t, res))
	})
	t.Run("a value-bearing key is rejected by the schema contract", func(t *testing.T) {
		res := callHandler(t, b.handleSubmitSecret, map[string]any{
			"targets": []string{"e1"}, "method": "password", "submission": "enter", "value": "canary-123",
		})
		require.True(t, res.IsError)
		require.Contains(t, resultText(t, res), "must never carry a value field")
	})
}

// TestHandleRequestSecret_PrepareValidation walks the prepare table on a
// fully stubbed session: no consent would be asked for any of these failures.
func TestHandleRequestSecret_PrepareValidation(t *testing.T) {
	elements := []SnapshotElement{
		{Ref: "e1", Role: "textbox", Name: "Password"},
		{Ref: "e2", Role: "button", Name: "Sign in"},
	}
	goodProbe := secretProbe{Framed: false, FP: formFingerprint{LocalName: "input", Type: "password"}}

	setup := func(t *testing.T, mutate func(s *profileSession)) (*browserTools, *manager) {
		b, m := newTestTools(t, "work")
		s := m.sessions["work"]
		s.originFn = func(context.Context) (string, string) { return "https://site.test/sign-in", "Sign in" }
		s.isolationFn = func(context.Context, context.Context, string) error { return nil }
		s.snapshotFn = func(context.Context) ([]SnapshotElement, error) { return elements, nil }
		s.probeFn = func(context.Context, context.Context, string) (secretProbe, error) { return goodProbe, nil }
		if mutate != nil {
			mutate(s)
		}
		return b, m
	}
	passwordCall := map[string]any{
		"targets": []any{"Password"}, "method": "password", "submission": "click", "submitTarget": "Sign in",
		"profile": "work",
	}

	t.Run("https origin prepares ready and arms protection", func(t *testing.T) {
		b, m := setup(t, nil)
		res := callHandler(t, b.handleRequestSecret, passwordCall)
		require.False(t, res.IsError, "prepare succeeds: %s", resultText(t, res))
		var ready secretReadyResult
		require.NoError(t, json.Unmarshal([]byte(resultText(t, res)), &ready))
		require.Equal(t, "ready", ready.Status)
		require.Equal(t, "https://site.test", ready.Origin)
		require.Equal(t, "password", ready.Method)
		require.Equal(t, 6, ready.Digits, "absent digits ride the default")
		require.Len(t, ready.RequestID, 32, "the request ID is hex")
		s := m.sessions["work"]
		require.NotNil(t, s.protection, "the freeze starts at ready, not at delivery")
		require.Equal(t, ready.RequestID, s.protection.requestID)
		parked, ok := m.findAttention(ready.RequestID)
		require.True(t, ok, "the fill plan is parked for the parent")
		require.Equal(t, attentionSecret, parked.Kind)
		require.Equal(t, "work", parked.Profile)
		require.Len(t, parked.Secret.targets, 1)
		require.Equal(t, "e1", parked.Secret.targets[0].ref)
		require.Equal(t, "e2", parked.Secret.submitTarget, "names resolve to refs")
	})

	t.Run("http origin is refused", func(t *testing.T) {
		b, _ := setup(t, func(s *profileSession) {
			s.originFn = func(context.Context) (string, string) { return "http://site.test/sign-in", "Sign in" }
		})
		res := callHandler(t, b.handleRequestSecret, passwordCall)
		require.False(t, res.IsError)
		var got unavailableResult
		require.NoError(t, json.Unmarshal([]byte(resultText(t, res)), &got))
		require.Equal(t, "unavailable", got.Status)
		require.Equal(t, reasonInsecureOrigin, got.ReasonCode)
	})

	t.Run("loopback http passes under allow-private", func(t *testing.T) {
		b, _ := setup(t, func(s *profileSession) {
			s.originFn = func(context.Context) (string, string) { return "http://127.0.0.1:8811/sign-in", "Sign in" }
		})
		b.cfg.AllowPrivate = true
		res := callHandler(t, b.handleRequestSecret, passwordCall)
		require.False(t, res.IsError, "the allow-private escape admits loopback http for dev and tests")
		require.False(t, strings.Contains(resultText(t, res), "unavailable"))
	})

	t.Run("loopback http still refuses without allow-private", func(t *testing.T) {
		b, _ := setup(t, func(s *profileSession) {
			s.originFn = func(context.Context) (string, string) { return "http://127.0.0.1:8811/sign-in", "Sign in" }
		})
		res := callHandler(t, b.handleRequestSecret, passwordCall)
		require.Contains(t, resultText(t, res), reasonInsecureOrigin)
	})

	t.Run("framed target is refused", func(t *testing.T) {
		b, _ := setup(t, func(s *profileSession) {
			s.probeFn = func(context.Context, context.Context, string) (secretProbe, error) {
				return secretProbe{Framed: true, FP: goodProbe.FP}, nil
			}
		})
		res := callHandler(t, b.handleRequestSecret, passwordCall)
		require.Contains(t, resultText(t, res), reasonFramed)
	})

	t.Run("unresolvable target is refused", func(t *testing.T) {
		b, _ := setup(t, func(s *profileSession) {
			s.snapshotFn = func(context.Context) ([]SnapshotElement, error) {
				return []SnapshotElement{{Ref: "e9", Role: "textbox", Name: "Login"}}, nil
			}
		})
		res := callHandler(t, b.handleRequestSecret, passwordCall)
		require.Contains(t, resultText(t, res), reasonTargetUnresolved)
	})

	t.Run("same-site isolation is refused", func(t *testing.T) {
		b, _ := setup(t, func(s *profileSession) {
			s.isolationFn = func(context.Context, context.Context, string) error { return errors.New("another tab") }
		})
		res := callHandler(t, b.handleRequestSecret, passwordCall)
		require.Contains(t, resultText(t, res), reasonIsolation)
	})

	t.Run("an already-pending secret is refused", func(t *testing.T) {
		b, m := setup(t, nil)
		m.attention["pending"] = &attentionRequest{ID: "pending", Kind: attentionSecret, Profile: "work"}
		res := callHandler(t, b.handleRequestSecret, passwordCall)
		require.Contains(t, resultText(t, res), reasonAlreadyPending)
	})

	t.Run("a held session is refused", func(t *testing.T) {
		b, m := setup(t, nil)
		m.sessions["work"].held = true
		res := callHandler(t, b.handleRequestSecret, passwordCall)
		require.Contains(t, resultText(t, res), reasonSessionHeld)
	})

	t.Run("a value-bearing argument is rejected on the channel too", func(t *testing.T) {
		b, _ := setup(t, nil)
		res := callHandler(t, b.handleRequestSecret, map[string]any{
			"targets": []any{"e1"}, "method": "password", "submission": "enter", "value": "canary-123",
		})
		require.True(t, res.IsError)
		require.Contains(t, resultText(t, res), "must never carry a value field")
	})

	t.Run("argument-shape failures answer readable tool errors", func(t *testing.T) {
		b, _ := setup(t, nil)
		for _, tc := range []struct {
			name string
			args map[string]any
			want string
		}{
			{"password with two targets", map[string]any{"targets": []any{"e1", "e2"}, "method": "password", "submission": "enter"}, "exactly 1 target"},
			{"click without submitTarget", map[string]any{"targets": []any{"e1"}, "method": "password", "submission": "click"}, "click requires submitTarget"},
			{"bad digits", map[string]any{"targets": []any{"e1"}, "method": "otp", "submission": "enter", "digits": 13}, "within 0-12"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				res := callHandler(t, b.handleRequestSecret, tc.args)
				require.True(t, res.IsError)
				require.Contains(t, resultText(t, res), tc.want)
			})
		}
	})

	t.Run("names resolve like click/type: ambiguity is a refusal", func(t *testing.T) {
		b, _ := setup(t, func(s *profileSession) {
			s.snapshotFn = func(context.Context) ([]SnapshotElement, error) {
				return []SnapshotElement{
					{Ref: "e1", Role: "button", Name: "Sign in"},
					{Ref: "e2", Role: "link", Name: "Sign in here"},
				}, nil
			}
		})
		res := callHandler(t, b.handleRequestSecret, map[string]any{
			"targets": []any{"Sign"}, "method": "password", "submission": "enter", "profile": "work",
		})
		require.Contains(t, resultText(t, res), reasonTargetUnresolved,
			"an ambiguous name must not resolve arbitrarily")
	})
}

// TestProtectionFreeze_InspectionRefusalAndRelease pins the freeze: inspection
// tools refuse on the protected profile only, and __resolve_attention ends it.
func TestProtectionFreeze_InspectionRefusalAndRelease(t *testing.T) {
	b, m := newTestTools(t, "work", "other")
	protected := m.sessions["work"]
	armedProtection(protected, "req-freeze", "canary-123")
	m.attention["req-freeze"] = &attentionRequest{ID: "req-freeze", Kind: attentionSecret, Profile: "work"}

	t.Run("inspection tools refuse with the protection refusal", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			h    func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
			args map[string]any
		}{
			{"snapshot", b.handleSnapshot, map[string]any{"profile": "work"}},
			{"screenshot", b.handleScreenshot, map[string]any{"profile": "work"}},
			{"cookies", b.handleCookies, map[string]any{"op": "list", "profile": "work"}},
			{"read", b.handleRead, map[string]any{"profile": "work"}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				res := callHandler(t, tc.h, tc.args)
				require.True(t, res.IsError)
				text := resultText(t, res)
				require.Contains(t, text, "Secure authentication is in progress",
					"inspection answers one fixed protection refusal")
				require.NotContains(t, text, "canary-123", "no refusal ever echoes the value")
			})
		}
	})

	t.Run("another profile is unaffected", func(t *testing.T) {
		other := m.sessions["other"]
		require.NoError(t, other.guardInspection())
		require.NoError(t, other.guardEvaluate())
	})

	t.Run("navigate with the value in the URL is blocked", func(t *testing.T) {
		res := callHandler(t, b.handleNavigate, map[string]any{
			// A public IP literal passes the navigate policy offline, so the
			// urlCarriesSecret guard is what refuses.
			"url": "https://93.184.216.34/collect?pw=canary-123", "profile": "work",
		})
		require.True(t, res.IsError)
		require.Contains(t, resultText(t, res), "navigation blocked")
	})

	t.Run("a clean URL passes the navigation guard", func(t *testing.T) {
		require.NoError(t, protected.guardNavigation("https://site.test/next"),
			"the guard blocks URLs carrying the value only")
	})

	t.Run("resolve_attention releases the freeze", func(t *testing.T) {
		require.True(t, b.mgr.resolveAttention("req-freeze"))
		require.Nil(t, protected.protection)
		_, parked := b.mgr.findAttention("req-freeze")
		require.False(t, parked, "the parked request is gone")
		require.NoError(t, protected.guardInspection())
	})

	t.Run("navigation guard is inert once protection ends", func(t *testing.T) {
		require.NoError(t, protected.guardNavigation("https://evil.test/?pw=canary-123"),
			"the value is out of child memory after release")
	})
}

// TestProtectionStates pins the freeze state machine transitions.
func TestProtectionStates(t *testing.T) {
	s := &profileSession{}

	require.NoError(t, s.checkHeld(), "a fresh session is not held")
	s.holdSession("req-hold")
	require.ErrorContains(t, s.checkHeld(), "A person controls this browser")
	s.releaseSessionHold()
	require.NoError(t, s.checkHeld())

	s.beginProtection("req-1")
	require.Error(t, s.guardInspection(), "strong protection refuses inspection")
	require.Error(t, s.guardEvaluate(), "strong protection refuses evaluate paths")
	armedProtection(s, "req-1", "canary-123")
	require.ErrorContains(t, s.guardNavigation("https://x.test/?v=canary-123"), "navigation blocked")
	s.weakenProtection()
	require.NoError(t, s.guardInspection(), "the weak SPA-keep state allows inspection")
	require.Error(t, s.guardEvaluate(), "the weak state still holds the evaluate paths")
	require.NoError(t, s.guardNavigation("https://x.test/?v=canary-123"),
		"the value is dropped from child memory at the weak release")
	require.Equal(t, "", s.protection.value)
	s.releaseWeakProtection()
	require.Nil(t, s.protection, "the next real main-frame navigation ends the hold")

	s.beginProtection("req-2")
	s.releaseProtection()
	require.Nil(t, s.protection)
}

// TestDeliverSecret_InertWithoutLiveRequest pins the channel inertness: no
// matching live request means a fixed refusal and no browser contact.
func TestDeliverSecret_InertWithoutLiveRequest(t *testing.T) {
	b, _ := newTestTools(t, "work")

	t.Run("unknown requestID", func(t *testing.T) {
		res := callHandler(t, b.handleDeliverSecret, map[string]any{"requestID": "unknown", "value": "canary-123"})
		require.True(t, res.IsError)
		require.Equal(t, inertRefusal, resultText(t, res))
	})

	t.Run("missing arguments", func(t *testing.T) {
		res := callHandler(t, b.handleDeliverSecret, map[string]any{"requestID": "x"})
		require.True(t, res.IsError)
		require.Contains(t, resultText(t, res), "requestID and value are required")
	})

	t.Run("an ID parked for a different kind stays inert", func(t *testing.T) {
		b.mgr.attention["tk"] = &attentionRequest{ID: "tk", Kind: attentionTakeover, Profile: "work"}
		res := callHandler(t, b.handleDeliverSecret, map[string]any{"requestID": "tk", "value": "canary-123"})
		require.Equal(t, inertRefusal, resultText(t, res))
	})

	t.Run("a delivered request never retries", func(t *testing.T) {
		b, m := newTestTools(t, "work")
		s := m.sessions["work"]
		var fills int
		s.fillFn = func(context.Context, context.Context, string, string) error { fills++; return nil }
		s.probeFn = func(context.Context, context.Context, string) (secretProbe, error) { return secretProbe{}, nil }
		navigated := make(chan struct{})
		close(navigated)
		s.watchNavFn = func(context.Context, context.Context) *navWatch {
			return &navWatch{fired: navigated, stop: func() {}}
		}
		s.beginProtection("req-1")
		m.attention["req-1"] = &attentionRequest{
			ID: "req-1", Kind: attentionSecret, Profile: "work",
			Secret: &parkedSecret{submission: "on_input", targets: []secretField{{ref: "e1"}}},
		}
		res := callHandler(t, b.handleDeliverSecret, map[string]any{"requestID": "req-1", "value": "canary-123"})
		require.False(t, res.IsError, "first delivery proceeds: %s", resultText(t, res))
		parked, ok := m.findAttention("req-1")
		require.True(t, ok, "the entry stays resolvable for __resolve_attention")
		require.True(t, parked.delivered, "the request is marked delivered")
		require.True(t, m.resolveAttention("req-1"), "the freeze stays resolvable after delivery")
		res2 := callHandler(t, b.handleDeliverSecret, map[string]any{"requestID": "req-1", "value": "canary-123"})
		require.Equal(t, inertRefusal, resultText(t, res2), "a second call with the same ID is inert")
		require.Equal(t, 1, fills, "secrets are never filled twice")
	})
}

// TestDeliverSecret_Orchestration walks the fill/submission/scrub table on
// stubbed seams: the receipt path must never carry the canary value.
func TestDeliverSecret_Orchestration(t *testing.T) {
	canary := "canary-secrect-value-42"
	plan := func(submission, submitTarget string, targets ...string) *parkedSecret {
		p := &parkedSecret{submission: submission, submitTarget: submitTarget}
		for _, ref := range targets {
			p.targets = append(p.targets, secretField{ref: ref})
		}
		return p
	}

	type recorder struct {
		fills   []string
		pressed []string
		clicked []string
		cleared [][]string
		reloads int
		scans   int
	}

	setup := func(t *testing.T, p *parkedSecret, scanFinds bool, watch func(context.Context, context.Context) *navWatch) (*browserTools, *manager, *recorder) {
		b, m := newTestTools(t, "work")
		s := m.sessions["work"]
		rec := &recorder{}
		s.fillFn = func(_ context.Context, _ context.Context, ref, text string) error {
			rec.fills = append(rec.fills, ref+"="+text)
			return nil
		}
		s.submitClickFn = func(_ context.Context, _ context.Context, ref string) error {
			rec.clicked = append(rec.clicked, ref)
			return nil
		}
		s.submitEnterFn = func(_ context.Context, _ context.Context, ref string) error {
			rec.pressed = append(rec.pressed, ref)
			return nil
		}
		s.clearFieldsFn = func(_ context.Context, _ context.Context, refs []string) error {
			rec.cleared = append(rec.cleared, refs)
			return nil
		}
		s.scanFn = func(context.Context, context.Context, string) (bool, error) {
			rec.scans++
			return scanFinds, nil
		}
		s.reloadFn = func(context.Context, context.Context) error { rec.reloads++; return nil }
		s.probeFn = func(context.Context, context.Context, string) (secretProbe, error) { return secretProbe{}, nil }
		if watch != nil {
			s.watchNavFn = watch
		} else {
			// Default: no navigation fires within the wait.
			s.watchNavFn = func(context.Context, context.Context) *navWatch {
				return &navWatch{fired: make(chan struct{}), stop: func() {}}
			}
		}
		s.beginProtection("req-1")
		m.attention["req-1"] = &attentionRequest{ID: "req-1", Kind: attentionSecret, Profile: "work", Secret: p}
		return b, m, rec
	}
	deliver := func(t *testing.T, b *browserTools) (string, mcp.CallToolResult) {
		res := callHandler(t, b.handleDeliverSecret, map[string]any{"requestID": "req-1", "value": canary})
		return resultText(t, res), res
	}

	t.Run("fill plus click plus navigation completes and releases", func(t *testing.T) {
		navigated := make(chan struct{})
		close(navigated)
		b, m, rec := setup(t, plan("click", "e9", "e1", "e3"), false,
			func(context.Context, context.Context) *navWatch { return &navWatch{fired: navigated, stop: func() {}} })
		text, res := deliver(t, b)
		require.False(t, res.IsError)
		var got deliverCompletedResult
		require.NoError(t, json.Unmarshal([]byte(text), &got))
		require.Equal(t, "completed", got.Status)
		require.NotContains(t, text, canary, "the receipt path never echoes the value")
		require.Equal(t, []string{"e1=" + canary, "e3=" + canary}, rec.fills, "targets fill in order")
		require.Equal(t, []string{"e9"}, rec.clicked, "the submit target clicks")
		require.Empty(t, rec.cleared, "a real navigation needs no field clearing")
		require.Equal(t, 0, rec.scans, "a real navigation skips the in-page scan")
		require.Nil(t, m.sessions["work"].protection, "the navigation releases protection entirely")
	})

	t.Run("fill plus enter refocuses the last field", func(t *testing.T) {
		navigated := make(chan struct{})
		close(navigated)
		b, _, rec := setup(t, plan("enter", "", "e7"), false,
			func(context.Context, context.Context) *navWatch { return &navWatch{fired: navigated, stop: func() {}} })
		_, res := deliver(t, b)
		require.False(t, res.IsError)
		require.Equal(t, []string{"e7"}, rec.pressed, "enter presses on the last filled field")
	})

	t.Run("on_input only fills", func(t *testing.T) {
		b, _, rec := setup(t, plan("on_input", "", "e1"), false, nil)
		_, res := deliver(t, b)
		require.False(t, res.IsError)
		require.Empty(t, rec.clicked)
		require.Empty(t, rec.pressed)
	})

	t.Run("clean same-document page: fields cleared, SPA-keep", func(t *testing.T) {
		b, m, rec := setup(t, plan("click", "e9", "e1"), false, nil)
		text, res := deliver(t, b)
		require.False(t, res.IsError)
		require.Contains(t, text, `"completed"`)
		require.Equal(t, [][]string{{"e1"}}, rec.cleared, "the filled fields are cleared")
		require.Equal(t, 1, rec.scans, "the scrub scans the same-document page")
		require.Equal(t, 0, rec.reloads)
		s := m.sessions["work"]
		require.NotNil(t, s.protection, "SPA-keep keeps a weaker protection")
		require.True(t, s.protection.weak)
		require.Equal(t, "", s.protection.value, "the value is out of child memory")
		require.NoError(t, s.guardInspection(), "inspection is allowed in the weak state")
		require.Error(t, s.guardEvaluate(), "evaluate stays blocked until the next navigation")
	})

	t.Run("value echoed in the document: GET-reload", func(t *testing.T) {
		b, m, rec := setup(t, plan("click", "e9", "e1"), true, nil)
		text, res := deliver(t, b)
		require.False(t, res.IsError)
		require.Contains(t, text, `"completed"`)
		require.Equal(t, 1, rec.reloads, "the document is replaced with a GET reload")
		require.Equal(t, [][]string{{"e1"}}, rec.cleared)
		require.Nil(t, m.sessions["work"].protection, "the reload is a real navigation: protection ends")
	})

	t.Run("fingerprint mismatch fails safe before any fill", func(t *testing.T) {
		b, m, rec := setup(t, plan("click", "e9", "e1"), false, nil)
		s := m.sessions["work"]
		s.probeFn = func(context.Context, context.Context, string) (secretProbe, error) {
			return secretProbe{FP: formFingerprint{LocalName: "div"}}, nil
		}
		text, res := deliver(t, b)
		require.False(t, res.IsError)
		var got channelFailedResult
		require.NoError(t, json.Unmarshal([]byte(text), &got))
		require.Equal(t, "failed", got.Status)
		require.Equal(t, reasonTargetChanged, got.ReasonCode)
		require.NotContains(t, text, canary)
		require.Empty(t, rec.fills, "nothing is typed into a swapped form")
		require.NotNil(t, s.protection, "protection holds until __resolve_attention")
		require.Equal(t, "", s.protection.value)
		parked, ok := m.findAttention("req-1")
		require.True(t, ok, "the freeze stays resolvable after the failure")
		require.True(t, parked.delivered, "the delivered request never retries")
	})

	t.Run("fill failure clears partial work and fails fixed", func(t *testing.T) {
		b, m, rec := setup(t, plan("click", "e9", "e1", "e2"), false, nil)
		s := m.sessions["work"]
		s.fillFn = func(_ context.Context, _ context.Context, ref, text string) error {
			rec.fills = append(rec.fills, ref)
			if ref == "e2" {
				return errors.New("cdp gone")
			}
			return nil
		}
		text, res := deliver(t, b)
		require.False(t, res.IsError)
		var got channelFailedResult
		require.NoError(t, json.Unmarshal([]byte(text), &got))
		require.Equal(t, reasonFillFailed, got.ReasonCode)
		require.Equal(t, [][]string{{"e1"}}, rec.cleared, "the partially filled field is cleared")
		require.NotNil(t, s.protection, "protection holds for resolve")
		require.NotContains(t, text, canary)
	})

	t.Run("submission failure fails fixed", func(t *testing.T) {
		b, m, rec := setup(t, plan("click", "e9", "e1"), false, nil)
		s := m.sessions["work"]
		s.submitClickFn = func(context.Context, context.Context, string) error { return errors.New("click failed") }
		text, res := deliver(t, b)
		require.False(t, res.IsError)
		var got channelFailedResult
		require.NoError(t, json.Unmarshal([]byte(text), &got))
		require.Equal(t, reasonSubmitFailed, got.ReasonCode)
		require.Equal(t, [][]string{{"e1"}}, rec.cleared, "the filled field is cleared on submit failure")
		require.NotNil(t, s.protection)
	})

	t.Run("delivery without the armed freeze answers request-gone", func(t *testing.T) {
		b, m, _ := setup(t, plan("click", "e9", "e1"), false, nil)
		m.sessions["work"].protection = nil
		text, res := deliver(t, b)
		require.False(t, res.IsError)
		var got channelFailedResult
		require.NoError(t, json.Unmarshal([]byte(text), &got))
		require.Equal(t, reasonRequestGone, got.ReasonCode)
	})
}

// TestDeliveredSecretEntry_Lifecycle pins the delivered entry's lifecycle:
// after one completed __deliver_secret the entry must never answer
// already-pending for the profile again, __resolve_attention still ends it,
// and the delivered entry leaves the registry at the SPA-keep release
// navigation and at the profile's session close.
func TestDeliveredSecretEntry_Lifecycle(t *testing.T) {
	// completedDelivery drives one completed __deliver_secret on stub seams:
	// the pre-closed navigation watcher takes the real-navigation completion,
	// which releases protection entirely and needs no waits.
	completedDelivery := func(t *testing.T) (*browserTools, *manager) {
		b, m := newTestTools(t, "work")
		s := m.sessions["work"]
		s.fillFn = func(context.Context, context.Context, string, string) error { return nil }
		s.probeFn = func(context.Context, context.Context, string) (secretProbe, error) { return secretProbe{}, nil }
		navigated := make(chan struct{})
		close(navigated)
		s.watchNavFn = func(context.Context, context.Context) *navWatch {
			return &navWatch{fired: navigated, stop: func() {}}
		}
		s.beginProtection("req-1")
		m.attention["req-1"] = &attentionRequest{
			ID: "req-1", Kind: attentionSecret, Profile: "work",
			Secret: &parkedSecret{submission: "on_input", targets: []secretField{{ref: "e1"}}},
		}
		res := callHandler(t, b.handleDeliverSecret, map[string]any{"requestID": "req-1", "value": "canary-123"})
		require.False(t, res.IsError, "the delivery completes: %s", resultText(t, res))
		require.Contains(t, resultText(t, res), `"completed"`)
		return b, m
	}

	t.Run("a completed delivery never answers already-pending again", func(t *testing.T) {
		b, m := completedDelivery(t)
		parked, ok := m.findAttention("req-1")
		require.True(t, ok)
		require.True(t, parked.delivered, "the entry is delivered after the completed receipt")

		// The prepare seams for a second request on the same profile.
		s := m.sessions["work"]
		s.originFn = func(context.Context) (string, string) { return "https://site.test/sign-in", "Sign in" }
		s.isolationFn = func(context.Context, context.Context, string) error { return nil }
		s.snapshotFn = func(context.Context) ([]SnapshotElement, error) {
			return []SnapshotElement{
				{Ref: "e1", Role: "textbox", Name: "Password"},
				{Ref: "e2", Role: "button", Name: "Sign in"},
			}, nil
		}
		res := callHandler(t, b.handleRequestSecret, map[string]any{
			"targets": []any{"Password"}, "method": "password", "submission": "click", "submitTarget": "Sign in",
			"profile": "work",
		})
		require.False(t, res.IsError, "a second prepare must not answer already-pending: %s", resultText(t, res))
		var ready secretReadyResult
		require.NoError(t, json.Unmarshal([]byte(resultText(t, res)), &ready))
		require.Equal(t, "ready", ready.Status)
		require.NotEqual(t, "req-1", ready.RequestID)

		// The delivered entry stays resolvable: __resolve_attention ends it.
		require.True(t, m.resolveAttention("req-1"), "the delivered entry stays resolvable")
		parked, ok = m.findAttention("req-1")
		require.False(t, ok, "resolution removes the delivered entry")
	})

	t.Run("the release navigation ends the delivered entry", func(t *testing.T) {
		b, m := newTestTools(t, "work")
		s := m.sessions["work"]
		m.attention["req-weak"] = &attentionRequest{ID: "req-weak", Kind: attentionSecret, Profile: "work", delivered: true}
		s.beginProtection("req-weak")
		s.weakenProtection()
		// handleNavigate captures the released ID and drops the entry after
		// the session lock releases; the same sequence at unit level.
		id := s.releaseWeakProtection()
		require.Equal(t, "req-weak", id, "the release reports the delivered request's ID")
		require.Nil(t, s.protection, "the SPA-keep hold ends")
		b.mgr.dropDeliveredAttention(id)
		_, parked := m.findAttention("req-weak")
		require.False(t, parked, "the delivered entry leaves the registry at the release navigation")
	})

	t.Run("the delivered drop leaves pending and takeover entries untouched", func(t *testing.T) {
		b, m := newTestTools(t, "work")
		m.attention["req-pending"] = &attentionRequest{ID: "req-pending", Kind: attentionSecret, Profile: "work"}
		m.attention["req-take"] = &attentionRequest{ID: "req-take", Kind: attentionTakeover, Profile: "work"}
		b.mgr.dropDeliveredAttention("req-pending")
		b.mgr.dropDeliveredAttention("req-take")
		b.mgr.dropDeliveredAttention("req-unknown")
		_, pending := m.findAttention("req-pending")
		require.True(t, pending, "a pending secret entry is untouched")
		_, take := m.findAttention("req-take")
		require.True(t, take, "a takeover entry is untouched")
	})

	t.Run("the session close ends the profile's delivered entries", func(t *testing.T) {
		_, m := completedDelivery(t)
		m.attention["req-keep"] = &attentionRequest{ID: "req-keep", Kind: attentionSecret, Profile: "work", delivered: true}
		m.attention["req-live"] = &attentionRequest{ID: "req-live", Kind: attentionSecret, Profile: "work"}
		m.attention["req-other"] = &attentionRequest{ID: "req-other", Kind: attentionSecret, Profile: "other", delivered: true}
		require.NoError(t, m.shutdown("work"))
		_, parked := m.findAttention("req-1")
		require.False(t, parked, "the completed entry does not outlive its session")
		_, kept := m.findAttention("req-keep")
		require.False(t, kept, "a delivered entry parked for the profile is dropped at close")
		_, live := m.findAttention("req-live")
		require.True(t, live, "a pending entry survives the close for __resolve_attention")
		_, other := m.findAttention("req-other")
		require.True(t, other, "another profile's entries are untouched")
	})

	t.Run("the idle sweep ends the delivered entries of the reaped session", func(t *testing.T) {
		_, m := completedDelivery(t)
		// newTestTools sessions carry a zero lastUsed and idleTTL, so the
		// first sweep reaps the session; the completed delivery left no
		// protection behind (the real-navigation path released it).
		require.Nil(t, m.sessions["work"].protection)
		m.sweep()
		_, live := m.sessions["work"]
		require.False(t, live, "the session is reaped")
		_, parked := m.findAttention("req-1")
		require.False(t, parked, "the delivered entry does not outlive the swept session")
	})
}

// TestScanOnceJS pins the scrub scan's coverage contract: text nodes, element
// values, attributes, document.title, location.href, and open shadow roots.
func TestScanOnceJS(t *testing.T) {
	require.Contains(t, scanOnceJS(`"x"`), "document.title")
	require.Contains(t, scanOnceJS(`"x"`), "location.href")
	require.Contains(t, scanOnceJS(`"x"`), "shadowRoot")
	require.Contains(t, scanOnceJS(`"x"`), "nodeType === Node.TEXT_NODE")
	require.Contains(t, scanOnceJS(`"x"`), "attributes")
	require.Contains(t, scanOnceJS(`"x"`), "typeof el.value === 'string'")
}
