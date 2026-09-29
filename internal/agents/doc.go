// Package agents implements the per-workspace AI agent runtime: an Eino ADK-based
// agent that composes tools, skills, middleware, and a filesystem jail into
// streamed transcript events.
//
// # Architecture
//
//	The runtime execution is handled by [Runner] (in runner.go), which drives an agent
//	turn in four phases:
//	  1. **Load** — resolves workspace, agent profile, user, and membership from
//	     the store ports injected at construction time.
//	  2. **Resolve** — resolves the tool surface from the built-in registry and the
//	     three-tier skill resolver; validates capability flags.
//	  3. **Compose** — assembles the system instruction via the instruction composer
//	     (injects the embedded base prompt per build, reads IDENTITY.md, SOUL.md
//	     from the agent dir),
//	     and delegates to the pure [Compose] function (in agent.go) to wire the
//	     middleware stack (patchtoolcalls → reduction → summarization → skill → filesystem)
//	     and build the Eino ADK ChatModelAgent.
//	  4. **Execute** — runs the agent through the typed runner; a goroutine maps
//	     every Eino event onto an [EventStream] so callers receive domain-level
//	     [TranscriptEvent] s concurrently with execution.
//
// # Layout
//
// The package's key source files:
//   - agent.go: Pure, stateless ADK agent composition ([Compose], [Config], capability configs).
//     Capability middlewares (patchtoolcalls, reduction, summarization, skill, filesystem) and
//     the delegation stack are inlined directly here.
//   - subagents.go: The delegation capability's composition pieces (add-agent-subagents-background):
//     config validation, the general-purpose clone builder, and the background-lane config types.
//   - background.go: The per-run background task space ([BackgroundTaskSpace]), the notification
//     pump, and the completion payload mapping shared by the delegation and shell lanes.
//   - shell_lane.go: The shell background lane's execute tool — foreground runs reach the shell
//     directly (approval interrupts intact); explicit launches route through the task space with
//     a pre-execution approval check.
//   - runner.go: Production execution pipeline ([Runner], [NewRunner]), instruction composition
//     ([InstructionComposer]), and streaming execution.
//   - runmanager.go: Live-run tracking ([RunKey], runManager): run contexts derive from the
//     runner's base context (never request or stream contexts); explicit cancel and bounded
//     graceful-shutdown drain.
//   - events.go: Domain contracts, transcript events, and execution requests ([ExecRequest], [TranscriptEvent]).
//   - history.go: Session history and event retrieval methods on [Runner].
//   - stream.go: Best-effort live-view tap ([EventStream]): drop-new when unwatched, so an
//     abandoned stream never stalls a run; durable history flows through the session adapter.
//   - tool_registry.go: Built-in tool surface and denylist filtering (extension seam).
//   - model_factory.go: Provider type to Eino chat model mapping (extension seam).
//   - session_adapter.go: Eino ADK session/checkpoint persistence adapter to store interfaces (extension seam / port adapter).
//   - backend/fs_jailed_backend.go: Restricted filesystem backend enforcing the agent directory
//     boundary (extension seam).
//   - backend/skill_backend.go: Three-tier (system, workspace, agent) skill resolution backend (extension seam).
//
// Middlewares are inlined directly into agent.go, and extension seams (tool_registry.go,
// model_factory.go, session_adapter.go, jail.go, backend/skill_backend.go) stand alone with dedicated unit tests.
//
// # Key types
//
//   - [Runner] – the production runtime engine backed by cloudwego/eino/adk.
//   - [Compose] / [Config] – pure, stateless composition step constructing an ADK agent.
//   - [ExecRequest] / [TranscriptEvent] – request and output contracts.
//   - [EventStream] – live delivery channel for transcript events.
//   - [InstructionComposer] – reads disk-prompt files + generates virtual docs.
//   - [SkillsResolver] – three-tier (system / workspace / agent) skill lookup.
//   - [ToolRegistry] – built-in tool surface with denylist filtering.
//   - jail – restricted filesystem backend that prevents path escape.
//
// # Import direction
//
// internal/promptgen produces prompt documents *for* the agents package at
// agent-creation time. Agents never imports promptgen — the dependency flows
// unidirectionally: promptgen → agents.  This keeps the agents package
// independently buildable and avoids circularity between the generation
// subsystem (Phase 2) and the runtime engine (this phase).

package agents // import "github.com/oniharnantyo/onclaw/internal/agents"
