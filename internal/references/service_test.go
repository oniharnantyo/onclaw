package references_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/go-pdf/fpdf"
	"github.com/xuri/excelize/v2"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/references"
	"github.com/oniharnantyo/onclaw/internal/storage"
	storagefake "github.com/oniharnantyo/onclaw/internal/storage/fake"
	refstore "github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// ---------------------------------------------------------------------------
// Harness and fixtures
// ---------------------------------------------------------------------------

// harness is the wired service over the fake stores, with the seeded ids the
// tests attach against. stor is the instance-default local driver the
// scripted resolver falls back to for unconfigured workspaces; res is the
// fake workspace-storage resolver the storage-routing tests script.
type harness struct {
	ctx         context.Context
	svc         *references.Service
	st          refstore.Store
	stor        storage.Storage
	res         *scriptedResolver
	wsID        string
	userID      string
	atlasID     string
	beaconID    string
	incidentsID string
	opsID       string
}

func newHarness(t *testing.T, slug string) harness {
	t.Helper()
	s := storefake.New()
	stor := storagefake.New()
	res := newScriptedResolver(stor)
	ctx := context.Background()

	ws := &domain.Workspace{Slug: slug, Name: "WS " + slug}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	user := &domain.User{Email: slug + "-uploader@example.com", Name: "Uploader"}
	if err := s.Users().Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	atlas := &domain.Agent{WorkspaceID: ws.ID, Slug: "atlas", Name: "Atlas"}
	if err := s.Agents().Create(ctx, atlas); err != nil {
		t.Fatalf("create agent atlas: %v", err)
	}
	beacon := &domain.Agent{WorkspaceID: ws.ID, Slug: "beacon", Name: "Beacon"}
	if err := s.Agents().Create(ctx, beacon); err != nil {
		t.Fatalf("create agent beacon: %v", err)
	}
	incidents := &domain.Channel{WorkspaceID: ws.ID, Slug: "incidents", Name: "#incidents"}
	if err := s.Channels().CreateChannel(ctx, incidents); err != nil {
		t.Fatalf("create channel incidents: %v", err)
	}
	ops := &domain.Channel{WorkspaceID: ws.ID, Slug: "ops", Name: "#ops"}
	if err := s.Channels().CreateChannel(ctx, ops); err != nil {
		t.Fatalf("create channel ops: %v", err)
	}

	return harness{
		ctx:         ctx,
		svc:         references.NewService(s, res),
		st:          s,
		stor:        stor,
		res:         res,
		wsID:        ws.ID,
		userID:      user.ID,
		atlasID:     atlas.ID,
		beaconID:    beacon.ID,
		incidentsID: incidents.ID,
		opsID:       ops.ID,
	}
}

const mdRunbook = "# Setup\nInstall steps go here.\n\n# Webhooks\nSigning requires the secret token.\n"

const mdRunbookV2 = "# Deployment\nRollout goes to staging first.\n"

// pdfBytes builds a multi-page PDF with fpdf, one optional text line per page.
func pdfBytes(t *testing.T, pages ...string) []byte {
	t.Helper()
	doc := fpdf.New("P", "mm", "A4", "")
	for _, text := range pages {
		doc.AddPage()
		if text != "" {
			doc.SetFont("helvetica", "", 14)
			doc.Text(10, 20, text)
		}
	}
	var buf bytes.Buffer
	if err := doc.Output(&buf); err != nil {
		t.Fatalf("build pdf fixture: %v", err)
	}
	return buf.Bytes()
}

// xlsxBytes builds a two-sheet workbook with excelize.
func xlsxBytes(t *testing.T) []byte {
	t.Helper()
	f := excelize.NewFile()
	if err := f.SetCellValue("Sheet1", "A1", "region"); err != nil {
		t.Fatalf("set cell: %v", err)
	}
	if err := f.SetCellValue("Sheet1", "B1", "sales"); err != nil {
		t.Fatalf("set cell: %v", err)
	}
	if err := f.SetCellValue("Sheet1", "A2", "north"); err != nil {
		t.Fatalf("set cell: %v", err)
	}
	if err := f.SetCellValue("Sheet1", "B2", "42"); err != nil {
		t.Fatalf("set cell: %v", err)
	}
	if _, err := f.NewSheet("Notes"); err != nil {
		t.Fatalf("new sheet: %v", err)
	}
	if err := f.SetCellValue("Notes", "A1", "remember"); err != nil {
		t.Fatalf("set cell: %v", err)
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("write xlsx: %v", err)
	}
	return buf.Bytes()
}

// upload is the one-call upload the happy paths share, failing the test on
// any error.
func upload(t *testing.T, h harness, in references.UploadInput) domain.ReferenceDocument {
	t.Helper()
	doc, err := h.svc.Upload(h.ctx, h.wsID, h.userID, in)
	if err != nil {
		t.Fatalf("upload %q: %v", in.Filename, err)
	}
	return doc
}

// searchHits runs a scope-filtered search and fails the test on error.
func searchHits(t *testing.T, h harness, scope domain.DocumentRunScope, query string, limit int) []domain.DocumentSectionHit {
	t.Helper()
	hits, err := h.svc.SearchDocuments(h.ctx, h.wsID, scope, query, limit)
	if err != nil {
		t.Fatalf("search %q: %v", query, err)
	}
	return hits
}

// blobBytes loads stored bytes through the fake storage; the error is the
// storage port's (domain.ErrNotFound when the blob is gone).
func blobBytes(t *testing.T, h harness, key string) ([]byte, error) {
	t.Helper()
	f, err := h.stor.Open(context.Background(), key)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// sameIDSet compares id slices order-insensitively.
func sameIDSet(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("id set mismatch: got %v, want %v", got, want)
	}
	seen := make(map[string]int, len(want))
	for _, id := range got {
		seen[id]++
	}
	for _, id := range want {
		seen[id]--
		if seen[id] < 0 {
			t.Fatalf("id set mismatch: got %v, want %v", got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Upload happy paths (tasks 3.3)
// ---------------------------------------------------------------------------

func TestUploadMarkdownPassthrough(t *testing.T) {
	h := newHarness(t, "ref-md")

	doc := upload(t, h, references.UploadInput{
		Filename: "runbook.md",
		Data:     []byte(mdRunbook),
		AgentIDs: []string{h.atlasID},
	})
	if doc.Name != "runbook.md" {
		t.Errorf("Name = %q, want runbook.md (filename base default)", doc.Name)
	}
	if doc.MimeType != "text/markdown" {
		t.Errorf("MimeType = %q, want text/markdown", doc.MimeType)
	}
	if doc.IndexStatus != domain.RefDocIndexReady {
		t.Errorf("IndexStatus = %q, want ready", doc.IndexStatus)
	}
	if doc.PageCount != 0 {
		t.Errorf("PageCount = %d, want 0 for markdown", doc.PageCount)
	}
	if doc.Scope != domain.RefDocScopeAttached {
		t.Errorf("Scope = %q, want the attached default", doc.Scope)
	}
	if len(doc.AgentIDs) != 1 || doc.AgentIDs[0] != h.atlasID {
		t.Errorf("AgentIDs = %v, want the uploaded attach list", doc.AgentIDs)
	}

	// The blob is stored as-is (D1).
	stored, err := blobBytes(t, h, doc.StorageKey)
	if err != nil {
		t.Fatalf("open stored blob: %v", err)
	}
	if !bytes.Equal(stored, []byte(mdRunbook)) {
		t.Errorf("stored blob differs from the uploaded bytes")
	}

	hits := searchHits(t, h, domain.DocumentRunScope{AgentID: h.atlasID}, "install", 0)
	if len(hits) != 1 {
		t.Fatalf("search hits = %d, want 1", len(hits))
	}
	hit := hits[0]
	if hit.DocumentID != doc.ID || hit.DocumentName != "runbook.md" {
		t.Errorf("hit document = %s/%q, want %s/runbook.md", hit.DocumentID, hit.DocumentName, doc.ID)
	}
	if hit.Heading != "Setup" || hit.Locator != "Setup" || hit.LocatorKind != domain.LocatorKindHeading {
		t.Errorf("hit locator = %s/%s/%s, want Setup/Setup/heading", hit.Heading, hit.Locator, hit.LocatorKind)
	}
	if !strings.Contains(hit.Snippet, "Install steps") {
		t.Errorf("Snippet = %q, want it to contain the section body", hit.Snippet)
	}
}

func TestUploadPDFSectionsCarryPageLocators(t *testing.T) {
	h := newHarness(t, "ref-pdf")

	doc := upload(t, h, references.UploadInput{
		Filename:    "manual.pdf",
		Description: "The service manual",
		Data: pdfBytes(t,
			"Alpha intro",
			"# Beta topic\nBeta details."),
		AgentIDs: []string{h.atlasID},
	})
	if doc.MimeType != "application/pdf" {
		t.Errorf("MimeType = %q, want application/pdf", doc.MimeType)
	}
	if doc.IndexStatus != domain.RefDocIndexReady {
		t.Errorf("IndexStatus = %q, want ready", doc.IndexStatus)
	}
	if doc.PageCount != 2 {
		t.Errorf("PageCount = %d, want 2", doc.PageCount)
	}
	if doc.Description != "The service manual" {
		t.Errorf("Description = %q, want the manifest description", doc.Description)
	}

	hits := searchHits(t, h, domain.DocumentRunScope{AgentID: h.atlasID}, "alpha", 0)
	if len(hits) != 1 {
		t.Fatalf("alpha hits = %d, want 1", len(hits))
	}
	if hits[0].Heading != "Page 1" || hits[0].Locator != "p. 1" || hits[0].LocatorKind != domain.LocatorKindPage {
		t.Errorf("alpha hit = %s/%s/%s, want Page 1/p. 1/page", hits[0].Heading, hits[0].Locator, hits[0].LocatorKind)
	}

	hits = searchHits(t, h, domain.DocumentRunScope{AgentID: h.atlasID}, "beta", 0)
	if len(hits) != 1 {
		t.Fatalf("beta hits = %d, want 1", len(hits))
	}
	if hits[0].Heading != "Beta topic" || hits[0].Locator != "p. 2" || hits[0].LocatorKind != domain.LocatorKindPage {
		t.Errorf("beta hit = %s/%s/%s, want Beta topic/p. 2/page", hits[0].Heading, hits[0].Locator, hits[0].LocatorKind)
	}
}

func TestUploadXLSXSheetsAreSections(t *testing.T) {
	h := newHarness(t, "ref-xlsx")

	doc := upload(t, h, references.UploadInput{
		Filename: "metrics.xlsx",
		Data:     xlsxBytes(t),
		AgentIDs: []string{h.atlasID},
	})
	if doc.MimeType != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
		t.Errorf("MimeType = %q, want the xlsx office mime", doc.MimeType)
	}
	if doc.IndexStatus != domain.RefDocIndexReady {
		t.Errorf("IndexStatus = %q, want ready", doc.IndexStatus)
	}

	hits := searchHits(t, h, domain.DocumentRunScope{AgentID: h.atlasID}, "remember", 0)
	if len(hits) != 1 {
		t.Fatalf("remember hits = %d, want 1", len(hits))
	}
	if hits[0].Heading != "Notes" || hits[0].Locator != "sheet Notes" || hits[0].LocatorKind != domain.LocatorKindSheet {
		t.Errorf("Notes hit = %s/%s/%s, want Notes/sheet Notes/sheet", hits[0].Heading, hits[0].Locator, hits[0].LocatorKind)
	}

	hits = searchHits(t, h, domain.DocumentRunScope{AgentID: h.atlasID}, "north", 0)
	if len(hits) != 1 || hits[0].Locator != "sheet Sheet1" {
		t.Fatalf("north hits = %+v, want one hit on sheet Sheet1", hits)
	}
}

func TestUploadTXTSingleSection(t *testing.T) {
	h := newHarness(t, "ref-txt")

	doc := upload(t, h, references.UploadInput{
		Filename: "integration.txt",
		Data:     []byte("plain integration note about the sandbox rate limit"),
		AgentIDs: []string{h.atlasID},
	})
	if doc.MimeType != "text/plain" {
		t.Errorf("MimeType = %q, want text/plain", doc.MimeType)
	}

	hits := searchHits(t, h, domain.DocumentRunScope{AgentID: h.atlasID}, "sandbox", 0)
	if len(hits) != 1 {
		t.Fatalf("sandbox hits = %d, want 1 (unstructured text stays searchable)", len(hits))
	}
	if hits[0].Locator != "" || hits[0].LocatorKind != domain.LocatorKindNone {
		t.Errorf("txt locator = %q/%q, want none", hits[0].Locator, hits[0].LocatorKind)
	}
	if hits[0].Heading != "integration.txt" {
		t.Errorf("txt heading = %q, want the filename base as the synthetic heading", hits[0].Heading)
	}
}

func TestUploadScannedPDFSurfacesNoTextLayer(t *testing.T) {
	h := newHarness(t, "ref-scan")

	// A PDF page with no text: upload succeeds, index stays empty, status
	// names the limitation (spec scenario).
	doc := upload(t, h, references.UploadInput{
		Filename: "scan.pdf",
		Data:     pdfBytes(t, ""),
		AgentIDs: []string{h.atlasID},
	})
	if doc.IndexStatus != domain.RefDocIndexNoTextLayer {
		t.Fatalf("IndexStatus = %q, want no_text_layer", doc.IndexStatus)
	}
	if doc.PageCount != 1 {
		t.Errorf("PageCount = %d, want 1", doc.PageCount)
	}
	hits, err := h.st.DocumentSections().Search(h.ctx, h.wsID, []string{doc.ID}, "anything", 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("scanned pdf hits = %d, want 0 (index stays empty)", len(hits))
	}

	// Rebuild from the blob alone lands the same classification.
	if err := h.svc.RebuildIndex(h.ctx, h.wsID, doc.ID); err != nil {
		t.Fatalf("RebuildIndex: %v", err)
	}
	got, err := h.svc.Get(h.ctx, h.wsID, doc.ID)
	if err != nil {
		t.Fatalf("get after rebuild: %v", err)
	}
	if got.IndexStatus != domain.RefDocIndexNoTextLayer {
		t.Errorf("IndexStatus after rebuild = %q, want no_text_layer", got.IndexStatus)
	}
}

// ---------------------------------------------------------------------------
// Upload rejection lane
// ---------------------------------------------------------------------------

func TestUploadRejectsExecutableRenamedPDF(t *testing.T) {
	h := newHarness(t, "ref-exe")

	// MZ header (PE executable) renamed to manual.pdf: magic-byte sniffing
	// identifies the real type (spec scenario).
	data := append([]byte("MZ"), bytes.Repeat([]byte("A"), 600)...)
	_, err := h.svc.Upload(h.ctx, h.wsID, h.userID, references.UploadInput{Filename: "manual.pdf", Data: data})
	if !errors.Is(err, references.ErrUnsupportedType) {
		t.Fatalf("executable renamed .pdf: expected ErrUnsupportedType, got %v", err)
	}
	if !strings.Contains(err.Error(), "pdf, docx, pptx, xlsx") {
		t.Errorf("rejection message = %q, want the allowed-type list", err.Error())
	}
}

func TestUploadRejectsLegacyDocWithGuidance(t *testing.T) {
	h := newHarness(t, "ref-legacy")

	// By extension.
	_, err := h.svc.Upload(h.ctx, h.wsID, h.userID, references.UploadInput{Filename: "manual.doc", Data: []byte("whatever")})
	if !errors.Is(err, references.ErrUnsupportedType) {
		t.Fatalf(".doc upload: expected ErrUnsupportedType, got %v", err)
	}
	if !strings.Contains(err.Error(), ".docx") || !strings.Contains(err.Error(), "PDF") {
		t.Errorf(".doc guidance = %q, want the convert-to-modern-format message", err.Error())
	}

	// By OLE magic under a modern name — the sniff beats the claim.
	ole := append([]byte{0xD0, 0xCF, 0x11, 0xE0}, bytes.Repeat([]byte{0xA5}, 600)...)
	_, err = h.svc.Upload(h.ctx, h.wsID, h.userID, references.UploadInput{Filename: "renamed.pptx", Data: ole})
	if !errors.Is(err, references.ErrUnsupportedType) {
		t.Fatalf("OLE magic renamed .pptx: expected ErrUnsupportedType, got %v", err)
	}
	if !strings.Contains(err.Error(), "convert") {
		t.Errorf("OLE guidance = %q, want conversion guidance", err.Error())
	}

	_, err = h.svc.Upload(h.ctx, h.wsID, h.userID, references.UploadInput{Filename: "deck.ppt", Data: []byte("slides")})
	if !errors.Is(err, references.ErrUnsupportedType) || !strings.Contains(err.Error(), ".pptx") {
		t.Fatalf(".ppt upload = %v, want ErrUnsupportedType with .pptx guidance", err)
	}
}

func TestUploadRejectsOversize(t *testing.T) {
	h := newHarness(t, "ref-oversize")

	// Text over the drop-lane cap.
	bigText := bytes.Repeat([]byte("a"), references.MaxReferenceFileBytes+1)
	_, err := h.svc.Upload(h.ctx, h.wsID, h.userID, references.UploadInput{Filename: "big.md", Data: bigText})
	if !errors.Is(err, references.ErrTooLarge) {
		t.Fatalf("oversize md: expected ErrTooLarge, got %v", err)
	}
	if !errors.Is(err, domain.ErrPayloadTooLarge) {
		t.Errorf("oversize md: expected the error chain to reach domain.ErrPayloadTooLarge for the 413 mapping, got %v", err)
	}

	// PDF over its own cap.
	pdf := pdfBytes(t, "tiny")
	bigPDF := make([]byte, references.MaxReferencePDFBytes+1)
	copy(bigPDF, pdf)
	_, err = h.svc.Upload(h.ctx, h.wsID, h.userID, references.UploadInput{Filename: "big.pdf", Data: bigPDF})
	if !errors.Is(err, references.ErrTooLarge) {
		t.Fatalf("oversize pdf: expected ErrTooLarge, got %v", err)
	}
}

func TestUploadValidatesAttachLists(t *testing.T) {
	h := newHarness(t, "ref-attach")

	_, err := h.svc.Upload(h.ctx, h.wsID, h.userID, references.UploadInput{
		Filename: "runbook.md",
		Data:     []byte(mdRunbook),
		AgentIDs: []string{"no-such-agent"},
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown agent id: expected ErrNotFound, got %v", err)
	}

	_, err = h.svc.Upload(h.ctx, h.wsID, h.userID, references.UploadInput{
		Filename:   "runbook.md",
		Data:       []byte(mdRunbook),
		ChannelIDs: []string{"no-such-channel"},
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown channel id: expected ErrNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tiered visibility (D7)
// ---------------------------------------------------------------------------

func TestVisibleDocumentsAcrossScopeBranches(t *testing.T) {
	h := newHarness(t, "ref-visible")

	a := upload(t, h, references.UploadInput{
		Filename: "a.md", Data: []byte(mdRunbook), AgentIDs: []string{h.atlasID}, ChannelIDs: []string{h.incidentsID},
	})
	b := upload(t, h, references.UploadInput{Filename: "b.md", Data: []byte(mdRunbook)})
	b, err := h.svc.Promote(h.ctx, h.wsID, b.ID)
	if err != nil {
		t.Fatalf("promote b: %v", err)
	}
	c := upload(t, h, references.UploadInput{
		Filename: "c.md", Data: []byte(mdRunbook), AgentIDs: []string{h.beaconID},
	})
	d := upload(t, h, references.UploadInput{
		Filename: "d.md", Data: []byte(mdRunbook), ChannelIDs: []string{h.opsID},
	})

	docIDs := func(docs []domain.ReferenceDocument) []string {
		ids := make([]string, 0, len(docs))
		for _, doc := range docs {
			ids = append(ids, doc.ID)
		}
		return ids
	}

	for _, tc := range []struct {
		name  string
		scope domain.DocumentRunScope
		want  []string
	}{
		{"direct chat by agent attachment", domain.DocumentRunScope{AgentID: h.atlasID}, []string{a.ID, b.ID}},
		{"other agent", domain.DocumentRunScope{AgentID: h.beaconID}, []string{b.ID, c.ID}},
		{"channel session", domain.DocumentRunScope{IsChannelSession: true, ChannelID: h.incidentsID}, []string{a.ID, b.ID}},
		{"other channel", domain.DocumentRunScope{IsChannelSession: true, ChannelID: h.opsID}, []string{b.ID, d.ID}},
		{"agent inside a channel session", domain.DocumentRunScope{AgentID: h.beaconID, IsChannelSession: true, ChannelID: h.incidentsID}, []string{a.ID, b.ID, c.ID}},
		// Scheduler rule: the run resolves by agent bindings only — the
		// delivery channel never widens scope.
		{"scheduled run with channel delivery target", domain.DocumentRunScope{AgentID: h.atlasID, ChannelID: h.incidentsID, IsChannelSession: false}, []string{a.ID, b.ID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := h.svc.VisibleDocuments(h.ctx, h.wsID, tc.scope)
			if err != nil {
				t.Fatalf("VisibleDocuments: %v", err)
			}
			sameIDSet(t, docIDs(got), tc.want)
		})
	}
}

func TestSearchDocumentsFiltersByVisibility(t *testing.T) {
	h := newHarness(t, "ref-search")

	runbook := upload(t, h, references.UploadInput{
		Filename: "runbook.md", Data: []byte(mdRunbook), AgentIDs: []string{h.atlasID},
	})
	limits := upload(t, h, references.UploadInput{
		Filename: "limits.txt", Data: []byte("the sandbox rate limits are 60 per minute"),
	})
	limits, err := h.svc.Promote(h.ctx, h.wsID, limits.ID)
	if err != nil {
		t.Fatalf("promote limits: %v", err)
	}

	// Atlas sees the agent-attached runbook; Beacon must not.
	hits := searchHits(t, h, domain.DocumentRunScope{AgentID: h.atlasID}, "signing", 0)
	if len(hits) != 1 || hits[0].DocumentID != runbook.ID {
		t.Fatalf("atlas signing hits = %+v, want the runbook", hits)
	}
	if hits := searchHits(t, h, domain.DocumentRunScope{AgentID: h.beaconID}, "signing", 0); len(hits) != 0 {
		t.Fatalf("beacon signing hits = %+v, want none", hits)
	}

	// The promoted document is visible to every agent's search.
	hits = searchHits(t, h, domain.DocumentRunScope{AgentID: h.beaconID}, "limits", 0)
	if len(hits) != 1 || hits[0].DocumentID != limits.ID {
		t.Fatalf("beacon limits hits = %+v, want the promoted document", hits)
	}
}

func TestSearchDocumentsEmptyVisibleShortCircuits(t *testing.T) {
	h := newHarness(t, "ref-search-empty")

	// One agent-attached document, nothing promoted: the ghost agent's scope
	// resolves an empty visible set.
	upload(t, h, references.UploadInput{
		Filename: "runbook.md", Data: []byte(mdRunbook), AgentIDs: []string{h.atlasID},
	})

	hits, err := h.svc.SearchDocuments(h.ctx, h.wsID, domain.DocumentRunScope{AgentID: "ghost"}, "", 0)
	if err != nil {
		t.Fatalf("empty-visible search: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("empty-visible search hits = %d, want 0", len(hits))
	}
}

// ---------------------------------------------------------------------------
// Lifecycle: promote/demote, meta, replace, delete, rebuild
// ---------------------------------------------------------------------------

func TestPromoteDemoteAndUpdateMeta(t *testing.T) {
	h := newHarness(t, "ref-promote")

	doc := upload(t, h, references.UploadInput{
		Filename: "runbook.md", Data: []byte(mdRunbook), AgentIDs: []string{h.atlasID},
	})

	got, err := h.svc.Promote(h.ctx, h.wsID, doc.ID)
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if got.Scope != domain.RefDocScopeWorkspace {
		t.Errorf("scope after promote = %q, want workspace", got.Scope)
	}

	got, err = h.svc.Demote(h.ctx, h.wsID, doc.ID)
	if err != nil {
		t.Fatalf("demote: %v", err)
	}
	if got.Scope != domain.RefDocScopeAttached {
		t.Errorf("scope after demote = %q, want attached", got.Scope)
	}

	got, err = h.svc.UpdateMeta(h.ctx, h.wsID, doc.ID, "Runbook renamed", "the one-line description")
	if err != nil {
		t.Fatalf("update meta: %v", err)
	}
	if got.Name != "Runbook renamed" || got.Description != "the one-line description" {
		t.Errorf("meta = %q/%q, want the edited values", got.Name, got.Description)
	}

	got, err = h.svc.SetAgents(h.ctx, h.wsID, doc.ID, []string{h.beaconID})
	if err != nil {
		t.Fatalf("set agents: %v", err)
	}
	if len(got.AgentIDs) != 1 || got.AgentIDs[0] != h.beaconID {
		t.Errorf("agents = %v, want the replaced set", got.AgentIDs)
	}

	// Cross-workspace id resolves not-found, indistinguishably from unknown.
	if _, err := h.svc.Get(h.ctx, "other-workspace", doc.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("foreign workspace get = %v, want ErrNotFound", err)
	}
}

func TestReplaceSwapsBlobAndRebuildsSections(t *testing.T) {
	h := newHarness(t, "ref-replace")

	v1 := upload(t, h, references.UploadInput{
		Filename: "runbook.md", Data: []byte(mdRunbook),
		AgentIDs:   []string{h.atlasID},
		ChannelIDs: []string{h.incidentsID},
	})
	oldKey := v1.StorageKey

	got, err := h.svc.Replace(h.ctx, h.wsID, v1.ID, references.UploadInput{Filename: "runbook.md", Data: []byte(mdRunbookV2)})
	if err != nil {
		t.Fatalf("replace: %v", err)
	}

	// Meta, scope, and attach lists are left as stored (blob-only swap).
	if got.Name != "runbook.md" || got.Scope != domain.RefDocScopeAttached {
		t.Errorf("meta after replace = %q/%q, want runbook.md/attached", got.Name, got.Scope)
	}
	if len(got.AgentIDs) != 1 || got.AgentIDs[0] != h.atlasID || len(got.ChannelIDs) != 1 || got.ChannelIDs[0] != h.incidentsID {
		t.Errorf("attach lists after replace = %v/%v, want the stored sets", got.AgentIDs, got.ChannelIDs)
	}
	// The blob is swapped.
	if got.StorageKey == oldKey {
		t.Errorf("storage key unchanged after replace")
	}
	if got.SizeBytes != int64(len(mdRunbookV2)) {
		t.Errorf("SizeBytes = %d, want %d", got.SizeBytes, len(mdRunbookV2))
	}
	if _, err := blobBytes(t, h, oldKey); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("old blob still in storage: %v, want gone", err)
	}

	// No stale sections from the previous edition; the new content is
	// searchable (spec scenario: replace rebuilds derived state).
	if hits := searchHits(t, h, domain.DocumentRunScope{AgentID: h.atlasID}, "webhooks", 0); len(hits) != 0 {
		t.Errorf("stale sections survived replace: %+v", hits)
	}
	hits := searchHits(t, h, domain.DocumentRunScope{AgentID: h.atlasID}, "staging", 0)
	if len(hits) != 1 || hits[0].DocumentID != v1.ID {
		t.Fatalf("staging hits = %+v, want the rebuilt Deployment section", hits)
	}
}

func TestDeleteCascades(t *testing.T) {
	h := newHarness(t, "ref-delete")

	doc := upload(t, h, references.UploadInput{
		Filename: "runbook.md", Data: []byte(mdRunbook),
		AgentIDs:   []string{h.atlasID},
		ChannelIDs: []string{h.incidentsID},
	})
	key := doc.StorageKey

	if err := h.svc.Delete(h.ctx, h.wsID, doc.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := h.svc.Get(h.ctx, h.wsID, doc.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("get after delete = %v, want ErrNotFound", err)
	}
	if _, err := blobBytes(t, h, key); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("blob survived delete: %v, want gone", err)
	}
	hits, err := h.st.DocumentSections().Search(h.ctx, h.wsID, []string{doc.ID}, "webhooks", 0)
	if err != nil {
		t.Fatalf("search after delete: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("sections survived delete: %+v", hits)
	}
	if err := h.svc.Delete(h.ctx, h.wsID, doc.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("second delete = %v, want ErrNotFound", err)
	}
}

func TestRebuildIndexRecoversFromMidRebuildFailure(t *testing.T) {
	h := newHarness(t, "ref-rebuild")

	doc := upload(t, h, references.UploadInput{
		Filename: "runbook.md", Data: []byte(mdRunbook), AgentIDs: []string{h.atlasID},
	})

	// Simulate the failure-mid-rebuild state: the section index is gone
	// while the row and blob survive.
	if err := h.st.DocumentSections().DeleteForDocument(h.ctx, h.wsID, doc.ID); err != nil {
		t.Fatalf("wipe sections: %v", err)
	}
	if hits := searchHits(t, h, domain.DocumentRunScope{AgentID: h.atlasID}, "webhooks", 0); len(hits) != 0 {
		t.Fatalf("sections survived the wipe: %+v", hits)
	}

	if err := h.svc.RebuildIndex(h.ctx, h.wsID, doc.ID); err != nil {
		t.Fatalf("RebuildIndex: %v", err)
	}
	hits := searchHits(t, h, domain.DocumentRunScope{AgentID: h.atlasID}, "webhooks", 0)
	if len(hits) != 1 || hits[0].Heading != "Webhooks" {
		t.Fatalf("post-rebuild hits = %+v, want the rebuilt Webhooks section", hits)
	}
	got, err := h.svc.Get(h.ctx, h.wsID, doc.ID)
	if err != nil {
		t.Fatalf("get after rebuild: %v", err)
	}
	if got.IndexStatus != domain.RefDocIndexReady {
		t.Errorf("IndexStatus after rebuild = %q, want ready", got.IndexStatus)
	}
}

// ---------------------------------------------------------------------------
// Capability URL and the run-scoped mount (D8)
// ---------------------------------------------------------------------------

func TestMountDirName(t *testing.T) {
	if references.MountDirName != "references" {
		t.Fatalf("MountDirName = %q, want references", references.MountDirName)
	}
}

// ---------------------------------------------------------------------------
// Table of contents (D6 manifest projection)
// ---------------------------------------------------------------------------

func TestTableOfContentsTopLevelHeadings(t *testing.T) {
	h := newHarness(t, "ref-toc")

	doc := upload(t, h, references.UploadInput{
		Filename: "manual.md",
		Data: []byte(
			"# Intro\nintro body\n\n" +
				"# Setup\nsetup body\n\n" +
				"## Setup details\ndetail body\n\n" +
				"### Deep dive\ndeep body\n\n" +
				"# Setup\nrepeated heading\n\n" +
				"# Webhooks\nwebhook body\n"),
		AgentIDs: []string{h.atlasID},
	})

	// Distinct Level<=1 headings, ordinal order: "Setup" appears twice and is
	// kept once; Level 2 ("Setup details") and Level 3 ("Deep dive") are
	// excluded.
	toc, err := h.svc.TableOfContents(h.ctx, h.wsID, doc.ID, 6)
	if err != nil {
		t.Fatalf("TableOfContents: %v", err)
	}
	want := []string{"Intro", "Setup", "Webhooks"}
	if len(toc) != len(want) {
		t.Fatalf("toc = %v, want %v", toc, want)
	}
	for i := range want {
		if toc[i] != want[i] {
			t.Errorf("toc[%d] = %q, want %q", i, toc[i], want[i])
		}
	}

	// The cap bounds the projection.
	capped, err := h.svc.TableOfContents(h.ctx, h.wsID, doc.ID, 2)
	if err != nil {
		t.Fatalf("TableOfContents capped: %v", err)
	}
	if len(capped) != 2 || capped[0] != "Intro" || capped[1] != "Setup" {
		t.Errorf("capped toc = %v, want the first two entries", capped)
	}

	// topN <= 0 requests the uncapped projection — the manifest's convention
	// (fix-reference-document-retrieval D1): every distinct Level<=1 heading,
	// ordinal order, regardless of how many mixed-level headings precede.
	uncapped, err := h.svc.TableOfContents(h.ctx, h.wsID, doc.ID, 0)
	if err != nil {
		t.Fatalf("TableOfContents uncapped: %v", err)
	}
	if len(uncapped) != len(want) {
		t.Fatalf("uncapped toc = %v, want %v", uncapped, want)
	}
	for i := range want {
		if uncapped[i] != want[i] {
			t.Errorf("uncapped toc[%d] = %q, want %q", i, uncapped[i], want[i])
		}
	}

	// A scanned PDF's empty index projects no headings, never an error.
	scan := upload(t, h, references.UploadInput{
		Filename: "scan.pdf", Data: pdfBytes(t, ""), AgentIDs: []string{h.atlasID},
	})
	empty, err := h.svc.TableOfContents(h.ctx, h.wsID, scan.ID, 6)
	if err != nil {
		t.Fatalf("TableOfContents (no sections): %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("no-text-layer toc = %v, want empty", empty)
	}

	// A foreign id resolves not-found, indistinguishably from unknown.
	if _, err := h.svc.TableOfContents(h.ctx, "other-ws", doc.ID, 6); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("foreign toc = %v, want ErrNotFound", err)
	}
}


func TestURLShape(t *testing.T) {
	h := newHarness(t, "ref-url")

	doc := upload(t, h, references.UploadInput{
		Filename: "runbook.md", Data: []byte(mdRunbook), AgentIDs: []string{h.atlasID},
	})
	// The fake storage serves /api/v1/files/<key> — the capability URL is
	// the storage port's URL over the capability key, exactly the shape the
	// attachment upload handler returns.
	want := "/api/v1/files/" + doc.StorageKey
	if got := h.svc.URL(doc); got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}
}

func TestWriteMountCollisionsSortingAndJail(t *testing.T) {
	h := newHarness(t, "ref-mount")

	notes1 := upload(t, h, references.UploadInput{
		Filename: "first.md", Name: "notes.md", Data: []byte("# First\nfirst content"), AgentIDs: []string{h.atlasID},
	})
	notes2 := upload(t, h, references.UploadInput{
		Filename: "second.md", Name: "notes.md", Data: []byte("# Second\nsecond content"), AgentIDs: []string{h.atlasID},
	})
	api := upload(t, h, references.UploadInput{
		Filename: "api.md", Name: "api.md", Data: []byte("# API\napi content"), AgentIDs: []string{h.atlasID},
	})
	tricky := upload(t, h, references.UploadInput{
		Filename: "tricky.md", Name: "../escape.md", Data: []byte("# Tricky\ntricky content"), AgentIDs: []string{h.atlasID},
	})

	writeAndCollect := func(order []domain.ReferenceDocument) map[string][]byte {
		t.Helper()
		dir := t.TempDir()
		if err := h.svc.WriteMount(h.ctx, dir, order); err != nil {
			t.Fatalf("WriteMount: %v", err)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read mount dir: %v", err)
		}
		files := make(map[string][]byte, len(entries))
		for _, e := range entries {
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatalf("read mount file: %v", err)
			}
			files[e.Name()] = data
		}
		return files
	}

	docs := []domain.ReferenceDocument{notes1, notes2, api, tricky}
	forward := writeAndCollect(docs)
	// Reversed input order lands the identical mount: docs are sorted by
	// name before collision suffixes are assigned.
	reversed := make([]domain.ReferenceDocument, len(docs))
	for i, d := range docs {
		reversed[len(docs)-1-i] = d
	}
	backward := writeAndCollect(reversed)
	if len(forward) != len(backward) {
		t.Fatalf("mount file count = %d (forward) vs %d (reversed), want identical", len(forward), len(backward))
	}
	for name, data := range forward {
		if !bytes.Equal(data, backward[name]) {
			t.Errorf("mount not deterministic for %q", name)
		}
	}

	// Exactly the expected file set: the distinct names plus one
	// deterministic collision suffix, and the traversal-looking display name
	// stays jailed inside the mount as a single element.
	if _, ok := forward["api.md"]; !ok {
		t.Errorf("api.md missing from mount: %v", fileNames(forward))
	}
	notesFiles := make([]string, 0, 2)
	for name := range forward {
		if name == "notes.md" || name == "notes-2.md" {
			notesFiles = append(notesFiles, name)
		}
	}
	if len(notesFiles) != 2 {
		t.Errorf("collision files = %v (%v), want notes.md and notes-2.md", notesFiles, fileNames(forward))
	}
	trickyFiles := 0
	for name := range forward {
		if strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..") {
			t.Errorf("mount file %q escapes the naming contract", name)
			continue
		}
		if name == "__escape.md" {
			trickyFiles++
		}
	}
	if trickyFiles != 1 {
		t.Errorf("sanitized tricky name missing from mount: %v", fileNames(forward))
	}

	// Bytes are the originals, as-is (D1).
	if !bytes.Equal(forward["api.md"], []byte("# API\napi content")) {
		t.Errorf("api.md content differs from the stored blob")
	}
	// The two notes.md contents land on the two collision names — the
	// (name, id) sort decides which is which, and the forward/reversed
	// equality above proves that decision is deterministic.
	notesContents := []string{string(forward["notes.md"]), string(forward["notes-2.md"])}
	sort.Strings(notesContents)
	if notesContents[0] != "# First\nfirst content" || notesContents[1] != "# Second\nsecond content" {
		t.Errorf("collision contents = %q/%q, want the two uploaded blobs", notesContents[0], notesContents[1])
	}
}

func TestWriteMountEmptyDirRejected(t *testing.T) {
	h := newHarness(t, "ref-mount-empty")
	if err := h.svc.WriteMount(h.ctx, "  ", nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("empty mount dir = %v, want ErrInvalid", err)
	}
}

func fileNames(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
