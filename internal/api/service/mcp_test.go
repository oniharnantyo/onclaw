package service_test

import (
	"context"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/api/service"
	"github.com/oniharnantyo/onclaw/internal/store"
)

func TestService_AddMCP_Upsert(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// 1. Create a server
	_, err := f.svc.AddMCP(ctx, &service.MCPServerInput{
		Name:      "test-mcp",
		Transport: "stdio",
		Command:   "node",
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("AddMCP: %v", err)
	}

	// 2. Add with same name (updates/upserts it)
	_, err = f.svc.AddMCP(ctx, &service.MCPServerInput{
		Name:      "test-mcp",
		Transport: "stdio",
		Command:   "bun",
		Enabled:   false,
	})
	if err != nil {
		t.Fatalf("AddMCP (upsert): %v", err)
	}

	v, _ := f.svc.GetMCP(ctx, "test-mcp")
	if v.Command != "bun" {
		t.Errorf("expected command updated to 'bun', got %q", v.Command)
	}
	if v.Enabled {
		t.Error("expected Enabled updated to false")
	}
}

func TestValidateMCPServerInput_Valid_Stdio(t *testing.T) {
	input := &service.MCPServerInput{
		Name:      "test-mcp",
		Transport: "stdio",
		Command:   "node",
	}
	err := service.ValidateMCPServerInput(input)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateMCPServerInput_Valid_HTTP(t *testing.T) {
	input := &service.MCPServerInput{
		Name:      "test-mcp",
		Transport: "http",
		URL:       "http://localhost:8080/mcp",
	}
	err := service.ValidateMCPServerInput(input)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateMCPServerInput_Valid_SSE(t *testing.T) {
	input := &service.MCPServerInput{
		Name:      "test-mcp",
		Transport: "sse",
		URL:       "http://localhost:8080/sse",
	}
	err := service.ValidateMCPServerInput(input)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateMCPServerInput_MissingName(t *testing.T) {
	input := &service.MCPServerInput{
		Transport: "stdio",
		Command:   "node",
	}
	err := service.ValidateMCPServerInput(input)
	if err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Errorf("expected missing name error, got %v", err)
	}
}

func TestValidateMCPServerInput_InvalidTransport(t *testing.T) {
	input := &service.MCPServerInput{
		Name:      "test",
		Transport: "ftp",
	}
	err := service.ValidateMCPServerInput(input)
	if err == nil || !strings.Contains(err.Error(), "transport must be stdio, http, or sse") {
		t.Errorf("expected invalid transport error, got %v", err)
	}
}

func TestValidateMCPServerInput_StdioMissingCommand(t *testing.T) {
	input := &service.MCPServerInput{
		Name:      "test",
		Transport: "stdio",
	}
	err := service.ValidateMCPServerInput(input)
	if err == nil || !strings.Contains(err.Error(), "stdio transport requires a command") {
		t.Errorf("expected command is required error, got %v", err)
	}
}

func TestValidateMCPServerInput_HTTPMissingURL(t *testing.T) {
	input := &service.MCPServerInput{
		Name:      "test",
		Transport: "http",
	}
	err := service.ValidateMCPServerInput(input)
	if err == nil || !strings.Contains(err.Error(), "URL is required") {
		t.Errorf("expected url is required error, got %v", err)
	}
}

func TestValidateMCPServerInput_SSEMissingURL(t *testing.T) {
	input := &service.MCPServerInput{
		Name:      "test",
		Transport: "sse",
	}
	err := service.ValidateMCPServerInput(input)
	if err == nil || !strings.Contains(err.Error(), "URL is required") {
		t.Errorf("expected url is required error, got %v", err)
	}
}

func TestValidateMCPServerInput_InvalidArgsJSON(t *testing.T) {
	input := &service.MCPServerInput{
		Name:      "test",
		Transport: "stdio",
		Command:   "node",
		Args:      "invalid-json{",
	}
	err := service.ValidateMCPServerInput(input)
	if err == nil || !strings.Contains(err.Error(), "args must be a valid JSON array") {
		t.Errorf("expected invalid JSON array error, got %v", err)
	}
}

func TestValidateMCPServerInput_ArgsNotArray(t *testing.T) {
	input := &service.MCPServerInput{
		Name:      "test",
		Transport: "stdio",
		Command:   "node",
		Args:      `{"key": "value"}`,
	}
	err := service.ValidateMCPServerInput(input)
	if err == nil || !strings.Contains(err.Error(), "args must be a valid JSON array") {
		t.Errorf("expected args not array error, got %v", err)
	}
}

func TestValidateMCPServerInput_ValidArgsArray(t *testing.T) {
	input := &service.MCPServerInput{
		Name:      "test",
		Transport: "stdio",
		Command:   "node",
		Args:      `["arg1", "arg2"]`,
	}
	err := service.ValidateMCPServerInput(input)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateMCPServerInput_InvalidEnvJSON(t *testing.T) {
	input := &service.MCPServerInput{
		Name:      "test",
		Transport: "stdio",
		Command:   "node",
		Env:       "invalid-json{",
	}
	err := service.ValidateMCPServerInput(input)
	if err == nil || !strings.Contains(err.Error(), "env must be a valid JSON object") {
		t.Errorf("expected invalid JSON object error, got %v", err)
	}
}

func TestValidateMCPServerInput_ValidEnvObject(t *testing.T) {
	input := &service.MCPServerInput{
		Name:      "test",
		Transport: "stdio",
		Command:   "node",
		Env:       `{"KEY": "value"}`,
	}
	err := service.ValidateMCPServerInput(input)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRedactEnv_EmptyString(t *testing.T) {
	res := service.RedactEnv("")
	if res != "{}" {
		t.Errorf("expected '{}', got %q", res)
	}
}

func TestRedactEnv_InvalidJSON(t *testing.T) {
	res := service.RedactEnv("invalid-json{")
	if res != "{}" {
		t.Errorf("expected '{}' on invalid JSON, got %q", res)
	}
}

func TestRedactEnv_SingleKey(t *testing.T) {
	env := `{"API_KEY": "secret_value_123"}`
	res := service.RedactEnv(env)
	if res != `{"API_KEY":"***"}` {
		t.Errorf("redact failed, got %q", res)
	}
}

func TestRedactEnv_MultipleKeys(t *testing.T) {
	env := `{"URL": "http://api.com", "TOKEN": "xyz123"}`
	res := service.RedactEnv(env)
	if !strings.Contains(res, `"URL":"***"`) || !strings.Contains(res, `"TOKEN":"***"`) {
		t.Errorf("expected all keys to be redacted to ***, got %q", res)
	}
}

func TestRedactEnv_EmptyObject(t *testing.T) {
	res := service.RedactEnv("{}")
	if res != "{}" {
		t.Errorf("expected '{}', got %q", res)
	}
}

func TestToMCPServerView_FieldMapping(t *testing.T) {
	m := &store.MCPServer{
		Name:      "test-mcp",
		Transport: "stdio",
		Command:   "node",
		Args:      `["arg1"]`,
		Env:       `{"API_KEY": "val"}`,
		Enabled:   1,
	}
	view := service.ToMCPServerView(m)
	if view.Name != "test-mcp" {
		t.Errorf("expected 'test-mcp', got %q", view.Name)
	}
	if !view.Enabled {
		t.Error("expected Enabled=true")
	}
	if strings.Contains(view.Env, "val") {
		t.Error("expected Env sensitive value to be redacted")
	}
}

func TestToMCPServerView_DisabledServer(t *testing.T) {
	m := &store.MCPServer{
		Name:    "disabled-mcp",
		Enabled: 0,
	}
	view := service.ToMCPServerView(m)
	if view.Enabled {
		t.Error("expected Enabled=false")
	}
}

func TestToMCPServerView_EmptyEnv(t *testing.T) {
	m := &store.MCPServer{
		Name: "empty-env",
		Env:  "",
	}
	view := service.ToMCPServerView(m)
	if view.Env != "{}" {
		t.Errorf("expected '{}', got %q", view.Env)
	}
}

func TestService_ListMCP_Empty(t *testing.T) {
	f := newFixture(t)
	list, err := f.svc.ListMCP(context.Background())
	if err != nil {
		t.Fatalf("ListMCP: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d", len(list))
	}
}

func TestService_AddMCP_And_ListMCP(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	_, err := f.svc.AddMCP(ctx, &service.MCPServerInput{
		Name:      "test-mcp",
		Transport: "stdio",
		Command:   "node",
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("AddMCP: %v", err)
	}

	list, err := f.svc.ListMCP(ctx)
	if err != nil {
		t.Fatalf("ListMCP: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 server, got %d", len(list))
	}
}

func TestService_GetMCP(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.svc.AddMCP(ctx, &service.MCPServerInput{Name: "get-mcp", Transport: "stdio", Command: "node"})

	view, err := f.svc.GetMCP(ctx, "get-mcp")
	if err != nil {
		t.Fatalf("GetMCP: %v", err)
	}
	if view.Name != "get-mcp" {
		t.Errorf("expected 'get-mcp', got %q", view.Name)
	}
}

func TestService_GetMCP_NotFound(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.GetMCP(context.Background(), "ghost")
	if err == nil {
		t.Error("expected error for missing mcp server")
	}
}

func TestService_AddMCP_InvalidInput(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.AddMCP(context.Background(), &service.MCPServerInput{
		Transport: "stdio",
		Command:   "", // missing command
	})
	if err == nil {
		t.Error("expected validation error")
	}
}

func TestService_UpdateMCP(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.svc.AddMCP(ctx, &service.MCPServerInput{Name: "upd-mcp", Transport: "stdio", Command: "node"})

	_, err := f.svc.UpdateMCP(ctx, "upd-mcp", &service.MCPServerInput{
		Transport: "http",
		URL:       "http://localhost/mcp",
	})
	if err != nil {
		t.Fatalf("UpdateMCP: %v", err)
	}

	v, _ := f.svc.GetMCP(ctx, "upd-mcp")
	if v.Transport != "http" {
		t.Errorf("expected HTTP transport, got %q", v.Transport)
	}
}

func TestService_UpdateMCP_NotFound(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.UpdateMCP(context.Background(), "ghost", &service.MCPServerInput{
		Name:      "ghost",
		Transport: "stdio",
		Command:   "node",
	})
	if err == nil {
		t.Error("expected error updating missing mcp")
	}
}

func TestService_RemoveMCP(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.svc.AddMCP(ctx, &service.MCPServerInput{Name: "del-mcp", Transport: "stdio", Command: "node"})
	if err := f.svc.RemoveMCP(ctx, "del-mcp"); err != nil {
		t.Fatalf("RemoveMCP: %v", err)
	}

	_, err := f.svc.GetMCP(ctx, "del-mcp")
	if err == nil {
		t.Error("expected error after removal")
	}
}

func TestService_ToggleMCPServer(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.svc.AddMCP(ctx, &service.MCPServerInput{Name: "toggle-mcp", Transport: "stdio", Command: "node", Enabled: false})

	err := f.svc.ToggleMCPServer(ctx, "toggle-mcp", true)
	if err != nil {
		t.Fatalf("ToggleMCPServer: %v", err)
	}

	v, _ := f.svc.GetMCP(ctx, "toggle-mcp")
	if !v.Enabled {
		t.Error("expected server to be enabled")
	}
}

func TestService_TestMCP_ValidInput(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	res, err := f.svc.TestMCP(ctx, &service.MCPServerInput{
		Name:      "test-connection",
		Transport: "stdio",
		Command:   "node",
	})
	if err != nil {
		t.Fatalf("TestMCP: %v", err)
	}
	if len(res) != 2 || res[0] != "tool_a" {
		t.Errorf("unexpected test result: %v", res)
	}
}

func TestService_TestMCP_InvalidInput(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.TestMCP(context.Background(), &service.MCPServerInput{
		Transport: "stdio",
		Command:   "", // missing command
	})
	if err == nil {
		t.Error("expected validation error")
	}
}
