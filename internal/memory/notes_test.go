package memory

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParseNote_Valid covers the valid-parse contract: a note whose
// frontmatter carries the schema's fields round-trips into a Note with every
// field intact, the body below the fences, and the wiki-links extracted.
func TestParseNote_Valid(t *testing.T) {
	tests := []struct {
		name string
		file string
		raw  string
		want Note
	}{
		{
			name: "full note",
			file: "prefers-dark-mode.md",
			raw: `---
name: prefers-dark-mode
description: The user prefers dark mode in every tool
type: user
concepts:
  - dark-mode
  - ui-preferences
relations:
  - "prefers dark-mode"
  - "related_to ui-preferences"
  - "has_property durable"
supersedes:
  - old-preference
---

The user asked for dark mode on 2026-10-20.
See [[ui-preferences]] and [[dark-mode]].
`,
			want: Note{
				Name:        "prefers-dark-mode",
				Description: "The user prefers dark mode in every tool",
				Type:        TypeUser,
				Concepts:    []string{"dark-mode", "ui-preferences"},
				Relations: []Relation{
					{Predicate: PredPrefers, Target: "dark-mode"},
					{Predicate: PredRelatedTo, Target: "ui-preferences"},
					{Predicate: PredHasProperty, Target: "durable"},
				},
				Supersedes: []string{"old-preference"},
				Links:      []string{"ui-preferences", "dark-mode"},
				Body:       "The user asked for dark mode on 2026-10-20.\nSee [[ui-preferences]] and [[dark-mode]].\n",
				Path:       "prefers-dark-mode.md",
			},
		},
		{
			name: "minimal note only required fields",
			file: "api-base-url.md",
			raw: `---
name: api-base-url
description: The staging API base URL the team deploys to
type: project
---

https://staging.example.com
`,
			want: Note{
				Name:        "api-base-url",
				Description: "The staging API base URL the team deploys to",
				Type:        TypeProject,
				Body:        "https://staging.example.com\n",
				Path:        "api-base-url.md",
			},
		},
		{
			name: "closing fence at end of file",
			file: "deploy-checklist.md",
			raw: `---
name: deploy-checklist
description: Steps to deploy the service
type: reference
---`,
			want: Note{
				Name:        "deploy-checklist",
				Description: "Steps to deploy the service",
				Type:        TypeReference,
				Path:        "deploy-checklist.md",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			note, ok := ParseNote(tt.file, []byte(tt.raw))
			require.True(t, ok, "valid note must parse")
			require.Equal(t, tt.want, note)
		})
	}
}

// TestParseNote_InvalidSkipped covers the malformed-note contract: every
// invalid-frontmatter case is skipped — (Note{}, false), no error, no panic —
// so a bad note can never fail a turn or a sweep.
func TestParseNote_InvalidSkipped(t *testing.T) {
	tests := []struct {
		name string
		file string
		raw  string
	}{
		{
			name: "malformed YAML",
			file: "broken.md",
			raw: `---
name: [unclosed
type: user
---
body
`,
		},
		{
			name: "missing name",
			file: "no-name.md",
			raw: `---
description: A note without a name
type: user
---
body
`,
		},
		{
			name: "empty name",
			file: "empty-name.md",
			raw: `---
name: ""
description: A note with an empty name
type: user
---
body
`,
		},
		{
			name: "missing description",
			file: "no-description.md",
			raw: `---
name: no-description
type: user
---
body
`,
		},
		{
			name: "missing type",
			file: "no-type.md",
			raw: `---
name: no-type
description: A note without a type
---
body
`,
		},
		{
			name: "unknown type value",
			file: "wrong-type.md",
			raw: `---
name: wrong-type
description: A note with a type outside the taxonomy
type: persona
---
body
`,
		},
		{
			name: "name does not match filename",
			file: "prefers-tea.md",
			raw: `---
name: prefers-coffee
description: A note whose slug differs from its filename
type: user
---
body
`,
		},
		{
			name: "unknown relation predicate",
			file: "bad-relation.md",
			raw: `---
name: bad-relation
description: A relation outside the SCM vocabulary
type: feedback
relations:
  - "knows alice"
---
body
`,
		},
		{
			name: "relation without target",
			file: "targetless-relation.md",
			raw: `---
name: targetless-relation
description: A relation entry naming no target
type: feedback
relations:
  - "prefers"
---
body
`,
		},
		{
			name: "no frontmatter block",
			file: "plain.md",
			raw:  "Just some text without frontmatter.\n",
		},
		{
			name: "unclosed frontmatter",
			file: "unclosed.md",
			raw: `---
name: unclosed
description: The closing fence never comes
type: user
body
`,
		},
		{
			name: "non-markdown filename",
			file: "note.txt",
			raw: `---
name: note
description: A note whose file is not markdown
type: user
---
body
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			note, ok := ParseNote(tt.file, []byte(tt.raw))
			require.False(t, ok, "invalid note must be skipped")
			require.Equal(t, Note{}, note)
		})
	}
}

// TestParseNote_ValidAlongsideInvalid covers the spec scenario: a malformed
// note must not affect the parsing of other notes.
func TestParseNote_ValidAlongsideInvalid(t *testing.T) {
	invalid := "---\nname: [broken\n---\n"
	_, ok := ParseNote("broken.md", []byte(invalid))
	require.False(t, ok)

	valid := "---\nname: fine\ndescription: Still parses\ntype: user\n---\nbody\n"
	note, ok := ParseNote("fine.md", []byte(valid))
	require.True(t, ok)
	require.Equal(t, "fine", note.Name)
}

// TestExtractWikiLinks covers wiki-link extraction from note bodies.
func TestExtractWikiLinks(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string
	}{
		{name: "no links", body: "plain text only", want: nil},
		{name: "single link", body: "see [[auth-flow]] for details", want: []string{"auth-flow"}},
		{name: "multiple links", body: "[[a]] then [[b]] then [[c]]", want: []string{"a", "b", "c"}},
		{
			name: "duplicates deduplicated in first-seen order",
			body: "[[auth-flow]] and [[api-base]] and [[auth-flow]] again",
			want: []string{"auth-flow", "api-base"},
		},
		{name: "inner spaces trimmed", body: "[[ spaced-name ]] ok", want: []string{"spaced-name"}},
		{name: "empty link ignored", body: "[[]] and [[ ]] ignored", want: nil},
		{name: "unclosed link ignored", body: "a [[dangling link", want: nil},
		{name: "nested brackets not a link", body: "[[no[pe]] ignored", want: nil},
		{name: "links across lines", body: "first [[one]]\nsecond [[two]]", want: []string{"one", "two"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ExtractWikiLinks(tt.body))
		})
	}
}
