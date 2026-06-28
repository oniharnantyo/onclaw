package tools

import (
	"context"
	"regexp"

	"github.com/cloudwego/eino/components/tool"
)

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)sk-[a-zA-Z0-9_-]{20,}`),
	regexp.MustCompile(`(?i)nvapi-[a-zA-Z0-9_-]{20,}`),
}

// Redact replaces any secret patterns in the input string with [REDACTED].
func Redact(s string) string {
	for _, re := range secretPatterns {
		s = re.ReplaceAllString(s, "[REDACTED]")
	}
	return s
}

// RedactedTool wraps an eino InvokableTool and redacts input arguments and output results.
type RedactedTool struct {
	tool.InvokableTool
}

// InvokableRun executes the underlying tool with redacted inputs and outputs.
func (r *RedactedTool) InvokableRun(ctx context.Context, input string, opts ...tool.Option) (string, error) {
	maskedInput := Redact(input)
	output, err := r.InvokableTool.InvokableRun(ctx, maskedInput, opts...)
	if err != nil {
		return "", err
	}
	return Redact(output), nil
}

// WrapRedacted wraps a tool.InvokableTool into a RedactedTool.
func WrapRedacted(t tool.InvokableTool) tool.InvokableTool {
	return &RedactedTool{t}
}
