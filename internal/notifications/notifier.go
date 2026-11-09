package notifications

// NativeNotifier is the host seam the engine delivers built payloads through
// (design D1): the desktop host wraps Wails' notification service, the
// server host injects a no-op. Implementations must return promptly — slow
// native work such as design D6's lazy authorization belongs on the host's
// own goroutine — and report delivery failures as errors: the engine logs
// and drops them, never propagating into turns, chat, or schedules (design
// D6).
type NativeNotifier interface {
	// Send delivers one built notification. An error means the notification
	// did not surface; it is never the caller's signal to retry.
	Send(p Payload) error
}
