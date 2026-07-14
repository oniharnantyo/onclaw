package middlewares

import "errors"

// FloorSafetyFraction is the maximum share of the context window that the fixed
// input floor (system instruction + tool schemas) may consume before the agent
// is considered unsafe to run, because too little room remains for conversation
// history. Half the window is the conservative default: a tool-heavy agent whose
// static schema alone would eat more than this leaves no usable working context.
const FloorSafetyFraction = 0.5

// FloorSafetyLimit returns the token budget that the input floor may not reach
// given a context window. When the floor reaches this limit the agent fails
// fast rather than running with an effectively unusable context.
func FloorSafetyLimit(window int) int {
	return int(float64(window) * FloorSafetyFraction)
}

// ErrInputFloorExceedsSafetyLimit indicates the fixed input floor (system
// instruction plus tool schemas) would consume too much of the context window.
var ErrInputFloorExceedsSafetyLimit = errors.New("input floor exceeds safety limit")
