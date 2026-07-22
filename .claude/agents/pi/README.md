# Pi Agent Isolation Guide

## Purpose
Prevent the `pi` agent from reading other coding agents' configurations to reduce noise and conflicts.

## Implementation Steps

### 1. Context Isolation
```bash
# Create pi-specific context directory
mkdir -p .claude/agents/pi/

# Place only pi-relevant context files here
# - CLAUDE.md (pi-specific rules)
# - AGENTS.md (minimal agent context)
```

### 2. Agent Configuration Scoping
When invoking the `pi` agent, use scoped context loading:
- Specify exact context files to load
- Exclude wildcard patterns that would capture other agents
- Use explicit file paths rather than directory globs

### 3. MCP Server Filtering
- Create `.mcp.json` with disabled servers
- Only enable MCP servers essential for `pi` agent functionality
- Prevent connections to servers used by other agents

### 4. Skill Isolation
- Define which skills the `pi` agent should use
- Create a skills allowlist in agent configuration
- Exclude skills that load broader agent contexts

### 5. Memory/State Separation
- Use separate session tracking for `pi` agent
- Prevent reading from other agents' session states
- Isolate memory stores if applicable

## Verification
Test that isolation is working:
```bash
# Check pi agent only loads its own context
# Verify no cross-agent configuration access
# Confirm MCP server restrictions
```

## Benefits
- **Reduced noise**: Pi agent won't be confused by other agent configurations
- **Conflict prevention**: No conflicting rules or instructions
- **Performance**: Faster startup with minimal context loading
- **Clarity**: Clear boundaries for pi agent functionality