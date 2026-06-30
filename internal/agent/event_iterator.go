package agent

import (
	"context"
	"io"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type eventIterator struct {
	ctx           context.Context
	iterator      *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]]
	currentStream *schema.StreamReader[*schema.AgenticMessage]
	err           error
}

func (it *eventIterator) Next() (*schema.AgenticMessage, bool) {
	if it.err != nil {
		return nil, false
	}
	if err := it.ctx.Err(); err != nil {
		it.err = err
		return nil, false
	}

	// 1. Drain current message stream if any
	if it.currentStream != nil {
		chunk, err := it.currentStream.Recv()
		if err == nil {
			return chunk, true
		}
		it.currentStream.Close()
		it.currentStream = nil
		if err != nil && err != io.EOF {
			it.err = err
			return nil, false
		}
	}

	// 2. Fetch the next event from Eino agent
	for {
		event, ok := it.iterator.Next()
		if !ok {
			return nil, false
		}

		if event.Err != nil {
			it.err = event.Err
			return nil, false
		}

		if event.Action != nil && event.Action.Interrupted != nil {
			// Interrupt is a terminal event
			return nil, false
		}

		if event.Output != nil && event.Output.MessageOutput != nil {
			mv := event.Output.MessageOutput
			if mv.IsStreaming && mv.MessageStream != nil {
				it.currentStream = mv.MessageStream
				return it.Next()
			} else if mv.Message != nil {
				return mv.Message, true
			}
		}
	}
}

func (it *eventIterator) Err() error {
	return it.err
}
