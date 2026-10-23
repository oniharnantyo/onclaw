package agents

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/sessions"
)

func writePlanFile(t *testing.T, dir, name, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
}

func TestReadPlanSnapshot_OrdersByIDAndSkipsDeleted(t *testing.T) {
	dir := t.TempDir()
	writePlanFile(t, dir, "10.json", `{"id":"10","subject":"ten","status":"pending","blockedBy":["2"]}`)
	writePlanFile(t, dir, "2.json", `{"id":"2","subject":"two","status":"in_progress","activeForm":"Doing two"}`)
	writePlanFile(t, dir, "3.json", `{"id":"3","subject":"gone","status":"deleted"}`)
	writePlanFile(t, dir, ".highwatermark", `10`)
	writePlanFile(t, dir, "task-1.md", `{"step":"task"}`)
	writePlanFile(t, dir, "broken.json", `{not json`)

	snap, err := readPlanSnapshot(dir)
	require.NoError(t, err)
	require.Len(t, snap.Tasks, 2)
	assert.Equal(t, "2", snap.Tasks[0].ID)
	assert.Equal(t, "Doing two", snap.Tasks[0].ActiveForm)
	assert.Equal(t, "10", snap.Tasks[1].ID)
	assert.Equal(t, []string{"2"}, snap.Tasks[1].BlockedBy)
}

func TestReadPlanSnapshot_MissingDirIsEmptyPlan(t *testing.T) {
	snap, err := readPlanSnapshot(filepath.Join(t.TempDir(), "nope"))
	require.NoError(t, err)
	assert.Empty(t, snap.Tasks)
}

func toolResultMessage(name, callID string) *schema.AgenticMessage {
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeUser,
		ContentBlocks: []*schema.ContentBlock{{
			Type: schema.ContentBlockTypeFunctionToolResult,
			FunctionToolResult: &schema.FunctionToolResult{
				CallID: callID,
				Name:   name,
			},
		}},
	}
}

func TestRecordPlanSnapshot_OnlyAfterSuccessfulPlanTools(t *testing.T) {
	dir := t.TempDir()
	writePlanFile(t, dir, "1.json", `{"id":"1","subject":"one","status":"pending"}`)

	var recorded []*sessions.Event
	record := func(ev *sessions.Event, durable bool) error {
		assert.True(t, durable, "plan snapshots persist")
		recorded = append(recorded, ev)
		return nil
	}

	recordPlanSnapshot(toolResultMessage(ToolReadFile, "c1"), dir, record, newToolErrorResult())
	assert.Empty(t, recorded, "a non-plan tool records nothing")

	failed := newToolErrorResult()
	failed.RecordFailure("c2", "boom")
	recordPlanSnapshot(toolResultMessage(ToolTaskUpdate, "c2"), dir, record, failed)
	assert.Empty(t, recorded, "a failed plan tool records nothing")

	recordPlanSnapshot(toolResultMessage(ToolTaskCreate, "c3"), dir, record, newToolErrorResult())
	require.Len(t, recorded, 1)
	assert.Equal(t, sessions.KindPlanActivity, recorded[0].Kind)
	require.NotNil(t, recorded[0].Plan)
	require.Len(t, recorded[0].Plan.Tasks, 1)
	assert.Equal(t, "one", recorded[0].Plan.Tasks[0].Subject)

	recordPlanSnapshot(toolResultMessage(ToolTaskUpdate, "c4"), "", record, newToolErrorResult())
	assert.Len(t, recorded, 1, "no plan directory, no snapshot")
}
