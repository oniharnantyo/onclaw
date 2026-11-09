// Command driverstub is the test stand-in for the vendored cua-driver binary
// (mirrors internal/agents/mcp/testdata/mockmcpserver): it accepts the real
// daemon's `serve --embedded --socket <path>` argv, creates the socket file
// (a real unix socket when the path allows, else a plain file), records the
// exact environment it was spawned with beside the socket (<socket>.env), and
// exits when its stdin hits EOF — the CUA_DRIVER_PARENT_LIVENESS_STDIN=1
// contract the parent relies on. It is compiled by driver_test.go at test
// time and never built by the go tool (testdata).
package main

import (
	"net"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	// argv: <bin> serve --embedded --socket <path>
	var socketPath string
	for i, arg := range os.Args {
		if arg == "--socket" && i+1 < len(os.Args) {
			socketPath = os.Args[i+1]
		}
	}
	if socketPath == "" {
		os.Exit(2)
	}

	// A real unix socket when the path is short enough (the OS limit), a
	// plain file otherwise — both satisfy the parent's existence wait.
	if ln, err := net.Listen("unix", socketPath); err == nil {
		go func() {
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				_ = conn.Close()
			}
		}()
	} else {
		f, err := os.OpenFile(socketPath, os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			os.Exit(3)
		}
		_ = f.Close()
	}

	// Record the exact spawn environment: the test asserts the env contract
	// byte for byte from this file.
	envPath := socketPath + ".env"
	envData := ""
	for _, row := range os.Environ() {
		envData += row + "\n"
	}
	if err := os.WriteFile(envPath, []byte(envData), 0o600); err != nil {
		os.Exit(4)
	}

	// Exit on SIGTERM/SIGINT (the graceful teardown path) and on stdin EOF
	// (the parent liveness contract). On exit the env record is removed so
	// the test can await the exit by watching the file disappear.
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 512)
		for {
			_, err := os.Stdin.Read(buf)
			if err != nil {
				_ = os.Remove(envPath)
				os.Exit(0)
			}
		}
	}()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case <-sig:
		_ = os.Remove(envPath)
		os.Exit(0)
	case <-done:
	}
}
