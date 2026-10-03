// Package suite defines the bench task-fixture format: the JSON schema for
// task suites, loading, semantic validation, and the ABC authoring lint
// (solvability without the agent, grader consistency, unintervention).
//
// Suite files are plain JSON data authored under bench/suites/*.json. The
// package is pure data + pure functions — no HTTP, no model calls — so lane
// files can be validated in unit tests before they ever cost live tokens.
//
// This module is deliberately decoupled from the onclaw module (design D1 of
// openspec/changes/add-harness-bench): it must never import
// github.com/oniharnantyo/onclaw and instead re-implements the small pieces
// of semantics it needs (e.g. NormalizeText, ported from internal/memory/eval).
package suite

// Canonical lane ids (design D6). A suite file covers exactly one lane; the
// lane id must be one of these six.
const (
	LaneCompaction          = "compaction"
	LaneToolError           = "tool-error"
	LaneInstructionFidelity = "instruction-fidelity"
	LaneDelegation          = "delegation"
	LaneMemory              = "memory"
	LaneAnchor              = "anchor"
)

// canonicalLanes is the fixed canonical order used for deterministic
// scoreboard lane ordering and lint output.
var canonicalLanes = [6]string{
	LaneCompaction,
	LaneToolError,
	LaneInstructionFidelity,
	LaneDelegation,
	LaneMemory,
	LaneAnchor,
}

// CanonicalLanes returns the six canonical lane ids in their fixed canonical
// order. Callers may mutate the returned slice freely.
func CanonicalLanes() []string {
	out := make([]string, len(canonicalLanes))
	copy(out, canonicalLanes[:])
	return out
}

// IsCanonicalLane reports whether lane is one of the six canonical lane ids.
func IsCanonicalLane(lane string) bool {
	for _, l := range canonicalLanes {
		if lane == l {
			return true
		}
	}
	return false
}

// Setup step discriminator values.
const (
	StepChatTurns   = "chat_turns"
	StepAgentConfig = "agent_config"
	StepHook        = "hook"
	StepConnection  = "connection"
	StepWait        = "wait"
)

// Check kind discriminator values. All checks of a task are AND-ed into the
// task verdict.
const (
	CheckAnswerContains      = "answer_contains"
	CheckAnswerNotContains   = "answer_not_contains"
	CheckToolCalled          = "tool_called"
	CheckToolNotCalled       = "tool_not_called"
	CheckTranscriptFileWrite = "transcript_file_write"
	CheckAPICEffect          = "api_effect"
	CheckJudge               = "judge"
)

// Hook handler types. The bench deterministically blocks a tool call mid-run
// by registering a workspace hook, in one of two variants:
//
//   - HandlerHTTP: the handler URL is served by the bench process's own
//     loopback control server. Usable only against a harness whose hook http
//     fetcher allows private/loopback targets.
//   - HandlerCommand: the handler is a shell script body the runner writes to
//     a temp file and the harness executes via /bin/sh (exit 2 = block, exit
//     0 = allow). This assumes the bench shares the filesystem with the
//     onclaw server — the documented local protocol — and sidesteps the http
//     hook fetcher's private-target rejection entirely.
const (
	HandlerHTTP    = "http"
	HandlerCommand = "command"
)

// Placeholders substituted by the runner at execution time:
//
//   - PlaceholderControlURL inside a hook handler URL is replaced with the
//     bench's local control-server base URL.
//   - PlaceholderControlFile inside a command handler script body is replaced
//     with a per-hook control-file path the runner provisions (optional).
//   - PlaceholderWorkspace / PlaceholderAgent inside an api_effect path are
//     replaced with the fixture workspace / agent identifiers.
const (
	PlaceholderControlURL  = "{{control_url}}"
	PlaceholderControlFile = "{{control_file}}"
	PlaceholderWorkspace   = "{{workspace}}"
	PlaceholderAgent       = "{{agent}}"
)

// TaskSuite is one lane's suite file. Suite files live at
// bench/suites/*.json, one per lane; every field below is authored in the
// JSON file with these exact snake_case names.
type TaskSuite struct {
	// SuiteVersion identifies the suite content generation. All lane files
	// executed in one bench run must carry the same version — the runner
	// enforces that and records it in the scoreboard.
	SuiteVersion string `json:"suite_version"`
	// Lane is the canonical lane id this suite covers. Lint-enforced.
	Lane string `json:"lane"`
	// Description is a human-facing summary of what the lane measures.
	Description string `json:"description"`
	// JudgeAllowed gates the `judge` check kind: a judge check is a lint
	// failure unless the suite sets judge_allowed: true (deterministic
	// graders first, design D5).
	JudgeAllowed bool `json:"judge_allowed"`
	// Tasks is the lane's task list; at least one task (lint-enforced).
	Tasks []Task `json:"tasks"`
}

// Task is one benchmark task: scripted setup, measured turns, and the
// verification checks that decide the verdict. All checks are AND-ed.
type Task struct {
	// ID is unique within the suite and kebab-case (lint-enforced).
	ID string `json:"id"`
	// Title is a short human-facing name.
	Title string `json:"title"`
	// Summary describes the scenario for reviewers. It participates in the
	// unintervention check: it must not contain long expected literals from
	// the task's own checks.
	Summary string `json:"summary"`
	// ToolsUsed lists the tools the task is expected to exercise, e.g.
	// "files.write". Non-empty, dotted-lowercase shaped (lint-enforced).
	ToolsUsed []string `json:"tools_used"`
	// Setup runs before the measured turns: context seeding, agent
	// configuration, hooks, connections, waits.
	Setup []SetupStep `json:"setup,omitempty"`
	// Turns is the measured interaction: each turn is one chat message sent
	// through the live agent.
	Turns []TaskTurn `json:"turns"`
	// Verify are the checks run against the whole task transcript (final
	// answer text + all tool calls) to decide the verdict.
	Verify []Check `json:"verify"`
}

// SetupStep is a discriminated union on Type. The variant's fields live flat
// on the step; unused variant fields must be omitted. The variants:
//
//   - chat_turns:   scripted context-seeding turns sent through the real chat
//     path as the named fixture actor in the named logical session.
//   - agent_config: fields applied to the fixture agent before the task
//     (empty/nil = leave unchanged). ContextWindow is an agent-level token
//     override the harness uses to force early compaction.
//   - hook:         registers a workspace hook. An http handler's URL may
//     contain PlaceholderControlURL, substituted by the runner with its local
//     control-server base URL; a command handler's script body may contain
//     PlaceholderControlFile, substituted with a per-hook control-file path.
//     This is how the bench deterministically blocks a tool call mid-run.
//   - connection:   registers an integration connection (e.g. a deliberately
//     dead MCP server for tool-error tasks).
//   - wait:         sleeps the given number of seconds.
type SetupStep struct {
	Type string `json:"type"`

	// chat_turns
	Actor   string   `json:"actor,omitempty"` // fixture actor name
	Session string   `json:"session,omitempty"`
	Turns   []string `json:"turns,omitempty"`

	// agent_config
	Model         string   `json:"model,omitempty"`
	ContextWindow *int     `json:"context_window,omitempty"` // agent-level token override
	DisabledTools []string `json:"disabled_tools,omitempty"`

	// hook
	Name        string       `json:"name,omitempty"`
	Event       string       `json:"event,omitempty"` // e.g. "pre_tool_use"
	MatcherTool string       `json:"matcher_tool,omitempty"`
	Handler     *HookHandler `json:"handler,omitempty"`

	// connection
	Recipe string            `json:"recipe,omitempty"`
	Config map[string]string `json:"config,omitempty"`

	// wait
	Seconds int `json:"seconds,omitempty"`
}

// HookHandler is the handler registered for a hook step. Two variants:
//
//   - http: Handler.URL is served by the bench's loopback control server and
//     must contain PlaceholderControlURL (lint-enforced). Usable only against
//     a harness that allows private hook targets.
//   - command: Handler.Command is a shell script body the runner writes to a
//     temp file and the harness executes via /bin/sh — exit 2 blocks (stderr
//     becomes the block reason), exit 0 allows — assuming the bench shares
//     the filesystem with the onclaw server (the documented local protocol).
//     Command is required (lint-enforced); it may contain
//     PlaceholderControlFile, which the runner substitutes with a per-hook
//     control-file path.
type HookHandler struct {
	Type    string `json:"type"`
	URL     string `json:"url"`
	Command string `json:"command,omitempty"`
}

// TaskTurn is one measured chat message. Actor is optional: empty means the
// runner's primary fixture actor for the suite.
type TaskTurn struct {
	Actor string `json:"actor,omitempty"`
	Text  string `json:"text"`
}

// Check is a discriminated union on Kind; all fields of the variant live flat
// on the struct and unused fields must be omitted. All checks of a task are
// AND-ed into the task verdict. Verification runs against the whole task
// transcript (final answer text + all tool calls).
//
// String probes match on normalized text (see NormalizeText) unless stated
// otherwise; a probe documented as optional is skipped when empty.
type Check struct {
	Kind string `json:"kind"`

	// answer_contains: any_of groups are alias groups (hit if any group has
	// at least one alias present); all_of groups must ALL hit.
	AnyOf [][]string `json:"any_of,omitempty"`
	AllOf [][]string `json:"all_of,omitempty"`

	// answer_not_contains
	NoneOf []string `json:"none_of,omitempty"`

	// tool_called / tool_not_called
	Tool string `json:"tool,omitempty"`

	// tool_called: optional substring probes over the tool call's arguments
	// and output; empty = skipped.
	ArgsContains   string `json:"args_contains,omitempty"`
	OutputContains string `json:"output_contains,omitempty"`

	// transcript_file_write: asserts a tool card wrote Path (matched against
	// files.write-style tool calls in the transcript). ContentContains is an
	// optional probe over the written content — empty asserts only that the
	// path was written.
	Path            string `json:"path,omitempty"`
	ContentContains string `json:"content_contains,omitempty"`

	// api_effect: the runner performs an authenticated workspace API call.
	// Path may use PlaceholderWorkspace / PlaceholderAgent.
	Method       string   `json:"method,omitempty"`
	Status       int      `json:"status,omitempty"`
	JSONContains []string `json:"json_contains,omitempty"`

	// judge: pinned-rubric LLM judge, only where unavoidable (design D5).
	// Lint failure unless the suite sets judge_allowed: true.
	Rubric string `json:"rubric,omitempty"`
}

// String renders a compact identifier for the check, used in scoreboard
// details and lint messages.
func (c Check) String() string {
	return c.Kind
}
