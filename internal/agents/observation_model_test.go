package agents

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	agenticclaude "github.com/cloudwego/eino-ext/components/model/agenticclaude"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// The observation image lane's unit tests (add-computer-use design D5, task
// 4.1): injection order and count, elision text, the marker path jail,
// missing-file degradation, the text-only pass-through, idempotency, and the
// streaming path's parity.

const (
	observationTestSession = "0123456789abcdef0123456789abcdef"
	observationTestSlug    = "agent"
)

// computerMarker is the marker text the computer-use adapter stamps into
// observation results (add-computer-use marker contract).
func computerMarker(sessionID, name string) string {
	return fmt.Sprintf("[screenshot: sessions/%s/computer/%s]", sessionID, name)
}

// computerToolResultMessage is one tool result whose text output carries
// text — the carrier the computer-use adapter stamps markers into.
func computerToolResultMessage(callID, name, text string) *schema.AgenticMessage {
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeUser,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.FunctionToolResult{
				CallID: callID,
				Name:   name,
				Content: []*schema.FunctionToolResultContentBlock{
					{Type: schema.FunctionToolResultContentBlockTypeText, Text: &schema.UserInputText{Text: text}},
				},
			}),
		},
	}
}

// userTextMessage is one user message with a plain text block.
func userTextMessage(text string) *schema.AgenticMessage {
	return &schema.AgenticMessage{
		Role:          schema.AgenticRoleTypeUser,
		ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.UserInputText{Text: text})},
	}
}

// toolResultText reads back the first text content of a tool result message.
func toolResultText(msg *schema.AgenticMessage) string {
	for _, block := range msg.ContentBlocks {
		if block != nil && block.FunctionToolResult != nil {
			for _, content := range block.FunctionToolResult.Content {
				if content != nil && content.Text != nil {
					return content.Text.Text
				}
			}
		}
	}
	return ""
}

// newObservationFixture returns a lane context rooted at a fresh workspace.
func newObservationFixture(t *testing.T) observationContext {
	t.Helper()
	return observationContext{
		sessionID:  observationTestSession,
		slug:       observationTestSlug,
		workspaces: workspaces.NewManager(t.TempDir()),
		images:     true,
	}
}

// seedComputerScreens writes screenshot bytes into the fixture's session
// computer directory, the location the jail resolves markers to.
func seedComputerScreens(t *testing.T, obs observationContext, files map[string][]byte) {
	t.Helper()
	dir, err := obs.workspaces.SessionComputerDir(obs.slug, obs.sessionID)
	require.NoError(t, err)
	for name, data := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, 0o600))
	}
}

// runObservationTest drives one Generate and one Stream call over the same
// input through a capturing model, so both delegation paths are asserted
// identically.
func runObservationTest(t *testing.T, obs observationContext, input []*schema.AgenticMessage, check func(t *testing.T, view []*schema.AgenticMessage)) {
	t.Helper()
	fm := &capturingModel{}
	m := newObservationModel(fm, obs)

	_, err := m.Generate(context.Background(), input)
	require.NoError(t, err)
	require.Len(t, fm.inputs, 1)
	t.Run("generate", func(t *testing.T) { check(t, fm.inputs[0]) })

	fm.inputs = nil
	reader, err := m.Stream(context.Background(), input)
	require.NoError(t, err)
	reader.Close()
	require.Len(t, fm.inputs, 1)
	t.Run("stream", func(t *testing.T) { check(t, fm.inputs[0]) })
}

// TestObservationModel_InjectsWindowedScreenshots covers the lane's core:
// three markers leave the oldest elided and the newest two injected as one
// trailing user message, oldest image first, with the caller's history
// untouched (copy-on-write).
func TestObservationModel_InjectsWindowedScreenshots(t *testing.T) {
	obs := newObservationFixture(t)
	seedComputerScreens(t, obs, map[string][]byte{
		"111.png": []byte("png-one"),
		"222.png": []byte("png-two"),
		"333.png": []byte("png-three"),
	})
	input := []*schema.AgenticMessage{
		computerToolResultMessage("call-1", "mcp__computer-use__observe_window", "tree one "+computerMarker(observationTestSession, "111.png")),
		computerToolResultMessage("call-2", "mcp__computer-use__click", "tree two "+computerMarker(observationTestSession, "222.png")),
		computerToolResultMessage("call-3", "mcp__computer-use__observe_desktop", "tree three "+computerMarker(observationTestSession, "333.png")),
	}

	runObservationTest(t, obs, input, func(t *testing.T, view []*schema.AgenticMessage) {
		require.Len(t, view, 4, "one synthesized user message rides last")

		// The oldest marker elides; the windowed two stay verbatim.
		require.Equal(t, "tree one "+observationElided, toolResultText(view[0]))
		require.Equal(t, "tree two "+computerMarker(observationTestSession, "222.png"), toolResultText(view[1]))
		require.Equal(t, "tree three "+computerMarker(observationTestSession, "333.png"), toolResultText(view[2]))

		// The synthesized message: label text, then the window's images,
		// oldest to newest, each a user_input_image block the way
		// turninput.go builds them.
		last := view[3]
		require.Equal(t, schema.AgenticRoleTypeUser, last.Role)
		require.Len(t, last.ContentBlocks, 3)
		require.NotNil(t, last.ContentBlocks[0].UserInputText)
		require.Equal(t, observationText, last.ContentBlocks[0].UserInputText.Text)
		require.NotNil(t, last.ContentBlocks[1].UserInputImage)
		require.Equal(t, base64.StdEncoding.EncodeToString([]byte("png-two")), last.ContentBlocks[1].UserInputImage.Base64Data)
		require.Equal(t, observationMIME, last.ContentBlocks[1].UserInputImage.MIMEType)
		require.Equal(t, schema.ContentBlockTypeUserInputImage, last.ContentBlocks[1].Type)
		require.NotNil(t, last.ContentBlocks[2].UserInputImage)
		require.Equal(t, base64.StdEncoding.EncodeToString([]byte("png-three")), last.ContentBlocks[2].UserInputImage.Base64Data)

		// Copy-on-write: untouched messages stay pointer-identical and the
		// caller's own history keeps the verbatim marker.
		require.Same(t, input[1], view[1])
		require.Same(t, input[2], view[2])
		require.Contains(t, toolResultText(input[0]), "[screenshot:")
	})
}

// TestObservationModel_ElidesAcrossTextCarriers covers the elision text's
// exact splice on a non-tool-result carrier: a marker the model echoed in
// its own assistant text is lane text like any other.
func TestObservationModel_ElidesAcrossTextCarriers(t *testing.T) {
	obs := newObservationFixture(t)
	seedComputerScreens(t, obs, map[string][]byte{
		"111.png": []byte("png-one"),
		"222.png": []byte("png-two"),
		"333.png": []byte("png-three"),
	})
	input := []*schema.AgenticMessage{
		{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{
				{Type: schema.ContentBlockTypeAssistantGenText, AssistantGenText: &schema.AssistantGenText{
					Text: "before " + computerMarker(observationTestSession, "111.png") + " after",
				}},
			},
		},
		computerToolResultMessage("call-1", "mcp__computer-use__observe_window", "tree "+computerMarker(observationTestSession, "222.png")),
		computerToolResultMessage("call-2", "mcp__computer-use__observe_desktop", "more "+computerMarker(observationTestSession, "333.png")),
	}

	runObservationTest(t, obs, input, func(t *testing.T, view []*schema.AgenticMessage) {
		require.Len(t, view, 4)
		require.Equal(t, "before "+observationElided+" after", view[0].ContentBlocks[0].AssistantGenText.Text)
		// The windowed markers' text stands; their images ride the
		// synthesized message.
		require.Equal(t, "tree "+computerMarker(observationTestSession, "222.png"), toolResultText(view[1]))
		require.Equal(t, "more "+computerMarker(observationTestSession, "333.png"), toolResultText(view[2]))
		require.Len(t, view[3].ContentBlocks, 3)
		require.Equal(t, base64.StdEncoding.EncodeToString([]byte("png-two")), view[3].ContentBlocks[1].UserInputImage.Base64Data)
		require.Equal(t, base64.StdEncoding.EncodeToString([]byte("png-three")), view[3].ContentBlocks[2].UserInputImage.Base64Data)
	})
}

// TestObservationModel_ElidesMultipleMarkersInOneText covers the span math
// of two elisions inside one tool result text: replacements apply
// right-to-left, so both markers rewrite and the surrounding text survives.
func TestObservationModel_ElidesMultipleMarkersInOneText(t *testing.T) {
	obs := newObservationFixture(t)
	seedComputerScreens(t, obs, map[string][]byte{
		"111.png": []byte("png-one"),
		"222.png": []byte("png-two"),
		"333.png": []byte("png-three"),
		"444.png": []byte("png-four"),
	})
	text := "a " + computerMarker(observationTestSession, "111.png") +
		" b " + computerMarker(observationTestSession, "222.png") +
		" c " + computerMarker(observationTestSession, "333.png") +
		" d " + computerMarker(observationTestSession, "444.png") + " e"
	input := []*schema.AgenticMessage{computerToolResultMessage("call-1", "mcp__computer-use__observe_window", text)}

	runObservationTest(t, obs, input, func(t *testing.T, view []*schema.AgenticMessage) {
		require.Len(t, view, 2)
		require.Equal(t,
			"a "+observationElided+" b "+observationElided+" c "+computerMarker(observationTestSession, "333.png")+" d "+computerMarker(observationTestSession, "444.png")+" e",
			toolResultText(view[0]))
		require.Len(t, view[1].ContentBlocks, 3)
		require.Equal(t, base64.StdEncoding.EncodeToString([]byte("png-three")), view[1].ContentBlocks[1].UserInputImage.Base64Data)
		require.Equal(t, base64.StdEncoding.EncodeToString([]byte("png-four")), view[1].ContentBlocks[2].UserInputImage.Base64Data)
	})
}

// TestObservationModel_MarkerFreeHistoryIsNoOp covers the pure fast path: a
// history without markers comes back as the very same slice.
func TestObservationModel_MarkerFreeHistoryIsNoOp(t *testing.T) {
	obs := newObservationFixture(t)
	input := []*schema.AgenticMessage{userTextMessage("hello"), computerToolResultMessage("call-1", "read_file", "no markers here")}

	runObservationTest(t, obs, input, func(t *testing.T, view []*schema.AgenticMessage) {
		require.Len(t, view, 2)
		require.Same(t, &input[0], &view[0], "marker-free history must share the input's backing array")
	})
}

// TestObservationModel_JailRejectsEscapingMarkers covers the path jail at
// the view level: a marker-shaped text whose path escapes the calling
// session's computer directory is never injected and never elided — the
// text stands verbatim.
func TestObservationModel_JailRejectsEscapingMarkers(t *testing.T) {
	obs := newObservationFixture(t)
	seedComputerScreens(t, obs, map[string][]byte{"123.png": []byte("png")})
	for name, rel := range map[string]string{
		"foreign session": "sessions/ffffffffffffffffffffffffffffffff/computer/123.png",
		"clean mismatch":  "sessions/../computer/123.png",
	} {
		t.Run(name, func(t *testing.T) {
			require.True(t, observationMarkerPattern.MatchString("[screenshot: "+rel+"]"), "the case must be marker-shaped to exercise the view")
			input := []*schema.AgenticMessage{
				computerToolResultMessage("call-1", "mcp__computer-use__observe_window", "tree [screenshot: "+rel+"]"),
			}
			fm := &capturingModel{}
			_, err := newObservationModel(fm, obs).Generate(context.Background(), input)
			require.NoError(t, err)
			require.Len(t, fm.inputs[0], 1, "escaping marker must not inject")
			require.Equal(t, "tree [screenshot: "+rel+"]", toolResultText(fm.inputs[0][0]), "escaping marker text stays verbatim")
		})
	}
}

// TestObservationModel_JailRejectsMalformedPaths covers the jail's
// structural rejections through paths the marker regex would never emit —
// the jail is re-checked on every use because the marker is model-visible
// text, never trusted input.
func TestObservationModel_JailRejectsMalformedPaths(t *testing.T) {
	obs := newObservationFixture(t)
	for name, rel := range map[string]string{
		"wrong directory": "sessions/" + observationTestSession + "/browser/123.png",
		"extra segment":   "sessions/" + observationTestSession + "/computer/sub/123.png",
		"absolute path":   "/sessions/" + observationTestSession + "/computer/123.png",
		"non-png name":    "sessions/" + observationTestSession + "/computer/123.gif",
		"traversal name":  "sessions/" + observationTestSession + "/computer/../123.png",
		"ungoverned name": "sessions/" + observationTestSession + "/computer/x.png",
		"capital dir":     "Sessions/" + observationTestSession + "/computer/123.png",
		"empty path":      "",
	} {
		t.Run(name, func(t *testing.T) {
			_, ok := obs.jailedRel(rel)
			require.False(t, ok, "the jail must reject %q", rel)
		})
	}
}

// TestObservationModel_MissingFileSkipsInjection covers the graceful
// degradation: a windowed marker whose file is gone is skipped — its text
// stands, the older windowed marker still injects, and an all-missing
// window changes nothing.
func TestObservationModel_MissingFileSkipsInjection(t *testing.T) {
	obs := newObservationFixture(t)
	seedComputerScreens(t, obs, map[string][]byte{"111.png": []byte("png-one")})
	input := []*schema.AgenticMessage{
		computerToolResultMessage("call-1", "mcp__computer-use__observe_window", "one "+computerMarker(observationTestSession, "111.png")),
		computerToolResultMessage("call-2", "mcp__computer-use__observe_window", "two "+computerMarker(observationTestSession, "222.png")),
	}

	runObservationTest(t, obs, input, func(t *testing.T, view []*schema.AgenticMessage) {
		// 222.png is gone: no elision (nothing older), one image injected.
		require.Len(t, view, 3)
		require.Equal(t, "one "+computerMarker(observationTestSession, "111.png"), toolResultText(view[0]))
		require.Equal(t, "two "+computerMarker(observationTestSession, "222.png"), toolResultText(view[1]))
		require.Len(t, view[2].ContentBlocks, 2)
		require.Equal(t, base64.StdEncoding.EncodeToString([]byte("png-one")), view[2].ContentBlocks[1].UserInputImage.Base64Data)
	})

	t.Run("all missing", func(t *testing.T) {
		empty := newObservationFixture(t)
		seedComputerScreens(t, empty, map[string][]byte{})
		missing := []*schema.AgenticMessage{
			computerToolResultMessage("call-1", "mcp__computer-use__observe_window", "one "+computerMarker(empty.sessionID, "111.png")),
			computerToolResultMessage("call-2", "mcp__computer-use__observe_window", "two "+computerMarker(empty.sessionID, "222.png")),
		}
		fm := &capturingModel{}
		_, err := newObservationModel(fm, empty).Generate(context.Background(), missing)
		require.NoError(t, err)
		require.Len(t, fm.inputs[0], 2, "a fully missing window changes nothing")
		require.Same(t, &missing[0], &fm.inputs[0][0])
	})
}

// TestObservationModel_TextOnlyModelPassThrough pins the caps-gate decision
// (design D5 step 4): without image input the decorator is a complete
// pass-through — no injection AND no elision, markers stay verbatim as the
// text-only model's sole observation channel.
func TestObservationModel_TextOnlyModelPassThrough(t *testing.T) {
	obs := newObservationFixture(t)
	obs.images = false
	seedComputerScreens(t, obs, map[string][]byte{"111.png": []byte("png-one")})
	input := []*schema.AgenticMessage{
		computerToolResultMessage("call-1", "mcp__computer-use__observe_window", "one "+computerMarker(observationTestSession, "111.png")),
		computerToolResultMessage("call-2", "mcp__computer-use__observe_window", "two "+computerMarker(observationTestSession, "222.png")),
		computerToolResultMessage("call-3", "mcp__computer-use__observe_window", "three "+computerMarker(observationTestSession, "333.png")),
	}

	runObservationTest(t, obs, input, func(t *testing.T, view []*schema.AgenticMessage) {
		require.Len(t, view, 3, "no injection on a text-only model")
		require.Same(t, &input[0], &view[0], "no elision rewrite either — complete pass-through")
		require.Contains(t, toolResultText(view[0]), "[screenshot:")
	})
}

// TestObservationModel_Idempotent covers design D5's idempotency: injection
// is a pure function of history — re-deriving the view from the same
// history yields the same shape. (The view never feeds back: the decorator
// writes neither the caller's slice nor ADK state, so every Generate
// re-derives from the same raw, marker-text history; a resumed run's
// rebuilt transcript gets the identical view.)
func TestObservationModel_Idempotent(t *testing.T) {
	obs := newObservationFixture(t)
	seedComputerScreens(t, obs, map[string][]byte{
		"111.png": []byte("png-one"),
		"222.png": []byte("png-two"),
		"333.png": []byte("png-three"),
	})
	input := []*schema.AgenticMessage{
		computerToolResultMessage("call-1", "mcp__computer-use__observe_window", "one "+computerMarker(observationTestSession, "111.png")),
		computerToolResultMessage("call-2", "mcp__computer-use__observe_window", "two "+computerMarker(observationTestSession, "222.png")),
		computerToolResultMessage("call-3", "mcp__computer-use__observe_window", "three "+computerMarker(observationTestSession, "333.png")),
	}
	fm := &capturingModel{}
	m := newObservationModel(fm, obs)

	_, err := m.Generate(context.Background(), input)
	require.NoError(t, err)
	first := fm.inputs[0]
	_, err = m.Generate(context.Background(), input)
	require.NoError(t, err)
	second := fm.inputs[1]

	require.Equal(t, first, second, "running the view twice on the same history yields the same shape")
	require.Contains(t, toolResultText(input[0]), "[screenshot:", "the caller's history keeps marker text")
}

// TestObservationContext_Jail pins the jail's unit-level contract beyond
// what the marker regex can express.
func TestObservationContext_Jail(t *testing.T) {
	obs := newObservationFixture(t)
	t.Run("accepts the session's own computer capture", func(t *testing.T) {
		name, ok := obs.jailedRel("sessions/" + observationTestSession + "/computer/1730000000000.png")
		require.True(t, ok)
		require.Equal(t, "1730000000000.png", name)
	})
	t.Run("rejects a non-conforming session id", func(t *testing.T) {
		_, ok := obs.jailedRel("sessions/not-a-session-id/computer/1.png")
		require.False(t, ok)
	})
	t.Run("rejects a name with a path separator", func(t *testing.T) {
		_, ok := obs.jailedRel("sessions/" + observationTestSession + "/computer/1.png/2.png")
		require.False(t, ok)
	})
}

// TestRegistry_BuildObservationLane covers the build-site wiring (design
// D5): session context plus an image-capable model wraps the sanitize
// decorator in the observation decorator; a text-only model or a zero
// context stays plain sanitize.
func TestRegistry_BuildObservationLane(t *testing.T) {
	t.Run("vision model with session context wraps observation over sanitize", func(t *testing.T) {
		fs := fakeStoreForRegistry(t)
		_, agent := seedAgentOn(fs, ProviderAnthropic, "claude-sonnet-4-5")
		obs := observationContext{
			sessionID:  observationTestSession,
			slug:       observationTestSlug,
			workspaces: workspaces.NewManager(t.TempDir()),
		}

		mdl, err := newModelRegistry(fs).Build(t.Context(), agent, ModelOverride{}, obs)
		require.NoError(t, err)
		lane, ok := mdl.(*observationModel)
		require.True(t, ok, "Build must wrap the sanitize decorator in the observation decorator")
		require.True(t, lane.obs.images, "the capability gate must enable the lane")
		require.Equal(t, observationTestSession, lane.obs.sessionID)
		require.Equal(t, observationTestSlug, lane.obs.slug)
		san, ok := lane.base.(*sanitizeModel)
		require.True(t, ok, "the observation decorator must wrap the sanitize decorator")
		require.IsType(t, &agenticclaude.Model{}, san.base)
	})

	t.Run("text-only model stays plain sanitize", func(t *testing.T) {
		fs := fakeStoreForRegistry(t)
		_, agent := seedAgentOn(fs, ProviderOpenRouter, "some/vendor-model")
		obs := observationContext{
			sessionID:  observationTestSession,
			slug:       observationTestSlug,
			workspaces: workspaces.NewManager(t.TempDir()),
		}

		mdl, err := newModelRegistry(fs).Build(t.Context(), agent, ModelOverride{}, obs)
		require.NoError(t, err)
		require.IsType(t, &sanitizeModel{}, mdl, "a text-only model is the lane absent")
	})

	t.Run("zero context stays plain sanitize", func(t *testing.T) {
		fs := fakeStoreForRegistry(t)
		_, agent := seedAgentOn(fs, ProviderAnthropic, "claude-sonnet-4-5")

		mdl, err := newModelRegistry(fs).Build(t.Context(), agent, ModelOverride{}, observationContext{})
		require.NoError(t, err)
		require.IsType(t, &sanitizeModel{}, mdl, "the zero context is the lane absent")
	})
}
