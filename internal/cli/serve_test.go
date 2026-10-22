//go:build !server

package cli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/services"
)

// TestServeAction_NotImplemented pins the desktop build's serve seam.
func TestServeAction_NotImplemented(t *testing.T) {
	err := serveAction(context.Background(), nil)
	require.ErrorIs(t, err, ErrServeNotImplemented)
}

func TestRootCommand_Serve_NotImplementedInDesktopBuild(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cmd := NewRootCommand(&recordingEmitter{}, services.NoBrowser{}, guiShouldNotRun)

	err := cmd.Run(context.Background(), []string{services.AppName, "serve"})
	require.ErrorIs(t, err, ErrServeNotImplemented)
}
