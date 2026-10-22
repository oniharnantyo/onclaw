// Package services holds thin Wails-bound service wrappers over the core
// packages. Services carry no business logic and the core must not import
// Wails (design D1's insurance clause): event forwarding goes through the
// Emitter interface, implemented by a thin Wails adapter in the entrypoints,
// so services stay unit-testable and a future host can reuse them verbatim.
package services
