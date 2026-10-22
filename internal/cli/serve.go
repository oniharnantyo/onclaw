//go:build !server

package cli

import (
	"context"
	"fmt"
)

// ErrServeNotImplemented reports that server mode is not part of this
// build. The desktop build serves only the GUI.
var ErrServeNotImplemented = fmt.Errorf("serve: server mode is not implemented in this build")

// serveAction is the desktop build's serve seam: server mode lives in the
// -tags server binary (design D4).
func serveAction(_ context.Context, _ *App) error {
	return ErrServeNotImplemented
}
