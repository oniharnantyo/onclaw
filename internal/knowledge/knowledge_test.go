package knowledge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
)

// newTestStore roots a knowledge store in a temp directory over the fake
// store's document registry.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	st := fake.New()
	return NewStore(t.TempDir(), st.Documents())
}

// TestValidateIncoming_RejectsUnsupportedKinds pins the D5 gate's kind
// verdict: everything but text, PDF, and OOXML office documents is rejected
// with the reason naming the supported kinds (add-document-editing task 5.4:
// the reason names text, PDF, and OOXML documents).
func TestValidateIncoming_RejectsUnsupportedKinds(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		mime     string
	}{
		{"video", "clip.mp4", "video/mp4"},
		{"image", "shot.png", "image/png"},
		{"archive", "bundle.zip", "application/zip"},
		{"opaque binary", "blob.bin", "application/octet-stream"},
		{"legacy word processor", "report.doc", "application/msword"},
		{"legacy spreadsheet", "sheet.xls", "application/vnd.ms-excel"},
		{"legacy presentation", "deck.ppt", "application/vnd.ms-powerpoint"},
	}
	for _, tc := range cases {
		err := ValidateIncoming(tc.filename, tc.mime, 10)
		require.ErrorIs(t, err, ErrRejected, "%s must be rejected", tc.name)
		require.ErrorContains(t, err,
			"only plain text (text/*, up to 1 MB), PDF (up to 20 MB), and OOXML office documents (.docx, .xlsx, .pptx, up to 20 MB) are supported",
			"the rejection names the supported kinds")
	}
}

// TestValidateIncoming_AcceptsOOXML covers the widened gate (add-document-editing
// task 5.4): the three OOXML kinds are accepted, both by declared MIME and by
// the filename extension when the declared MIME is empty.
func TestValidateIncoming_AcceptsOOXML(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		mime     string
	}{
		{"declared docx", "report.docx", DocxMime},
		{"declared xlsx", "sheet.xlsx", XlsxMime},
		{"declared pptx", "deck.pptx", PptxMime},
		{"extension-resolved docx", "report.docx", ""},
		{"extension-resolved xlsx", "sheet.xlsx", ""},
		{"extension-resolved pptx", "deck.pptx", ""},
	}
	for _, tc := range cases {
		require.NoError(t, ValidateIncoming(tc.filename, tc.mime, 10), "%s must be accepted", tc.name)
	}
}

// TestValidateIncoming_RejectsOversize pins the size caps with their reasons.
func TestValidateIncoming_RejectsOversize(t *testing.T) {
	err := ValidateIncoming("notes.txt", "text/plain", MaxTextBytes+1)
	require.ErrorIs(t, err, ErrRejected)
	require.ErrorContains(t, err, "limited to 1 MB")

	err = ValidateIncoming("big.pdf", PDFMime, MaxPDFBytes+1)
	require.ErrorIs(t, err, ErrRejected)
	require.ErrorContains(t, err, "limited to 20 MB")

	err = ValidateIncoming("big.xlsx", XlsxMime, MaxOOXMLBytes+1)
	require.ErrorIs(t, err, ErrRejected)
	require.ErrorContains(t, err, "limited to 20 MB")
}

// TestValidateIncoming_BoundarySizesAccepted pins the caps as inclusive.
func TestValidateIncoming_BoundarySizesAccepted(t *testing.T) {
	require.NoError(t, ValidateIncoming("notes.txt", "text/plain", MaxTextBytes))
	require.NoError(t, ValidateIncoming("big.pdf", PDFMime, MaxPDFBytes))
	require.NoError(t, ValidateIncoming("big.docx", DocxMime, MaxOOXMLBytes))
}

// TestValidateIncoming_EmptyMimeFallsBackByExtension covers the extension
// fallback: PDF, text, and OOXML extensions resolve; the legacy
// word-processing extensions resolve to their real kind and are still
// rejected.
func TestValidateIncoming_EmptyMimeFallsBackByExtension(t *testing.T) {
	require.NoError(t, ValidateIncoming("paper.pdf", "", 10), ".pdf resolves to a PDF")
	for _, ext := range []string{".txt", ".md", ".markdown", ".json", ".csv", ".yaml"} {
		require.NoError(t, ValidateIncoming("notes"+ext, "", 10), "%s resolves to text", ext)
	}
	require.NoError(t, ValidateIncoming("report.docx", "", 10), ".docx resolves to an OOXML document")
	err := ValidateIncoming("report.doc", "", 10)
	require.ErrorIs(t, err, ErrRejected, "a legacy binary extension is rejected despite the empty MIME")
	require.ErrorContains(t, err, "application/msword")
}

// TestSave_RejectionLeavesNothingBehind pins the gate's side-effect contract:
// a rejected save creates no row and no file.
func TestSave_RejectionLeavesNothingBehind(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Save(ctx, SaveInput{Filename: "clip.mp4", MIME: "video/mp4", Scope: ScopeGlobal, Data: []byte("x")})
	require.ErrorIs(t, err, ErrRejected)
	require.ErrorContains(t, err, "OOXML office documents", "the rejection names the supported kinds")

	docs, err := s.List(ctx)
	require.NoError(t, err)
	require.Empty(t, docs, "no catalog row may be created")

	_, statErr := os.Stat(filepath.Join(s.Roots().Global, "clip.mp4"))
	require.True(t, os.IsNotExist(statErr), "no file may be written")
}

// TestSave_AcceptsOOXML covers the widened gate through the save path
// (add-document-editing task 5.4, spec knowledge "Office document is accepted
// at upload"): a .docx is stored raw, cataloged with its kind, and readable.
func TestSave_AcceptsOOXML(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	data := []byte("PK\x03\x04 docx-bytes")
	doc, err := s.Save(ctx, SaveInput{
		Filename: "report.docx", MIME: DocxMime, Data: data,
		Scope: ScopeGlobal, Source: SourceUser, Description: "quarterly report",
	})
	require.NoError(t, err)
	require.Equal(t, DocxMime, doc.MIME, "the declared OOXML kind is stored")
	require.Equal(t, int64(len(data)), doc.Size)

	stored, err := os.ReadFile(filepath.Join(s.Roots().Global, "report.docx"))
	require.NoError(t, err)
	require.Equal(t, data, stored, "raw bytes are stored unmodified")

	content, err := s.Content(ctx, doc.ID)
	require.NoError(t, err)
	require.Equal(t, data, content, "the document is readable like any other")
}

// TestSave_WritesFileAndRow pins the happy path: bytes at the scope path, the
// catalog row matching, and a 32-hex store-assigned id.
func TestSave_WritesFileAndRow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	doc, err := s.Save(ctx, SaveInput{
		Filename: "notes.txt", MIME: "text/plain", Data: []byte("hello"),
		Scope: ScopeGlobal, Source: SourceUser, Description: "meeting notes",
	})
	require.NoError(t, err)
	require.Regexp(t, `^[0-9a-f]{32}$`, doc.ID, "ids are 32-char lowercase hex")
	require.Equal(t, "notes.txt", doc.Filename)
	require.Equal(t, "text/plain", doc.MIME)
	require.Equal(t, int64(5), doc.Size)
	require.Equal(t, ScopeGlobal, doc.Scope)
	require.Empty(t, doc.AgentID)
	require.False(t, doc.CreatedAt.IsZero())

	data, err := os.ReadFile(filepath.Join(s.Roots().Global, "notes.txt"))
	require.NoError(t, err)
	require.Equal(t, []byte("hello"), data, "raw bytes are stored unmodified")

	got, err := s.Get(ctx, doc.ID)
	require.NoError(t, err)
	require.Equal(t, doc, got)

	content, err := s.Content(ctx, doc.ID)
	require.NoError(t, err)
	require.Equal(t, []byte("hello"), content)
}

// TestSave_RejectsDuplicateWithinScope pins D6: a duplicate in one scope is
// rejected and the existing document is unchanged.
func TestSave_RejectsDuplicateWithinScope(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	first, err := s.Save(ctx, SaveInput{Filename: "a.txt", MIME: "text/plain", Data: []byte("v1"), Scope: ScopeGlobal, Source: SourceUser})
	require.NoError(t, err)

	_, err = s.Save(ctx, SaveInput{Filename: "a.txt", MIME: "text/plain", Data: []byte("v2"), Scope: ScopeGlobal, Source: SourceUser})
	require.ErrorIs(t, err, store.ErrDuplicate)
	require.ErrorContains(t, err, "a.txt")

	got, err := s.Get(ctx, first.ID)
	require.NoError(t, err)
	require.Equal(t, int64(2), got.Size, "the existing document is unchanged")
	content, err := s.Content(ctx, first.ID)
	require.NoError(t, err)
	require.Equal(t, []byte("v1"), content)
}

// TestSave_SameFilenameAcrossScopesAllowed pins D6's scope-local uniqueness:
// two agents and global may each hold the same filename.
func TestSave_SameFilenameAcrossScopesAllowed(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, in := range []SaveInput{
		{Filename: "runbook.md", MIME: "text/plain", Data: []byte("global"), Scope: ScopeGlobal, Source: SourceUser},
		{Filename: "runbook.md", MIME: "text/plain", Data: []byte("scout"), Scope: ScopeAgent, AgentID: "agt-1", AgentSlug: "scout", Source: SourceUser},
		{Filename: "runbook.md", MIME: "text/plain", Data: []byte("mapper"), Scope: ScopeAgent, AgentID: "agt-2", AgentSlug: "mapper", Source: SourceUser},
	} {
		_, err := s.Save(ctx, in)
		require.NoError(t, err, "%s scope may hold runbook.md", in.Scope)
	}

	docs, err := s.List(ctx)
	require.NoError(t, err)
	require.Len(t, docs, 3)
}

// TestSave_RejectsBadInput pins the input guards: unusable filenames and the
// reserved index name are rejected before anything is touched.
func TestSave_RejectsBadInput(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, name := range []string{"", "dir/notes.txt", "..", ".", IndexFileName} {
		_, err := s.Save(ctx, SaveInput{Filename: name, MIME: "text/plain", Data: []byte("x"), Scope: ScopeGlobal})
		require.ErrorIs(t, err, ErrRejected, "filename %q must be rejected", name)
	}
	_, err := s.Save(ctx, SaveInput{Filename: "a.txt", MIME: "text/plain", Data: []byte("x"), Scope: "bogus"})
	require.ErrorIs(t, err, ErrRejected, "unknown scopes are rejected")
	_, err = s.Save(ctx, SaveInput{Filename: "a.txt", MIME: "text/plain", Data: []byte("x"), Scope: ScopeAgent})
	require.ErrorIs(t, err, ErrRejected, "agent scope without the agent's id and slug is rejected")
	_, err = s.Save(ctx, SaveInput{Filename: "a.txt", MIME: "text/plain", Data: []byte("x"), Scope: ScopeGlobal, Source: "bogus"})
	require.ErrorIs(t, err, ErrRejected, "unknown sources are rejected")
}

// TestSetScope_MovesFileAndRow pins the atomic scope change: the file ends up
// only under the new scope and the row reflects it (D6).
func TestSetScope_MovesFileAndRow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	doc, err := s.Save(ctx, SaveInput{
		Filename: "a.txt", MIME: "text/plain", Data: []byte("x"),
		Scope: ScopeAgent, AgentID: "agt-1", AgentSlug: "scout",
		Source: SourceUser,
	})
	require.NoError(t, err)
	agentPath := filepath.Join(s.AgentDir("scout"), "a.txt")
	_, err = os.Stat(agentPath)
	require.NoError(t, err)

	got, err := s.SetScope(ctx, doc.ID, ScopeGlobal, "", "")
	require.NoError(t, err)
	require.Equal(t, ScopeGlobal, got.Scope)
	require.Empty(t, got.AgentID)

	_, statErr := os.Stat(agentPath)
	require.True(t, os.IsNotExist(statErr), "the old scope must not keep the file")
	data, err := os.ReadFile(filepath.Join(s.Roots().Global, "a.txt"))
	require.NoError(t, err)
	require.Equal(t, []byte("x"), data)

	// And back down: demote to a (different) agent.
	got, err = s.SetScope(ctx, doc.ID, ScopeAgent, "agt-2", "mapper")
	require.NoError(t, err)
	require.Equal(t, ScopeAgent, got.Scope)
	require.Equal(t, "mapper", got.AgentSlug)
	_, err = os.ReadFile(filepath.Join(s.Roots().Global, "a.txt"))
	require.True(t, os.IsNotExist(err), "global must not keep the file")
	_, err = os.Stat(filepath.Join(s.AgentDir("mapper"), "a.txt"))
	require.NoError(t, err)
}

// TestSetScope_RejectsTargetCollision pins the promote/demote uniqueness: the
// same-name document in the target scope rejects the move.
func TestSetScope_RejectsTargetCollision(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	doc, err := s.Save(ctx, SaveInput{Filename: "a.txt", MIME: "text/plain", Data: []byte("x"), Scope: ScopeGlobal, Source: SourceUser})
	require.NoError(t, err)
	_, err = s.Save(ctx, SaveInput{
		Filename: "a.txt", MIME: "text/plain", Data: []byte("y"),
		Scope: ScopeAgent, AgentID: "agt-1", AgentSlug: "scout",
		Source: SourceUser,
	})
	require.NoError(t, err)

	_, err = s.SetScope(ctx, doc.ID, ScopeAgent, "agt-1", "scout")
	require.ErrorIs(t, err, store.ErrDuplicate)

	// Both files are untouched by the rejected move.
	data, err := os.ReadFile(filepath.Join(s.Roots().Global, "a.txt"))
	require.NoError(t, err)
	require.Equal(t, []byte("x"), data)
	data, err = os.ReadFile(filepath.Join(s.AgentDir("scout"), "a.txt"))
	require.NoError(t, err)
	require.Equal(t, []byte("y"), data)
}

// TestSetScope_RowFailureMovesFileBack pins the compensating move: a failing
// catalog update leaves the file at its original scope location (D6: a
// cataloged document always has its file at its scope's location).
func TestSetScope_RowFailureMovesFileBack(t *testing.T) {
	st := fake.New()
	inner := st.Documents()
	s := NewStore(t.TempDir(), inner)
	ctx := context.Background()

	doc, err := s.Save(ctx, SaveInput{
		Filename: "a.txt", MIME: "text/plain", Data: []byte("x"),
		Scope: ScopeAgent, AgentID: "agt-1", AgentSlug: "scout",
		Source: SourceUser,
	})
	require.NoError(t, err)

	failing := NewStore(s.root, failingScopeStore{DocumentStore: inner, err: errors.New("catalog down")})
	_, err = failing.SetScope(ctx, doc.ID, ScopeGlobal, "", "")
	require.Error(t, err)

	_, statErr := os.Stat(filepath.Join(s.AgentDir("scout"), "a.txt"))
	require.NoError(t, statErr, "the file must move back to its original scope")
	_, statErr = os.Stat(filepath.Join(s.Roots().Global, "a.txt"))
	require.True(t, os.IsNotExist(statErr), "the target scope must not keep the file")

	got, err := s.Get(ctx, doc.ID)
	require.NoError(t, err)
	require.Equal(t, ScopeAgent, got.Scope, "the row still names the original scope")
}

// failingScopeStore fails exactly UpdateDocumentScope, delegating everything
// else.
type failingScopeStore struct {
	store.DocumentStore
	err error
}

func (f failingScopeStore) UpdateDocumentScope(_ context.Context, _, _, _, _ string) error {
	return f.err
}

// TestDelete_RemovesRowAndFile pins the delete path: the row goes, the file
// goes, and an unknown id wraps store.ErrNotFound.
func TestDelete_RemovesRowAndFile(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	doc, err := s.Save(ctx, SaveInput{Filename: "a.txt", MIME: "text/plain", Data: []byte("x"), Scope: ScopeGlobal, Source: SourceUser})
	require.NoError(t, err)

	require.NoError(t, s.Delete(ctx, doc.ID))
	docs, err := s.List(ctx)
	require.NoError(t, err)
	require.Empty(t, docs)
	_, statErr := os.Stat(filepath.Join(s.Roots().Global, "a.txt"))
	require.True(t, os.IsNotExist(statErr))

	err = s.Delete(ctx, "ffffffffffffffffffffffffffffffff")
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestIndex_RegexeneratedOnEveryMutation pins D11: INDEX.md lists the scope's
// documents (filename, description, size, sorted by filename), regenerates on
// every mutation touching the scope, and is never part of List.
func TestIndex_RegexeneratedOnEveryMutation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	indexPath := filepath.Join(s.Roots().Global, IndexFileName)

	first, err := s.Save(ctx, SaveInput{
		Filename: "b.txt", MIME: "text/plain", Data: []byte("bb"),
		Scope: ScopeGlobal, Description: "second doc", Source: SourceUser,
	})
	require.NoError(t, err)
	second, err := s.Save(ctx, SaveInput{Filename: "a.txt", MIME: "text/plain", Data: []byte("c"), Scope: ScopeGlobal, Source: SourceUser})
	require.NoError(t, err)

	raw, err := os.ReadFile(indexPath)
	require.NoError(t, err)
	require.Equal(t, "# Knowledge index\n- a.txt (1 bytes)\n- b.txt — second doc (2 bytes)\n", string(raw),
		"the index lists each document sorted by filename with description and size")

	// Description edit regenerates.
	_, err = s.SetDescription(ctx, second.ID, "first doc")
	require.NoError(t, err)
	raw, err = os.ReadFile(indexPath)
	require.NoError(t, err)
	require.Equal(t, "# Knowledge index\n- a.txt — first doc (1 bytes)\n- b.txt — second doc (2 bytes)\n", string(raw))

	// Scope change regenerates both scopes: the entry leaves global.
	_, err = s.SetScope(ctx, second.ID, ScopeAgent, "agt-1", "scout")
	require.NoError(t, err)
	raw, err = os.ReadFile(indexPath)
	require.NoError(t, err)
	require.Equal(t, "# Knowledge index\n- b.txt — second doc (2 bytes)\n", string(raw))
	agentRaw, err := os.ReadFile(filepath.Join(s.AgentDir("scout"), IndexFileName))
	require.NoError(t, err)
	require.Equal(t, "# Knowledge index\n- a.txt — first doc (1 bytes)\n", string(agentRaw))

	// Delete regenerates: with both documents gone the scope's index carries
	// only the header.
	require.NoError(t, s.Delete(ctx, second.ID))
	require.NoError(t, s.Delete(ctx, first.ID))
	raw, err = os.ReadFile(indexPath)
	require.NoError(t, err)
	require.Equal(t, "# Knowledge index\n", string(raw), "an empty scope's index carries only the header")

	// The index is never a catalog row (D11).
	docs, err := s.List(ctx)
	require.NoError(t, err)
	for _, doc := range docs {
		require.NotEqual(t, IndexFileName, doc.Filename, "INDEX.md must never be listed")
	}
}

// TestRemoveAgentScope_RemovesDir covers the agent-deletion sweep: the
// agent's scope directory (rows already cascaded by the agent row delete) is
// removed.
func TestRemoveAgentScope_RemovesDir(t *testing.T) {
	st := fake.New()
	s := NewStore(t.TempDir(), st.Documents())
	ctx := context.Background()

	_, err := s.Save(ctx, SaveInput{
		Filename: "a.txt", MIME: "text/plain", Data: []byte("x"),
		Scope: ScopeAgent, AgentID: "agt-1", AgentSlug: "scout",
		Source: SourceUser,
	})
	require.NoError(t, err)
	agent := &store.Agent{ID: "agt-1", Slug: "scout"}
	require.NoError(t, st.Agents().Create(ctx, agent))

	// Simulate the FK cascade first: the agent row delete removes the rows.
	require.NoError(t, st.Agents().Delete(ctx, agent.ID))
	require.NoError(t, s.RemoveAgentScope(ctx, "scout"))

	_, statErr := os.Stat(s.AgentDir("scout"))
	require.True(t, os.IsNotExist(statErr), "the agent's scope directory must go with the agent")
}
