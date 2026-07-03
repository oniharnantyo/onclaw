package service

import "github.com/oniharnantyo/onclaw/internal/store"

func ValidateMCPServerInput(input *MCPServerInput) error {
	return validateMCPServerInput(input)
}

func RedactEnv(envJSON string) string {
	return redactEnv(envJSON)
}

func ToMCPServerView(srv *store.MCPServer) *MCPServerView {
	return toMCPServerView(srv)
}
