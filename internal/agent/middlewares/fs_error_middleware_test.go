package middlewares_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/onclaw/internal/agent/middlewares"
	"github.com/oniharnantyo/onclaw/internal/agent/tools"
)

// TestFSErrorMiddlewareConvertsExpected verifies an expected filesystem error
// returned by an fs tool becomes a recoverable observation (message, nil).
func TestFSErrorMiddlewareConvertsExpected(t *testing.T) {
	mw := middlewares.NewFSErrorMiddleware()

	passthrough := func(_ context.Context, _ string, _ ...tool.Option) (string, error) {
		return "", tools.ErrFileNotFound
	}
	ep, err := mw.WrapInvokableToolCall(context.Background(), passthrough, &adk.ToolContext{Name: "read_file"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	out, err := ep(context.Background(), "args")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "file not found") {
		t.Errorf("expected observation mentioning file not found, got %q", out)
	}
}

// TestFSErrorMiddlewareLeavesUnknownFatal verifies a genuine (unclassified)
// infrastructure error still propagates as fatal.
func TestFSErrorMiddlewareLeavesUnknownFatal(t *testing.T) {
	mw := middlewares.NewFSErrorMiddleware()
	someErr := errors.New("disk exploded")
	passthrough := func(_ context.Context, _ string, _ ...tool.Option) (string, error) {
		return "", someErr
	}
	ep, err := mw.WrapInvokableToolCall(context.Background(), passthrough, &adk.ToolContext{Name: "read_file"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	out, err := ep(context.Background(), "args")
	if !errors.Is(err, someErr) {
		t.Errorf("expected fatal error to propagate, got %v", err)
	}
	if out != "" {
		t.Errorf("expected empty output on fatal error, got %q", out)
	}
}

// TestFSErrorMiddlewarePropagatesCancellation verifies a context cancellation
// error is never converted into an observation, even for an fs tool.
func TestFSErrorMiddlewarePropagatesCancellation(t *testing.T) {
	mw := middlewares.NewFSErrorMiddleware()
	passthrough := func(_ context.Context, _ string, _ ...tool.Option) (string, error) {
		return "", context.Canceled
	}
	ep, err := mw.WrapInvokableToolCall(context.Background(), passthrough, &adk.ToolContext{Name: "read_file"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = ep(ctx, "args")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled to propagate, got %v", err)
	}
}

// TestFSErrorMiddlewareSkipsNonFSTool verifies the middleware only governs fs
// tools; an error from a non-fs tool is left untouched even if it happens to
// match a sentinel.
func TestFSErrorMiddlewareSkipsNonFSTool(t *testing.T) {
	mw := middlewares.NewFSErrorMiddleware()
	passthrough := func(_ context.Context, _ string, _ ...tool.Option) (string, error) {
		return "", tools.ErrFileNotFound
	}
	ep, err := mw.WrapInvokableToolCall(context.Background(), passthrough, &adk.ToolContext{Name: "memory"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	_, err = ep(context.Background(), "args")
	if err == nil {
		t.Error("expected non-fs tool error to propagate unchanged")
	}
}

// TestFSErrorMiddlewareEnhancedConvertsExpected verifies the enhanced
// (multimodal) path also converts expected errors to an observation part.
func TestFSErrorMiddlewareEnhancedConvertsExpected(t *testing.T) {
	mw := middlewares.NewFSErrorMiddleware()
	passthrough := func(_ context.Context, _ *schema.ToolArgument, _ ...tool.Option) (*schema.ToolResult, error) {
		return nil, tools.ErrPermissionDenied
	}
	ep, err := mw.WrapEnhancedInvokableToolCall(context.Background(), passthrough, &adk.ToolContext{Name: "read_file"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	res, err := ep(context.Background(), &schema.ToolArgument{})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if res == nil || len(res.Parts) == 0 || res.Parts[0].Type != schema.ToolPartTypeText {
		t.Fatalf("expected a text observation part, got %+v", res)
	}
	if !strings.Contains(res.Parts[0].Text, "permission denied") {
		t.Errorf("expected observation mentioning permission denied, got %q", res.Parts[0].Text)
	}
}

// TestFSErrorMiddlewareEnhancedPropagatesCancellation verifies cancellation is
// preserved on the enhanced path too.
func TestFSErrorMiddlewareEnhancedPropagatesCancellation(t *testing.T) {
	mw := middlewares.NewFSErrorMiddleware()
	passthrough := func(_ context.Context, _ *schema.ToolArgument, _ ...tool.Option) (*schema.ToolResult, error) {
		return nil, context.Canceled
	}
	ep, err := mw.WrapEnhancedInvokableToolCall(context.Background(), passthrough, &adk.ToolContext{Name: "read_file"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = ep(ctx, &schema.ToolArgument{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled to propagate, got %v", err)
	}
}
