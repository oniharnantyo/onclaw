package adapter

import (
	"context"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/cloudwego/eino-ext/components/model/agenticclaude"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// claudePrefixCache wraps an Anthropic model so the cache breakpoint is pinned
// explicitly at the stable prefix boundary instead of only at the final message
// block. eino's agenticclaude adapter applies config.CacheControl at the
// request level, which the SDK serializes as a single cache_control on the last
// block of the last message — a coarse, moving boundary (it shifts as the agent
// appends tool results within a turn). This wrapper instead marks the boundary
// message (the last message before the current user turn) with a content-block
// cache_control, and marks every tool definition, so the system + tools +
// history prefix is explicitly cached and stable across turns.
//
// Caching is additive and disabled via Settings "prompt_caching": false.
type claudePrefixCache struct {
	inner   model.AgenticModel
	enabled bool
	ttl     anthropic.CacheControlEphemeralTTL
}

func newClaudePrefixCache(inner model.AgenticModel, enabled bool) model.AgenticModel {
	return &claudePrefixCache{inner: inner, enabled: enabled, ttl: anthropic.CacheControlEphemeralTTLTTL5m}
}

func (c *claudePrefixCache) cacheControl() *anthropic.CacheControlEphemeralParam {
	cc := anthropic.NewCacheControlEphemeralParam()
	cc.TTL = c.ttl
	return &cc
}

// pin copies the input and opts, applying a content-block cache_control to the
// last block of the boundary message (the message just before the current user
// turn) and a cache_control to every tool definition. Returns the originals
// unchanged when caching is disabled or no stable prefix exists.
func (c *claudePrefixCache) pin(input []*schema.AgenticMessage, opts []model.Option) ([]*schema.AgenticMessage, []model.Option) {
	if !c.enabled {
		return input, opts
	}

	cut := -1
	for i := len(input) - 1; i >= 0; i-- {
		if input[i].Role == schema.AgenticRoleTypeUser {
			cut = i
			break
		}
	}

	out := input
	if cut > 0 {
		boundary := *input[cut-1]
		blocks := make([]*schema.ContentBlock, len(boundary.ContentBlocks))
		copy(blocks, boundary.ContentBlocks)
		if len(blocks) > 0 {
			blocks[len(blocks)-1] = agenticclaude.SetContentBlockCacheControl(blocks[len(blocks)-1], c.cacheControl())
		}
		boundary.ContentBlocks = blocks
		out = make([]*schema.AgenticMessage, len(input))
		copy(out, input)
		out[cut-1] = &boundary
	}

	co := model.GetCommonOptions(&model.Options{}, opts...)
	if len(co.Tools) > 0 {
		pinned := make([]*schema.ToolInfo, 0, len(co.Tools))
		for _, t := range co.Tools {
			pinned = append(pinned, agenticclaude.SetToolInfoCacheControl(t, c.cacheControl()))
		}
		opts = append(opts, model.WithTools(pinned))
	}

	return out, opts
}

func (c *claudePrefixCache) Generate(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	in, o := c.pin(input, opts)
	return c.inner.Generate(ctx, in, o...)
}

func (c *claudePrefixCache) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	in, o := c.pin(input, opts)
	return c.inner.Stream(ctx, in, o...)
}
