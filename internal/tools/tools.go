//go:build tools

// Package tools pins dependencies that are not yet imported by application
// code so that `go mod tidy` keeps them recorded in go.mod. Nothing in this
// file is compiled into any binary.
package tools

import (
	_ "github.com/cloudwego/eino"
	_ "github.com/cloudwego/eino-ext/components/model/claude"
	_ "github.com/cloudwego/eino-ext/components/model/gemini"
	_ "github.com/cloudwego/eino-ext/components/model/openai"
	_ "github.com/urfave/cli/v3"
	_ "modernc.org/sqlite"
)
