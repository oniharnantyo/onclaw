package agents

import (
	"testing"

	agenthooks "github.com/oniharnantyo/onclaw/internal/agents/hooks"
	"github.com/oniharnantyo/onclaw/internal/agents/tools"
)

// TestToolCatalog_AlwaysOnPinned pins the always-on marker against drift
// (always-on-channel-tools risks): the runner strips always-on tools by name
// outside their execution context (withoutChannelTools / withoutSessionTools),
// so every AlwaysOn catalog entry must be a member of ChannelToolNames +
// SessionToolNames, and exactly the three known keys carry the marker — a
// fourth always-on tool fails here until the strip lists follow consciously.
func TestToolCatalog_AlwaysOnPinned(t *testing.T) {
	stripListed := map[string]bool{}
	for _, names := range [][]string{ChannelToolNames, SessionToolNames} {
		for _, name := range names {
			stripListed[name] = true
		}
	}

	alwaysOn := map[string]bool{}
	for _, entry := range ToolCatalog() {
		if !entry.AlwaysOn {
			continue
		}
		alwaysOn[entry.Key] = true
		if !stripListed[entry.Key] {
			t.Errorf("%s is AlwaysOn but absent from ChannelToolNames + SessionToolNames; extend the strip lists consciously", entry.Key)
		}
	}

	for _, key := range []string{ChannelToolPost, ChannelToolHistory, SessionToolClose} {
		if !alwaysOn[key] {
			t.Errorf("%s must be AlwaysOn", key)
		}
		delete(alwaysOn, key)
	}
	for key := range alwaysOn {
		t.Errorf("%s carries AlwaysOn but is not one of the three pinned channel tools", key)
	}
}

// TestToolCatalog_SubagentAndBackgroundShellRows pins the two reserved
// capability rows (add-agent-subagents-background 5.2, design D9/D11): both
// must be toggleable catalog entries — default-allowed, never AlwaysOn — so
// the agent-config tool picker and the workspace tools gate offer them, and
// both must sit immediately after the Shell row so surfaces render the
// shell/background-shell/sub-agents sequence deterministically.
func TestToolCatalog_SubagentAndBackgroundShellRows(t *testing.T) {
	catalog := ToolCatalog()

	shellIndex := -1
	for i, entry := range catalog {
		if entry.Key == ReservedShellTool {
			shellIndex = i
			break
		}
	}
	if shellIndex < 0 {
		t.Fatalf("Shell row (%s) missing from catalog", ReservedShellTool)
	}

	wantOrder := []string{ReservedBackgroundShellTool, ReservedSubagentsTool}
	for offset, wantKey := range wantOrder {
		index := shellIndex + 1 + offset
		if index >= len(catalog) {
			t.Fatalf("catalog ends before %s", wantKey)
		}
		entry := catalog[index]
		if entry.Key != wantKey {
			t.Errorf("catalog[%d] = %s, want %s (reserved rows must sit right after Shell)", index, entry.Key, wantKey)
		}
	}

	byKey := map[string]ToolCatalogEntry{}
	for _, entry := range catalog {
		byKey[entry.Key] = entry
	}

	backgroundShell, ok := byKey[ReservedBackgroundShellTool]
	if !ok {
		t.Fatalf("catalog missing the %s row", ReservedBackgroundShellTool)
	}
	if backgroundShell.DisplayName != "Background Shell" {
		t.Errorf("Background Shell display name = %q", backgroundShell.DisplayName)
	}
	if backgroundShell.Group != "shell" {
		t.Errorf("Background Shell group = %q, want %q", backgroundShell.Group, "shell")
	}
	if backgroundShell.AlwaysOn {
		t.Error("Background Shell must be toggleable, not AlwaysOn")
	}

	subagents, ok := byKey[ReservedSubagentsTool]
	if !ok {
		t.Fatalf("catalog missing the %s row", ReservedSubagentsTool)
	}
	if subagents.DisplayName != "Sub-agents" {
		t.Errorf("Sub-agents display name = %q", subagents.DisplayName)
	}
	if subagents.Group != "agents" {
		t.Errorf("Sub-agents group = %q, want %q", subagents.Group, "agents")
	}
	if subagents.AlwaysOn {
		t.Error("Sub-agents must be toggleable, not AlwaysOn")
	}
}

// TestToolCatalog_DocumentSearchRow pins the document.search catalog row
// (add-reference-documents 4.1): it rides the document family's seam — a
// member of the document group sitting inside it (after Create Document,
// before Shell), toggleable, never AlwaysOn, not configurable.
func TestToolCatalog_DocumentSearchRow(t *testing.T) {
	catalog := ToolCatalog()

	indexByKey := make(map[string]int, len(catalog))
	for i, entry := range catalog {
		if _, dup := indexByKey[entry.Key]; dup {
			t.Errorf("catalog carries duplicate key %q", entry.Key)
		}
		indexByKey[entry.Key] = i
	}
	searchIndex, ok := indexByKey[tools.NameDocumentSearch]
	if !ok {
		t.Fatal("catalog missing the document.search row")
	}
	createIndex, ok := indexByKey[tools.NameDocumentCreate]
	if !ok {
		t.Fatal("catalog missing the document.create row")
	}
	if searchIndex != createIndex+1 {
		t.Errorf("document.search sits at %d, want immediately after document.create (%d) inside the document group", searchIndex, createIndex)
	}
	shellIndex, ok := indexByKey[ReservedShellTool]
	if !ok || searchIndex >= shellIndex {
		t.Errorf("document.search must precede the shell row (%d), got %d", shellIndex, searchIndex)
	}

	entry := catalog[searchIndex]
	if entry.Group != "document" {
		t.Errorf("group = %q, want document", entry.Group)
	}
	if entry.DisplayName != "Search Documents" {
		t.Errorf("display name = %q", entry.DisplayName)
	}
	if entry.Configurable || entry.AlwaysOn {
		t.Errorf("document.search must be an ordinary toggleable row: %+v", entry)
	}

	// ByKey lookup agrees with the scan.
	byKey, ok := ToolCatalogEntryByKey(tools.NameDocumentSearch)
	if !ok || byKey.Group != "document" {
		t.Errorf("ToolCatalogEntryByKey(document.search) = %+v, ok=%v", byKey, ok)
	}
}

// TestDocumentSearchHookTargetMatches pins the family seam's hook half
// (add-reference-documents 4.1): "document.search" is a valid hook matcher
// value — exact and document.* family matchers both select it, the same way
// they select document.read today.
func TestDocumentSearchHookTargetMatches(t *testing.T) {
	exact, err := agenthooks.CompileMatcher("document.search")
	if err != nil {
		t.Fatalf("compile exact matcher: %v", err)
	}
	if !exact.Matches(tools.NameDocumentSearch) {
		t.Error("exact matcher must select document.search")
	}
	if exact.Matches(tools.NameDocumentRead) {
		t.Error("exact matcher must not select document.read")
	}

	family, err := agenthooks.CompileMatcher("document.*")
	if err != nil {
		t.Fatalf("compile family matcher: %v", err)
	}
	for _, name := range []string{tools.NameDocumentSearch, tools.NameDocumentRead, tools.NameDocumentCreate} {
		if !family.Matches(name) {
			t.Errorf("family matcher must select %s", name)
		}
	}
}
