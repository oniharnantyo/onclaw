// Package notifications is the platform-neutral desktop-notification decision
// core (add-desktop-notifications design D1): it gates trigger-source
// candidates through the user's category settings and the foreground-
// suppression rule (window visible AND that exact conversation on screen),
// builds the title/body payloads, and hands them to a host-provided native
// seam for delivery.
//
// Single responsibility: deciding whether a candidate is delivered, and what
// it says. The package imports only the standard library — hosts (desktop
// main.go, server main_server.go) map their real types onto this package's
// at the composition root — and delivery is best-effort: send errors are
// logged at debug and dropped, never propagated into turns, chat, or
// schedules (design D6).
package notifications
