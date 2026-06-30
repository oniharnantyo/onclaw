package render

import (
	"github.com/cloudwego/eino/schema"
)

// Renderer defines the interface for formatting and outputting AgenticMessages.
type Renderer interface {
	Render(msg *schema.AgenticMessage) error
	Flush() error
}
