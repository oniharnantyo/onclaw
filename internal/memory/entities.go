package memory

// The entity-resolution write path (wave3-memory-vectors-and-graph D6/D7):
// entity proposals ride the SAME extraction side-calls that commit notes and
// gists — the gate's op schema and the gister's gist schema each carry an
// optional entities array, so linking costs zero additional model calls.
// Labels are pointers, not content (D7): resolution is idempotent per
// (workspace, normalized label), the label keeps its birth spelling, and all
// access control lives on the edge — which inherits the narrowest tier of
// what it links, i.e. the linked row's own visibility tier. Malformed
// proposals skip individually (the batch rule): one bad entity never poisons
// its op, its gist, or the batch.

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// entityProposal is one extraction-proposed entity mention: the label as the
// material spelled it plus the model's normalized spelling. The normalized
// field is advisory — the pipeline recomputes identity through
// domain.NormalizeEntityLabel, the same function the store verifies parity
// against, so a drifting model normalization can never fork an entity.
type entityProposal struct {
	Label           string `json:"label"`
	NormalizedLabel string `json:"normalized_label"`
}

// normalizeProposedEntity computes the proposal's identity; ok=false marks a
// malformed proposal (empty label, or one that normalizes to nothing) which
// skips alone (D6).
func normalizeProposedEntity(p entityProposal) (label, normalized string, ok bool) {
	label = strings.TrimSpace(p.Label)
	if label == "" {
		return "", "", false
	}
	normalized = domain.NormalizeEntityLabel(label)
	if normalized == "" {
		return "", "", false
	}
	return label, normalized, true
}

// linkEntities resolves each proposed label and links it to the row the
// extraction just committed. Resolution is the store's idempotent
// resolve-or-create (first spelling wins at birth); edges stamp the row's
// own visibility tier — the narrowest-endpoint rule (D7) — plus the dialogue
// origin and the committing event id as provenance. Failures skip
// individually and log: linking is fail-soft and never fails the committed
// row or its batch. Labels never reach the log — entity labels are tenant
// content, the same discipline the chip payload follows.
func linkEntities(ctx context.Context, entities store.MemoryEntityStore, log *slog.Logger, workspaceID, sourceEventID string, proposals []entityProposal, targetType domain.MemoryTargetType, targetID string, visibility domain.MemoryVisibility, now time.Time) {
	if len(proposals) == 0 {
		return
	}

	// One resolution per distinct label: two ops mentioning the same entity
	// resolve once; the store's unique (entity, target) index keeps the
	// edges idempotent regardless.
	seen := make(map[string]struct{}, len(proposals))
	labels := make([]string, 0, len(proposals))
	skipped := 0
	for _, p := range proposals {
		label, normalized, ok := normalizeProposedEntity(p)
		if !ok {
			skipped++
			continue
		}
		if _, dup := seen[normalized]; dup {
			continue
		}
		seen[normalized] = struct{}{}
		labels = append(labels, label)
	}
	if skipped > 0 {
		log.Warn("memory: malformed entity proposals skipped", "workspace_id", workspaceID, "count", skipped)
	}
	if len(labels) == 0 {
		return
	}

	edges := make([]domain.MemoryEntityEdge, 0, len(labels))
	for _, label := range labels {
		entity := &domain.MemoryEntity{
			WorkspaceID:     workspaceID,
			Label:           label,
			NormalizedLabel: domain.NormalizeEntityLabel(label),
			// Pipeline writes are dialogue-provenanced (D5); the committing
			// event is the birth evidence.
			Origin:        domain.MemoryOriginDialogue,
			SourceEventID: sourceEventID,
			LearnedAt:     now,
		}
		if err := entities.ResolveEntity(ctx, entity); err != nil {
			log.Warn("memory: entity resolution skipped", "workspace_id", workspaceID, "error", err)
			continue
		}
		edges = append(edges, domain.MemoryEntityEdge{
			EntityID:      entity.ID,
			TargetType:    targetType,
			TargetID:      targetID,
			Visibility:    visibility,
			Origin:        domain.MemoryOriginDialogue,
			SourceEventID: sourceEventID,
		})
	}
	if len(edges) == 0 {
		return
	}
	if _, err := entities.AddEdges(ctx, workspaceID, edges); err != nil {
		log.Warn("memory: entity edges skipped", "workspace_id", workspaceID, "error", err)
	}
}
