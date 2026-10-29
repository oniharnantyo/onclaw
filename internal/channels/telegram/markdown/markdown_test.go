package markdown

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestToMarkdownV2(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "plain text untouched", in: "hello world", want: "hello world"},
		{name: "bold", in: "**strong**", want: "*strong*"},
		{name: "italic star", in: "*soft*", want: "_soft_"},
		{name: "italic underscore", in: "this is _soft_ ok", want: "this is _soft_ ok"},
		{name: "intraword underscore stays literal", in: "snake_case_name", want: "snake\\_case\\_name"},
		{name: "strikethrough", in: "~~gone~~", want: "~gone~"},
		{name: "spoiler kept", in: "||secret||", want: "||secret||"},
		{
			name: "inline code preserved",
			in:   "run `go test ./...` now",
			want: "run `go test ./...` now",
		},
		{
			name: "inline code escapes backslash",
			in:   "`a\\b`",
			want: "`a\\\\b`",
		},
		{name: "heading to bold line", in: "## Hello World", want: "*Hello World*"},
		{
			name: "heading strips bold",
			in:   "## Title with **bold**",
			want: "*Title with bold*",
		},
		{name: "blockquote escapes content", in: "> quoted a_b", want: "> quoted a\\_b"},
		{
			name: "link escapes text keeps shape",
			in:   "[my_link](https://x.com/a_b)",
			want: "[my\\_link](https://x.com/a_b)",
		},
		{
			name: "link with nested paren in url",
			in:   "[docs](https://x.io/a(b))",
			want: "[docs](https://x.io/a\\(b\\))",
		},
		{
			name: "reserved chars escaped",
			in:   "a.b c-d e_f g|h i!j#k x = y + z",
			want: "a\\.b c\\-d e\\_f g\\|h i\\!j\\#k x \\= y \\+ z",
		},
		{
			name: "fenced block verbatim when no code-span chars",
			in:   "```\nsome_code(x): a_b [c] #tag\n```",
			want: "```\nsome_code(x): a_b [c] #tag\n```",
		},
		{
			name: "fenced block escapes backslash and backtick only",
			in:   "```go\npath\\to `q` end\n```",
			want: "```go\npath\\\\to \\`q\\` end\n```",
		},
		{
			name: "fenced block keeps info string",
			in:   "```python\nx = 1\n```",
			want: "```python\nx = 1\n```",
		},
		{
			name: "table flattened to bullet groups",
			in:   "| Name | Value |\n| ---- | ----- |\n| a_b  | 1     |\n| c_d  | 2     |",
			want: "*a\\_b*\n• Value: 1\n\n*c\\_d*\n• Value: 2",
		},
		{
			name: "table left alone inside fence",
			in:   "```\n| a | b |\n| - | - |\n```",
			want: "```\n| a | b |\n| - | - |\n```",
		},
		{
			name: "safety net escapes stray apostrophe",
			in:   "it's {odd}",
			want: "it\\'s \\{odd\\}",
		},
		{
			name: "safety net escapes braces inside link url",
			in:   "[t](http://x/{a})",
			want: "[t](http://x/\\{a\\})",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ToMarkdownV2(tt.in))
		})
	}
}

func TestPlainText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "escapes removed", in: "a\\.b c\\-d e\\_f", want: "a.b c-d e_f"},
		{
			name: "formatting markers stripped",
			in:   "*b* _i_ ~s~ ||p||",
			want: "b i s p",
		},
		{name: "snake case survives", in: "snake_case_name", want: "snake_case_name"},
		{name: "inline code backticks dropped", in: "run `go test`", want: "run go test"},
		{
			name: "fence lines dropped, content kept",
			in:   "```go\nx := 1\n```",
			want: "x := 1\n",
		},
		{name: "link becomes text (url)", in: "[docs](https://x.io/a)", want: "docs (https://x.io/a)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, PlainText(tt.in))
		})
	}
}

// TestPlainTextMixedDocument runs a mixed doc through conversion then the
// plain fallback, the exact path a Telegram parse rejection takes.
func TestPlainTextMixedDocument(t *testing.T) {
	src := "# Release\n\nHello **world**, see `docs link`.\n\n> a quote\n\n" +
		"- item one\n- item two\n\n" +
		"| Name | Value |\n| ---- | ----- |\n| a_b  | 1     |\n\n" +
		"[site](https://x.io)"
	md := ToMarkdownV2(src)
	got := PlainText(md)
	want := "Release\n\nHello world, see docs link.\n\n> a quote\n\n" +
		"- item one\n- item two\n\n" +
		"a_b\n• Value: 1\n\n" +
		"site (https://x.io)"
	require.Equal(t, want, got)
}
