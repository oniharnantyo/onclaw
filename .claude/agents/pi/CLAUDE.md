# Pi Agent Context

This context is exclusive to the pi agent. Other agents should not read this configuration.

## Pi Agent Configuration
- **Purpose**: [Define your pi agent's specific purpose]
- **Scope**: Only read files in this directory
- **Excluded**: Other agents' configurations and general project context

## Agent-Specific Rules
- Only load configurations from `.claude/agents/pi/`
- Ignore other agent directories
- Minimal context loading to reduce noise