package config

// defaults returns the conservative, low-resource baseline for a 2 GB RAM /
// 8 GB storage device. Every layer above (config file, ONCLAW_* env, CLI flags)
// can override these.
func defaults() Config {
	return Config{
		LogLevel:         "info",
		LogFormat:        "text",
		Concurrency:      1,
		MaxContextTokens: 8192,
		Model:            "", // left blank intentionally; the agent picks a default later
	}
}
