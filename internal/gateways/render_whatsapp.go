// WhatsApp render flavor (add-whatsapp-gateway design D9): GFM markdown
// degraded to the WhatsApp markdown subset — *bold*, _italic_, ~strike~,
// `mono`, fenced code blocks — with lists as • lines and headings as bold
// lines. Links stay inline; there is no preview toggle, so SendOptions'
// DisablePreview is ignored by the adapter. Unlike the Telegram flavor,
// no escaping layer exists: WhatsApp renders unknown markup literally, so
// the renderer transforms markers and nothing else.

package gateways

import (
	"strings"
)

// RenderWhatsAppMarkdown converts GFM markdown into the WhatsApp markdown
// subset (design D9): **bold** → *bold*, *italic*/_italic_ → _italic_,
// ~~strike~~ → ~strike~, `code` preserved, fenced blocks preserved without
// the language tag, list items as "• " lines, headings as bold lines, links
// as "text (url)". No HTML escaping — the WhatsApp subset has no entity
// syntax to inject into.
func RenderWhatsAppMarkdown(markdown string) string {
	var b strings.Builder
	lines := strings.Split(markdown, "\n")

	inFence := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Fenced blocks are preserved verbatim; WhatsApp has no language
		// tags, so only the ``` delimiter lines survive.
		if _, isFence := fenceDelimiter(line); isFence {
			inFence = !inFence
			b.WriteString("```\n")
			continue
		}
		if inFence {
			b.WriteString(line)
			b.WriteString("\n")
			continue
		}

		switch {
		case trimmed == "":
			b.WriteString("\n")
		case isHeading(trimmed):
			b.WriteString("*")
			renderWhatsAppInline(&b, strings.TrimSpace(strings.TrimLeft(trimmed, "#")))
			b.WriteString("*\n")
		case isListItem(trimmed):
			if item, ok := listItemBody(trimmed); ok {
				b.WriteString("• ")
				renderWhatsAppInline(&b, item)
				b.WriteString("\n")
			} else {
				b.WriteString(renderWhatsAppInlineStr(line))
				b.WriteString("\n")
			}
		default:
			b.WriteString(renderWhatsAppInlineStr(line))
			b.WriteString("\n")
		}
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// renderWhatsAppInlineStr is the string-returning inline transform.
func renderWhatsAppInlineStr(src string) string {
	var b strings.Builder
	renderWhatsAppInline(&b, src)
	return b.String()
}

// renderWhatsAppInline converts one stretch of markdown text (no block
// constructs) to the WhatsApp subset: emphasis markers are re-emitted in
// WhatsApp's vocabulary, code spans are passed through verbatim, and links
// degrade to inline text. Unpaired markers survive literally.
func renderWhatsAppInline(b *strings.Builder, src string) {
	i := 0
	n := len(src)
	for i < n {
		c := src[i]
		switch c {
		case '\\':
			if i+1 < n {
				b.WriteByte(src[i+1])
				i += 2
				continue
			}
			b.WriteByte('\\')
			i++

		case '`':
			if j := strings.IndexByte(src[i+1:], '`'); j >= 0 {
				b.WriteByte('`')
				b.WriteString(src[i+1 : i+1+j])
				b.WriteByte('`')
				i += j + 2
				continue
			}
			// Unclosed code span: the rest passes through verbatim.
			b.WriteByte('`')
			b.WriteString(src[i+1:])
			i = n

		case '*':
			if i+1 < n && src[i+1] == '*' {
				b.WriteByte('*') // **bold** → *bold* (open and close share the marker)
				i += 2
				continue
			}
			b.WriteByte('_') // *italic* → _italic_
			i++

		case '_':
			if i+1 < n && src[i+1] == '_' {
				b.WriteByte('*') // __bold__ → *bold*
				i += 2
				continue
			}
			b.WriteByte('_') // _italic_ is already WhatsApp-native
			i++

		case '~':
			if i+1 < n && src[i+1] == '~' {
				b.WriteByte('~') // ~~strike~~ → ~strike~
				i += 2
				continue
			}
			b.WriteByte('~')
			i++

		case '[':
			text, url, ok := parseLink(src, i)
			if !ok {
				b.WriteByte('[')
				i++
				continue
			}
			// Links stay inline (design D9): text (url), or the bare URL
			// when the text carries no extra information.
			if text == url {
				b.WriteString(url)
			} else {
				renderWhatsAppInline(b, text)
				b.WriteString(" (")
				b.WriteString(url)
				b.WriteString(")")
			}
			i = linkEnd(src, i)

		default:
			j := i
			for j < n && !strings.ContainsRune("\\`*_~[", rune(src[j])) {
				j++
			}
			if j == i {
				j++
			}
			b.WriteString(src[i:j])
			i = j
		}
	}
}
