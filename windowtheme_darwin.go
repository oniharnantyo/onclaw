//go:build darwin && !ios && !server

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit
#import <AppKit/AppKit.h>

static void buddiesSetWindowAppearance(void *nsWindow, bool light) {
	NSWindow *window = (NSWindow *)nsWindow;
	NSString *name = light ? NSAppearanceNameAqua : NSAppearanceNameDarkAqua;
	[window setAppearance:[NSAppearance appearanceNamed:name]];
}
*/
import "C"

import "unsafe"

func setWindowAppearance(handle unsafe.Pointer, light bool) error {
	C.buddiesSetWindowAppearance(handle, C.bool(light))
	return nil
}
