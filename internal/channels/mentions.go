package channels

import (
	"strings"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// Mention token syntax: `@` followed by handle characters. Handles are
// kebab-case slugs (agents) and name-derived dashed handles (humans — the
// agents.ChannelHandles rule: lowercase, spaces → dashes), so the character
// class below covers every producible handle.
const mentionHandleChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_"

// parseMentionTokens returns the @handles appearing in body, in first
// occurrence order, deduplicated case-insensitively. A token is `@` plus the
// maximal run of handle characters; a bare `@` or `@@` stays plain text.
func parseMentionTokens(body string) []string {
	tokens := make([]string, 0, 2)
	seen := make(map[string]struct{})

	for i := 0; i < len(body); i++ {
		if body[i] != '@' {
			continue
		}
		j := i + 1
		for j < len(body) && isHandleChar(body[j]) {
			j++
		}
		if j == i+1 {
			continue // bare '@' — plain text
		}
		token := body[i+1 : j]
		key := strings.ToLower(token)
		if _, dup := seen[key]; dup {
			i = j - 1
			continue
		}
		seen[key] = struct{}{}
		tokens = append(tokens, token)
		i = j - 1
	}
	return tokens
}

func isHandleChar(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	case b == '-' || b == '_':
		return true
	default:
		return false
	}
}

// memberRef is one roster entry's mention identity: who it is and the handle
// it answers to.
type memberRef struct {
	id         string
	handle     string
	memberType domain.ChannelMemberType
}

// handleIndex builds the lowercase-handle → member index over the roster,
// walking members in add order (deterministic). resolved maps each member
// reference id to its handle; members without a handle are absent — their
// @tokens stay plain text. When two members' handles collide
// case-insensitively, the earlier roster member wins.
func handleIndex(resolved map[string]string, members []domain.ChannelMember) map[string]memberRef {
	index := make(map[string]memberRef, len(resolved))
	for _, m := range members {
		handle, ok := resolved[m.RefID()]
		if !ok || handle == "" {
			continue
		}
		key := strings.ToLower(handle)
		if _, exists := index[key]; exists {
			continue
		}
		index[key] = memberRef{id: m.RefID(), handle: handle, memberType: m.MemberType}
	}
	return index
}

// resolveMentions matches @tokens in body against the roster index:
// case-insensitive exact token match. Unresolved tokens stay plain text —
// they simply produce no Mention. The result preserves first-occurrence
// order and dedupes by (type, id).
func resolveMentions(tokens []string, index map[string]memberRef) []domain.Mention {
	mentions := make([]domain.Mention, 0, len(tokens))
	seen := make(map[string]struct{}, len(tokens))

	for _, token := range tokens {
		ref, ok := index[strings.ToLower(token)]
		if !ok {
			continue
		}
		key := string(ref.memberType) + ":" + ref.id
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		mentions = append(mentions, domain.Mention{
			Type:   ref.memberType,
			ID:     ref.id,
			Handle: ref.handle,
		})
	}
	return mentions
}

// agentIDsOf filters a resolved mention list down to the mentioned agent ids,
// preserving order.
func agentIDsOf(mentions []domain.Mention) []string {
	ids := make([]string, 0, len(mentions))
	for _, m := range mentions {
		if m.Type == domain.ChannelMemberTypeAgent {
			ids = append(ids, m.ID)
		}
	}
	return ids
}
