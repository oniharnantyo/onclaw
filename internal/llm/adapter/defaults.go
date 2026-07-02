package adapter

// DefaultAdapters registers all builtin provider adapters on the given registry.
func DefaultAdapters(r Registry) {
	r.Register("openai", NewAgenticOpenAIAdapter)
	r.Register("anthropic", NewAgenticClaudeAdapter)
	r.Register("openai-compatible", NewAgenticOpenAIAdapter)
	r.Register("ollama", NewAgenticOpenAIAdapter)
	r.Register("google", NewAgenticGeminiAdapter)
	r.Register("gemini", NewAgenticGeminiAdapter)
	r.Register("deepseek", NewAgenticDeepSeekAdapter)
	r.Register("qwen", NewAgenticQwenAdapter)
	r.Register("ark", NewAgenticArkAdapter)
	r.Register("stub", NewStubAdapter)
}
