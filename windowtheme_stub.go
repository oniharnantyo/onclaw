//go:build !darwin || ios || server

package main

import "unsafe"

func setWindowAppearance(handle unsafe.Pointer, light bool) error {
	return nil
}
