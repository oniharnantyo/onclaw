package agents

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// The memory block's composition tests (tasks.md 2.2/2.3): the block rides
// after the knowledge guidance and before the compaction summary on fresh
// and resumed turns, is withheld from subagent child turns, and a read
// failure degrades the block to guidance plus the unavailable notice without
// failing the turn (spec agent-memory "Memory block in every turn",
// "Subagent turns receive no memory block", "Read failure does not fail the
// turn").

// TestComposeSystem_MemoryBlockOrder pins the block's position (add-agent-memory
// design D3): after the operating contract and the knowledge guidance, before
// the compaction summary — the same pinned order the fresh and resumed
// compositions share.
func TestComposeSystem_MemoryBlockOrder(t *testing.T) {
	got := composeSystem("PERSONA", "", knowledgeGuidance, "", memoryGuidance, "SUM")
	require.Less(t, strings.Index(got, "PERSONA"), strings.Index(got, "# AGENTS.md"),
		"persona documents come before the contract")
	require.Less(t, strings.Index(got, "# AGENTS.md"), strings.Index(got, "## Knowledge corpus"),
		"the contract comes before the knowledge guidance")
	require.Less(t, strings.Index(got, "## Knowledge corpus"), strings.Index(got, "## Memory"),
		"the knowledge guidance comes before the memory block")
	require.Less(t, strings.Index(got, "## Memory"), strings.Index(got, "# Earlier conversation summary"),
		"the memory block comes before the compaction summary")
	require.Less(t, strings.Index(got, "# Earlier conversation summary"), strings.Index(got, "SUM"),
		"the summary heading comes before the summary body")
}

// TestComposeSystem_EmptyMemoryByteIdentical pins the block-free shape
// (design D14): with no memory text the composed output is exactly the old
// persona → contract → knowledge → summary join, so block-free turns compose
// byte-identical system context.
func TestComposeSystem_EmptyMemoryByteIdentical(t *testing.T) {
	guidance := knowledgeGuidance
	want := "PERSONA" + "\n\n" + operatingContract + "\n\n" + guidance + "\n\n" + "# Earlier conversation summary\n\n" + "SUM"
	require.Equal(t, want, composeSystem("PERSONA", "", guidance, "", "", "SUM"))
}

// TestTurn_ComposesMemoryBlock covers the fresh-turn wiring end to end: the
// block rides the system context between the contract and the environment
// block, the memory layout is created on demand before composition (spec
// agent-memory "Memory directory created on demand"), and an empty memory
// carries the guidance plus the empty notice.
func TestTurn_ComposesMemoryBlock(t *testing.T) {
	f, ws := newWorkspaceFixture(t, func(input []*schema.Message) fakeReply {
		return fakeReply{text: "noted", promptTokens: 5, completionTokens: 1}
	})
	sess := f.newSession(t)
	turn, err := f.mgr.Send(t.Context(), sess.ID, "hello")
	require.NoError(t, err)
	require.NoError(t, turn.Wait(), "the turn must complete with the memory block composed")

	// The layout exists after the turn — created on demand at turn start.
	memDir := filepath.Join(ws.Root(), f.agent.Slug, "memory")
	require.DirExists(t, memDir)
	require.DirExists(t, filepath.Join(memDir, "archive"))

	require.Len(t, f.model.calls(), 1)
	sys := textOfMessages(f.model.calls()[0], schema.System)
	require.Contains(t, sys, "## Memory")
	require.Contains(t, sys, memoryEmptyNotice, "an empty memory states so")
	require.NotContains(t, sys, memoryUnavailableNotice)
	require.Less(t, strings.Index(sys, "# AGENTS.md"), strings.Index(sys, "## Memory"),
		"the contract comes before the memory block")
	require.Less(t, strings.Index(sys, "## Memory"), strings.Index(sys, "# Environment"),
		"the memory block comes before the environment block")
}

// TestTurn_MemoryBlockCarriesIndexAndNotes covers the populated block: the
// index text rides, a note above the injection threshold rides in the notes
// section, and the empty notice stays silent.
func TestTurn_MemoryBlockCarriesIndexAndNotes(t *testing.T) {
	f, ws := newWorkspaceFixture(t, func(input []*schema.Message) fakeReply {
		return fakeReply{text: "recalled", promptTokens: 5, completionTokens: 1}
	})
	memDir := filepath.Join(ws.Root(), f.agent.Slug, "memory")
	require.NoError(t, os.MkdirAll(memDir, 0o700))
	index := "# Memory index\n\n- prefers-dark-mode — the user prefers dark mode\n"
	require.NoError(t, os.WriteFile(filepath.Join(memDir, "MEMORY.md"), []byte(index), 0o600))
	note := "---\nname: prefers-dark-mode\ndescription: the user prefers dark mode\ntype: user\n---\n\nThe user prefers dark mode in every editor.\n"
	require.NoError(t, os.WriteFile(filepath.Join(memDir, "prefers-dark-mode.md"), []byte(note), 0o600))
	require.NoError(t, f.store.Memory().UpsertNoteState(t.Context(), store.MemoryNoteState{
		AgentID:   f.agent.ID,
		Name:      "prefers-dark-mode",
		Retention: 0.9,
	}))

	sess := f.newSession(t)
	turn, err := f.mgr.Send(t.Context(), sess.ID, "what does the user prefer?")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	require.Len(t, f.model.calls(), 1)
	sys := textOfMessages(f.model.calls()[0], schema.System)
	require.Contains(t, sys, "### Index")
	require.Contains(t, sys, "- prefers-dark-mode — the user prefers dark mode")
	require.Contains(t, sys, "### Recalled notes")
	require.Contains(t, sys, "**prefers-dark-mode** (user)")
	require.NotContains(t, sys, memoryEmptyNotice)
}

// TestSubagent_ChildHasNoMemoryBlock pins the child-turn exclusion (spec
// agent-memory "Subagent turns receive no memory block"): the parent's
// system context carries the block, the delegated child's does not.
func TestSubagent_ChildHasNoMemoryBlock(t *testing.T) {
	f, _ := newWorkspaceFixture(t, delegateOnceReply(
		"summarize the repository layout", "the layout is: cmd, internal, frontend", "Delegated: the layout is known."))

	ctx := t.Context()
	sess := f.newSession(t)
	turn, err := f.mgr.Send(ctx, sess.ID, "start the delegation")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	var parentSys, childSys string
	for _, call := range f.model.calls() {
		sys := textOfMessages(call, schema.System)
		if _, ok := childCall(call, "summarize the repository layout"); ok {
			childSys = sys
		} else if sys != "" && parentSys == "" {
			parentSys = sys
		}
	}
	require.Contains(t, parentSys, "## Memory", "the parent's system context carries the memory block")
	require.NotContains(t, childSys, "## Memory", "the child's system context carries no memory block")
	require.NotContains(t, childSys, memoryEmptyNotice)
}

// TestResumedTurn_CarriesMemoryBlock covers the resumed-turn wiring: the
// cold-rebuilt placeholder instruction composes the memory block in the
// pinned position, and the resumed run's model-visible instruction —
// restored from the checkpoint that the fresh turn composed — carries the
// block too.
func TestResumedTurn_CarriesMemoryBlock(t *testing.T) {
	root := t.TempDir()
	f := approvalFixture(t, root, func(input []*schema.Message) fakeReply {
		if !sawToolMessage(input) {
			return dangerousCall("call-1", "doomed.txt")
		}
		return fakeReply{text: "resumed and cleaned", promptTokens: 10, completionTokens: 2}
	})
	sess, err := f.mgr.CreateSession(t.Context(), f.agent.ID)
	require.NoError(t, err)
	_, pending := parkOnApproval(t, f, sess)

	// The cold-resume placeholder composes persona, contract, and the memory
	// block in the pinned order (structurally — the checkpoint restores the
	// real instruction over it).
	placeholder := f.mgr.resumedTurnInstruction(t.Context(), f.agent)
	require.Less(t, strings.Index(placeholder, "# AGENTS.md"), strings.Index(placeholder, "## Memory"),
		"the contract comes before the memory block in the cold-resume placeholder")
	require.NotContains(t, placeholder, "# Earlier conversation summary",
		"the placeholder composes no compaction summary")

	// Process loss: a brand-new runtime over the same store resumes the run;
	// the resumed call's system instruction is the checkpointed one.
	reg := &fakeRegistry{model: &fakeModel{reply: func(input []*schema.Message) fakeReply {
		if !sawToolMessage(input) {
			return fakeReply{err: errors.New("the resumed tool result never reached the model")}
		}
		return fakeReply{text: "resumed and cleaned", promptTokens: 10, completionTokens: 2}
	}}}
	m2 := newManager(f.store, f.sessions, reg, WithWorkspaces(workspaces.NewManager(root)), WithBrowserDir(t.TempDir()))
	require.NoError(t, m2.ResolveApproval(t.Context(), sess.ID, pending.Approval.InterruptID, true))

	require.Eventually(t, func() bool {
		events, err := f.sessions.Events(t.Context(), sess.ID)
		return err == nil && len(events) > 0 && events[len(events)-1].Kind == sessions.KindTurnCompleted
	}, 10*time.Second, 20*time.Millisecond, "the cold-resumed turn must complete")

	require.NotEmpty(t, reg.model.calls())
	sys := textOfMessages(reg.model.calls()[0], schema.System)
	require.Contains(t, sys, "## Memory",
		"the resumed run's model-visible instruction carries the memory block")
}

// TestTurn_UnreadableMemoryDirStillComposes covers the fail-soft contract
// (tasks.md 2.3, spec agent-memory "Read failure does not fail the turn"):
// an unreadable memory directory degrades the block to guidance plus the
// unavailable notice and the turn completes normally.
func TestTurn_UnreadableMemoryDirStillComposes(t *testing.T) {
	if runtime.GOOS != "windows" && os.Geteuid() == 0 {
		t.Skip("the permission-based unreadable directory needs a non-root user")
	}
	f, ws := newWorkspaceFixture(t, func(input []*schema.Message) fakeReply {
		return fakeReply{text: "completed anyway", promptTokens: 5, completionTokens: 1}
	})
	memDir := filepath.Join(ws.Root(), f.agent.Slug, "memory")
	require.NoError(t, os.MkdirAll(memDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(memDir, "MEMORY.md"), []byte("secret index"), 0o600))
	require.NoError(t, os.Chmod(memDir, 0o000))
	t.Cleanup(func() { _ = os.Chmod(memDir, 0o700) })

	sess := f.newSession(t)
	turn, err := f.mgr.Send(t.Context(), sess.ID, "hello")
	require.NoError(t, err)
	require.NoError(t, turn.Wait(), "an unreadable memory directory must not fail the turn")

	require.Len(t, f.model.calls(), 1)
	sys := textOfMessages(f.model.calls()[0], schema.System)
	require.Contains(t, sys, "## Memory", "the degraded block keeps the guidance")
	require.Contains(t, sys, memoryUnavailableNotice)
	require.NotContains(t, sys, "secret index")
	require.NotContains(t, sys, memoryEmptyNotice)
}
