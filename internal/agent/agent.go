package agent

import (
	"context"

	"github.com/cloudwego/eino/adk"
)

func New() {
	_, _ = adk.NewChatModelAgent(context.Background(), nil)
}
