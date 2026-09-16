package gateways

import (
	"strings"
	"testing"
)

// TestRenderWhatsAppMarkdownMappings covers the GFM → WhatsApp-subset
// degradations (add-whatsapp-gateway design D9).
func TestRenderWhatsAppMarkdownMappings(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bold", "**bold**", "*bold*"},
		{"underscore bold", "__bold__", "*bold*"},
		{"asterisk italic", "*it*", "_it_"},
		{"underscore italic", "_it_", "_it_"},
		{"intraword underscore literal", "foo_bar_baz", "foo_bar_baz"},
		{"strikethrough", "~~gone~~", "~gone~"},
		{"lone tilde literal", "approx ~5", "approx ~5"},
		{"inline code", "`x < y`", "`x < y`"},
		{"heading degrades to bold", "## Title", "*Title*"},
		{"top-level heading", "# Big", "*Big*"},
		{"list item", "- one", "• one"},
		{"ordered item", "1. one", "• one"},
		{"link stays inline", "[docs](https://example.com)", "docs (https://example.com)"},
		{"bare-url link collapses", "[https://example.com](https://example.com)", "https://example.com"},
		{"no html escaping", "<b>&</b>", "<b>&</b>"},
		{"mixed inline", "a **b** and _c_ and ~~d~~ and `e`", "a *b* and _c_ and ~d~ and `e`"},
		{
			"fence preserved without language tag",
			"```go\ncode < 1\n```",
			"```\ncode < 1\n```",
		},
		{
			"unclosed fence preserved mid-stream",
			"```go\nfoo := 1",
			"```\nfoo := 1",
		},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RenderWhatsAppMarkdown(tc.in); got != tc.want {
				t.Fatalf("RenderWhatsAppMarkdown(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestWhatsAppFlavorSplitKeepsFences checks the split machinery under the
// WhatsApp budget: a fenced block longer than one message is cut with the
// fence closed and reopened, and every part stays within budget (design D9
// shares SplitMarkdown with the Telegram flavor).
func TestWhatsAppFlavorSplitKeepsFences(t *testing.T) {
	fence := "```go\n" + strings.Repeat("x", 5000) + "\n```"
	parts := SplitForFlavor(fence, WhatsAppFlavor)
	if len(parts) < 2 {
		t.Fatalf("expected the fence to split, got %d parts", len(parts))
	}
	for i, p := range parts {
		if runeLen(p) > WhatsAppFlavor.Budget {
			t.Fatalf("part %d is %d runes, over the %d budget", i, runeLen(p), WhatsAppFlavor.Budget)
		}
		// WhatsApp preserves fences by delimiter count: each part must carry
		// an even number of ``` lines (closed block).
		if n := strings.Count(p, "```"); n%2 != 0 {
			t.Fatalf("part %d carries an unbalanced fence (%d delimiters): %q", i, n, p[:40])
		}
	}
	// Content survives: the tail is present in the last part.
	if !strings.Contains(parts[len(parts)-1], "x") {
		t.Fatalf("split lost content: %q", parts[len(parts)-1][:40])
	}
}

func TestWhatsAppFlavorIdentityPlainFallback(t *testing.T) {
	body := "*bold* and `code`"
	if got := WhatsAppFlavor.PlainFallback(body); got != body {
		t.Fatalf("WhatsApp plain fallback must be the identity, got %q", got)
	}
}

func TestPlatformRenderFlavorSelection(t *testing.T) {
	if got := platformRenderFlavor(PlatformWhatsApp); got.Name != FlavorWhatsAppMD {
		t.Fatalf("whatsapp platform must select the whatsapp flavor, got %q", got.Name)
	}
	if got := platformRenderFlavor(PlatformTelegram); got.Name != FlavorTelegramHTML {
		t.Fatalf("telegram platform must select the telegram flavor, got %q", got.Name)
	}
	if got := platformRenderFlavor("unknown"); got.Name != FlavorTelegramHTML {
		t.Fatalf("unknown platforms default to telegram, got %q", got.Name)
	}
	if TelegramFlavor.Budget != telegramChunkBudget {
		t.Fatalf("telegram flavor budget must stay byte-identical: %d", TelegramFlavor.Budget)
	}
	// Golden parity: the Telegram specialization is SplitForFlavor with the
	// Telegram flavor (the byte-level golden tests live in render_test.go
	// against SplitForTelegram itself).
	if got, want := len(SplitForTelegram("hello")), len(SplitForFlavor("hello", TelegramFlavor)); got != want {
		t.Fatalf("SplitForTelegram diverged from SplitForFlavor: %d != %d", got, want)
	}
}
