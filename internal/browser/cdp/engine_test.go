package cdp_test

import (
	"testing"

	"github.com/go-rod/rod/lib/proto"
	"github.com/oniharnantyo/onclaw/internal/browser/cdp"
	"github.com/ysmood/gson"
)

func TestFormatAXTree(t *testing.T) {
	// Construct a canned accessibility tree:
	// root (ignored: false, BackendDOMNodeID: 0)
	//   +- child1 (ignored: false, BackendDOMNodeID: 101, role: "button", name: "Submit")
	//   +- child2 (ignored: true)
	//        +- grandchild1 (ignored: false, BackendDOMNodeID: 102, role: "link", name: "Home", value: "active")
	//   +- child3 (ignored: false, BackendDOMNodeID: 0, role: "text")

	nodes := []*proto.AccessibilityAXNode{
		{
			NodeID:           "root",
			Ignored:          false,
			Role:             &proto.AccessibilityAXValue{Value: gson.New("rootWebArea")},
			BackendDOMNodeID: 0,
			ChildIDs:         []proto.AccessibilityAXNodeID{"child1", "child2", "child3"},
		},
		{
			NodeID:           "child1",
			Ignored:          false,
			Role:             &proto.AccessibilityAXValue{Value: gson.New("button")},
			Name:             &proto.AccessibilityAXValue{Value: gson.New("Submit")},
			BackendDOMNodeID: 101,
			ChildIDs:         nil,
		},
		{
			NodeID:           "child2",
			Ignored:          true,
			ChildIDs:         []proto.AccessibilityAXNodeID{"grandchild1"},
			BackendDOMNodeID: 0,
		},
		{
			NodeID:           "grandchild1",
			Ignored:          false,
			Role:             &proto.AccessibilityAXValue{Value: gson.New("link")},
			Name:             &proto.AccessibilityAXValue{Value: gson.New("Home")},
			Value:            &proto.AccessibilityAXValue{Value: gson.New("active")},
			BackendDOMNodeID: 102,
			ChildIDs:         nil,
		},
		{
			NodeID:           "child3",
			Ignored:          false,
			Role:             &proto.AccessibilityAXValue{Value: gson.New("text")},
			BackendDOMNodeID: 0,
			ChildIDs:         nil,
		},
	}

	lines, refs := cdp.FormatAXTree(nodes)

	expectedLines := []string{
		`rootWebArea`,
		`  [e1] button "Submit"`,
		`  [e2] link "Home" value="active"`,
		`  text`,
	}

	if len(lines) != len(expectedLines) {
		t.Fatalf("expected %d output lines, got %d", len(expectedLines), len(lines))
	}

	for i, line := range lines {
		if line != expectedLines[i] {
			t.Errorf("line %d: expected %q, got %q", i, expectedLines[i], line)
		}
	}

	// Verify reference ID mappings
	if len(refs) != 2 {
		t.Errorf("expected 2 reference mappings, got %d", len(refs))
	}
	if refs["e1"] != 101 {
		t.Errorf("expected e1 to map to 101, got %v", refs["e1"])
	}
	if refs["e2"] != 102 {
		t.Errorf("expected e2 to map to 102, got %v", refs["e2"])
	}
}
