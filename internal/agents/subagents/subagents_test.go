package subagents

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskRegistry_AllocateAndFinish(t *testing.T) {
	reg := NewTaskRegistry("turn-1", nil)
	require.NotNil(t, reg)

	id1 := reg.AllocateTask("/tmp/tasks")
	assert.Equal(t, "task-1", id1)

	id2 := reg.AllocateTask("/tmp/tasks")
	assert.Equal(t, "task-2", id2)

	reg.Finish(id1, TaskStatusCompleted, "done")
	reg.Close()
}
