package agents

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/attachments"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store/fake"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// The uploads-mirror tests (fix-pdf-attachment-carriage design D2, spec
// chat-attachments "Upload lands in the workspace" and "Mirror name
// collision"): copy-if-absent idempotency, collision suffixing, sanitized
// names, and the skip-don't-fail degradation for entries the mirror cannot
// serve.

// newMirrorStore builds an attachment store over a fresh registry and
// directory, for driving mirrorUploads directly.
func newMirrorStore(t *testing.T) *attachments.Store {
	t.Helper()
	return attachments.NewStore(t.TempDir(), fake.New().Attachments())
}

// upload saves one attachment through the store.
func upload(t *testing.T, atts *attachments.Store, name, content string) sessions.Attachment {
	t.Helper()
	att, err := atts.Save(t.Context(), name, "application/pdf", []byte(content))
	require.NoError(t, err)
	return att
}

// dirEntries lists a directory's child names.
func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestMirrorUploads_CopiesIfAbsentAndIsIdempotent(t *testing.T) {
	uploads := filepath.Join(t.TempDir(), workspaces.DirUploads)
	atts := newMirrorStore(t)
	att := upload(t, atts, "report.pdf", "%PDF-1.4")

	got := mirrorUploads(t.Context(), uploads, atts, []sessions.Attachment{att})
	require.Equal(t, map[string]string{att.Key: "report.pdf"}, got)
	data, err := os.ReadFile(filepath.Join(uploads, "report.pdf"))
	require.NoError(t, err)
	require.Equal(t, "%PDF-1.4", string(data), "the mirror holds the store's bytes")

	// A replayed turn mirrors the same attachment: the idempotent hit keeps
	// the name and the directory holds exactly one copy.
	got = mirrorUploads(t.Context(), uploads, atts, []sessions.Attachment{att})
	require.Equal(t, map[string]string{att.Key: "report.pdf"}, got)
	require.Equal(t, []string{"report.pdf"}, dirEntries(t, uploads))
}

func TestMirrorUploads_CollisionTakesNumericSuffix(t *testing.T) {
	uploads := filepath.Join(t.TempDir(), workspaces.DirUploads)
	atts := newMirrorStore(t)
	first := upload(t, atts, "report.pdf", "first-bytes")
	second := upload(t, atts, "report.pdf", "second-bytes")

	got := mirrorUploads(t.Context(), uploads, atts, []sessions.Attachment{first, second})
	require.Equal(t, map[string]string{first.Key: "report.pdf", second.Key: "report-1.pdf"}, got)

	data, err := os.ReadFile(filepath.Join(uploads, "report.pdf"))
	require.NoError(t, err)
	require.Equal(t, "first-bytes", string(data), "the first mirror keeps its name and bytes")
	data, err = os.ReadFile(filepath.Join(uploads, "report-1.pdf"))
	require.NoError(t, err)
	require.Equal(t, "second-bytes", string(data), "the colliding copy lands on the suffixed name")

	// Replaying both in either order stays stable.
	got = mirrorUploads(t.Context(), uploads, atts, []sessions.Attachment{second, first})
	require.Equal(t, map[string]string{first.Key: "report.pdf", second.Key: "report-1.pdf"}, got)
	require.ElementsMatch(t, []string{"report.pdf", "report-1.pdf"}, dirEntries(t, uploads))
}

func TestMirrorUploads_IdenticalContentSharesTheName(t *testing.T) {
	uploads := filepath.Join(t.TempDir(), workspaces.DirUploads)
	atts := newMirrorStore(t)
	first := upload(t, atts, "report.pdf", "same-bytes")
	second := upload(t, atts, "report.pdf", "same-bytes")

	got := mirrorUploads(t.Context(), uploads, atts, []sessions.Attachment{first, second})
	require.Equal(t, map[string]string{first.Key: "report.pdf", second.Key: "report.pdf"}, got,
		"equal bytes under one name are one mirror copy")
	require.Equal(t, []string{"report.pdf"}, dirEntries(t, uploads))
}

func TestMirrorUploads_SkipsUnreadableEntries(t *testing.T) {
	uploads := filepath.Join(t.TempDir(), workspaces.DirUploads)
	atts := newMirrorStore(t)
	good := upload(t, atts, "good.pdf", "good-bytes")
	ghost := sessions.Attachment{Key: "ffffffffffffffffffffffffffffffff", Name: "ghost.pdf", Mime: "application/pdf", Size: 4}

	got := mirrorUploads(t.Context(), uploads, atts, []sessions.Attachment{ghost, good})
	require.Equal(t, map[string]string{good.Key: "good.pdf"}, got,
		"an unknown key is skipped, the rest still mirrors")
	require.Equal(t, []string{"good.pdf"}, dirEntries(t, uploads))

	// A store reading from the wrong directory fails every read: nothing is
	// mirrored and the result is nil.
	lost := attachments.NewStore(t.TempDir(), fake.New().Attachments())
	require.Nil(t, mirrorUploads(t.Context(), uploads, lost, []sessions.Attachment{good}))
}

func TestMirrorUploads_SanitizesNames(t *testing.T) {
	uploads := filepath.Join(t.TempDir(), workspaces.DirUploads)
	atts := newMirrorStore(t)
	hostPath := upload(t, atts, "../etc/evil.pdf", "b")
	windowsPath := upload(t, atts, `C:\Users\me\doc.pdf`, "c")
	controls := upload(t, atts, "re\x00\x1fport.pdf", "d")
	spaced := upload(t, atts, "  spaced.pdf  ", "e")

	got := mirrorUploads(t.Context(), uploads, atts, []sessions.Attachment{hostPath, windowsPath, controls, spaced})
	require.Equal(t, map[string]string{
		hostPath.Key:    "evil.pdf",
		windowsPath.Key: "doc.pdf",
		controls.Key:    "report.pdf",
		spaced.Key:      "spaced.pdf",
	}, got, "the base name only, control characters stripped, whitespace trimmed")

	// Percent-encoded separators are not separators: the name survives as-is
	// (a legal, if ugly, filename — and no traversal either way).
	encoded := upload(t, atts, "..%2F..%2Fescape.pdf", "f")
	got = mirrorUploads(t.Context(), uploads, atts, []sessions.Attachment{encoded})
	require.Equal(t, map[string]string{encoded.Key: "..%2F..%2Fescape.pdf"}, got)
	require.FileExists(t, filepath.Join(uploads, "..%2F..%2Fescape.pdf"))
}

func TestMirrorUploads_EmptyNamesAreSkipped(t *testing.T) {
	uploads := filepath.Join(t.TempDir(), workspaces.DirUploads)
	atts := newMirrorStore(t)
	unnamed := upload(t, atts, "placeholder.pdf", "bytes")

	unnamed.Name = ""
	require.Nil(t, mirrorUploads(t.Context(), uploads, atts, []sessions.Attachment{unnamed}),
		"an empty name skips the entry")

	dotted := unnamed
	dotted.Name = ".."
	require.Nil(t, mirrorUploads(t.Context(), uploads, atts, []sessions.Attachment{dotted}),
		"a bare parent-directory name skips the entry")

	controls := unnamed
	controls.Name = "\x01\x7f"
	require.Nil(t, mirrorUploads(t.Context(), uploads, atts, []sessions.Attachment{controls}),
		"a name of only control characters skips the entry")

	entries, err := os.ReadDir(filepath.Dir(uploads))
	require.NoError(t, err)
	require.Empty(t, entries, "nothing was written for skipped entries")
}

func TestMirrorUploads_NilTargetsDoNothing(t *testing.T) {
	uploads := filepath.Join(t.TempDir(), workspaces.DirUploads)
	atts := newMirrorStore(t)
	att := upload(t, atts, "report.pdf", "bytes")

	require.Nil(t, mirrorUploads(t.Context(), "", atts, []sessions.Attachment{att}),
		"an empty uploads directory mirrors nothing")
	require.Nil(t, mirrorUploads(t.Context(), uploads, nil, []sessions.Attachment{att}),
		"a nil store mirrors nothing")
	require.Nil(t, mirrorUploads(t.Context(), uploads, atts, nil),
		"an empty list mirrors nothing")
	_, err := os.Stat(uploads)
	require.True(t, os.IsNotExist(err), "the helper itself creates no directories")
}

func TestTurnAttachments_DedupesByKeyInFirstSeenOrder(t *testing.T) {
	events := []sessions.Event{
		{Kind: sessions.KindUserMessage, Attachments: []sessions.Attachment{{Key: "a"}, {Key: "b"}}},
		{Kind: sessions.KindAssistantMessage, Text: "hi"},
		{Kind: sessions.KindUserMessage, Attachments: []sessions.Attachment{{Key: "b"}, {Key: "c"}, {Key: ""}}},
		{Kind: sessions.KindTurnCompleted},
	}
	fresh := []sessions.Attachment{{Key: "c"}, {Key: "d"}}

	var keys []string
	for _, att := range turnAttachments(events, fresh) {
		keys = append(keys, att.Key)
	}
	require.Equal(t, []string{"a", "b", "c", "d"}, keys,
		"replayed user-message attachments ride first, the fresh send last, deduped by key, empty keys dropped")
}

func TestSuffixedUploadName(t *testing.T) {
	require.Equal(t, "report-1.pdf", suffixedUploadName("report.pdf", 1))
	require.Equal(t, "report-12.pdf", suffixedUploadName("report.pdf", 12))
	require.Equal(t, "archive-3", suffixedUploadName("archive", 3), "no extension: the suffix trails the name")
	require.Equal(t, "notes.markdown-2.md", suffixedUploadName("notes.markdown.md", 2),
		"only the last extension is displaced")
}
