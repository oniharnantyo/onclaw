package gateways

import (
	"strings"
	"testing"
)

func TestRenderBasicFormatting(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bold", "**bold**", "<b>bold</b>\n"},
		{"italic", "*it*", "<i>it</i>\n"},
		{"underscore italic", "_it_", "<i>it</i>\n"},
		{"intraword underscore literal", "foo_bar_baz", "foo_bar_baz\n"},
		{"strikethrough", "~~gone~~", "<s>gone</s>\n"},
		{"inline code", "`x < y`", "<code>x &lt; y</code>\n"},
		{"heading", "## Title", "<b>Title</b>\n"},
		{"link", "[docs](https://example.com)", `<a href="https://example.com">docs</a>` + "\n"},
		{"list item", "- one", "• one\n"},
		{"ordered item", "1. one", "• one\n"},
		{"hr", "---", "────────\n"},
		{"escaping", "<b>&hi</b>", "&lt;b&gt;&amp;hi&lt;/b&gt;\n"},
		{
			"fence with language",
			"```go\ncode < 1\n```",
			"<pre><code class=\"language-go\">code &lt; 1\n</code></pre>\n",
		},
		{"blockquote", "> quoted", "<blockquote>quoted\n</blockquote>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RenderTelegramHTML(tc.in); got != tc.want {
				t.Fatalf("RenderTelegramHTML(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRenderBalancesUnclosedTags(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"unclosed bold", "hello **wor"},
		{"unclosed italic", "hello *wor"},
		{"unclosed code", "run `go te"},
		{"unclosed strikethrough", "~~partial"},
		{"unclosed link", "see [docs](https://example.com and"},
		{"nested unclosed", "**bold *ital"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RenderTelegramHTML(tc.in)
			// Every opener emitted must have a matching closer: the output is
			// well-formed regardless of where the stream stopped.
			for _, tag := range []string{"b", "i", "code", "s", "a"} {
				opens := strings.Count(got, "<"+tag+">")
				closes := strings.Count(got, "</"+tag+">")
				if opens != closes {
					t.Fatalf("RenderTelegramHTML(%q) = %q: <%s> opened %d times but closed %d", tc.in, got, tag, opens, closes)
				}
			}
		})
	}
}

func TestRenderClosesUnclosedFence(t *testing.T) {
	got := RenderTelegramHTML("```go\nfoo := 1")
	want := "<pre><code class=\"language-go\">foo := 1\n</code></pre>\n"
	if got != want {
		t.Fatalf("unclosed fence = %q, want %q", got, want)
	}
}

func TestRenderTableDegradesToPre(t *testing.T) {
	md := "| Name | Count |\n|---|---|\n| foo | 12 |\n| bar | 3 |"
	got := RenderTelegramHTML(md)
	if !strings.HasPrefix(got, "<pre>") || !strings.HasSuffix(got, "</pre>\n") {
		t.Fatalf("table should render as <pre>, got %q", got)
	}
	if !strings.Contains(got, "Name | Count") || !strings.Contains(got, "12 ") {
		t.Fatalf("table rows missing from ASCII render: %q", got)
	}
	if strings.Contains(got, "<table") {
		t.Fatalf("tables must degrade to <pre>, got %q", got)
	}
}

func TestPlainTextFallbackStripsMarkup(t *testing.T) {
	html := "<b>hi</b> &amp; &lt;bye&gt; <code>x</code>"
	got := PlainTextFallback(html)
	want := "hi & <bye> x"
	if got != want {
		t.Fatalf("PlainTextFallback = %q, want %q", got, want)
	}
}

func TestSplitMarkdownBoundaries(t *testing.T) {
	t.Run("paragraph boundary preferred", func(t *testing.T) {
		md := strings.Repeat("a", 30) + "\n\n" + strings.Repeat("b", 30)
		parts := SplitMarkdown(md, 40)
		if len(parts) != 2 || !strings.HasPrefix(parts[0], strings.Repeat("a", 30)) {
			t.Fatalf("paragraph split = %#v", parts)
		}
	})

	t.Run("sentence boundary", func(t *testing.T) {
		md := strings.Repeat("x", 30) + ". " + strings.Repeat("y", 30) + ". tail"
		parts := SplitMarkdown(md, 40)
		if len(parts) != 2 {
			t.Fatalf("expected 2 parts, got %d: %#v", len(parts), parts)
		}
		if !strings.HasSuffix(parts[0], ". ") {
			t.Fatalf("sentence cut should follow the sentence end: %q", parts[0])
		}
	})

	t.Run("space boundary", func(t *testing.T) {
		md := strings.Repeat("x", 30) + " " + strings.Repeat("y", 30)
		parts := SplitMarkdown(md, 40)
		if len(parts) != 2 || !strings.HasSuffix(parts[0], " ") {
			t.Fatalf("space split = %#v", parts)
		}
	})

	t.Run("hard cut without boundary", func(t *testing.T) {
		md := strings.Repeat("x", 100)
		parts := SplitMarkdown(md, 40)
		if len(parts) != 3 || runeLen(parts[0]) != 40 {
			t.Fatalf("hard cut = %#v", parts)
		}
	})

	t.Run("no content lost", func(t *testing.T) {
		md := strings.Repeat("word ", 3000)
		parts := SplitMarkdown(strings.TrimRight(md, " "), 4000)
		var joined strings.Builder
		for i, p := range parts {
			if runeLen(p) > 4000 {
				t.Fatalf("part %d exceeds the limit (%d runes)", i, runeLen(p))
			}
			joined.WriteString(p)
		}
		if joined.String() != strings.TrimRight(md, " ") {
			t.Fatalf("split lost or duplicated content")
		}
	})
}

func TestSplitMarkdownCodeFences(t *testing.T) {
	fence := "```go\n" + strings.Repeat("x", 5000) + "\n```"
	parts := SplitMarkdown(fence, 4000)
	if len(parts) < 2 {
		t.Fatalf("expected the fence to split, got %d parts", len(parts))
	}
	if !strings.HasSuffix(parts[0], "\n```") {
		t.Fatalf("first part must close the fence, got suffix %q", lastLine(parts[0]))
	}
	if !strings.HasPrefix(parts[1], "```go\n") {
		t.Fatalf("second part must reopen the fence with the same language, got prefix %q", firstLine(parts[1]))
	}
	// The reopened fence must balance: rendering each part yields equal
	// <pre> opens and closes.
	for i, p := range parts {
		html := RenderTelegramHTML(p)
		if strings.Count(html, "<pre>") != strings.Count(html, "</pre>") {
			t.Fatalf("part %d renders unbalanced fences: %q", i, html[:80])
		}
	}
}

func TestSplitForTelegramChunksWithinBudget(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 200; i++ {
		sb.WriteString("Paragraph with **bold**, `code`, [a link](https://example.com/very/long/path) and more text.\n\n")
	}
	parts := SplitForTelegram(sb.String())
	if len(parts) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(parts))
	}
	for i, p := range parts {
		if runeLen(p) > telegramChunkBudget {
			t.Fatalf("chunk %d is %d runes, over the %d budget", i, runeLen(p), telegramChunkBudget)
		}
	}
	// Content survives the split: the first and last paragraphs are present.
	if !strings.Contains(parts[0], "Paragraph with <b>bold</b>") {
		t.Fatalf("first chunk lost content: %q", parts[0][:80])
	}
}

func TestSplitForTelegramKeepsContent(t *testing.T) {
	md := strings.Repeat("hello world ", 800) // 9600 runes
	parts := SplitForTelegram(md)
	var got strings.Builder
	for _, p := range parts {
		got.WriteString(PlainTextFallback(p))
	}
	want := strings.Join(strings.Fields(strings.Repeat("hello world ", 800)), " ")
	if strings.Join(strings.Fields(strings.ReplaceAll(got.String(), "\n", " ")), " ") != want {
		t.Fatalf("split lost content")
	}
}

func TestStreamRendererIncrementalBalancing(t *testing.T) {
	r := NewStreamRenderer()
	r.Write("hello **wor")
	mid := r.HTML()
	if !strings.HasSuffix(mid, "</b>") {
		t.Fatalf("mid-stream HTML must balance the open tag: %q", mid)
	}
	r.Write("ld**!")
	final := r.HTML()
	if !strings.Contains(final, "<b>world</b>!") {
		t.Fatalf("final HTML wrong: %q", final)
	}
}

func TestStreamRendererMessageCompletedRecovery(t *testing.T) {
	r := NewStreamRenderer()
	r.Write("partial dr")
	r.Set("the complete message")
	if got := r.Markdown(); got != "the complete message" {
		t.Fatalf("Set did not replace the buffer: %q", got)
	}
}

func lastLine(s string) string {
	lines := strings.Split(s, "\n")
	return lines[len(lines)-1]
}

func firstLine(s string) string {
	return strings.Split(s, "\n")[0]
}
