package config

// defaults returns the conservative, low-resource baseline for a 2 GB RAM /
// 8 GB storage device. Every layer above (config file, ONCLAW_* env, CLI flags)
// can override these.
func defaults() Config {
	return Config{
		LogLevel:         "info",
		LogFormat:        "text",
		Concurrency:      1,
		MaxContextTokens: 64000,
		Model:            "", // left blank intentionally; the agent picks a default later
		DbPath:           "",
		Workspace:        "", // empty string = use current working directory
		Tools: ToolsConfig{
			Shell: ShellConfig{
				Policy:    "deny", // conservative default
				Allowlist: []string{},
			},
		},
		Agent: AgentConfig{
			MaxIterations: 20,
		},
		Langfuse: LangfuseConfig{
			Host:      "",
			PublicKey: "",
			SecretKey: "",
			SessionID: "",
			Release:   "",
			Mask:      true,
		},
		Web: WebConfig{
			Bind: "0.0.0.0",
			Port: 8484,
		},
		Memory: MemoryConfig{
			Enabled:           true,
			EmbeddingProvider: "",
			EmbeddingModel:    "",
			CharLimit:         3200,
			WriteApproval:     false,
			FtsWeight:         0.3,
			VectorWeight:      0.7,
			ReviewModel:       "",
			DreamThreshold:    5,
			EpisodicTTLDays:   90,
			KGEnabled:         true,
			KGTraversalDepth:  3,
		},
	}
}
