package domain

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Validation (tasks 1.1)
// ---------------------------------------------------------------------------

func TestValidateReferenceDocumentName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"plain name", "Twilio API Reference", false},
		{"single char", "a", false},
		{"exactly 200 chars", strings.Repeat("n", 200), false},
		{"201 chars rejected", strings.Repeat("n", 201), true},
		{"empty rejected", "", true},
		{"whitespace-only rejected", "   ", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateReferenceDocumentName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateReferenceDocumentName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), ErrInvalid.Error()) {
				t.Errorf("expected wrapped ErrInvalid, got %v", err)
			}
		})
	}
}

func TestValidateReferenceDocumentDescription(t *testing.T) {
	if err := ValidateReferenceDocumentDescription(""); err != nil {
		t.Errorf("empty description is valid, got %v", err)
	}
	if err := ValidateReferenceDocumentDescription(strings.Repeat("d", 2000)); err != nil {
		t.Errorf("2000-char description is valid, got %v", err)
	}
	if err := ValidateReferenceDocumentDescription(strings.Repeat("d", 2001)); err == nil {
		t.Error("2001-char description must be rejected")
	}
}

func TestValidReferenceDocMime(t *testing.T) {
	allowed := []string{
		"application/pdf",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/vnd.openxmlformats-officedocument.presentationml.presentation",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"text/markdown",
		"text/plain",
		"text/html",
		"text/csv",
	}
	for _, mime := range allowed {
		if !ValidReferenceDocMime(mime) {
			t.Errorf("expected %q to be allowed", mime)
		}
	}
	rejected := []string{
		"",
		"application/msword",            // legacy .doc
		"application/vnd.ms-powerpoint", // legacy .ppt
		"application/zip",
		"application/octet-stream",
		"application/x-msdownload", // renamed executables
		"image/png",
		"application/PDF", // case-sensitive canonical forms
	}
	for _, mime := range rejected {
		if ValidReferenceDocMime(mime) {
			t.Errorf("expected %q to be rejected", mime)
		}
	}
}

func TestValidateRefDocScopeAndIndexStatus(t *testing.T) {
	for _, scope := range []string{RefDocScopeAttached, RefDocScopeWorkspace} {
		if !ValidRefDocScope(scope) {
			t.Errorf("expected scope %q to be valid", scope)
		}
		if err := ValidateRefDocScope(scope); err != nil {
			t.Errorf("ValidateRefDocScope(%q) = %v", scope, err)
		}
	}
	for _, bad := range []string{"", "promoted", "all", "workspace-wide"} {
		if ValidRefDocScope(bad) {
			t.Errorf("expected scope %q to be invalid", bad)
		}
		if err := ValidateRefDocScope(bad); err == nil {
			t.Errorf("ValidateRefDocScope(%q) must fail", bad)
		}
	}

	for _, status := range []string{RefDocIndexReady, RefDocIndexNoTextLayer, RefDocIndexProcessing} {
		if !ValidRefDocIndexStatus(status) {
			t.Errorf("expected index status %q to be valid", status)
		}
		if err := ValidateRefDocIndexStatus(status); err != nil {
			t.Errorf("ValidateRefDocIndexStatus(%q) = %v", status, err)
		}
	}
	for _, bad := range []string{"", "indexed", "failed", "pending"} {
		if ValidRefDocIndexStatus(bad) {
			t.Errorf("expected index status %q to be invalid", bad)
		}
		if err := ValidateRefDocIndexStatus(bad); err == nil {
			t.Errorf("ValidateRefDocIndexStatus(%q) must fail", bad)
		}
	}
}

func TestValidateDocumentLocatorKind(t *testing.T) {
	for _, kind := range []string{LocatorKindPage, LocatorKindSlide, LocatorKindSheet, LocatorKindHeading, LocatorKindNone} {
		if !ValidDocumentLocatorKind(kind) {
			t.Errorf("expected locator kind %q to be valid", kind)
		}
		if err := ValidateDocumentLocatorKind(kind); err != nil {
			t.Errorf("ValidateDocumentLocatorKind(%q) = %v", kind, err)
		}
	}
	for _, bad := range []string{"", "chapter", "Page", "section"} {
		if ValidDocumentLocatorKind(bad) {
			t.Errorf("expected locator kind %q to be invalid", bad)
		}
		if err := ValidateDocumentLocatorKind(bad); err == nil {
			t.Errorf("ValidateDocumentLocatorKind(%q) must fail", bad)
		}
	}
}

func TestValidateReferenceDocument(t *testing.T) {
	valid := func() *ReferenceDocument {
		return &ReferenceDocument{
			WorkspaceID: "ws-1",
			Name:        "Twilio API",
			Description: "Full API manual",
			MimeType:    "application/pdf",
			SizeBytes:   1024,
			StorageKey:  "ref/abc123",
			Backend:     "local",
			PageCount:   42,
			Scope:       RefDocScopeAttached,
			IndexStatus: RefDocIndexReady,
			UploadedBy:  "user-1",
		}
	}
	if err := ValidateReferenceDocument(valid()); err != nil {
		t.Errorf("valid document rejected: %v", err)
	}

	// Nil and missing identity fields.
	if err := ValidateReferenceDocument(nil); err == nil {
		t.Error("nil document must be rejected")
	}
	for _, mutate := range []func(*ReferenceDocument){
		func(d *ReferenceDocument) { d.WorkspaceID = "" },
		func(d *ReferenceDocument) { d.StorageKey = "" },
		func(d *ReferenceDocument) { d.Backend = "" },
		func(d *ReferenceDocument) { d.UploadedBy = "" },
		func(d *ReferenceDocument) { d.Name = "" },
		func(d *ReferenceDocument) { d.MimeType = "application/msword" },
		func(d *ReferenceDocument) { d.Scope = "promoted" },
		func(d *ReferenceDocument) { d.IndexStatus = "failed" },
		func(d *ReferenceDocument) { d.SizeBytes = -1 },
		func(d *ReferenceDocument) { d.PageCount = -1 },
	} {
		doc := valid()
		mutate(doc)
		if err := ValidateReferenceDocument(doc); err == nil {
			t.Errorf("expected mutation to invalidate the document: %+v", doc)
		}
	}
}

func TestValidateDocumentSection(t *testing.T) {
	valid := func() *DocumentSection {
		return &DocumentSection{
			DocumentID:  "doc-1",
			Heading:     "Webhooks",
			Locator:     "p. 30",
			LocatorKind: LocatorKindPage,
			Level:       2,
			Ordinal:     7,
			Body:        "Signing key rotation…",
		}
	}
	if err := ValidateDocumentSection(valid()); err != nil {
		t.Errorf("valid section rejected: %v", err)
	}
	if err := ValidateDocumentSection(nil); err == nil {
		t.Error("nil section must be rejected")
	}
	for _, mutate := range []func(*DocumentSection){
		func(s *DocumentSection) { s.DocumentID = "" },
		func(s *DocumentSection) { s.LocatorKind = "" },
		func(s *DocumentSection) { s.LocatorKind = "chapter" },
		func(s *DocumentSection) { s.Level = 4 },
		func(s *DocumentSection) { s.Level = -1 },
		func(s *DocumentSection) { s.Ordinal = -1 },
	} {
		section := valid()
		mutate(section)
		if err := ValidateDocumentSection(section); err == nil {
			t.Errorf("expected mutation to invalidate the section: %+v", section)
		}
	}

	// Level 0 is the synthetic preamble/whole-doc form.
	synthetic := valid()
	synthetic.Level = 0
	synthetic.LocatorKind = LocatorKindNone
	synthetic.Locator = ""
	if err := ValidateDocumentSection(synthetic); err != nil {
		t.Errorf("synthetic section rejected: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Visibility predicate (tasks 1.2, D7)
// ---------------------------------------------------------------------------

func TestReferenceDocumentVisibility(t *testing.T) {
	attachedDoc := ReferenceDocument{
		Scope:      RefDocScopeAttached,
		AgentIDs:   []string{"atlas"},
		ChannelIDs: []string{"incidents"},
	}
	promotedDoc := ReferenceDocument{
		Scope:    RefDocScopeWorkspace,
		AgentIDs: nil,
	}
	unattachedDoc := ReferenceDocument{
		Scope:      RefDocScopeAttached,
		AgentIDs:   nil,
		ChannelIDs: nil,
	}

	direct := DocumentRunScope{AgentID: "atlas"}
	directOtherAgent := DocumentRunScope{AgentID: "beacon"}
	channelRun := DocumentRunScope{AgentID: "beacon", ChannelID: "incidents", IsChannelSession: true}
	channelRunOtherChannel := DocumentRunScope{AgentID: "beacon", ChannelID: "ops", IsChannelSession: true}
	// Scheduler rule: scheduled runs resolve by agent bindings only — the
	// channel delivery target is never passed as run scope.
	scheduledRun := DocumentRunScope{AgentID: "beacon"}

	tests := []struct {
		name string
		doc  ReferenceDocument
		run  DocumentRunScope
		want bool
	}{
		{"agent-attached doc visible to its agent's direct chat", attachedDoc, direct, true},
		{"agent-attached doc hidden from other agents", attachedDoc, directOtherAgent, false},
		{"agent-attached doc hidden from other agents in other channels", attachedDoc, channelRunOtherChannel, false},
		{"agent-attached doc visible to another agent inside an attached channel", attachedDoc, channelRun, true},
		{"agent-attached doc hidden from a scheduled run of a non-attached agent", attachedDoc, scheduledRun, false},
		{"agent-attached doc visible to a scheduled run of its own agent", attachedDoc, DocumentRunScope{AgentID: "atlas"}, true},

		{"promoted doc visible to any agent", promotedDoc, directOtherAgent, true},
		{"promoted doc visible to scheduled runs", promotedDoc, scheduledRun, true},
		{"promoted doc visible with no run identity at all", promotedDoc, DocumentRunScope{}, true},

		{"unattached doc hidden from everyone", unattachedDoc, direct, false},
		{"unattached doc hidden in channels", unattachedDoc, channelRun, false},

		// The channel half of the predicate requires IsChannelSession: a
		// channel id without a channel session (malformed/legacy run scope)
		// matches nothing.
		{"channel id without channel session matches nothing", attachedDoc, DocumentRunScope{AgentID: "beacon", ChannelID: "incidents"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.doc.VisibleTo(tt.run); got != tt.want {
				t.Errorf("VisibleTo(%+v) on %+v = %v, want %v", tt.run, tt.doc, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Permission catalog (task 1.3)
// ---------------------------------------------------------------------------

func TestReferenceDocumentsPermissions(t *testing.T) {
	// reference_documents.promote is admin-only surface: built-in Owner and
	// Admin hold it (Superadmin via its all-workspace-permissions set);
	// Member never does.
	for _, tier := range []struct {
		name  string
		perms []string
		want  bool
	}{
		{"Owner", OwnerPermissions, true},
		{"Admin", AdminPermissions, true},
		{"Superadmin", SuperadminPermissions, true},
		{"Member", MemberPermissions, false},
	} {
		if got := HasPermission(tier.perms, PermissionReferenceDocumentsPromote); got != tier.want {
			t.Errorf("expected %s to hold %s: %v", tier.name, PermissionReferenceDocumentsPromote, got)
		}
	}

	if !IsValidPermission(PermissionReferenceDocumentsPromote) {
		t.Errorf("expected %s to be a valid catalog permission", PermissionReferenceDocumentsPromote)
	}
	if PermissionReferenceDocumentsPromote != "reference_documents.promote" {
		t.Errorf("permission string drifted: %q", PermissionReferenceDocumentsPromote)
	}

	found := false
	for _, p := range AllPermissions() {
		if p == PermissionReferenceDocumentsPromote {
			found = true
		}
	}
	if !found {
		t.Error("AllPermissions() must contain reference_documents.promote")
	}
}
