package browseruse

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBoundReadLength(t *testing.T) {
	require.Equal(t, DefaultReadChars, boundReadLength(0), "absent means the default")
	require.Equal(t, DefaultReadChars, boundReadLength(-10), "negative means the default")
	require.Equal(t, 500, boundReadLength(500))
	require.Equal(t, MaxReadChars, boundReadLength(MaxReadChars+1), "larger requests clamp to the cap")
	require.Equal(t, DefaultReadChars, boundReadLength(DefaultReadChars))
}

func TestBoundReadText(t *testing.T) {
	t.Run("short text returns unchanged without the flag", func(t *testing.T) {
		got, truncated := boundReadText("hello", 10)
		require.Equal(t, "hello", got)
		require.False(t, truncated)
	})

	t.Run("long text truncates mid-page with an indication", func(t *testing.T) {
		text := "abcdefgh"
		got, truncated := boundReadText(text, 4)
		require.True(t, truncated)
		require.Equal(t, "abcd\n…[truncated]", got)
		require.Less(t, len([]rune(got)), len([]rune(text))+20, "the truncation indication stays small")
	})

	t.Run("exact-length text is not truncated", func(t *testing.T) {
		got, truncated := boundReadText("abcd", 4)
		require.Equal(t, "abcd", got)
		require.False(t, truncated)
	})

	t.Run("read result carries the truncation fields", func(t *testing.T) {
		out, err := json.Marshal(ReadResult{OK: true, Text: "abc\n…[truncated]", Truncated: true, Length: 16})
		require.NoError(t, err)
		require.Contains(t, string(out), `"truncated":true`)
		require.Contains(t, string(out), `"length":16`)
	})
}

func TestBrowserKey(t *testing.T) {
	for _, name := range []string{"Enter", "Tab", "Escape", "Backspace", "Delete", "Space",
		"ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight", "Home", "End", "PageUp", "PageDown", "a", "1"} {
		_, ok := browserKey(name)
		require.True(t, ok, name)
	}
	for _, name := range []string{"", "ChannelUp", "F12", "ab", "é"} {
		_, ok := browserKey(name)
		require.False(t, ok, name)
	}
}
