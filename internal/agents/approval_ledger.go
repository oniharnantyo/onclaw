package agents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/oniharnantyo/onclaw/internal/agents/backend"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// approvalDecisionPrefix namespaces approval decisions inside the checkpoint
// KV store.
const approvalDecisionPrefix = "approval/"

// checkpointDecisionLedger adapts the session checkpoint KV store to the
// shell's DecisionLedger. Decisions are consumed on read so a resolved
// approval cannot silently approve a later identical command.
type checkpointDecisionLedger struct {
	checkpoints store.SessionCheckpointStore
}

// Lookup implements backend.DecisionLedger.
func (l *checkpointDecisionLedger) Lookup(ctx context.Context, command string) (bool, bool) {
	key := ApprovalDecisionKey(command)
	data, ok, err := l.checkpoints.Get(ctx, key)
	if err != nil || !ok {
		return false, false
	}
	if err := l.checkpoints.Delete(ctx, key); err != nil {
		// A stale decision errs toward re-interrupting; surface but don't fail.
		return false, false
	}
	return string(data) == "approved", true
}

// recordApprovalDecision durably records the human decision for a command.
func recordApprovalDecision(ctx context.Context, checkpoints store.SessionCheckpointStore, command string, approved bool) error {
	if command == "" {
		// Without the command text (history lost it), the ledger path is
		// unusable; the same-process resume-target path still applies.
		return nil
	}
	value := "denied"
	if approved {
		value = "approved"
	}
	if err := checkpoints.Set(ctx, ApprovalDecisionKey(command), []byte(value)); err != nil {
		return fmt.Errorf("store approval decision: %w", err)
	}
	return nil
}

// ApprovalDecisionKey derives the durable key for a command's decision.
func ApprovalDecisionKey(command string) string {
	sum := sha256.Sum256([]byte(command))
	return approvalDecisionPrefix + hex.EncodeToString(sum[:])
}

var _ backend.DecisionLedger = (*checkpointDecisionLedger)(nil)
