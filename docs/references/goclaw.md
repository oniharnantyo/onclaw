# Agent Implementation in GoClaw

Based on the codebase exploration, here's a comprehensive overview of how agents are implemented:

## 1. Agent Definition & Storage

Agents are stored in the database with a rich data model that supports both multi-tenancy and flexible configuration:

```go
type AgentData struct {
    BaseModel
    TenantID            uuid.UUID
    AgentKey            string    // Human-readable key (e.g., "goctech-leader")
    DisplayName         string
    Frontmatter         string    // Expertise summary
    OwnerID             string
    Provider            string    // Anthropic, OpenAI, etc.
    Model               string    // Model ID
    ContextWindow       int
    MaxToolIterations   int
    Workspace           string
    AgentType           string    // "open" or "predefined"
    Status              string    // "active", "inactive", "summoning"
    
    // Per-agent JSONB configs (nullable)
    ToolsConfig      json.RawMessage
    SandboxConfig    json.RawMessage
    SubagentsConfig  json.RawMessage
    MemoryConfig     json.RawMessage
}
```

**Store Interface** (`internal/store/agent_store.go`):
- `AgentCRUDStore` - Core CRUD operations
- `AgentAccessStore` - Sharing and access control
- `AgentContextStore` - Context file management
- `AgentProfileStore` - User-agent profiles

`★ Insight ─────────────────────────────────────`
**JSONB for Flexibility:** Agent configs use `json.RawMessage` (nullable JSONB) allowing per-agent customization without schema changes. Each config can use global defaults or agent-specific overrides.

**Multi-Store Pattern:** Four specialized store interfaces separate concerns—CRUD, access control, context, and profiles—making the system modular and testable.
`─────────────────────────────────────────────────`

## 2. Agent Execution Loop (Think→Act→Observe)

The agent loop follows a classic cognitive pattern:

### Main Loop Structure
```go
func (l *Loop) Run(ctx context.Context, req RunRequest) (*RunResult, error) {
    // Emit run.started event
    // Create trace
    // Execute via v3 pipeline
    return l.runViaPipeline(ctx, req)
}
```

### 8-Stage Pipeline Architecture

The pipeline (`internal/pipeline/`) implements a sophisticated execution flow:

1. **ContextStage** - Resolve workspace, load context files, build system prompt
2. **ThinkStage** - Build filtered tools, call LLM, process response
3. **PruneStage** - Trim history, sanitize, compact if needed
4. **ToolStage** - Execute tool calls (parallel or sequential)
5. **ObserveStage** - Drain injection channel, accumulate results
6. **CheckpointStage** - Flush messages to session store
7. **MemoryFlushStage** - Trigger memory consolidation
8. **FinalizeStage** - Persist images, update metadata, cleanup

`★ Insight ─────────────────────────────────────`
**Pipeline Pattern:** The 8-stage pipeline separates concerns elegantly—context loading, thinking, pruning, tool execution, observation, persistence, memory consolidation, and finalization. This makes the system composable and testable.

**Loop Detection:** The system includes loop detection in `internal/agent/toolloop.go` to prevent infinite tool call loops.
`─────────────────────────────────────────────────`

## 3. Agent Types: "Open" vs "Predefined"

```go
const (
    AgentTypeOpen       = "open"         // Per-user context files
    AgentTypePredefined = "predefined"   // Shared agent-level context
)
```

**Open Agents:**
- Each user gets their own context files (USER.md, BOOTSTRAP.md)
- Personalized experience per user
- Seeded on first chat

**Predefined Agents:**
- Shared agent-level context files (SOUL.md, IDENTITY.md)
- Consistent personality and behavior across all users
- Enterprise-grade standardization

`★ Insight ─────────────────────────────────────`
**Dual-Type Architecture:** Open agents provide personalization (like having a personal assistant), while predefined agents provide consistency (like a standardized service). This serves different use cases—personal productivity vs enterprise automation.

**Context File Strategy:** Different context files per type enables the same execution engine to support both personalized and standardized agent personalities.
`─────────────────────────────────────────────────`

## 4. Agent Identity Pattern (Dual Identity)

GoClaw uses a sophisticated dual-identity pattern:

```go
type Loop struct {
    id          string    // Human-readable agent_key (logs, UI, paths)
    agentUUID   uuid.UUID // Canonical DB primary key (SQL, events, OTel)
    tenantID    uuid.UUID // Owning tenant
}
```

**When to use which:**
- **agent_key** (`id`): Logs, UI events, filesystem paths, context keys
- **UUID** (`agentUUID`): SQL WHERE/JOIN, DomainEvent.AgentID, OTel spans, context propagation

`★ Insight ─────────────────────────────────────`
**Dual Identity Pattern:** Using human-readable keys for user-facing surfaces and UUIDs for database operations provides the best of both worlds—readable logs/events and reliable database joins. This pattern applies consistently across agents, teams, and tenants.

**Context Propagation:** Identity is injected into request context via `store.WithAgentID()`, `store.WithAgentKey()`, and `store.WithTenantID()` for tool-level scoping.
`─────────────────────────────────────────────────`

## 5. Agent Configuration & Context Files

### Bootstrap File System

```go
var templateFiles = []string{
    AgentsFile,         // AGENTS.md - Operating instructions
    SoulFile,           // SOUL.md - Persona, tone, boundaries
    ToolsFile,          // TOOLS.md - Local tool notes
    IdentityFile,       // IDENTITY.md - Agent name, emoji, vibe
    UserFile,           // USER.md - User profile
    CapabilitiesFile,   // CAPABILITIES.md - Domain expertise
}
```

### Prompt Modes

```go
type PromptMode string

const (
    PromptFull    PromptMode = "full"    // Main agent - all sections
    PromptTask    PromptMode = "task"    // Enterprise automation - lean
    PromptMinimal PromptMode = "minimal" // Subagent/cron - reduced
    PromptNone    PromptMode = "none"    // Identity line only
)
```

`★ Insight ─────────────────────────────────────`
**Modular Prompt System:** Different prompt modes for different execution contexts optimize token usage and performance. Full prompts for main agents, minimal for subagents—smart resource management.

**Context File Organization:** Separating concerns (identity, tools, capabilities, user profile) into different files enables composition and reuse. The same execution engine loads different files based on agent type and scenario.
`─────────────────────────────────────────────────`

## 6. Agent Invocation & Management

### Loop Configuration

```go
type LoopConfig struct {
    ID               string
    Provider         providers.Provider
    Model            string
    ContextWindow    int
    MaxTokens        int
    MaxIterations    int
    MaxToolCalls     int
    Workspace        string
    AgentUUID        uuid.UUID
    TenantID         uuid.UUID
    AgentType        string
    DisplayName      string
    
    // Per-user profile + file seeding
    EnsureUserProfile EnsureUserProfileFunc
    SeedUserFiles     SeedUserFilesFunc
    ContextFileLoader ContextFileLoaderFunc
}
```

### Run Request Structure

```go
type RunRequest struct {
    SessionKey         string    // Composite key
    Message            string    // User message
    Media              []bus.MediaFile
    Channel            string    // Source channel
    RunID              string    // Unique run identifier
    UserID             string
    Stream             bool
    MaxIterations      int
    ModelOverride      string
    
    // Run classification
    RunKind       string // "delegation", "announce"
    DelegationID  string
    TeamID        string
    TeamTaskID    string
    ParentAgentID string
}
```

`★ Insight ─────────────────────────────────────`
**Composite Session Keys:** Session keys use composite format (`agent:{agentId}:{channel}:{peerKind}:{chatId}`) enabling multi-dimensional routing and scoping. This supports complex delegation and team workflows.

**Flexible Configuration:** Loop config supports per-request overrides (model, max iterations) while maintaining agent defaults. This balances standardization with flexibility.
`─────────────────────────────────────────────────`

## Key Architecture Patterns

1. **Pipeline Architecture** - 8-stage pipeline for composable execution
2. **Dual Identity** - agent_key for humans, UUID for databases
3. **Store Interfaces** - Separated concerns (CRUD, access, context, profiles)
4. **Context Propagation** - Request-scoped context injection
5. **JSONB Configs** - Nullable per-agent configs with global defaults
6. **Prompt Modes** - Different prompts for different execution contexts
7. **Type System** - Open vs predefined agents for different use cases
8. **Bootstrap System** - Template-based context file seeding

This architecture provides a sophisticated, production-grade agent system that supports personalization, enterprise standardization, multi-tenancy, and complex delegation workflows.