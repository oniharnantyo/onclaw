package sessions_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/stretchr/testify/require"
)

// The browser-attention events (add-browser-login-handover tasks.md 3.1,
// design D3): the pending request parks the turn with a receipt-safe card
// description, the resolution records a receipt only — outcome, redacted
// length, optional saved-login id — and the typed value never enters any
// event payload.

// TestInput_JSONRoundTrip covers the wire contract of the attention payload:
// every field survives a JSON encode/decode for the required and resolved
// shapes, and a payload written before a field existed decodes unchanged.
func TestInput_JSONRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		in   sessions.Input
	}{
		{
			name: "secret request",
			in: sessions.Input{
				InterruptID:   "att-1",
				Prompt:        "The agent asks you to sign in at https://example.com.",
				Method:        "password",
				Origin:        "https://example.com",
				AttentionKind: sessions.AttentionSecret,
				Username:      "sam@example.com",
				LoginID:       "login-7",
			},
		},
		{
			name: "otp request",
			in: sessions.Input{
				InterruptID:   "att-2",
				Prompt:        "The agent asks you to sign in at https://example.com.",
				Method:        "otp",
				Digits:        6,
				Origin:        "https://example.com",
				AttentionKind: sessions.AttentionSecret,
			},
		},
		{
			name: "takeover request",
			in: sessions.Input{
				InterruptID:   "att-3",
				Prompt:        "The agent asks to hand over the work browser for sign-in.",
				Origin:        "https://example.com",
				AttentionKind: sessions.AttentionTakeover,
			},
		},
		{
			name: "completed receipt",
			in: sessions.Input{
				InterruptID:  "att-1",
				Outcome:      sessions.InputCompleted,
				Length:       14,
				SavedLoginID: "login-7",
			},
		},
		{
			name: "cancelled receipt",
			in: sessions.Input{
				InterruptID: "att-3",
				Outcome:     sessions.InputCancelled,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(sessions.Event{Kind: sessions.KindInputRequired, Input: &tc.in})
			require.NoError(t, err)

			var got sessions.Event
			require.NoError(t, json.Unmarshal(raw, &got))
			require.NotNil(t, got.Input)
			require.Equal(t, tc.in, *got.Input)
		})
	}

	// A minimal payload decodes into the zero-valued extension fields, so a
	// transcript written before a field existed keeps replaying.
	var minimal sessions.Input
	require.NoError(t, json.Unmarshal([]byte(`{"interrupt_id":"old-1"}`), &minimal))
	require.Equal(t, sessions.Input{InterruptID: "old-1"}, minimal)
}

// TestService_InputEventsReplay covers the persistence rule: the pending
// request and the resolution receipt append and replay intact through the
// store payload — with the value absent by construction.
func TestService_InputEventsReplay(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService(t)

	sess, err := svc.CreateSession(ctx, "agent-1", "")
	require.NoError(t, err)

	require.NoError(t, svc.Append(ctx, sess.ID, []sessions.Event{
		{Kind: sessions.KindUserMessage, Text: "sign in"},
		{TurnID: "turn-1", Kind: sessions.KindInputRequired, Input: &sessions.Input{
			InterruptID:   "att-1",
			Prompt:        "The agent asks you to sign in at https://example.com.",
			Method:        "password",
			Origin:        "https://example.com",
			AttentionKind: sessions.AttentionSecret,
		}},
		{TurnID: "turn-1", Kind: sessions.KindInputResolved, Input: &sessions.Input{
			InterruptID:  "att-1",
			Outcome:      sessions.InputCompleted,
			Length:       14,
			SavedLoginID: "login-7",
		}},
	}))

	events, err := svc.Events(ctx, sess.ID)
	require.NoError(t, err)
	require.Len(t, events, 3)

	req := events[1]
	require.Equal(t, sessions.KindInputRequired, req.Kind)
	require.Equal(t, "turn-1", req.TurnID)
	require.NotNil(t, req.Input)
	require.Equal(t, sessions.AttentionSecret, req.Input.AttentionKind)
	require.Equal(t, "https://example.com", req.Input.Origin)
	require.Empty(t, req.Input.Outcome, "a pending request carries no outcome")

	res := events[2]
	require.Equal(t, sessions.KindInputResolved, res.Kind)
	require.NotNil(t, res.Input)
	require.Equal(t, sessions.InputCompleted, res.Input.Outcome)
	require.Equal(t, 14, res.Input.Length)
	require.Equal(t, "login-7", res.Input.SavedLoginID)
}

// TestInput_ReceiptNeverCarriesValue pins design D3's invariant at the payload
// level: the KindInputResolved payload has no field a value could occupy —
// marshaling a receipt over a secret-shaped value leaves zero bytes of it in
// the encoded payload, and the fixed JSON shape carries the receipt fields
// only.
func TestInput_ReceiptNeverCarriesValue(t *testing.T) {
	const typedValue = "correct horse battery staple"

	res := sessions.Event{TurnID: "turn-1", Kind: sessions.KindInputResolved, Input: &sessions.Input{
		InterruptID:  "att-1",
		Outcome:      sessions.InputCompleted,
		Length:       len(typedValue),
		SavedLoginID: "login-7",
	}}
	raw, err := json.Marshal(res)
	require.NoError(t, err)

	require.NotContains(t, string(raw), typedValue,
		"the typed value must never appear in the receipt payload")
	require.NotContains(t, string(raw), "value",
		"the receipt shape has no value field at all")

	// The fixed shape: identity, outcome, redacted length, saved-login id.
	var payload map[string]any
	require.NoError(t, json.Unmarshal(raw, &payload))
	input, ok := payload["input"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "att-1", input["interrupt_id"])
	require.Equal(t, sessions.InputCompleted, input["outcome"])
	require.Equal(t, float64(len(typedValue)), input["length"])
	require.Equal(t, "login-7", input["saved_login_id"])
	require.Len(t, input, 4, "the receipt carries exactly the receipt fields")

	// Same invariant for the pending request's payload: the card description
	// names the origin, never a value.
	reqRaw, err := json.Marshal(sessions.Event{Kind: sessions.KindInputRequired, Input: &sessions.Input{
		InterruptID:   "att-1",
		Prompt:        "The agent asks you to sign in at https://example.com.",
		Method:        "password",
		Origin:        "https://example.com",
		AttentionKind: sessions.AttentionSecret,
	}})
	require.NoError(t, err)
	require.NotContains(t, string(reqRaw), typedValue)
	require.False(t, strings.Contains(string(reqRaw), `"value"`))
}

// TestInputPayload_IdentifierFlagRoundTrip: the identifier box flag rides
// the required payload; the resolved receipt stays value-free.
func TestInputPayload_IdentifierFlagRoundTrip(t *testing.T) {
	req := sessions.Input{InterruptID: "int-x", AttentionKind: sessions.AttentionSecret, Method: "password", Origin: "https://example.com", Identifier: true}
	raw, err := json.Marshal(req)
	require.NoError(t, err)
	var back sessions.Input
	require.NoError(t, json.Unmarshal(raw, &back))
	require.Equal(t, req, back)
	// The flag must not leak into the resolved side's JSON shape.
	res := sessions.Input{InterruptID: "int-x", Outcome: sessions.InputCompleted, Length: 9}
	rawRes, err := json.Marshal(res)
	require.NoError(t, err)
	require.NotContains(t, string(rawRes), "identifier")
}
