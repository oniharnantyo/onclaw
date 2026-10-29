package memory

import (
	"time"
)

// Retention constants (add-agent-memory design D9, SF-AMS survival
// potential): the importance weights, the per-day decay λ, the recall cap Γ,
// the archive floor, the injection threshold, and the creation grace.
const (
	// ImportanceNovelty, ImportanceRelevance and ImportanceRepetition weight
	// the mechanical importance I = 0.35·novelty + 0.45·relevance +
	// 0.20·repetition.
	ImportanceNovelty    = 0.35
	ImportanceRelevance  = 0.45
	ImportanceRepetition = 0.20
	// RetentionDecayPerDay is λ: the survival potential loses this much per
	// day, every day.
	RetentionDecayPerDay = 0.02
	// RecallCapPerSweep caps Γ, the user-origin recalls of one sweep window.
	RecallCapPerSweep = 5
	// RetentionFloor is the archive floor: a note whose Φ falls below it is
	// archived (never deleted).
	RetentionFloor = 0.1
	// InjectionThreshold is the score a note's Φ must meet to enter the
	// memory block's notes section (the block builder reads it).
	InjectionThreshold = 0.3
	// CreationGraceDays protects a note from archiving for this many days
	// after its creation (the note file's modification time).
	CreationGraceDays = 30
)

// Importance computes the mechanical importance I (design D9): a weighted
// blend of the note's novelty (1 − max token-set similarity to the other
// notes), its relevance (the mean user-origin hit score of the sweep window)
// and its repetition (the window's recall share).
func Importance(novelty, relevance, repetition float64) float64 {
	return ImportanceNovelty*novelty +
		ImportanceRelevance*relevance +
		ImportanceRepetition*repetition
}

// InitialRetention sets a first-sight note's survival potential to its
// importance (design D9: initial Φ = I).
func InitialRetention(importance float64) float64 {
	return roundRetention(importance)
}

// UpdateRetention advances one note's survival potential by one sweep
// (design D9): Φ ← max(0, Φ + I·Γ·Ψ − λ·Δt_days). Γ is the window's
// user-origin recall count capped at RecallCapPerSweep, Ψ the redundancy
// dampening (1 − max similarity to a stronger note), Δt the days elapsed
// since the previous update.
func UpdateRetention(prevPhi, importance float64, gamma int, psi, dtDays float64) float64 {
	if gamma > RecallCapPerSweep {
		gamma = RecallCapPerSweep
	}
	if gamma < 0 {
		gamma = 0
	}
	phi := prevPhi + importance*float64(gamma)*psi - RetentionDecayPerDay*dtDays
	if phi < 0 {
		return 0
	}
	return roundRetention(phi)
}

// BelowFloor reports whether a note's survival potential has fallen below
// the archive floor (strictly: the floor itself still survives).
func BelowFloor(phi float64) bool {
	return phi < RetentionFloor
}

// InGrace reports whether a note is inside its 30-day creation grace and so
// exempt from archiving regardless of Φ (design D11). Creation is the note
// file's modification time: writing or editing a note — including moving an
// archived one back — restarts its protection.
func InGrace(now, created time.Time) bool {
	return now.Sub(created) < CreationGraceDays*24*time.Hour
}

// roundRetention quantizes Φ to six decimals before it is stored, so a
// no-op re-run seconds after the previous sweep cannot perturb the stored
// value through sub-microsecond decay (the sweep's idempotency contract).
func roundRetention(phi float64) float64 {
	return float64(int64(phi*1e6+retentionRoundHalf)) / 1e6
}

// retentionRoundHalf is the rounding offset for roundRetention (values are
// non-negative).
const retentionRoundHalf = 0.5
