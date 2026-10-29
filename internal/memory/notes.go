package memory

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// frontmatterOpen and frontmatterClose delimit a note's YAML frontmatter
// block.
const (
	frontmatterOpen  = "---"
	frontmatterClose = "---"
)

// ParseNote parses one memory file into a Note. It reports whether the note
// is valid. A file whose frontmatter is missing, malformed, or invalid is
// skipped: (Note{}, false) with the reason logged (spec agent-memory: a
// malformed note is skipped with a logged reason and never fails a turn or a
// sweep) — ParseNote never returns an error.
func ParseNote(path string, raw []byte) (Note, bool) {
	note, err := parseNote(path, string(raw))
	if err != nil {
		slog.Warn("memory: skipping note", "path", path, "reason", err)
		return Note{}, false
	}
	return note, true
}

// parseNote does the work of ParseNote; its error is the skip reason.
func parseNote(path, raw string) (Note, error) {
	front, body, err := splitFrontmatter(raw)
	if err != nil {
		return Note{}, err
	}
	var fm Frontmatter
	if err := yaml.Unmarshal([]byte(front), &fm); err != nil {
		return Note{}, fmt.Errorf("malformed frontmatter: %w", err)
	}
	if err := validateFrontmatter(fm, path); err != nil {
		return Note{}, err
	}
	relations, err := parseRelations(fm.Relations)
	if err != nil {
		return Note{}, err
	}
	return Note{
		Name:        fm.Name,
		Description: fm.Description,
		Type:        fm.Type,
		Concepts:    fm.Concepts,
		Relations:   relations,
		Supersedes:  fm.Supersedes,
		Links:       ExtractWikiLinks(body),
		Body:        body,
		Path:        path,
	}, nil
}

// splitFrontmatter splits a note into its frontmatter text and body: the
// frontmatter is everything between an opening --- line and the next ---
// line, the body everything after it. A file without the opening fence or
// without a closing fence has no frontmatter.
func splitFrontmatter(raw string) (front, body string, err error) {
	lines := strings.Split(raw, "\n")
	if strings.TrimRight(lines[0], "\r") != frontmatterOpen {
		return "", "", errors.New("no frontmatter block")
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\r") != frontmatterClose {
			continue
		}
		// Rejoin without the fence lines; the body keeps its internal
		// newlines and drops the single blank line after the closing fence
		// (the frontmatter convention), so the body is stable whether or
		// not the author left one.
		front = strings.Join(lines[1:i], "\n")
		body = strings.TrimPrefix(strings.Join(lines[i+1:], "\n"), "\n")
		return front, body, nil
	}
	return "", "", errors.New("unclosed frontmatter block")
}

// validateFrontmatter applies the frontmatter schema (design D4, spec
// agent-memory): name, description, and type are required, type is
// restricted to the four-note taxonomy, and the name must equal the
// filename's base (the slug is the filename).
func validateFrontmatter(fm Frontmatter, path string) error {
	if fm.Name == "" {
		return errors.New("missing name")
	}
	if fm.Description == "" {
		return errors.New("missing description")
	}
	if !validType(fm.Type) {
		return fmt.Errorf("unknown type %q: must be one of %s, %s, %s, %s",
			fm.Type, TypeUser, TypeFeedback, TypeProject, TypeReference)
	}
	name, err := noteName(path)
	if err != nil {
		return err
	}
	if fm.Name != name {
		return fmt.Errorf("name %q does not match filename %q", fm.Name, name)
	}
	return nil
}

// validType reports whether t is one of the four note types.
func validType(t string) bool {
	switch t {
	case TypeUser, TypeFeedback, TypeProject, TypeReference:
		return true
	default:
		return false
	}
}

// noteName derives the note slug from its path: the base filename without
// the .md suffix.
func noteName(path string) (string, error) {
	base := filepath.Base(path)
	if !strings.HasSuffix(base, ".md") {
		return "", fmt.Errorf("filename %q is not a .md note", base)
	}
	name := strings.TrimSuffix(base, ".md")
	if name == "" {
		return "", fmt.Errorf("filename %q has no note name", base)
	}
	return name, nil
}

// parseRelations converts the raw "<predicate> <target>" frontmatter entries
// into Relations, rejecting entries outside the SCM predicate vocabulary
// (design D4).
func parseRelations(raw []string) ([]Relation, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]Relation, 0, len(raw))
	for _, entry := range raw {
		predicate, target, found := strings.Cut(entry, " ")
		predicate = strings.TrimSpace(predicate)
		target = strings.TrimSpace(target)
		if !validPredicate(predicate) {
			return nil, fmt.Errorf("unknown relation predicate in %q: must be one of %s, %s, %s, %s, %s, %s",
				entry, PredHasProperty, PredPrefers, PredRelatedTo, PredContradicts, PredCauses, PredPartOf)
		}
		if !found || target == "" {
			return nil, fmt.Errorf("relation %q must be \"<predicate> <target>\"", entry)
		}
		out = append(out, Relation{Predicate: predicate, Target: target})
	}
	return out, nil
}

// validPredicate reports whether p is one of SCM's six predicates.
func validPredicate(p string) bool {
	switch p {
	case PredHasProperty, PredPrefers, PredRelatedTo, PredContradicts, PredCauses, PredPartOf:
		return true
	default:
		return false
	}
}

// ExtractWikiLinks returns the [[wiki-links]] found in a note body as
// deduplicated names in order of first appearance. Link spellings are
// trimmed; empty links are ignored. The result is nil when the body carries
// no links.
func ExtractWikiLinks(body string) []string {
	var links []string
	seen := make(map[string]bool)
	for _, raw := range wikiLinkMatches(body) {
		link := strings.TrimSpace(raw)
		if link == "" || seen[link] {
			continue
		}
		seen[link] = true
		links = append(links, link)
	}
	return links
}

// wikiLinkMatches scans the body for [[...]] spans and returns their inner
// text. An opening bracket without a closing one yields nothing, and a link
// may not contain brackets of its own.
func wikiLinkMatches(body string) []string {
	var matches []string
	for {
		open := strings.Index(body, "[[")
		if open < 0 {
			return matches
		}
		rest := body[open+2:]
		end := strings.Index(rest, "]]")
		if end < 0 {
			return matches
		}
		inner := rest[:end]
		if !strings.ContainsAny(inner, "[]") {
			matches = append(matches, inner)
		}
		body = rest[end+2:]
	}
}
