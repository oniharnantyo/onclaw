package references

import (
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// TestSectionize is table-driven across the supported document types
// (add-reference-documents tasks 3.2). Section is comparable, so expected
// sections (including ordinals and normalized bodies) are compared directly.
func TestSectionize(t *testing.T) {
	for _, tc := range []struct {
		name     string
		docName  string
		docType  string
		markdown string
		want     []Section
	}{
		{
			name:    "pdf multi-page, headings on some pages only",
			docName: "manual.pdf",
			docType: "pdf",
			markdown: "<!-- onclaw:page 1 -->\n" +
				"Intro paragraph on page one.\n\n" +
				"<!-- onclaw:page 2 -->\n" +
				"# Setup\n" +
				"Install steps here.\n\n" +
				"<!-- onclaw:page 3 -->\n" +
				"Lead text on page three.\n" +
				"## Details\n" +
				"Details body.\n\n" +
				"<!-- onclaw:page 4 -->\n" +
				"Trailing text without heading.\n",
			want: []Section{
				{Heading: "Page 1", Locator: "p. 1", LocatorKind: domain.LocatorKindPage, Level: 1, Ordinal: 0, Body: "Intro paragraph on page one."},
				{Heading: "Setup", Locator: "p. 2", LocatorKind: domain.LocatorKindPage, Level: 1, Ordinal: 1, Body: "# Setup\nInstall steps here."},
				{Heading: "Page 3", Locator: "p. 3", LocatorKind: domain.LocatorKindPage, Level: 1, Ordinal: 2, Body: "Lead text on page three."},
				{Heading: "Details", Locator: "p. 3", LocatorKind: domain.LocatorKindPage, Level: 2, Ordinal: 3, Body: "## Details\nDetails body."},
				{Heading: "Page 4", Locator: "p. 4", LocatorKind: domain.LocatorKindPage, Level: 1, Ordinal: 4, Body: "Trailing text without heading."},
			},
		},
		{
			name:     "pdf without anchors degrades to one page-1 section",
			docName:  "orphan.pdf",
			docType:  "pdf",
			markdown: "Orphan text only",
			want: []Section{
				{Heading: "Page 1", Locator: "p. 1", LocatorKind: domain.LocatorKindPage, Level: 1, Ordinal: 0, Body: "Orphan text only"},
			},
		},
		{
			name:     "pptx two slides",
			docName:  "deck.pptx",
			docType:  "pptx",
			markdown: "## Slide 1\n\nWelcome to the deck\nSecond bullet line\n\n## Slide 2\n\nClosing thoughts\n",
			want: []Section{
				{Heading: "Slide 1", Locator: "slide 1", LocatorKind: domain.LocatorKindSlide, Level: 1, Ordinal: 0, Body: "## Slide 1\n\nWelcome to the deck\nSecond bullet line"},
				{Heading: "Slide 2", Locator: "slide 2", LocatorKind: domain.LocatorKindSlide, Level: 1, Ordinal: 1, Body: "## Slide 2\n\nClosing thoughts"},
			},
		},
		{
			name:     "xlsx two sheets",
			docName:  "book.xlsx",
			docType:  "xlsx",
			markdown: "## Users\n\n| region | sales |\n| --- | --- |\n| north | 42 |\n\n## Rate Limits\n\n| tier | cap |\n| --- | --- |\n| free | 10 |\n",
			want: []Section{
				{Heading: "Users", Locator: "sheet Users", LocatorKind: domain.LocatorKindSheet, Level: 1, Ordinal: 0, Body: "## Users\n\n| region | sales |\n| --- | --- |\n| north | 42 |"},
				{Heading: "Rate Limits", Locator: "sheet Rate Limits", LocatorKind: domain.LocatorKindSheet, Level: 1, Ordinal: 1, Body: "## Rate Limits\n\n| tier | cap |\n| --- | --- |\n| free | 10 |"},
			},
		},
		{
			name:     "md preamble plus nested headings",
			docName:  "integration-notes.md",
			docType:  "md",
			markdown: "Pre-heading introduction text.\n\n# Webhooks\nWebhook body.\n\n## Signing\nSigning details.\n",
			want: []Section{
				{Heading: "integration-notes.md", Locator: "", LocatorKind: domain.LocatorKindNone, Level: 0, Ordinal: 0, Body: "Pre-heading introduction text."},
				{Heading: "Webhooks", Locator: "Webhooks", LocatorKind: domain.LocatorKindHeading, Level: 1, Ordinal: 1, Body: "# Webhooks\nWebhook body."},
				{Heading: "Signing", Locator: "Signing", LocatorKind: domain.LocatorKindHeading, Level: 2, Ordinal: 2, Body: "## Signing\nSigning details."},
			},
		},
		{
			name:     "md without headings is a single preamble section",
			docName:  "notes.md",
			docType:  "md",
			markdown: "Just plain prose.\nMore prose.",
			want: []Section{
				{Heading: "notes.md", Locator: "", LocatorKind: domain.LocatorKindNone, Level: 0, Ordinal: 0, Body: "Just plain prose.\nMore prose."},
			},
		},
		{
			name:     "html uses the heading branch",
			docName:  "page.html",
			docType:  "html",
			markdown: "Intro paragraph.\n\n# Main Title\n\nBody text.\n\n## Section\n\nTrailing paragraph.",
			want: []Section{
				{Heading: "page.html", Locator: "", LocatorKind: domain.LocatorKindNone, Level: 0, Ordinal: 0, Body: "Intro paragraph."},
				{Heading: "Main Title", Locator: "Main Title", LocatorKind: domain.LocatorKindHeading, Level: 1, Ordinal: 1, Body: "# Main Title\n\nBody text."},
				{Heading: "Section", Locator: "Section", LocatorKind: domain.LocatorKindHeading, Level: 2, Ordinal: 2, Body: "## Section\n\nTrailing paragraph."},
			},
		},
		{
			name:     "docx uses the heading branch too",
			docName:  "spec.docx",
			docType:  "docx",
			markdown: "# Quarterly Report\n\nPlain paragraph.\n",
			want: []Section{
				{Heading: "Quarterly Report", Locator: "Quarterly Report", LocatorKind: domain.LocatorKindHeading, Level: 1, Ordinal: 0, Body: "# Quarterly Report\n\nPlain paragraph."},
			},
		},
		{
			name:     "csv is a single no-locator section",
			docName:  "cities.csv",
			docType:  "csv",
			markdown: "| city | population |\n| --- | --- |\n| Portland | 650000 |",
			want: []Section{
				{Heading: "cities.csv", Locator: "", LocatorKind: domain.LocatorKindNone, Level: 0, Ordinal: 0, Body: "| city | population |\n| --- | --- |\n| Portland | 650000 |"},
			},
		},
		{
			name:     "tsv is a single no-locator section",
			docName:  "pairs.tsv",
			docType:  "tsv",
			markdown: "| key | value |\n| --- | --- |\n| alpha | one |",
			want: []Section{
				{Heading: "pairs.tsv", Locator: "", LocatorKind: domain.LocatorKindNone, Level: 0, Ordinal: 0, Body: "| key | value |\n| --- | --- |\n| alpha | one |"},
			},
		},
		{
			name:     "txt whole text, blank runs collapsed",
			docName:  "note.txt",
			docType:  "txt",
			markdown: "Plain integration note.\n\n\n\nSecond paragraph after blank runs.",
			want: []Section{
				{Heading: "note.txt", Locator: "", LocatorKind: domain.LocatorKindNone, Level: 0, Ordinal: 0, Body: "Plain integration note.\n\nSecond paragraph after blank runs."},
			},
		},
		{
			name:     "docType is normalized (dot and case)",
			docName:  "scan.pdf",
			docType:  ".PDF",
			markdown: "",
			want: []Section{
				{Heading: "scan.pdf", Locator: "", LocatorKind: domain.LocatorKindNone, Level: 0, Ordinal: 0, Body: ""},
			},
		},
		{
			name:     "empty txt yields one empty-body section",
			docName:  "empty.txt",
			docType:  "txt",
			markdown: "",
			want: []Section{
				{Heading: "empty.txt", Locator: "", LocatorKind: domain.LocatorKindNone, Level: 0, Ordinal: 0, Body: ""},
			},
		},
		{
			name:     "empty csv yields one empty-body section",
			docName:  "blank.csv",
			docType:  "csv",
			markdown: "   \n\n  ",
			want: []Section{
				{Heading: "blank.csv", Locator: "", LocatorKind: domain.LocatorKindNone, Level: 0, Ordinal: 0, Body: ""},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Sectionize(tc.docName, tc.docType, tc.markdown)
			if len(got) != len(tc.want) {
				t.Fatalf("Sectionize returned %d sections, want %d:\n%+v", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("section %d:\n  got  %+v\n  want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestSectionizeOrdinalsSequential pins that ordinals are 0-based and
// sequential regardless of how many sections a type produces.
func TestSectionizeOrdinalsSequential(t *testing.T) {
	md := "<!-- onclaw:page 1 -->\na\n\n<!-- onclaw:page 2 -->\nb\n\n<!-- onclaw:page 3 -->\nc"
	sections := Sectionize("doc.pdf", "pdf", md)
	for i, sec := range sections {
		if sec.Ordinal != i {
			t.Errorf("section %d has ordinal %d", i, sec.Ordinal)
		}
	}
}
