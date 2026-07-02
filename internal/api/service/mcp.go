package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/oniharnantyo/onclaw/internal/store"
)

// ListMCP retrieves all MCP servers and redacts env values.
func (s *Service) ListMCP(ctx context.Context) ([]MCPServerView, error) {
	servers, err := s.mcpStore.ListServers(ctx)
	if err != nil {
		return nil, classify(err)
	}

	resp := make([]MCPServerView, 0, len(servers))
	for _, srv := range servers {
		resp = append(resp, *toMCPServerView(srv))
	}
	return resp, nil
}

// GetMCP retrieves a single MCP server by name, redacting env values.
func (s *Service) GetMCP(ctx context.Context, name string) (*MCPServerView, error) {
	srv, err := s.mcpStore.GetServer(ctx, name)
	if err != nil {
		return nil, classify(err)
	}
	return toMCPServerView(srv), nil
}

// AddMCP creates or updates (upserts) an MCP server.
func (s *Service) AddMCP(ctx context.Context, input *MCPServerInput) (*MCPServerView, error) {
	if err := validateMCPServerInput(input); err != nil {
		return nil, err
	}

	enabledInt := 0
	if input.Enabled {
		enabledInt = 1
	}

	srv := &store.MCPServer{
		Name:      input.Name,
		Transport: input.Transport,
		Command:   input.Command,
		Args:      input.Args,
		Env:       input.Env,
		URL:       input.URL,
		Enabled:   enabledInt,
	}

	existing, err := s.mcpStore.GetServer(ctx, input.Name)
	if err == nil && existing != nil {
		if err := s.mcpStore.UpdateServer(ctx, srv); err != nil {
			return nil, classify(err)
		}
	} else {
		if err := s.mcpStore.AddServer(ctx, srv); err != nil {
			return nil, classify(err)
		}
	}

	s.reloadMCP()
	return toMCPServerView(srv), nil
}

// UpdateMCP updates an existing MCP server.
func (s *Service) UpdateMCP(ctx context.Context, name string, input *MCPServerInput) (*MCPServerView, error) {
	input.Name = name
	if err := validateMCPServerInput(input); err != nil {
		return nil, err
	}

	enabledInt := 0
	if input.Enabled {
		enabledInt = 1
	}

	srv := &store.MCPServer{
		Name:      input.Name,
		Transport: input.Transport,
		Command:   input.Command,
		Args:      input.Args,
		Env:       input.Env,
		URL:       input.URL,
		Enabled:   enabledInt,
	}

	_, err := s.mcpStore.GetServer(ctx, name)
	if err != nil {
		return nil, classify(err)
	}

	if err := s.mcpStore.UpdateServer(ctx, srv); err != nil {
		return nil, classify(err)
	}

	s.reloadMCP()
	return toMCPServerView(srv), nil
}

// RemoveMCP deletes an MCP server by name.
func (s *Service) RemoveMCP(ctx context.Context, name string) error {
	_, err := s.mcpStore.GetServer(ctx, name)
	if err != nil {
		return classify(err)
	}

	if err := s.mcpStore.RemoveServer(ctx, name); err != nil {
		return classify(err)
	}

	s.reloadMCP()
	return nil
}

// ToggleMCPServer enables or disables an MCP server.
func (s *Service) ToggleMCPServer(ctx context.Context, name string, enabled bool) error {
	srv, err := s.mcpStore.GetServer(ctx, name)
	if err != nil {
		return classify(err)
	}

	if enabled {
		srv.Enabled = 1
	} else {
		srv.Enabled = 0
	}

	if err := s.mcpStore.UpdateServer(ctx, srv); err != nil {
		return classify(err)
	}

	s.reloadMCP()
	return nil
}

// TestMCP tests connection to an unsaved MCP server configuration.
func (s *Service) TestMCP(ctx context.Context, input *MCPServerInput) ([]string, error) {
	if err := validateMCPServerInput(input); err != nil {
		return nil, err
	}

	enabledInt := 0
	if input.Enabled {
		enabledInt = 1
	}

	srv := &store.MCPServer{
		Name:      input.Name,
		Transport: input.Transport,
		Command:   input.Command,
		Args:      input.Args,
		Env:       input.Env,
		URL:       input.URL,
		Enabled:   enabledInt,
	}

	return s.testMCP(ctx, srv)
}

func validateMCPServerInput(input *MCPServerInput) error {
	if input.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	if input.Transport != "stdio" && input.Transport != "http" && input.Transport != "sse" {
		return fmt.Errorf("%w: transport must be stdio, http, or sse", ErrInvalidInput)
	}
	if input.Transport == "stdio" && input.Command == "" {
		return fmt.Errorf("%w: stdio transport requires a command", ErrInvalidInput)
	}
	if (input.Transport == "http" || input.Transport == "sse") && input.URL == "" {
		return fmt.Errorf("%w: URL is required for transport %s", ErrInvalidInput, input.Transport)
	}
	if input.Args != "" {
		var args []interface{}
		if err := json.Unmarshal([]byte(input.Args), &args); err != nil {
			return fmt.Errorf("%w: args must be a valid JSON array: %v", ErrInvalidInput, err)
		}
	}
	if input.Env != "" {
		var env map[string]interface{}
		if err := json.Unmarshal([]byte(input.Env), &env); err != nil {
			return fmt.Errorf("%w: env must be a valid JSON object: %v", ErrInvalidInput, err)
		}
	}
	return nil
}

func toMCPServerView(srv *store.MCPServer) *MCPServerView {
	return &MCPServerView{
		Name:      srv.Name,
		Transport: srv.Transport,
		Command:   srv.Command,
		Args:      srv.Args,
		Env:       redactEnv(srv.Env),
		URL:       srv.URL,
		Enabled:   srv.Enabled != 0,
		CreatedAt: srv.CreatedAt,
		UpdatedAt: srv.UpdatedAt,
	}
}

func redactEnv(envJSON string) string {
	if envJSON == "" {
		return "{}"
	}
	var envMap map[string]string
	if err := json.Unmarshal([]byte(envJSON), &envMap); err != nil {
		return "{}"
	}
	redacted := make(map[string]string, len(envMap))
	for k := range envMap {
		redacted[k] = "***"
	}
	b, _ := json.Marshal(redacted)
	return string(b)
}
