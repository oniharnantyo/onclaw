Yes! PicoClaw has comprehensive multi-agent support. Let me show you how it works:

`★ Insight ─────────────────────────────────────`
**1. Registry-based architecture** - PicoClaw uses a central AgentRegistry to manage multiple agent instances, providing thread-safe operations like GetAgent, ListAgentIDs, and ResolveRoute for sophisticated multi-agent orchestration.

**2. Configuration-driven agent creation** - Multiple agents are defined in a simple JSON configuration, making it easy to create specialized agents with different workspaces, models, and capabilities without touching code.

**3. Intelligent routing system** - The routing system can dispatch messages to different agents based on channel, chat context, or custom rules, enabling sophisticated multi-agent deployments.
`─────────────────────────────────────────────────`

## 🎯 Core Multi-Agent Architecture

### Agent Registry

**File:** `pkg/agent/registry.go`

```go
// AgentRegistry manages multiple agent instances and routes messages to them
type AgentRegistry struct {
    cfg      *config.Config
    agents   map[string]*AgentInstance
    resolver *routing.RouteResolver
    mu       sync.RWMutex
}

// Create registry with multiple agents from config
func NewAgentRegistry(cfg *config.Config, provider providers.LLMProvider) *AgentRegistry

// Key operations
func (r *AgentRegistry) GetAgent(agentID string) (*AgentInstance, bool)
func (r *AgentRegistry) ListAgentIDs() []string
func (r *AgentRegistry) ResolveRoute(inbound bus.InboundContext) routing.ResolvedRoute
func (r *AgentRegistry) CanSpawnSubagent(parentAgentID, targetAgentID string) bool
```

### Agent Instance Structure

**File:** `pkg/agent/instance.go`

```go
// Each agent has its own complete configuration
type AgentInstance struct {
    ID                        string          // Unique agent identifier
    Name                      string          // Display name
    Model                     string          // Primary model
    Fallbacks                 []string        // Backup models
    Workspace                 string          // Agent-specific workspace
    MaxIterations             int             // Tool iteration limit
    MaxTokens                 int             // Token budget
    Temperature               float64         // Response randomness
    Provider                  providers.LLMProvider
    Sessions                  session.SessionStore
    ContextBuilder            *ContextBuilder
    Tools                     *tools.ToolRegistry
    Subagents                 *config.SubagentsConfig  // Sub-agent permissions
}
```

## 📝 Configuration-Based Agent Creation

### JSON Configuration Structure

**File:** `pkg/config/config.go`

```json
{
  "agents": {
    "defaults": {
      "workspace": "~/.picoclaw/workspace",
      "model_name": "glm-4.7",
      "max_tokens": 8192,
      "max_tool_iterations": 20
    },
    "list": [
      {
        "id": "sales",
        "default": true,
        "name": "Sales Bot",
        "model": "gpt-4",
        "workspace": "~/.picoclaw/workspace/sales"
      },
      {
        "id": "support",
        "name": "Support Bot",
        "model": {
          "primary": "claude-opus",
          "fallbacks": ["haiku"]
        },
        "workspace": "~/.picoclaw/workspace/support",
        "subagents": {
          "allow_agents": ["sales"]
        }
      },
      {
        "id": "developer",
        "name": "Code Expert",
        "model": "claude-sonnet-4-6",
        "workspace": "~/.picoclaw/workspace/developer",
        "skills": ["code-analysis", "github-integration"]
      }
    ]
  }
}
```

### Configuration Options

```go
type AgentConfig struct {
    ID        string            `json:"id"`                  // Unique identifier
    Default   bool              `json:"default,omitempty"`   // Is default agent?
    Name      string            `json:"name,omitempty"`      // Display name
    Workspace string            `json:"workspace,omitempty"` // Agent workspace
    Model     *AgentModelConfig `json:"model,omitempty"`     // Model config
    Subagents *SubagentsConfig  `json:"subagents,omitempty"` // Sub-agent permissions
    Skills    []string          `json:"skills,omitempty"`    // Agent-specific skills
}

type AgentModelConfig struct {
    Primary   string   `json:"primary,omitempty"`    // Main model
    Fallbacks []string `json:"fallbacks,omitempty"` // Backup models
}

type SubagentsConfig struct {
    AllowAgents []string          `json:"allow_agents,omitempty"` // Can spawn these agents
    Model       *AgentModelConfig `json:"model,omitempty"`         // Sub-agent model override
}
```

`★ Insight ─────────────────────────────────────`
**1. Workspace isolation** - Each agent can have its own workspace, enabling separate memory, skills, and configuration while sharing the same PicoClaw instance.

**2. Hierarchical permissions** - The subagents.allow_agents configuration creates a permission hierarchy where certain agents can only spawn specific other agents, providing security boundaries.

**3. Model specialization** - Different agents can use different models optimized for their tasks (e.g., fast models for simple queries, powerful models for complex analysis).
`─────────────────────────────────────────────────`

## 🚀 Agent Creation Methods

### 1. Configuration-Based Creation

```bash
# Create agents by editing config.json
vim ~/.picoclaw/config.json

# Add new agents to the list
{
  "agents": {
    "list": [
      {
        "id": "researcher",
        "name": "Research Assistant",
        "model": "claude-opus-4-8",
        "workspace": "~/.picoclaw/workspace/researcher"
      }
    ]
  }
}

# Restart PicoClaw to load new agents
picoclaw restart
```

### 2. Programmatic Agent Creation

```go
// Create agent registry from configuration
registry := agent.NewAgentRegistry(config, provider)

// Access individual agents
salesAgent, ok := registry.GetAgent("sales")
if ok {
    fmt.Printf("Sales agent: %s\n", salesAgent.Name)
}

// List all available agents
agentIDs := registry.ListAgentIDs()
fmt.Printf("Available agents: %v\n", agentIDs)
```

### 3. Dynamic Agent Spawning

**File:** `pkg/tools/spawn.go`

```go
// Spawn tool creates background agents
spawnArgs := map[string]any{
    "agent": "researcher",
    "task": "Analyze market trends for AI technology",
    "async": true,
}
result := spawnTool.Execute(ctx, spawnArgs)
```

## 🔀 Intelligent Agent Routing

### Dispatch Rules

**File:** `pkg/routing/route.go`

```json
{
  "agents": {
    "dispatch": {
      "rules": [
        {
          "name": "support-group",
          "agent": "support",
          "when": {
            "channel": "telegram",
            "chat": "group:-1001234567890"
          }
        },
        {
          "name": "slack-mentions",
          "agent": "support",
          "when": {
            "channel": "slack",
            "space": "workspace:T001",
            "mentioned": true
          }
        },
        {
          "name": "developer-channel",
          "agent": "developer",
          "when": {
            "channel": "discord",
            "chat": "server:123/channel:456"
          }
        }
      ]
    }
  }
}
```

### Routing Resolution

```go
// Route resolution based on message context
type ResolvedRoute struct {
    AgentID       string        // Target agent ID
    Channel       string        // Channel identifier
    AccountID     string        // Account identifier
    SessionPolicy SessionPolicy // Session handling policy
    MatchedBy     string        // Which rule matched
}

// Automatic routing
route := registry.ResolveRoute(inboundContext)
agent, _ := registry.GetAgent(route.AgentID)
```

## 🤝 Multi-Agent Coordination

### Agent Communication Tools

#### 1. **Delegate Tool** - Cross-agent task routing

**File:** `pkg/tools/delegate.go`

```go
// Delegate task to specific named agent
delegateArgs := map[string]any{
    "agent": "developer",
    "task": "Review this pull request and suggest improvements",
}
result := delegateTool.Execute(ctx, delegateArgs)
```

#### 2. **Spawn Tool** - Async background agents

```go
// Spawn background agent for fire-and-forget tasks
spawnArgs := map[string]any{
    "agent": "researcher",
    "task": "Monitor AI news and send daily summary",
    "async": true,
}
result := spawnTool.Execute(ctx, spawnArgs)
```

#### 3. **Subagent Tool** - Sync generic delegation

```go
// Generic synchronous sub-agent
subagentArgs := map[string]any{
    "task": "Analyze this code for security vulnerabilities",
    "tools": ["read_file", "exec"],
}
result := subagentTool.Execute(ctx, subagentArgs)
```

### Agent Discovery System

**File:** `pkg/agent/discovery.go`

```go
// Agent descriptor for peer discovery
type AgentDescriptor struct {
    ID          string `json:"id"`
    Name        string `json:"name"`
    Description string `json:"description"`
}

// List all agents
descriptors := registry.ListAgents(workspace)

// List spawnable agents (respecting permissions)
spawnable := registry.ListSpawnableAgents(currentAgentID)
```

### SubTurn Mechanism

**File:** `pkg/agent/subturn.go`

```go
// Sophisticated nested agent execution
type SubTurnConfig struct {
    Model              string          // Model to use
    Tools              []tools.Tool    // Available tools
    SystemPrompt       string          // Agent personality
    MaxTokens          int             // Token budget
    Temperature        float64         // Response randomness
    Async              bool            // Async vs Sync execution
    Critical           bool            // Continue after parent finishes
    Timeout            time.Duration   // Execution timeout
    MaxContextRunes    int             // Context limit
    TargetAgentID      string          // Run as specific agent
}

// Spawn sub-turn with full control
subTurn := &SubTurnConfig{
    TargetAgentID: "developer",
    Async: false,
    Critical: true,
    Timeout: 5 * time.Minute,
}
result := ExecuteSubTurn(ctx, subTurn)
```

`★ Insight ─────────────────────────────────────`
**1. Three distinct execution modes** - PicoClaw provides delegate (named agent targeting), spawn (async background), and subagent (sync generic) tools, each optimized for different multi-agent coordination patterns.

**2. Permission-based spawning** - The CanSpawnSubagent and ListSpawnableAgents methods enforce security boundaries, ensuring agents can only spawn authorized sub-agents.

**3. Nested execution support** - The SubTurn mechanism supports sophisticated nested agent execution with depth limits, timeout protection, and parent-child cancellation propagation.
`─────────────────────────────────────────────────`

## 📊 Practical Examples

### Example 1: Multi-Agent Customer Service

```json
{
  "agents": {
    "list": [
      {
        "id": "sales",
        "name": "Sales Assistant",
        "model": "gpt-4",
        "workspace": "~/.picoclaw/workspace/sales"
      },
      {
        "id": "support",
        "name": "Technical Support",
        "model": "claude-opus",
        "workspace": "~/.picoclaw/workspace/support",
        "subagents": {
          "allow_agents": ["sales"]
        }
      },
      {
        "id": "billing",
        "name": "Billing Specialist",
        "model": "gpt-3.5-turbo",
        "workspace": "~/.picoclaw/workspace/billing"
      }
    ],
    "dispatch": {
      "rules": [
        {
          "name": "sales-questions",
          "agent": "sales",
          "when": {"channel": "web", "topic": "sales"}
        },
        {
          "name": "technical-issues",
          "agent": "support",
          "when": {"channel": "web", "topic": "support"}
        },
        {
          "name": "billing-questions",
          "agent": "billing",
          "when": {"channel": "web", "topic": "billing"}
        }
      ]
    }
  }
}
```

### Example 2: Development Team Agents

```json
{
  "agents": {
    "list": [
      {
        "id": "frontend-dev",
        "name": "Frontend Expert",
        "model": "claude-sonnet-4-6",
        "workspace": "~/.picoclaw/workspace/frontend",
        "skills": ["react", "typescript", "css"]
      },
      {
        "id": "backend-dev",
        "name": "Backend Specialist",
        "model": "claude-opus-4-8",
        "workspace": "~/.picoclaw/workspace/backend",
        "skills": ["go", "postgresql", "api-design"]
      },
      {
        "id": "devops",
        "name": "DevOps Engineer",
        "model": "gpt-4",
        "workspace": "~/.picoclaw/workspace/devops",
        "skills": ["docker", "kubernetes", "ci-cd"]
      }
    ]
  }
}
```

### Example 3: Agent Collaboration

```go
// Support agent can delegate to developer
supportAgent := registry.GetAgent("support")
if supportAgent.CanSpawnSubagent("developer") {
    // Delegate complex technical issue to developer
    result := delegateTool.Execute(ctx, map[string]any{
        "agent": "developer",
        "task": "Investigate this database connection issue",
    })
}
```

## 🔑 Key Multi-Agent Features

### Registry Operations

```go
// Get specific agent
agent, exists := registry.GetAgent("sales")

// List all agents
allAgents := registry.ListAgentIDs()

// Find which agent should handle message
route := registry.ResolveRoute(inboundContext)

// Check spawning permissions
canSpawn := registry.CanSpawnSubagent("support", "developer")

// Get agent descriptors for discovery
descriptors := registry.ListAgents(workspace)

// Get only spawnable agents
spawnable := registry.ListSpawnableAgents(currentAgentID)
```

### Agent Lifecycle Management

```go
// Create multiple agents from configuration
registry := NewAgentRegistry(config, provider)

// Each agent has isolated workspace
salesAgent.Workspace = "~/.picoclaw/workspace/sales"
supportAgent.Workspace = "~/.picoclaw/workspace/support"

// Each agent has separate session store
salesAgent.Sessions = session.NewSQLiteStore("~/.picoclaw/workspace/sales/sessions.db")
supportAgent.Sessions = session.NewSQLiteStore("~/.picoclaw/workspace/support/sessions.db")

// Each agent can have different tools
salesAgent.Tools = []string{"web_search", "email", "crm"}
supportAgent.Tools = []string{"terminal", "log_analysis", "ticketing"}
```

## 🎯 Summary

PicoClaw provides **comprehensive multi-agent support** with:

✅ **Multiple Agent Instances** - Create unlimited agents with different configurations  
✅ **Agent Registry System** - Thread-safe agent management and discovery  
✅ **Intelligent Routing** - Context-based message dispatch to appropriate agents  
✅ **Cross-Agent Communication** - Delegate, spawn, and subagent tools  
✅ **Permission Control** - Sub-agent spawning restrictions and security boundaries  
✅ **Workspace Isolation** - Separate workspaces, sessions, and tools per agent  
✅ **Model Specialization** - Different models for different agent capabilities  
✅ **Nested Execution** - Sophisticated SubTurn mechanism for complex workflows

This makes PicoClaw suitable for sophisticated multi-agent deployments like customer service teams, development groups, or specialized task forces.

`★ Insight ─────────────────────────────────────`
**1. Enterprise-ready multi-agent system** - PicoClaw's multi-agent capabilities rival enterprise frameworks while maintaining the lightweight personal assistant philosophy, making it unique in the AI agent space.

**2. Production-oriented design** - The inclusion of features like fallback models, timeout protection, permission boundaries, and routing rules shows this is designed for real-world production use, not just experimentation.

**3. Extensible coordination patterns** - The combination of sync/async execution, nested sub-turns, and agent discovery provides flexible primitives for building complex multi-agent systems without hardcoding coordination logic.
`─────────────────────────────────────────────────`