package markdown

import (
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// indicatorReserve leaves room for the escaped "(i/n)" multi-part marker
// (e.g. " \\(XX/XX\\)") so a marker never pushes a part over the limit.
const indicatorReserve = 10

// fenceClose is appended when a part ends inside an open fenced block so the
// part stands alone; the next part re-opens the fence with the same language
// tag.
const fenceClose = "\n```"

// utf16Len returns the length of s in UTF-16 code units — the unit Telegram's
// 4096 message limit counts, so astral characters (emoji, CJK Extension B)
// cost two.
func utf16Len(s string) int {
	return len(utf16.Encode([]rune(s)))
}

// floorUTF16 returns the largest rune count n such that the first n runes of
// s measure at most budget UTF-16 code units.
func floorUTF16(s string, budget int) int {
	rs := []rune(s)
	if utf16Len(string(rs)) <= budget {
		return len(rs)
	}
	lo, hi := 0, len(rs)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if utf16Len(string(rs[:mid])) <= budget {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}

// fenceStateAfter walks text line by line toggling state on ``` lines and
// returns the final (open, language tag) pair.
func fenceStateAfter(text string, inCode bool, lang string) (bool, string) {
	for _, line := range strings.Split(text, "\n") {
		stripped := strings.TrimSpace(line)
		if !strings.HasPrefix(stripped, "```") {
			continue
		}
		if inCode {
			inCode, lang = false, ""
			continue
		}
		inCode = true
		if fields := strings.Fields(stripped[3:]); len(fields) > 0 {
			lang = fields[0]
		} else {
			lang = ""
		}
	}
	return inCode, lang
}

// Chunk splits converted MarkdownV2 text into parts each within limit UTF-16
// code units. Breaks prefer newlines, then spaces, then a hard cut; inline
// code spans are never split, and a fenced block open at a cut point is
// closed at the end of the part and re-opened (with its language tag) at the
// start of the next. When more than one part results, each gets an escaped
// "(i/n)" marker — kept off a synthesized closing fence line, which Telegram
// would reject. Parts may exceed limit only for degenerately small limits.
func Chunk(md string, limit int) []string {
	if utf16Len(md) <= limit {
		return []string{md}
	}
	var chunks []string
	remaining := md
	carrying := false // a fenced block is open across parts
	carryLang := ""   // its language tag ("" allowed)
	for remaining != "" {
		prefix := ""
		if carrying {
			prefix = "```" + carryLang + "\n"
		}
		// Body budget after prefix, fence close and marker reserve; floored
		// so a tiny limit cannot stall the loop.
		headroom := limit - indicatorReserve - utf16Len(prefix) - utf16Len(fenceClose)
		if headroom < 1 {
			headroom = max(1, limit/2)
		}
		// Remainder fits in one final part; close a reopened fence if open.
		if utf16Len(prefix)+utf16Len(remaining) <= limit-indicatorReserve {
			final := prefix + remaining
			if carrying {
				if open, _ := fenceStateAfter(remaining, true, carryLang); open {
					final += fenceClose
				}
			}
			chunks = append(chunks, final)
			break
		}
		// Natural split: newline preferred, then space, else hard cut.
		rs := []rune(remaining)
		cpLimit := floorUTF16(remaining, headroom)
		region := remaining[:len(string(rs[:cpLimit]))]
		splitAt := strings.LastIndexByte(region, '\n')
		if splitAt < 0 || utf8.RuneCountInString(remaining[:splitAt]) < cpLimit/2 {
			splitAt = strings.LastIndexByte(region, ' ')
		}
		if splitAt < 0 || utf8.RuneCountInString(remaining[:splitAt]) < 1 {
			splitAt = len(string(rs[:max(cpLimit, 1)]))
		}
		// Do not split inside an inline code span: an unpaired backtick
		// would break MarkdownV2 parsing.
		candidate := remaining[:splitAt]
		if (strings.Count(candidate, "`")-strings.Count(candidate, "\\`"))%2 == 1 {
			lastBt := strings.LastIndexByte(candidate, '`')
			for lastBt > 0 && candidate[lastBt-1] == '\\' {
				prev := strings.LastIndexByte(candidate[:lastBt], '`')
				if prev < 0 {
					lastBt = -1
					break
				}
				lastBt = prev
			}
			if lastBt > 0 {
				before := candidate[:lastBt]
				safe := strings.LastIndexByte(before, ' ')
				if nl := strings.LastIndexByte(before, '\n'); nl > safe {
					safe = nl
				}
				if safe > 0 && utf8.RuneCountInString(remaining[:safe]) > cpLimit/4 {
					splitAt = safe
				}
			}
		}
		chunkBody := remaining[:splitAt]
		remaining = strings.TrimLeft(remaining[splitAt:], " \t\n\v\f\r")
		full := prefix + chunkBody
		// Walk only chunkBody (not the prepended prefix) for the fence state;
		// close the orphaned fence so the part stands alone.
		inCode, lang := fenceStateAfter(chunkBody, carrying, carryLang)
		carrying, carryLang = inCode, lang
		if inCode {
			full += fenceClose
		}
		chunks = append(chunks, full)
	}
	if len(chunks) > 1 {
		total := strconv.Itoa(len(chunks))
		for i := range chunks {
			sep := " "
			if strings.HasSuffix(chunks[i], "```") {
				sep = "\n" // keep the marker off the closing fence line
			}
			chunks[i] += sep + "\\(" + strconv.Itoa(i+1) + "/" + total + "\\)"
		}
	}
	return chunks
}
