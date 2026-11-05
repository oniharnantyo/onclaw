package browseruse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	cdpage "github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// Snapshot bounds: the result must stay small enough for a tool payload.
const (
	maxSnapshotElements = 150
	maxNameLength       = 120
)

// axRoleHeading is the one non-interactive role kept in snapshots, for page
// orientation.
const axRoleHeading = "heading"

// TargetQuery describes an element target for click/type: a ref from the
// latest snapshot, or an accessible name (with an optional role narrowing
// both match phases).
type TargetQuery struct {
	Ref  string
	Name string
	Role string
}

// pageElement is a resolved snapshot ref: the live remote object handle the
// CDP primitives act on (the engine mapping of rod's Element handle).
type pageElement struct {
	objectID  runtime.RemoteObjectID
	backendID cdp.BackendNodeID
}

// resolveTarget resolves a click/type target against the latest snapshot's
// elements. Ref targeting is an exact ref lookup (refs reset on navigation,
// so a missing ref is a stale target). Name targeting matches
// case-insensitively by exact accessible name first, then falls back to a
// substring match; an optional role narrows both phases. An ambiguous name
// is rejected with the candidate list so the model can disambiguate.
func resolveTarget(elements []SnapshotElement, q TargetQuery) (SnapshotElement, error) {
	switch {
	case q.Ref != "":
		for _, el := range elements {
			if el.Ref == q.Ref {
				return el, nil
			}
		}
		return SnapshotElement{}, fmt.Errorf("browser: ref %s is stale; take a new snapshot", q.Ref)
	case q.Name != "":
		return resolveByName(elements, q)
	default:
		return SnapshotElement{}, errors.New("browser: either ref or name is required to address an element")
	}
}

// resolveByName runs the two-phase name match: exact (case-insensitive)
// accessible-name match, then role-and-substring.
func resolveByName(elements []SnapshotElement, q TargetQuery) (SnapshotElement, error) {
	if matches := filterTargets(elements, q.Role, func(name string) bool {
		return strings.EqualFold(name, q.Name)
	}); len(matches) == 1 {
		return matches[0], nil
	} else if len(matches) > 1 {
		return SnapshotElement{}, ambiguousTargetError(q, matches)
	}
	// No exact match: fall back to a substring match on the accessible name.
	substr := strings.ToLower(strings.TrimSpace(q.Name))
	matches := filterTargets(elements, q.Role, func(name string) bool {
		return strings.Contains(strings.ToLower(name), substr)
	})
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return SnapshotElement{}, fmt.Errorf(
			"browser: no element named %q%s in the latest snapshot; take a snapshot to list the page's elements", q.Name, roleSuffix(q.Role))
	default:
		return SnapshotElement{}, ambiguousTargetError(q, matches)
	}
}

// filterTargets narrows the snapshot's elements by optional role and a name
// predicate.
func filterTargets(elements []SnapshotElement, role string, match func(name string) bool) []SnapshotElement {
	var out []SnapshotElement
	for _, el := range elements {
		if role != "" && !strings.EqualFold(el.Role, role) {
			continue
		}
		if match(el.Name) {
			out = append(out, el)
		}
	}
	return out
}

// ambiguousTargetError builds the readable rejection listing every candidate
// with its ref, role, and name.
func ambiguousTargetError(q TargetQuery, matches []SnapshotElement) error {
	sorted := make([]SnapshotElement, len(matches))
	copy(sorted, matches)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Ref < sorted[j].Ref })
	var b strings.Builder
	fmt.Fprintf(&b, "browser: %d elements match name %q%s; add role or use ref — candidates:",
		len(sorted), q.Name, roleSuffix(q.Role))
	for _, el := range sorted {
		fmt.Fprintf(&b, " [%s %s %q]", el.Ref, el.Role, el.Name)
	}
	return errors.New(b.String())
}

// roleSuffix renders the role qualifier for error messages.
func roleSuffix(role string) string {
	if role == "" {
		return ""
	}
	return fmt.Sprintf(" (role %s)", role)
}

// diffElements returns the post-action elements that changed: refs that are
// new, and elements whose role, name, or value differs from the pre-action
// snapshot. This is the slim action result's delta — the full list rides
// snapshot/read only.
func diffElements(pre, post []SnapshotElement) []SnapshotElement {
	preByRef := make(map[string]SnapshotElement, len(pre))
	for _, el := range pre {
		preByRef[el.Ref] = el
	}
	changed := make([]SnapshotElement, 0, len(post))
	for _, el := range post {
		old, existed := preByRef[el.Ref]
		if !existed || old.Role != el.Role || old.Name != el.Name || old.Value != el.Value {
			changed = append(changed, el)
		}
	}
	return changed
}

// resolveRef resolves a snapshot ref ("e<backendNodeID>") to a live element
// handle via DOM.resolveNode — the node plumbing rod's ElementFromObject sat
// on, kept so refs survive across snapshot/action calls the same way.
// Caller holds s.mu.
func (s *profileSession) resolveRef(page context.Context, ref string) (*pageElement, error) {
	var id int64
	if _, err := fmt.Sscanf(ref, "e%d", &id); err != nil {
		return nil, fmt.Errorf("browser: ref %s is stale; take a new snapshot", ref)
	}
	var obj *runtime.RemoteObject
	_, err := chromedp.Run(page, chromedp.Func(func(c context.Context, t *chromedp.Target) error {
		res, err := cdp.Call(c, t, dom.ResolveNode, dom.ResolveNodeParams{
			BackendNodeID: cdp.BackendNodeID(id),
		})
		if err != nil {
			return err
		}
		obj = res.Object
		return nil
	}))
	if err != nil {
		return nil, fmt.Errorf("browser: ref %s is stale; take a new snapshot: %w", ref, err)
	}
	if obj == nil {
		return nil, fmt.Errorf("browser: ref %s is stale; take a new snapshot", ref)
	}
	return &pageElement{objectID: obj.ObjectID, backendID: cdp.BackendNodeID(id)}, nil
}

// snapshotPage captures the accessibility tree as bounded, ref-carrying
// elements and stores them as the session's latest snapshot.
// Caller holds s.mu.
func (s *profileSession) snapshotPage(page context.Context) ([]SnapshotElement, error) {
	elements, err := snapshotElements(page)
	if err != nil {
		return nil, err
	}
	s.elements = elements
	return elements, nil
}

// snapshotElements captures the accessibility tree as bounded elements
// without touching the session's latest snapshot.
func snapshotElements(page context.Context) ([]SnapshotElement, error) {
	var nodes []*accessibility.Node
	_, err := chromedp.Run(page, chromedp.Func(func(c context.Context, t *chromedp.Target) error {
		_, _ = cdp.Call(c, t, cdpage.Enable, cdpage.EnableParams{}) // best-effort, as before
		if _, err := cdp.Call(c, t, accessibility.Enable, cdp.Empty{}); err != nil {
			return fmt.Errorf("browser: enable accessibility: %w", err)
		}
		res, err := cdp.Call(c, t, accessibility.GetFullAXTree, accessibility.GetFullAXTreeParams{})
		if err != nil {
			return fmt.Errorf("browser: accessibility tree: %w", err)
		}
		nodes = res.Nodes
		return nil
	}))
	if err != nil {
		return nil, err
	}
	refs := make(map[string]bool, len(nodes))
	elements := make([]SnapshotElement, 0, min(maxSnapshotElements, len(nodes)))
	for _, node := range nodes {
		if node == nil || node.Ignored || node.BackendDOMNodeID == 0 {
			continue
		}
		role := axValueString(node.Role)
		interactive := interactiveAXRole(role)
		if !interactive && role != axRoleHeading {
			continue
		}
		name := truncateRunes(axValueString(node.Name), maxNameLength)
		if !interactive && name == "" {
			continue // orientation node with nothing to show
		}
		ref := fmt.Sprintf("e%d", node.BackendDOMNodeID)
		if refs[ref] {
			continue
		}
		if len(elements) >= maxSnapshotElements {
			break // refs the model never saw would only invite stale clicks
		}
		refs[ref] = true
		elements = append(elements, SnapshotElement{
			Ref:   ref,
			Role:  role,
			Name:  name,
			Value: truncateRunes(axValueString(node.Value), maxNameLength),
		})
	}
	return elements, nil
}

// targetInfo returns the session page's current target info (URL, title) —
// the same Target.getTargetInfo lookup rod's page.Info() performed, on the
// session's page target (the probe's route; see engine.go). The page
// parameter is a page-derived context; bind it through bindCall first when a
// deadline should apply.
func targetInfo(page context.Context) (*target.Info, error) {
	tgt, ok := pageTarget(page)
	if !ok {
		return nil, errors.New("browser: no page session")
	}
	res, err := cdp.Call(page, tgt, target.GetTargetInfo, target.GetTargetInfoParams{
		TargetID: tgt.TargetID,
	})
	if err != nil {
		return nil, err
	}
	return res.TargetInfo, nil
}

// pageInfo returns the page's current URL and title, tolerating a closing
// page (both come back empty).
func pageInfo(page context.Context) (string, string) {
	info, err := targetInfo(page)
	if err != nil {
		return "", ""
	}
	return info.URL, info.Title
}

// axValueString flattens an accessibility value payload to its string form.
func axValueString(v *accessibility.Value) string {
	if v == nil || len(v.Value) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(v.Value, &s); err != nil {
		return ""
	}
	return s
}

// interactiveAXRole reports whether an accessibility role is worth exposing
// as a ref target to the model.
func interactiveAXRole(role string) bool {
	switch role {
	case "button", "link", "textbox", "searchbox", "checkbox", "radio",
		"combobox", "listbox", "option", "menuitem", "menuitemcheckbox",
		"menuitemradio", "tab", "switch", "slider", "spinbutton":
		return true
	default:
		return false
	}
}

// truncateRunes cuts s to at most max runes (snapshot bound).
func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}
