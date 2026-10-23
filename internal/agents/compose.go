package agents

import (
	"context"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	agentcompose "github.com/oniharnantyo/buddies/internal/agents/compose"
	"github.com/oniharnantyo/buddies/internal/agents/subagents"
)

const (
	DefaultMaxIterations             = agentcompose.DefaultMaxIterations
	DefaultMaxLengthForTrunc         = agentcompose.DefaultMaxLengthForTrunc
	DefaultClearRetentionSuffixLimit = agentcompose.DefaultClearRetentionSuffixLimit
	DefaultPlantaskBaseDir           = agentcompose.DefaultPlantaskBaseDir

	ToolAgent      = subagents.ToolAgent
	ToolTaskOutput = subagents.ToolTaskOutput
	ToolTaskStop   = subagents.ToolTaskStop
)

type composeConfig = agentcompose.ComposeConfig
type reductionCapability = agentcompose.ReductionCapability
type plantaskCapability = agentcompose.PlantaskCapability
type filesystemCapability = agentcompose.FilesystemCapability
type skillsCapability = agentcompose.SkillsCapability
type hooksCapability = agentcompose.HooksCapability
type subagentCapability = agentcompose.SubagentCapability
type backgroundTaskCapability = agentcompose.BackgroundTaskCapability
type toolErrorResult = agentcompose.ToolErrorResult

func composeTurnAgent(ctx context.Context, cfg *composeConfig) (adk.TypedAgent[*schema.AgenticMessage], *toolErrorResult, error) {
	return agentcompose.ComposeTurnAgent(ctx, cfg)
}

func buildHandlers(ctx context.Context, cfg *composeConfig) ([]adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage], *toolErrorResult, error) {
	return agentcompose.BuildHandlers(ctx, cfg)
}

func newToolErrorResult() *toolErrorResult {
	return agentcompose.NewToolErrorResult()
}

func isDelegationTool(name string) bool {
	return name == ToolAgent || name == ToolTaskOutput || name == ToolTaskStop
}
