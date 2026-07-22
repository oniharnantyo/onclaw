package adapter

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/cloudwego/eino-ext/components/model/agenticgemini"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"google.golang.org/genai"
)

// geminiPrefixCacher is the subset of *agenticgemini.Model used to create a
// reusable cached-content resource from the stable prefix.
type geminiPrefixCacher interface {
	CreatePrefixCache(ctx context.Context, prefixMsgs []*schema.AgenticMessage, opts ...model.Option) (*genai.CachedContent, error)
}

// geminiPrefixCache wraps an AgenticModel so Gemini prefix caching is actually
// wired at runtime. Gemini requires a named cached-content resource built from
// the stable prefix (system + history, everything before the current turn's user
// message) and referenced on each request. eino's adapter only accepts the
// reference via WithCachedContentName; this wrapper creates the resource on the
// first request of each distinct prefix and reuses it for every subsequent
// Generate/Stream, so the prefix is cached rather than re-billed.
//
// The Eino agent loop runs tool rounds by re-invoking Stream with an appended
// message list, so within a single turn the stable prefix is identical across
// every call — the cached content is therefore reused for all of them. The
// prefix grows once per turn (history deepens), which naturally recreates the
// cache at each turn boundary.
//
// Any failure to create or use the cache degrades gracefully to calling the
// inner model with the full input (re-billing, never incorrect).
type geminiPrefixCache struct {
	inner   model.AgenticModel
	cacher  geminiPrefixCacher
	enabled bool

	mu         sync.Mutex
	name       string
	prefixHash string
}

func newGeminiPrefixCache(inner model.AgenticModel, cacher geminiPrefixCacher, enabled bool) model.AgenticModel {
	return &geminiPrefixCache{inner: inner, cacher: cacher, enabled: enabled}
}

// splitPrefixTail returns the stable prefix (everything before the last user
// message — i.e. system + prior history) and the tail (the current turn's
// messages, starting at the last user message). Returns (nil, input) when no
// cacheable prefix exists (no user message, or nothing before it): the caller
// then sends the full input unchanged.
func splitPrefixTail(input []*schema.AgenticMessage) (prefix, tail []*schema.AgenticMessage) {
	cut := -1
	for i := len(input) - 1; i >= 0; i-- {
		if input[i].Role == schema.AgenticRoleTypeUser {
			cut = i
			break
		}
	}
	if cut < 1 {
		return nil, input
	}
	return input[:cut], input[cut:]
}

func hashMessages(msgs []*schema.AgenticMessage) string {
	b, _ := json.Marshal(msgs)
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%x", sum)
}

// ensureCache returns the cached-content name for the given prefix, creating it
// once and reusing it while the prefix is unchanged.
func (g *geminiPrefixCache) ensureCache(ctx context.Context, prefix []*schema.AgenticMessage, opts []model.Option) (string, error) {
	h := hashMessages(prefix)
	g.mu.Lock()
	if g.name != "" && g.prefixHash == h {
		name := g.name
		g.mu.Unlock()
		return name, nil
	}
	g.mu.Unlock()

	cc, err := g.cacher.CreatePrefixCache(ctx, prefix, opts...)
	if err != nil {
		return "", err
	}
	g.mu.Lock()
	g.name = cc.Name
	g.prefixHash = h
	g.mu.Unlock()
	return cc.Name, nil
}

func (g *geminiPrefixCache) invalidate() {
	g.mu.Lock()
	g.name = ""
	g.prefixHash = ""
	g.mu.Unlock()
}

func (g *geminiPrefixCache) Generate(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	if !g.enabled {
		return g.inner.Generate(ctx, input, opts...)
	}
	prefix, tail := splitPrefixTail(input)
	if prefix == nil {
		return g.inner.Generate(ctx, input, opts...)
	}
	name, err := g.ensureCache(ctx, prefix, opts)
	if err != nil {
		return g.inner.Generate(ctx, input, opts...)
	}
	out, err := g.inner.Generate(ctx, tail, append(opts, agenticgemini.WithCachedContentName(name))...)
	if err != nil {
		if isGeminiCacheError(err) {
			g.invalidate()
			return g.inner.Generate(ctx, input, opts...)
		}
		return out, err
	}
	return out, nil
}

func (g *geminiPrefixCache) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	if !g.enabled {
		return g.inner.Stream(ctx, input, opts...)
	}
	prefix, tail := splitPrefixTail(input)
	if prefix == nil {
		return g.inner.Stream(ctx, input, opts...)
	}
	name, err := g.ensureCache(ctx, prefix, opts)
	if err != nil {
		return g.inner.Stream(ctx, input, opts...)
	}
	return g.inner.Stream(ctx, tail, append(opts, agenticgemini.WithCachedContentName(name))...)
}

// isGeminiCacheError reports whether an error indicates the referenced cached
// content was not found or expired, so the caller can recreate and retry.
func isGeminiCacheError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "cachedcontent") ||
		(strings.Contains(msg, "cache") && (strings.Contains(msg, "not found") || strings.Contains(msg, "expired") || strings.Contains(msg, "deleted")))
}
