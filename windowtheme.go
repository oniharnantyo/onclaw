//go:build !server

// WindowThemeService syncs the native window appearance with the app theme
// toggle. The MacBackdropLiquidGlass window renders its frost from
// NSWindow.appearance — which follows the SYSTEM, not the app — so a light
// app theme on a dark system greys the whole UI out. Forcing aqua/darkAqua
// here makes the native glass track the toggle instead. AppKit calls must
// run on the main thread, hence InvokeSync.
package main

import (
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type WindowThemeService struct {
	handle unsafe.Pointer
}

func NewWindowThemeService() *WindowThemeService {
	return &WindowThemeService{}
}

// setHandle receives the native window pointer after window creation.
// Unexported so the binding generator does not expose it to the frontend.
func (s *WindowThemeService) setHandle(handle unsafe.Pointer) {
	s.handle = handle
}

// SetLight forces the light ("aqua") or dark ("darkAqua") window appearance
// so the native glass and traffic lights match the app theme.
func (s *WindowThemeService) SetLight(light bool) error {
	if s.handle == nil {
		return nil
	}
	var err error
	application.InvokeSync(func() {
		err = setWindowAppearance(s.handle, light)
	})
	return err
}
