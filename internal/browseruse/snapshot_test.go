package browseruse

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveTarget(t *testing.T) {
	snapshot := []SnapshotElement{
		{Ref: "e1", Role: "heading", Name: "Welcome"},
		{Ref: "e2", Role: "button", Name: "Sign in"},
		{Ref: "e3", Role: "link", Name: "sign in here"},
		{Ref: "e4", Role: "textbox", Name: "Search", Value: ""},
		{Ref: "e5", Role: "button", Name: "Search", Value: "submit"},
	}

	t.Run("ref targeting resolves against the latest snapshot", func(t *testing.T) {
		el, err := resolveTarget(snapshot, TargetQuery{Ref: "e4"})
		require.NoError(t, err)
		require.Equal(t, "e4", el.Ref)
		require.Equal(t, "textbox", el.Role)
	})

	t.Run("unknown ref is a stale target", func(t *testing.T) {
		_, err := resolveTarget(snapshot, TargetQuery{Ref: "e99"})
		require.ErrorContains(t, err, "stale")
	})

	t.Run("empty target is rejected", func(t *testing.T) {
		_, err := resolveTarget(snapshot, TargetQuery{})
		require.ErrorContains(t, err, "ref or name is required")
	})

	t.Run("unique exact name resolves case-insensitively", func(t *testing.T) {
		el, err := resolveTarget(snapshot, TargetQuery{Name: "SIGN IN"})
		require.NoError(t, err, "the exact phase must ignore case")
		require.Equal(t, "e2", el.Ref)
	})

	t.Run("role narrows an ambiguous exact name", func(t *testing.T) {
		el, err := resolveTarget(snapshot, TargetQuery{Name: "Search", Role: "textbox"})
		require.NoError(t, err)
		require.Equal(t, "e4", el.Ref)
	})

	t.Run("ambiguous name lists every candidate with ref, role, and name", func(t *testing.T) {
		_, err := resolveTarget(snapshot, TargetQuery{Name: "Search"})
		require.Error(t, err)
		msg := err.Error()
		require.Contains(t, msg, `2 elements match name "Search"`)
		require.Contains(t, msg, "[e4 textbox \"Search\"]")
		require.Contains(t, msg, "[e5 button \"Search\"]", "candidates carry role, name, and ref")
	})

	t.Run("role alone cannot disambiguate: candidates listed", func(t *testing.T) {
		twoButtons := []SnapshotElement{
			{Ref: "e7", Role: "button", Name: "OK"},
			{Ref: "e8", Role: "button", Name: "OK"},
		}
		_, err := resolveTarget(twoButtons, TargetQuery{Name: "OK"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "[e7 button \"OK\"]")
	})

	t.Run("substring fallback matches", func(t *testing.T) {
		el, err := resolveTarget(snapshot, TargetQuery{Name: "sign in here"})
		require.NoError(t, err)
		require.Equal(t, "e3", el.Ref)

		el, err = resolveTarget(snapshot, TargetQuery{Name: "in here"})
		require.NoError(t, err, "the fallback phase matches by substring")
		require.Equal(t, "e3", el.Ref)
	})

	t.Run("role narrows the substring fallback", func(t *testing.T) {
		el, err := resolveTarget(snapshot, TargetQuery{Name: "sign", Role: "link"})
		require.NoError(t, err)
		require.Equal(t, "e3", el.Ref)
	})

	t.Run("substring fallback stays ambiguous across roles", func(t *testing.T) {
		_, err := resolveTarget(snapshot, TargetQuery{Name: "sign"})
		require.Error(t, err, "both e2 and e3 contain \"sign\"")
		require.Contains(t, err.Error(), "match name \"sign\"")
	})

	t.Run("no match is a readable not-found error", func(t *testing.T) {
		_, err := resolveTarget(snapshot, TargetQuery{Name: "Buy now", Role: "button"})
		require.ErrorContains(t, err, `no element named "Buy now" (role button)`)
	})
}

func TestDiffElements(t *testing.T) {
	pre := []SnapshotElement{
		{Ref: "e1", Role: "button", Name: "Save"},
		{Ref: "e2", Role: "textbox", Name: "Name", Value: ""},
	}
	post := []SnapshotElement{
		{Ref: "e1", Role: "button", Name: "Saving…"},           // name changed
		{Ref: "e2", Role: "textbox", Name: "Name", Value: "x"}, // value changed
		{Ref: "e3", Role: "link", Name: "Next"},                // new element
		{Ref: "e4", Role: "button", Name: "Cancel"},            // new element
	}

	changed := diffElements(pre, post)
	require.Len(t, changed, 4, "changed, valued-changed, and new elements are the delta")
	refs := map[string]bool{}
	for _, el := range changed {
		refs[el.Ref] = true
	}
	require.True(t, refs["e1"])
	require.True(t, refs["e2"])
	require.True(t, refs["e3"])

	t.Run("unchanged page yields an empty delta", func(t *testing.T) {
		require.Empty(t, diffElements(pre, pre))
	})

	t.Run("slim result JSON carries ok, url, title, and changed only", func(t *testing.T) {
		out, err := json.Marshal(SlimActionResult{OK: true, URL: "http://x/", Title: "T", Changed: []SnapshotElement{}})
		require.NoError(t, err)
		var decoded map[string]any
		require.NoError(t, json.Unmarshal(out, &decoded))
		require.Equal(t, true, decoded["ok"])
		require.Equal(t, "http://x/", decoded["url"])
		require.Equal(t, "T", decoded["title"])
		require.Contains(t, decoded, "changed")
		for _, forbidden := range []string{"elements", "text"} {
			require.NotContains(t, decoded, forbidden, "slim results must not carry full page lists")
		}
	})
}
