package agent

import (
	"context"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func SummarizationTrigger(contextWindow int) int {
	return summarizationTrigger(contextWindow)
}

const MaxPersonaBytes = maxPersonaBytes

type HandleSummarizationParams = handleSummarizationParams

var HandleSummarization = handleSummarization

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
