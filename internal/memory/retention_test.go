package memory

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestImportance covers the mechanical importance blend (design D9:
// 0.35·novelty + 0.45·relevance + 0.20·repetition).
func TestImportance(t *testing.T) {
	require.Equal(t, 0.35, Importance(1, 0, 0))
	require.Equal(t, 0.45, Importance(0, 1, 0))
	require.Equal(t, 0.20, Importance(0, 0, 1))
	require.Equal(t, 1.0, Importance(1, 1, 1))
	require.InDelta(t, 0.35*0.8+0.45*0.6+0.20*0.4, Importance(0.8, 0.6, 0.4), 1e-12)
}

// TestInitialRetention covers the first-sight rule: Φ starts at I.
func TestInitialRetention(t *testing.T) {
	require.Equal(t, 0.35, InitialRetention(Importance(1, 0, 0)))
	require.Equal(t, 0.0, InitialRetention(0))
}

// TestUpdateRetention_DecayToZero covers the decay arithmetic: an unrecalled
// note (Γ = 0) loses λ per day, floored at zero, starting from its initial
// importance.
func TestUpdateRetention_DecayToZero(t *testing.T) {
	phi := InitialRetention(Importance(1, 0, 0)) // 0.35
	require.Equal(t, 0.35, phi)

	// Thirteen unrecalled days take 0.35 below the 0.1 floor.
	for day := 1; day <= 13; day++ {
		phi = UpdateRetention(phi, 0, 0, 1, 1)
	}
	require.InDelta(t, math.Max(0, 0.35-13*RetentionDecayPerDay), phi, 1e-9)
	require.True(t, BelowFloor(phi))

	// The floor of the formula is zero, not negative.
	for day := 14; day <= 40; day++ {
		phi = UpdateRetention(phi, 0, 0, 1, 1)
	}
	require.Equal(t, 0.0, phi)
}

// TestUpdateRetention_ZeroDeltaIsNoop covers the sweep's idempotency
// contract at the arithmetic level: re-running with no elapsed time and no
// recalls leaves Φ exactly unchanged.
func TestUpdateRetention_ZeroDeltaIsNoop(t *testing.T) {
	phi := InitialRetention(Importance(1, 0.5, 0.5))
	require.Equal(t, phi, UpdateRetention(phi, Importance(1, 0.5, 0.5), 0, 1, 0))
}

// TestUpdateRetention_GammaCap covers Γ: window recalls above five do not
// reinforce further.
func TestUpdateRetention_GammaCap(t *testing.T) {
	five := UpdateRetention(0.2, 0.5, 5, 1, 0)
	ten := UpdateRetention(0.2, 0.5, 10, 1, 0)
	require.Equal(t, five, ten)

	gained := UpdateRetention(0.2, 0.5, 5, 1, 0)
	require.Equal(t, 0.2+0.5*RecallCapPerSweep, gained)
}

// TestUpdateRetention_PsiDampening covers the redundancy dampening: a
// near-duplicate of a stronger note (Ψ small) gains far less from the same
// recalls than a unique note (Ψ = 1).
func TestUpdateRetention_PsiDampening(t *testing.T) {
	unique := UpdateRetention(0.2, 0.5, 3, 1.0, 0)
	dampened := UpdateRetention(0.2, 0.5, 3, 0.05, 0)

	require.Equal(t, 0.2+0.5*3, unique)
	require.InDelta(t, 0.2+0.5*3*0.05, dampened, 1e-12)
	require.True(t, dampened < unique, "a near-duplicate must be dampened")
}

// TestRetentionThresholds pins the two published thresholds: the archive
// floor at 0.1 (strictly below archives) and the injection threshold at 0.3.
func TestRetentionThresholds(t *testing.T) {
	require.Equal(t, 0.1, RetentionFloor)
	require.Equal(t, 0.3, InjectionThreshold)
	require.False(t, BelowFloor(0.1), "a note exactly at the floor survives")
	require.True(t, BelowFloor(0.099999))
}

// TestInGrace covers the 30-day creation grace: a fresh note is protected,
// a note older than the grace is not, and the boundary itself is archivable
// — the spec forbids archiving only *earlier than* 30 days after creation.
func TestInGrace(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

	require.True(t, InGrace(now, now.Add(-10*24*time.Hour)), "a ten-day-old note is protected")
	require.True(t, InGrace(now, now), "a just-written note is protected")
	require.False(t, InGrace(now, now.Add(-30*24*time.Hour)), "at exactly 30 days the grace is over")
	require.False(t, InGrace(now, now.Add(-31*24*time.Hour)), "past the grace the note is exposed")
}
