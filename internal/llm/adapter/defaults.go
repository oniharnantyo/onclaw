package adapter

// DefaultAdapters registers all builtin provider adapters on the given registry.
func DefaultAdapters(r Registry) {
	r.Register("openai", NewOpenAICompatAdapter)
	r.Register("anthropic", NewStubAdapter)
	r.Register("openai-compatible", NewOpenAICompatAdapter)
	r.Register("ollama", NewOpenAICompatAdapter)
}
