package agents

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	bgtask "github.com/cloudwego/eino/adk/backgroundtask"
	backgroundlocal "github.com/cloudwego/eino/adk/backgroundtask/local"
	fsmw "github.com/cloudwego/eino/adk/middlewares/filesystem"
	"github.com/cloudwego/eino/adk/middlewares/subagent"
)

// TestNewBackgroundTaskSpace_IndependentTaskSpaces: each space owns its own
// Manager, store, and Runner, so two runs never share a task-id space
// (add-agent-subagents-background D5 — per-run construction).
func TestNewBackgroundTaskSpace_IndependentTaskSpaces(t *testing.T) {
	ctx := context.Background()

	first, err := newBackgroundTaskSpace(ctx)
	if err != nil {
		t.Fatalf("newBackgroundTaskSpace: %v", err)
	}
	second, err := newBackgroundTaskSpace(ctx)
	if err != nil {
		t.Fatalf("newBackgroundTaskSpace: %v", err)
	}

	if first.Manager == second.Manager || first.Store == second.Store || first.Runner == second.Runner {
		t.Fatal("two spaces share a Manager, store, or Runner")
	}

	tasks := make([]*bgtask.Task, 2)
	for i, space := range []*BackgroundTaskSpace{first, second} {
		task, err := space.Runner.Run(ctx, &backgroundlocal.Input{
			Kind:            subagent.TaskKindSubagent,
			Description:     "local task",
			SessionID:       "sess_independent",
			NotifySession:   true,
			RunInBackground: true,
		}, func(ctx context.Context, runtime bgtask.ExecutionRuntime) (string, error) {
			return "done", nil
		})
		if err != nil {
			t.Fatalf("Runner.Run: %v", err)
		}
		tasks[i] = task
	}
	if tasks[0].Spec.ID == tasks[1].Spec.ID {
		t.Fatalf("independent spaces allocated colliding task id %q", tasks[0].Spec.ID)
	}
	if _, err := first.Manager.Get(ctx, tasks[1].Spec.ID); !errors.Is(err, bgtask.ErrNotFound) {
		t.Fatalf("first Manager sees second space's task: err = %v, want ErrNotFound", err)
	}
	if _, err := second.Manager.Get(ctx, tasks[0].Spec.ID); !errors.Is(err, bgtask.ErrNotFound) {
		t.Fatalf("second Manager sees first space's task: err = %v, want ErrNotFound", err)
	}
}

// TestPumpBackgroundNotifications_EmitsSingleCompletion is task 3.3's
// verify: a real background closure through the local lane, one pump pass —
// exactly one completion payload with the right task id, outcome, lane kind,
// output path, and summary — and a second pump pass emits nothing (the acks
// consumed the outbox).
func TestPumpBackgroundNotifications_EmitsSingleCompletion(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	space, err := newBackgroundTaskSpace(ctx)
	if err != nil {
		t.Fatalf("newBackgroundTaskSpace: %v", err)
	}

	outputPath := TasksOutputDir + "/pump-test.output"
	task, err := space.Runner.Run(ctx, &backgroundlocal.Input{
		Kind:            subagent.TaskKindSubagent,
		Description:     "research sweep",
		SessionID:       "sess_test",
		NotifySession:   true,
		RunInBackground: true,
		OutputFile:      outputPath,
	}, func(ctx context.Context, runtime bgtask.ExecutionRuntime) (string, error) {
		return "report", nil
	})
	if err != nil {
		t.Fatalf("Runner.Run: %v", err)
	}
	waitForTaskStatus(t, space, task.Spec.ID, bgtask.StatusCompleted)

	// First pump pass: the outbox holds task_created and completed; only the
	// terminal one may surface.
	first := &emissionCollector{}
	stopFirst := runPump(ctx, space, first)
	defer stopFirst()

	waitForEmissions(t, first, 1, 5*time.Second)
	// Two more poll ticks: still exactly one — no duplicates.
	time.Sleep(2*notificationPollInterval + 100*time.Millisecond)
	if got := first.len(); got != 1 {
		t.Fatalf("emissions after settle = %d, want exactly 1", got)
	}

	got := first.snapshot()[0]
	want := TaskCompletedPayload{
		TaskID:     task.Spec.ID,
		Kind:       "delegation",
		Outcome:    "completed",
		OutputPath: outputPath,
		Summary:    "research sweep",
	}
	if got != want {
		t.Fatalf("completion payload = %+v, want %+v", got, want)
	}

	// Second pump pass over the same drained space: nothing left to emit.
	second := &emissionCollector{}
	stopSecond := runPump(ctx, space, second)
	defer stopSecond()
	time.Sleep(3*notificationPollInterval + 100*time.Millisecond)
	if got := second.len(); got != 0 {
		t.Fatalf("second pump pass emitted %d payloads, want 0 (acks consumed the outbox)", got)
	}
}

// TestPumpBackgroundNotifications_CanceledOutcome: a canceled background
// shell task surfaces as exactly one canceled-completion payload with the
// shell lane kind.
func TestPumpBackgroundNotifications_CanceledOutcome(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	space, err := newBackgroundTaskSpace(ctx)
	if err != nil {
		t.Fatalf("newBackgroundTaskSpace: %v", err)
	}

	task, err := space.Runner.Run(ctx, &backgroundlocal.Input{
		Kind:            fsmw.ExecuteTaskKind,
		Description:     "long sleep",
		SessionID:       "sess_test",
		NotifySession:   true,
		RunInBackground: true,
	}, func(ctx context.Context, runtime bgtask.ExecutionRuntime) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	if err != nil {
		t.Fatalf("Runner.Run: %v", err)
	}
	// Let the attempt start, then stop it: the executor's control branch
	// cancels the work and acknowledges the cancellation as terminal.
	time.Sleep(50 * time.Millisecond)
	if _, err := space.Manager.RequestCancel(context.Background(), task.Spec.ID); err != nil {
		t.Fatalf("RequestCancel: %v", err)
	}
	waitForTaskStatus(t, space, task.Spec.ID, bgtask.StatusCanceled)

	collected := &emissionCollector{}
	stop := runPump(ctx, space, collected)
	defer stop()

	waitForEmissions(t, collected, 1, 5*time.Second)
	got := collected.snapshot()[0]
	if got.TaskID != task.Spec.ID {
		t.Fatalf("payload task id = %q, want %q", got.TaskID, task.Spec.ID)
	}
	if got.Outcome != "canceled" {
		t.Fatalf("payload outcome = %q, want \"canceled\"", got.Outcome)
	}
	if got.Kind != "shell" {
		t.Fatalf("payload kind = %q, want \"shell\"", got.Kind)
	}
	if got.Summary != "long sleep" {
		t.Fatalf("payload summary = %q, want \"long sleep\"", got.Summary)
	}
}

// waitForTaskStatus polls the space's Manager until the task reaches the
// wanted terminal status.
func waitForTaskStatus(t *testing.T, space *BackgroundTaskSpace, taskID string, want bgtask.Status) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		task, err := space.Manager.Get(context.Background(), taskID)
		if err != nil {
			t.Fatalf("get task %s: %v", taskID, err)
		}
		if task.Status == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("task %s status = %q after 5s, want %q", taskID, task.Status, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// emissionCollector gathers the payloads the pump emits.
type emissionCollector struct {
	mu       sync.Mutex
	payloads []TaskCompletedPayload
}

func (c *emissionCollector) collect(p TaskCompletedPayload) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.payloads = append(c.payloads, p)
}

func (c *emissionCollector) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.payloads)
}

func (c *emissionCollector) snapshot() []TaskCompletedPayload {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]TaskCompletedPayload(nil), c.payloads...)
}

// runPump starts the pump on its own goroutine and returns its stopper.
func runPump(ctx context.Context, space *BackgroundTaskSpace, collected *emissionCollector) func() {
	pumpCtx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		pumpBackgroundNotifications(pumpCtx, space, collected.collect)
	}()
	return func() {
		stop()
		<-done
	}
}

// waitForEmissions blocks until the collector holds at least want payloads.
func waitForEmissions(t *testing.T, collected *emissionCollector, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for collected.len() < want {
		if time.Now().After(deadline) {
			t.Fatalf("expected %d emission(s), got %d after %s", want, collected.len(), timeout)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
