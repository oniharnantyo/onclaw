package config

import "github.com/oniharnantyo/onclaw/internal/shellpolicy"

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
				// Loose by default: every command runs except those matching the
				// catastrophic denylist (full-command evaluation). Switch to
				// deny/allowlist/ask via ONCLAW_TOOLS_SHELL_POLICY; override the
				// floor via ONCLAW_TOOLS_SHELL_DENYLIST.
				Policy: "denylist",
				Allowlist: []string{
					"ls", "cat", "git", "go", "make",
					"npm", "node", "python3", "python", "docker",
				},
				Denylist: shellpolicy.FloorPatterns(),
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
