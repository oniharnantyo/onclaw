package computeruse

// The fixed-refusal text vocabulary (add-computer-use design D3/D4, spec
// computer-use "Targeting and staleness": refusal and error texts are fixed
// strings carrying no system state beyond the target's own identity). These
// are the only canned sentences the adapter ever answers with; everything
// else is the driver's own message framed by ActionFailureGuidance.

// BusyRefusal is the single-dispatcher contention answer (design D3): one
// desktop, one actor. The text is load-bearing verbatim — no holder identity,
// mirroring the attention refusal discipline — so callers can match it.
const BusyRefusal = "Desktop is controlled by another agent. Wait for their turn to end, then observe again."

// StaleCaptureRefusal is the coordinate-action answer when the capture the
// coordinates were taken from is no longer the latest observation (spec
// computer-use "Targeting and staleness": a coordinate action against a
// stale capture is refused so the model re-observes).
const StaleCaptureRefusal = "That capture is stale: the screen changed after it was taken. Observe again and use coordinates from the fresh capture."

// MissingCaptureRefusal is the coordinate-action answer when no capture id
// was given at all: coordinates only mean something against an observation
// this adapter has seen.
const MissingCaptureRefusal = "No capture was given for the coordinates. Observe first, then act on the fresh capture's coordinates."

// ActionFailureGuidance is appended to driver-reported action failures —
// stale targets, unknown apps, timeouts, permission gaps — so every failure
// lands as a readable tool error that never fails the turn (spec
// computer-use "Curated tool surface"). The driver's own message rides
// alongside; the guidance sentence itself is fixed.
const ActionFailureGuidance = "Re-observe the screen to refresh what is on it, then retry; if it keeps failing, verify the state you expected before trying a different approach."
