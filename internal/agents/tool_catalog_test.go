package agents

import "testing"

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
