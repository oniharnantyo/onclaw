package services

import (
	"context"
	"errors"
)

// ClipboardService is the bound service over the OS clipboard (design D12):
// the chat view's copy action falls back to it when the webview refuses
// navigator.clipboard.writeText. The desktop build delegates to the Wails
// clipboard manager, which carries the platform implementation. The server
// build has no GUI run loop for that manager to dispatch to — a call would
// block instead of failing — so it refuses immediately and the frontend keeps
// its refusal toast (clipboard_server.go).
type ClipboardService struct{}

// NewClipboardService returns the clipboard service. It is stateless — the
// backend resolves the running application per call.
func NewClipboardService() *ClipboardService {
	return &ClipboardService{}
}

// SetText writes text to the OS clipboard; an error when the platform write
// fails or the build has no clipboard.
func (s *ClipboardService) SetText(_ context.Context, text string) error {
	cb, err := clipboard()
	if err != nil {
		return err
	}
	if !cb.SetText(text) {
		return errors.New("clipboard: write refused")
	}
	return nil
}

// GetText reads the OS clipboard's text content; an error when the platform
// read fails or the build has no clipboard.
func (s *ClipboardService) GetText(_ context.Context) (string, error) {
	cb, err := clipboard()
	if err != nil {
		return "", err
	}
	text, ok := cb.Text()
	if !ok {
		return "", errors.New("clipboard: read failed")
	}
	return text, nil
}
