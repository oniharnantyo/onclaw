package agents

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// docIDs projects a document slice to its ids.
func docIDs(docs []domain.ReferenceDocument) []string {
	ids := make([]string, 0, len(docs))
	for _, doc := range docs {
		ids = append(ids, doc.ID)
	}
	return ids
}

// manifestDocNames returns the document names the manifest block carries.
func manifestDocNames(manifest string) []string {
	var names []string
	for _, line := range strings.Split(manifest, "\n") {
		if strings.HasPrefix(line, "- ") {
			if name, _, ok := strings.Cut(strings.TrimPrefix(line, "- "), " — "); ok {
				names = append(names, name)
			} else {
				names = append(names, strings.TrimPrefix(line, "- "))
			}
		}
	}
	return names
}

// ---------------------------------------------------------------------------
// 11.1 — visibility enforced at both layers, end to end: for every scope
// combination the compose-time manifest membership and the document.search
// results agree, both driven through the real references service over the
// fake stores.
// ---------------------------------------------------------------------------

// TestReferencesVisibilityManifestAndSearchAgree is task 11.1's verify: the
// D7 predicate evaluated at compose time (manifest entries, via the runner's
// renderer) and at query time (document.search hits) return the same
// document set for every scope combination — promoted, agent-attached,
// channel-attached (inside the room / outside in direct chat / scheduled run
// delivering to the room), other-agent, and cross-workspace.
func TestReferencesVisibilityManifestAndSearchAgree(t *testing.T) {
	h := newReferencesRunHarness(t, "refs-agree")

	promoted := h.uploadDoc(t, "promoted-lib.md", nil, nil)
	atlasDoc := h.uploadDoc(t, "atlas-runbook.md", []string{h.atlasID}, nil)
	channelDoc := h.uploadDoc(t, "channel-runbook.md", nil, []string{h.incidentsID})
	beaconDoc := h.uploadDoc(t, "beacon-runbook.md", []string{h.beaconID}, nil)
	if _, err := h.svc.Promote(h.ctx, h.wsID, promoted.ID); err != nil {
		t.Fatalf("promote: %v", err)
	}

	// A second workspace: its documents are unreachable from runs in the
	// first (tenancy), indistinguishably from absent.
	other := newReferencesRunHarness(t, "refs-other-ws")
	otherDoc := other.uploadDoc(t, "foreign-doc.md", []string{other.atlasID}, nil)

	render := func(t *testing.T, scope domain.DocumentRunScope) string {
		t.Helper()
		docs, err := h.svc.VisibleDocuments(h.ctx, h.wsID, scope)
		if err != nil {
			t.Fatalf("VisibleDocuments: %v", err)
		}
		r := &Runner{references: h.svc}
		return r.renderReferenceManifest(h.ctx, &agentConfig{
			WorkspaceID:           h.wsID,
			documentSearchEnabled: true,
			visibleDocuments:      docs,
		})
	}

	search := func(t *testing.T, scope domain.DocumentRunScope) map[string]struct{} {
		t.Helper()
		hits, err := h.svc.SearchDocuments(h.ctx, h.wsID, scope, "signing", 0)
		if err != nil {
			t.Fatalf("SearchDocuments: %v", err)
		}
		names := make(map[string]struct{}, len(hits))
		for _, hit := range hits {
			names[hit.DocumentName] = struct{}{}
		}
		return names
	}

	for _, tc := range []struct {
		name string
		req  ExecRequest
		want []domain.ReferenceDocument
	}{
		{
			name: "promoted: visible to a plain direct chat",
			req:  ExecRequest{WorkspaceID: h.wsID, AgentID: h.atlasID, SessionID: "sess_a1"},
			want: []domain.ReferenceDocument{promoted, atlasDoc},
		},
		{
			name: "agent-attached: the bound agent's direct chat",
			req:  ExecRequest{WorkspaceID: h.wsID, AgentID: h.atlasID, SessionID: "sess_a2"},
			want: []domain.ReferenceDocument{promoted, atlasDoc},
		},
		{
			name: "channel-attached: inside the channel session",
			req:  ExecRequest{WorkspaceID: h.wsID, AgentID: h.atlasID, SessionID: "chan_c1", ChannelID: h.incidentsID, Origin: OriginChannel},
			want: []domain.ReferenceDocument{promoted, atlasDoc, channelDoc},
		},
		{
			name: "channel-attached: outside, in a direct chat",
			req:  ExecRequest{WorkspaceID: h.wsID, AgentID: h.atlasID, SessionID: "sess_a3"},
			want: []domain.ReferenceDocument{promoted, atlasDoc},
		},
		{
			name: "channel-attached: scheduled run delivering to that channel (D7 scheduler rule)",
			req:  ExecRequest{WorkspaceID: h.wsID, AgentID: h.atlasID, SessionID: "sched_s1", ChannelID: h.incidentsID, Origin: OriginScheduler},
			want: []domain.ReferenceDocument{promoted, atlasDoc},
		},
		{
			name: "other agent's direct chat: the atlas attachment is invisible",
			req:  ExecRequest{WorkspaceID: h.wsID, AgentID: h.beaconID, SessionID: "sess_b1"},
			want: []domain.ReferenceDocument{promoted, beaconDoc},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scope := referencesRunScope(tc.req)
			manifest := render(t, scope)
			if manifest == "" {
				t.Fatal("manifest rendered empty for a wired, gated-on run with visible documents")
			}

			// Compose layer: exactly the expected documents appear (order
			// follows the store's newest-first lens; membership is the D7
			// contract under test).
			gotNames := manifestDocNames(manifest)
			wantNames := make(map[string]struct{}, len(tc.want))
			for _, doc := range tc.want {
				wantNames[doc.Name] = struct{}{}
			}
			if len(gotNames) != len(wantNames) {
				t.Fatalf("manifest entries = %v, want %v", gotNames, wantNames)
			}
			for _, name := range gotNames {
				if _, ok := wantNames[name]; !ok {
					t.Errorf("unexpected manifest entry %q", name)
				}
			}

			// Query layer: document.search returns the same set.
			searchNames := search(t, scope)
			if len(searchNames) != len(wantNames) {
				t.Fatalf("search documents = %v, want %v", searchNames, wantNames)
			}
			for name := range wantNames {
				if _, ok := searchNames[name]; !ok {
					t.Errorf("manifest lists %q but document.search does not return it", name)
				}
			}

			// Cross-workspace: the foreign document is in neither layer.
			if strings.Contains(manifest, "foreign-doc.md") {
				t.Errorf("cross-workspace document leaked into the manifest")
			}
			if _, ok := searchNames["foreign-doc.md"]; ok {
				t.Errorf("cross-workspace document leaked into search")
			}
		})
	}

	// The other workspace sees its own library, never the first workspace's.
	otherScope := referencesRunScope(ExecRequest{WorkspaceID: other.wsID, AgentID: other.atlasID, SessionID: "sess_o1"})
	otherVisible, err := other.svc.VisibleDocuments(other.ctx, other.wsID, otherScope)
	if err != nil {
		t.Fatalf("other workspace VisibleDocuments: %v", err)
	}
	if len(otherVisible) != 1 || otherVisible[0].ID != otherDoc.ID {
		t.Fatalf("other workspace visible = %v, want exactly the foreign doc", docIDs(otherVisible))
	}
	otherHits, err := other.svc.SearchDocuments(other.ctx, other.wsID, otherScope, "signing", 0)
	if err != nil {
		t.Fatalf("other workspace search: %v", err)
	}
	if len(otherHits) != 1 || otherHits[0].DocumentID != otherDoc.ID {
		t.Fatalf("other workspace search = %+v, want exactly the foreign doc", otherHits)
	}
}

// manifestDocNamesWithNames — guard against accidental substring matches by
// comparing full names, exercised above through manifestDocNames; the JSON
// helper below keeps the subagent test honest about where the manifest rides.
func manifestJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(raw)
}
