package markdown

import (
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/require"
)

func chunkUnits(s string) int {
	return len(utf16.Encode([]rune(s)))
}

func TestChunk(t *testing.T) {
	t.Run("short input returns single part without marker", func(t *testing.T) {
		require.Equal(t, []string{"hello"}, Chunk("hello", 100))
	})

	t.Run("plain text splits at newline", func(t *testing.T) {
		line1 := strings.Repeat("a", 80)
		rest := strings.Repeat("b", 120)
		got := Chunk(line1+"\n"+rest, 100)
		require.Len(t, got, 3)
		require.Equal(t, line1+" \\(1/3\\)", got[0])
		require.Equal(t, strings.Repeat("b", 86)+" \\(2/3\\)", got[1])
		require.Equal(t, strings.Repeat("b", 34)+" \\(3/3\\)", got[2])
		for _, part := range got {
			require.LessOrEqual(t, chunkUnits(part), 100)
		}
	})

	t.Run("CJK text counts one unit per rune", func(t *testing.T) {
		got := Chunk(strings.Repeat("中", 300), 100)
		require.Len(t, got, 4)
		wantBodies := []string{
			strings.Repeat("中", 86) + " \\(1/4\\)",
			strings.Repeat("中", 86) + " \\(2/4\\)",
			strings.Repeat("中", 86) + " \\(3/4\\)",
			strings.Repeat("中", 42) + " \\(4/4\\)",
		}
		for i, part := range got {
			require.Equal(t, wantBodies[i], part)
			require.LessOrEqual(t, chunkUnits(part), 100)
		}
	})

	t.Run("astral emoji counts two units and never splits a rune", func(t *testing.T) {
		got := Chunk(strings.Repeat("😀", 15), 20)
		require.Len(t, got, 5)
		for i, part := range got {
			// Every rune intact: no replacement chars from a split surrogate pair.
			require.NotContains(t, part, "\uFFFD")
			require.LessOrEqual(t, chunkUnits(part), 20)
			if i < 4 {
				require.Equal(t, strings.Repeat("😀", 3)+" \\("+string(rune('1'+i))+"/5\\)", part)
			} else {
				require.Equal(t, strings.Repeat("😀", 3)+" \\(5/5\\)", part)
			}
		}
	})

	t.Run("fenced block spans chunks with reopened fences", func(t *testing.T) {
		a := strings.Repeat("a", 30)
		b := strings.Repeat("b", 30)
		c := strings.Repeat("c", 30)
		d := strings.Repeat("d", 30)
		md := "```go\n" + a + "\n" + b + "\n" + c + "\n" + d + "\n```"
		got := Chunk(md, 60)
		require.Len(t, got, 4)
		require.Equal(t, "```go\n"+a+"\n```\n\\(1/4\\)", got[0])
		require.Equal(t, "```go\n"+b+"\n```\n\\(2/4\\)", got[1])
		require.Equal(t, "```go\n"+c+"\n```\n\\(3/4\\)", got[2])
		require.Equal(t, "```go\n"+d+"\n```\n\\(4/4\\)", got[3])
		for _, part := range got {
			require.LessOrEqual(t, chunkUnits(part), 60)
		}
	})

	t.Run("inline code span is never split", func(t *testing.T) {
		md := strings.Repeat("a", 30) + " `short` " + strings.Repeat("b", 30)
		got := Chunk(md, 40)
		require.Len(t, got, 3)
		require.Equal(t, strings.Repeat("a", 26)+" \\(1/3\\)", got[0])
		require.Equal(t, "aaaa `short` \\(2/3\\)", got[1])
		require.Equal(t, strings.Repeat("b", 30)+" \\(3/3\\)", got[2])
		for _, part := range got {
			require.LessOrEqual(t, chunkUnits(part), 40)
			// An unpaired backtick would break MarkdownV2 parsing.
			require.Zero(t, strings.Count(part, "`")%2, "odd backtick count in %q", part)
		}
	})

	t.Run("hard cut terminates on tiny limit", func(t *testing.T) {
		got := Chunk("abc", 1)
		require.Len(t, got, 3)
		require.True(t, strings.HasPrefix(got[0], "a"))
		require.True(t, strings.HasPrefix(got[1], "b"))
		require.True(t, strings.HasPrefix(got[2], "c"))
	})
}
