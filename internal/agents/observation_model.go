package agents

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// The observation image lane (add-computer-use design D5, task 4.1): the
// computer-use adapter writes screenshots under the calling session's
// workspace computer directory and stamps tool results with a marker —
// [screenshot: sessions/<session id>/computer/<unixmilli>.png] — the
// workspace-relative, replay-safe form the persisted transcript keeps. This
// decorator is the pure view function that turns those markers into pixels
// for vision-capable models: on every Generate/Stream it scans the outbound
// history, delivers the rolling window's screenshots as one synthesized
// user-role message, and elides older markers — copy-on-write, ADK state
// untouched, so no hook-appended message can disturb the react loop.

const (
	// observationWindow is the rolling marker count whose pixels ride the
	// outbound view; older markers are elided (design D5: image tokens
	// inflate turns — the window is a constant, tunable post-smoke).
	observationWindow = 2
	// observationText leads the synthesized user message carrying the
	// windowed screenshots.
	observationText = "[computer-use observation]"
	// observationElided replaces an older marker's text in the outbound
	// view: the marker's pixels are gone, the fact of the capture remains.
	observationElided = "(earlier screenshot elided)"
	// observationMIME is the screenshot content type the adapter writes.
	observationMIME = "image/png"
)

// observationContextFor names the calling session for the observation image
// lane (add-computer-use design D5): the calling session's id and the
// agent's workspace. A manager without a workspace mount yields the zero
// value — the lane absent.
func (m *manager) observationContextFor(agent store.Agent, sessionID string) observationContext {
	if m.workspaces == nil {
		return observationContext{}
	}
	return observationContext{
		sessionID:  sessionID,
		slug:       agent.Slug,
		workspaces: m.workspaces,
	}
}

// observationMarkerPattern matches the marker the computer-use adapter
// stamps into observation results (add-computer-use marker contract).
// Capture group 1 is the workspace-relative path; it is jail-checked again
// on every use — the marker is model-visible text, never trusted input.
var observationMarkerPattern = regexp.MustCompile(`\[screenshot: (sessions/[^/\s]+/computer/\d+\.png)\]`)

// observationPNGName is the screenshot file-name shape the jail enforces on
// top of the marker pattern (add-computer-use: the same name-shape jail the
// frontend bound read applies over sessions/<sid>/computer/).
var observationPNGName = regexp.MustCompile(`^\d+\.png$`)

// observationContext carries what the observation image lane needs at model
// build time (add-computer-use design D5): the calling session's identity,
// the agent's workspace, and the effective model's image-input capability.
// The zero value is the lane absent — Build wraps plain sanitize.
type observationContext struct {
	// sessionID is the calling session's id: the only session whose
	// computer screenshots the lane may deliver.
	sessionID string
	// slug is the calling agent's workspace slug — the workspace root the
	// jailed marker paths resolve under.
	slug string
	// workspaces resolves the agent workspace on disk. Nil (a manager
	// without a workspace mount) means the lane is absent.
	workspaces *workspaces.Manager
	// images reports the effective model's image-input capability. False is
	// the complete pass-through: no injection AND no elision, markers stay
	// verbatim (design D5 step 4 — elision is part of the image lane, and a
	// text-only model keeps the marker text as its only observation
	// channel, spec computer-use "Observation image delivery").
	images bool
}

// present reports whether the context names a real session workspace — the
// lane's precondition before the capability gate is even consulted.
func (o observationContext) present() bool {
	return o.sessionID != "" && o.slug != "" && o.workspaces != nil
}

// jailedRel decomposes a marker's relative path through the path jail
// (design D5): it must be exactly sessions/<calling session>/computer/<name>
// with a digits-only .png name, filepath.Clean equal to itself, no ".."
// element. On success it returns the file name; anything else is refused.
func (o observationContext) jailedRel(rel string) (string, bool) {
	if rel == "" || rel != filepath.Clean(rel) || strings.Contains(rel, "..") {
		return "", false
	}
	parts := strings.Split(rel, "/")
	if len(parts) != 4 {
		return "", false
	}
	if parts[0] != workspaces.DirSessions || parts[1] != o.sessionID || parts[2] != workspaces.DirComputer {
		return "", false
	}
	if !observationPNGName.MatchString(parts[3]) {
		return "", false
	}
	return parts[3], true
}

// resolve maps a jailed marker path to its absolute file location: the
// session's computer directory under the agent's workspace — for a jailed
// path exactly workspace.Dir(slug) joined with the marker's relative form.
func (o observationContext) resolve(rel string) (string, bool) {
	name, ok := o.jailedRel(rel)
	if !ok {
		return "", false
	}
	sessionDir, err := o.workspaces.SessionDir(o.slug, o.sessionID)
	if err != nil {
		return "", false
	}
	return filepath.Join(sessionDir, workspaces.DirComputer, name), true
}

// observationModel is the observation lane's decorator, sibling of
// sanitizeModel and wrapped around it at the same build site (add-computer-use
// design D5): it rewrites only the provider's view of the outbound history,
// never the caller's slices, messages, or ADK state.
type observationModel struct {
	base model.BaseModel[*schema.AgenticMessage]
	obs  observationContext
}

// newObservationModel wraps base with the observation view. The caller
// (modelRegistry.Build) is responsible for the capability gate; the
// decorator re-checks both gates per call so a mis-wired context degrades
// to a pass-through rather than injecting.
func newObservationModel(base model.BaseModel[*schema.AgenticMessage], obs observationContext) *observationModel {
	return &observationModel{base: base, obs: obs}
}

func (m *observationModel) Generate(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	return m.base.Generate(ctx, m.view(input), opts...)
}

func (m *observationModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	return m.base.Stream(ctx, m.view(input), opts...)
}

// view returns the outbound history with the observation lane applied: the
// last observationWindow markers' pixels injected as one trailing
// synthesized user message (oldest first), older markers' text elided.
// Idempotency (design D5) is the pure-function sense: re-deriving the view
// from the same history yields the same shape — and since the decorator
// writes neither the caller's slice nor ADK state, every Generate re-derives
// from the same raw, marker-text history, so a resumed run's rebuilt
// transcript gets the identical view. Markers that fail the jail are foreign
// or malformed text: left verbatim, never injected, never elided.
func (m *observationModel) view(input []*schema.AgenticMessage) []*schema.AgenticMessage {
	// The complete pass-through (design D5 step 4): without a session
	// workspace or image input the lane does not exist — the marker text is
	// the only observation channel, untouched.
	if !m.obs.present() || !m.obs.images {
		return input
	}
	hits := scanObservationMarkers(input)
	if len(hits) == 0 {
		return input
	}
	windowStart := 0
	if len(hits) > observationWindow {
		windowStart = len(hits) - observationWindow
	}

	// The windowed markers resolve to image blocks, scan order preserved
	// (oldest to newest). A jail rejection or an unreadable file skips that
	// one marker — its text stands and the turn never fails on the lane.
	blocks := make([]*schema.ContentBlock, 0, observationWindow)
	for _, hit := range hits[windowStart:] {
		if block, ok := m.obs.observationImageBlock(hit.rel); ok {
			blocks = append(blocks, block)
		}
	}

	// Older markers elide right-to-left so each hit's span — measured on
	// the original text — stays valid against the live text: replacements
	// only ever touch positions at or after the span being replaced, and
	// spans within one text never overlap.
	out := input
	copied := false
	for i := windowStart - 1; i >= 0; i-- {
		hit := hits[i]
		if _, ok := m.obs.jailedRel(hit.rel); !ok {
			continue
		}
		if !copied {
			out = make([]*schema.AgenticMessage, len(input))
			copy(out, input)
			copied = true
		}
		out[hit.msgIdx] = elideMarker(out[hit.msgIdx], hit, observationElided)
	}

	// The synthesized user message rides last. A fresh backing array is
	// allocated for every injection: appending could otherwise write through
	// the caller's slice capacity — the history is never mutated.
	if len(blocks) > 0 {
		injected := make([]*schema.AgenticMessage, len(out)+1)
		copy(injected, out)
		injected[len(out)] = observationMessage(blocks)
		out = injected
	}
	return out
}

// markerHit locates one observation marker occurrence in the outbound view.
type markerHit struct {
	msgIdx     int    // index into the input slice
	blockIdx   int    // index into the message's content blocks
	contentIdx int    // index into a FunctionToolResult's content; -1 for plain text blocks
	start      int    // match start within the block's text
	end        int    // match end within the block's text
	rel        string // captured workspace-relative path
}

// scanObservationMarkers walks the history in conversation order and
// collects every marker occurrence across all text carriers: assistant text
// (a model echoing a marker counts), user text, and tool result text — the
// carrier the adapter stamps. Order defines the rolling window.
func scanObservationMarkers(input []*schema.AgenticMessage) []markerHit {
	var hits []markerHit
	for i, msg := range input {
		if msg == nil {
			continue
		}
		for j, block := range msg.ContentBlocks {
			if block == nil {
				continue
			}
			switch {
			case block.AssistantGenText != nil:
				hits = appendObservationHits(hits, i, j, -1, block.AssistantGenText.Text)
			case block.UserInputText != nil:
				hits = appendObservationHits(hits, i, j, -1, block.UserInputText.Text)
			case block.FunctionToolResult != nil:
				for k, content := range block.FunctionToolResult.Content {
					if content != nil && content.Type == schema.FunctionToolResultContentBlockTypeText && content.Text != nil {
						hits = appendObservationHits(hits, i, j, k, content.Text.Text)
					}
				}
			}
		}
	}
	return hits
}

// appendObservationHits appends the marker occurrences of one text, spans
// absolute within it.
func appendObservationHits(hits []markerHit, msgIdx, blockIdx, contentIdx int, text string) []markerHit {
	for _, loc := range observationMarkerPattern.FindAllStringSubmatchIndex(text, -1) {
		hits = append(hits, markerHit{
			msgIdx:     msgIdx,
			blockIdx:   blockIdx,
			contentIdx: contentIdx,
			start:      loc[0],
			end:        loc[1],
			rel:        text[loc[2]:loc[3]],
		})
	}
	return hits
}

// observationImageBlock builds one screenshot's model-input image block the
// way turninput.go builds user image parts: base64 content with an explicit
// MIME type (design D5). A jail rejection or unreadable file yields
// ok=false — the marker's text remains, the lane never fails the turn.
func (o observationContext) observationImageBlock(rel string) (*schema.ContentBlock, bool) {
	abs, ok := o.resolve(rel)
	if !ok {
		return nil, false
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, false
	}
	return schema.NewContentBlock(&schema.UserInputImage{
		Base64Data: base64.StdEncoding.EncodeToString(data),
		MIMEType:   observationMIME,
	}), true
}

// observationMessage builds the synthesized user-role message: the lane's
// label followed by one image block per injected marker, oldest to newest.
func observationMessage(blocks []*schema.ContentBlock) *schema.AgenticMessage {
	content := make([]*schema.ContentBlock, 0, len(blocks)+1)
	content = append(content, schema.NewContentBlock(&schema.UserInputText{Text: observationText}))
	content = append(content, blocks...)
	return &schema.AgenticMessage{
		Role:          schema.AgenticRoleTypeUser,
		ContentBlocks: content,
	}
}

// elideMarker returns a copy of msg with the hit's span replaced —
// copy-on-write at every level (message, block, tool result content, text),
// mirroring sanitizeToolCallArguments: shared messages and blocks stay
// pointer-identical, only the path to the rewritten text is duplicated.
func elideMarker(msg *schema.AgenticMessage, hit markerHit, replacement string) *schema.AgenticMessage {
	if msg == nil || hit.blockIdx >= len(msg.ContentBlocks) {
		return msg
	}
	block := msg.ContentBlocks[hit.blockIdx]
	if block == nil {
		return msg
	}
	newBlock := *block
	switch {
	case hit.contentIdx < 0 && block.AssistantGenText != nil:
		text := *block.AssistantGenText
		text.Text = replaceSpan(text.Text, hit.start, hit.end, replacement)
		newBlock.AssistantGenText = &text
	case hit.contentIdx < 0 && block.UserInputText != nil:
		text := *block.UserInputText
		text.Text = replaceSpan(text.Text, hit.start, hit.end, replacement)
		newBlock.UserInputText = &text
	case hit.contentIdx >= 0 && block.FunctionToolResult != nil:
		result := *block.FunctionToolResult
		if hit.contentIdx >= len(result.Content) || result.Content[hit.contentIdx] == nil ||
			result.Content[hit.contentIdx].Text == nil {
			return msg
		}
		content := make([]*schema.FunctionToolResultContentBlock, len(result.Content))
		copy(content, result.Content)
		cb := *content[hit.contentIdx]
		text := *cb.Text
		text.Text = replaceSpan(text.Text, hit.start, hit.end, replacement)
		cb.Text = &text
		content[hit.contentIdx] = &cb
		result.Content = content
		newBlock.FunctionToolResult = &result
	default:
		return msg
	}
	blocks := make([]*schema.ContentBlock, len(msg.ContentBlocks))
	copy(blocks, msg.ContentBlocks)
	blocks[hit.blockIdx] = &newBlock
	newMsg := *msg
	newMsg.ContentBlocks = blocks
	return &newMsg
}

// replaceSpan splices replacement over s[start:end]; a span outside the
// string leaves s unchanged (a defensive guard — spans are measured on the
// same text they rewrite, right-to-left).
func replaceSpan(s string, start, end int, replacement string) string {
	if start < 0 || end > len(s) || start > end {
		return s
	}
	return s[:start] + replacement + s[end:]
}
