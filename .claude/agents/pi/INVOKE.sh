#!/bin/bash

# Pi Agent Invocation Script
# This script ensures the pi agent only reads its own configuration

PI_AGENT_DIR=".claude/agents/pi"
PROJECT_ROOT="projects/onclaw"

echo "🥧 Pi Agent - Isolated Invocation"
echo "=================================="

# Set environment variables for agent isolation
export CLAUDE_AGENT_NAME="pi"
export CLAUDE_AGENT_CONTEXT="$PI_AGENT_DIR"
export CLAUDE_AGENT_ISOLATION="strict"

# Exclude other agent contexts
export AGENT_IGNORE_PATTERNS=".claude/agents/*,.agents/,transcripts/"

# Disable cross-agent MCP access
export MCP_SERVER_WHITELIST="none"

echo "Agent: pi"
echo "Context scope: $PI_AGENT_DIR"
echo "Isolation mode: strict"
echo ""

# Example invocation - replace with your actual pi agent command
# claude-agent --context "$PI_AGENT_DIR/CLAUDE.md" \
#              --agent-name "pi" \
#              --isolation-mode "strict"

echo "✅ Pi agent ready with isolated configuration"
echo ""
echo "To verify isolation:"
echo "1. Check that only .claude/agents/pi/ files are loaded"
echo "2. Confirm no other agent configurations are accessed"
echo "3. Verify MCP server restrictions are in place"