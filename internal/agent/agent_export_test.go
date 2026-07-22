package agent

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func NewTestAssembleOpts(t *testing.T, overrides ...func(*AssembleAgentOpts)) AssembleAgentOpts {
	opts := AssembleAgentOpts{
		ContextWindow:    64000,
		ConversationID:   1,
		Channel:          "test",
		KGTraversalDepth: 3,
		ShellPolicy:      "deny",
	}
	for _, o := range overrides {
		o(&opts)
	}
	return opts
}

var EstimateFloorTokens = estimateFloorTokens

const MaxPersonaBytes = maxPersonaBytes

var BuildTranscriptPath = buildTranscriptPath

// NewEventIterator is a test helper to construct an eventIterator.
func NewEventIterator(
	ctx context.Context,
	iterator *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]],
	currentStream *schema.StreamReader[*schema.AgenticMessage],
	err error,
	onTurnError func(error),
) EventIterator {
	return &eventIterator{
		ctx:           ctx,
		iterator:      iterator,
		currentStream: currentStream,
		err:           err,
		onTurnError:   onTurnError,
	}
}
