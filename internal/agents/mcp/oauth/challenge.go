package oauth

import "strings"

// Challenge is one parsed WWW-Authenticate challenge (RFC 9110 §11). Scheme
// keeps the header's spelling; Params keys are lowercased (auth-param names
// are case-insensitive) and values are unquoted with quoted-pair escapes
// undone.
type Challenge struct {
	Scheme string
	Params map[string]string
}

// ResourceMetadataParam is the WWW-Authenticate challenge parameter that
// carries the protected-resource metadata URI (RFC 9728 §5).
const ResourceMetadataParam = "resource_metadata"

// ParseWWWAuthenticate parses the given header values — one slice element per
// WWW-Authenticate header line; a single line may carry several challenges
// (`Basic realm="x", Bearer resource_metadata="..."`). Malformed pieces are
// skipped, never fatal: a challenge line is attacker- and provider-controlled
// input and the chain falls back to well-known probing when it yields nothing.
func ParseWWWAuthenticate(values []string) []Challenge {
	var challenges []Challenge
	for _, value := range values {
		challenges = append(challenges, parseChallengeValue(value)...)
	}
	return challenges
}

// ResourceMetadataURI walks the challenges in order and returns the
// resource_metadata URI of the first Bearer challenge carrying one
// (RFC 9728 §5). Empty when no challenge advertises it.
func ResourceMetadataURI(challenges []Challenge) string {
	for _, ch := range challenges {
		if !strings.EqualFold(ch.Scheme, "bearer") {
			continue
		}
		if uri := strings.TrimSpace(ch.Params[ResourceMetadataParam]); uri != "" {
			return uri
		}
	}
	return ""
}

// parseChallengeValue parses one header line into its challenges. The line is
// split on commas outside quoted strings; an element is either a parameter of
// the current challenge (`name=value`), a bare scheme token, or a new
// `<scheme> <params…>` challenge.
func parseChallengeValue(value string) []Challenge {
	var out []Challenge
	var cur *Challenge
	closeCur := func() {
		if cur != nil {
			out = append(out, *cur)
			cur = nil
		}
	}
	for _, element := range splitQuoted(value, ',') {
		element = strings.TrimSpace(element)
		if element == "" {
			continue
		}
		if head, rest, hasSpace := strings.Cut(element, " "); hasSpace && !strings.Contains(head, "=") {
			// "<scheme> <params>" — a new challenge header element.
			closeCur()
			cur = &Challenge{Scheme: head, Params: map[string]string{}}
			appendParams(cur, rest)
			continue
		}
		if !strings.Contains(element, "=") {
			// A bare token: the scheme of a challenge without parameters.
			closeCur()
			cur = &Challenge{Scheme: element, Params: map[string]string{}}
			continue
		}
		if cur == nil {
			// A parameter with no challenge before it — malformed; skip.
			continue
		}
		appendParam(cur, element)
	}
	closeCur()
	return out
}

// appendParams parses the parameter tail of a `<scheme> <params>` element.
func appendParams(ch *Challenge, tail string) {
	for _, element := range splitQuoted(tail, ',') {
		element = strings.TrimSpace(element)
		if element == "" {
			continue
		}
		appendParam(ch, element)
	}
}

// appendParam parses one `name=value` element into the challenge. Names are
// lowercased; values drop surrounding quotes and undo quoted-pair escapes.
func appendParam(ch *Challenge, element string) {
	name, value, ok := strings.Cut(element, "=")
	if !ok {
		return
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return
	}
	ch.Params[name] = unquoteAuthValue(strings.TrimSpace(value))
}

// unquoteAuthValue strips surrounding double quotes and undoes RFC 9110
// quoted-pair escapes (\X → X).
func unquoteAuthValue(value string) string {
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		return value
	}
	inner := value[1 : len(value)-1]
	var out strings.Builder
	out.Grow(len(inner))
	for i := 0; i < len(inner); i++ {
		if inner[i] == '\\' && i+1 < len(inner) {
			i++
		}
		out.WriteByte(inner[i])
	}
	return out.String()
}

// splitQuoted splits s on sep, ignoring separators inside double-quoted
// strings (RFC 9110 quoted-string) and bytes escaped inside them.
func splitQuoted(s string, sep byte) []string {
	var parts []string
	inQuotes := false
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			if inQuotes {
				i++ // skip the quoted-pair escape target
			}
		case '"':
			inQuotes = !inQuotes
		case sep:
			if !inQuotes {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, s[start:])
}
