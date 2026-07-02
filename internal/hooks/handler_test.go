package hooks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCommandHandler(t *testing.T) {
	// Command config testing exit codes
	// Exit 0 -> Allow
	h0, err := New("command", []byte(`{"command":"exit 0"}`))
	if err != nil {
		t.Fatalf("failed to create exit 0 handler: %v", err)
	}
	dec, err := h0.Run(context.Background(), Payload{})
	if err != nil || dec != DecisionAllow {
		t.Errorf("expected exit 0 to allow, got dec=%s, err=%v", dec, err)
	}

	// Exit 2 -> Block (with stderr)
	h2, err := New("command", []byte(`{"command":"echo 'blocked reason' >&2; exit 2"}`))
	if err != nil {
		t.Fatalf("failed to create exit 2 handler: %v", err)
	}
	dec, err = h2.Run(context.Background(), Payload{})
	if err == nil || !strings.Contains(err.Error(), "blocked reason") || dec != DecisionBlock {
		t.Errorf("expected exit 2 to block with reason, got dec=%s, err=%v", dec, err)
	}

	// Exit 1 -> Error
	h1, err := New("command", []byte(`{"command":"exit 1"}`))
	if err != nil {
		t.Fatalf("failed to create exit 1 handler: %v", err)
	}
	dec, err = h1.Run(context.Background(), Payload{})
	if err == nil || dec != DecisionBlock {
		t.Errorf("expected exit 1 to error and block, got dec=%s, err=%v", dec, err)
	}

	// Test environment filtering
	os.Setenv("TEST_ALLOWED_VAR", "secret_allow")
	os.Setenv("TEST_BLOCKED_VAR", "secret_block")
	defer os.Unsetenv("TEST_ALLOWED_VAR")
	defer os.Unsetenv("TEST_BLOCKED_VAR")

	// Since stdout of command handler is captured, let's write env to a temporary file
	tempDir, err := os.MkdirTemp("", "hook-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	outFile := filepath.Join(tempDir, "env.out")
	hEnvOut, err := New("command", []byte(`{
		"command": "env > `+outFile+`",
		"allowed_env_vars": ["TEST_ALLOWED_VAR"]
	}`))
	if err != nil {
		t.Fatalf("failed to create env out handler: %v", err)
	}

	_, err = hEnvOut.Run(context.Background(), Payload{})
	if err != nil {
		t.Fatalf("env command failed: %v", err)
	}

	contentBytes, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("failed to read env output: %v", err)
	}
	content := string(contentBytes)

	if !strings.Contains(content, "TEST_ALLOWED_VAR=secret_allow") {
		t.Error("expected TEST_ALLOWED_VAR in environment")
	}
	if strings.Contains(content, "TEST_BLOCKED_VAR") {
		t.Error("expected TEST_BLOCKED_VAR to be filtered out of environment")
	}

	// Test stdin payload
	stdinOut := filepath.Join(tempDir, "stdin.out")
	hStdin, err := New("command", []byte(`{
		"command": "cat > `+stdinOut+`"
	}`))
	if err != nil {
		t.Fatalf("failed to create stdin handler: %v", err)
	}

	_, err = hStdin.Run(context.Background(), Payload{Agent: "my-test-agent", ToolName: "my-tool"})
	if err != nil {
		t.Fatalf("stdin command failed: %v", err)
	}

	stdinBytes, err := os.ReadFile(stdinOut)
	if err != nil {
		t.Fatalf("failed to read stdin output: %v", err)
	}

	var p Payload
	if err := json.Unmarshal(stdinBytes, &p); err != nil {
		t.Fatalf("failed to parse payload from stdin: %v", err)
	}
	if p.Agent != "my-test-agent" || p.ToolName != "my-tool" {
		t.Errorf("payload mismatch in stdin, got %+v", p)
	}
}

func TestScriptHandler(t *testing.T) {
	// Script returns allow
	sAllow := `
	function handle(ctx) {
		return { decision: "allow" };
	}
	`
	hAllow, err := New("script", []byte(`{"script":`+stringJSON(sAllow)+`}`))
	if err != nil {
		t.Fatalf("failed to create script: %v", err)
	}
	dec, err := hAllow.Run(context.Background(), Payload{})
	if err != nil || dec != DecisionAllow {
		t.Errorf("expected allow, got dec=%s, err=%v", dec, err)
	}

	// Script returns block
	sBlock := `
	function handle(ctx) {
		return { decision: "block", reason: "no access" };
	}
	`
	hBlock, err := New("script", []byte(`{"script":`+stringJSON(sBlock)+`}`))
	if err != nil {
		t.Fatalf("failed to create script: %v", err)
	}
	dec, err = hBlock.Run(context.Background(), Payload{})
	if err == nil || !strings.Contains(err.Error(), "no access") || dec != DecisionBlock {
		t.Errorf("expected block with reason, got dec=%s, err=%v", dec, err)
	}

	// Script throws exception
	sThrow := `
	function handle(ctx) {
		throw new Error("oops");
	}
	`
	hThrow, err := New("script", []byte(`{"script":`+stringJSON(sThrow)+`}`))
	if err != nil {
		t.Fatalf("failed to create script: %v", err)
	}
	dec, err = hThrow.Run(context.Background(), Payload{})
	if err == nil || dec != DecisionBlock {
		t.Errorf("expected exception to error and block, got dec=%s, err=%v", dec, err)
	}

	// Sandbox verification: blocks require and fs (should throw error or evaluate to undefined)
	sSandbox := `
	function handle(ctx) {
		if (typeof require !== 'undefined') {
			return { decision: "block", reason: "require exposed" };
		}
		return { decision: "allow" };
	}
	`
	hSandbox, err := New("script", []byte(`{"script":`+stringJSON(sSandbox)+`}`))
	if err != nil {
		t.Fatalf("failed to create script: %v", err)
	}
	dec, err = hSandbox.Run(context.Background(), Payload{})
	if err != nil || dec != DecisionAllow {
		t.Errorf("sandbox requirement failed: dec=%s, err=%v", dec, err)
	}

	// Console capture
	sConsole := `
	function handle(ctx) {
		console.log("hello", "from", "js");
		return { decision: "allow" };
	}
	`
	hConsole, err := New("script", []byte(`{"script":`+stringJSON(sConsole)+`}`))
	if err != nil {
		t.Fatalf("failed to create script: %v", err)
	}

	var captured []string
	testConsoleMu.Lock()
	testConsoleCaptured = func(msg string) {
		captured = append(captured, msg)
	}
	testConsoleMu.Unlock()

	_, _ = hConsole.Run(context.Background(), Payload{})

	testConsoleMu.Lock()
	testConsoleCaptured = nil
	testConsoleMu.Unlock()

	if len(captured) != 1 || captured[0] != "hello from js" {
		t.Errorf("expected captured log 'hello from js', got %v", captured)
	}

	// vm.Interrupt timeout fires
	sHang := `
	function handle(ctx) {
		while (true) {}
	}
	`
	hHang, err := New("script", []byte(`{"script":`+stringJSON(sHang)+`}`))
	if err != nil {
		t.Fatalf("failed to create script: %v", err)
	}

	ctxTimeout, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err = hHang.Run(ctxTimeout, Payload{})
	if err == nil {
		t.Error("expected timeout error for infinite loop")
	}
}

func stringJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
