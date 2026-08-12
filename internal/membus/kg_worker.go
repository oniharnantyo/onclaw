package membus

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/cloudwego/eino/components/model"
	"github.com/oniharnantyo/onclaw/internal/memory"
)

// KGExtractionWorker subscribes to episode_created events and performs
// knowledge graph entity extraction + ingestion asynchronously.
type KGExtractionWorker struct {
	KGStore          memory.KGStore
	ChatModel        model.AgenticModel
	ReviewModel      model.AgenticModel
	AgentName        string
	SkipSecurityScan bool
}

func (w *KGExtractionWorker) Subscribes() []string {
	return []string{"episode_created"}
}

func (w *KGExtractionWorker) Handle(ctx context.Context, event Event) error {
	ep, ok := event.(EpisodeCreated)
	if !ok {
		return fmt.Errorf("kg_worker: unexpected event type %T", event)
	}

	if w.KGStore == nil {
		return nil
	}
	extractionModel := w.ChatModel
	if w.ReviewModel != nil {
		extractionModel = w.ReviewModel
	}
	if extractionModel == nil {
		return nil
	}

	sourceID := fmt.Sprintf("episodic_%d", ep.EpisodeID)

	ext, err := memory.ExtractEntitiesWithSecurity(ctx, extractionModel, ep.Summary, w.AgentName, sourceID, w.SkipSecurityScan)
	if err != nil {
		slog.Warn("kg_worker: extraction failed",
			"episode_id", ep.EpisodeID,
			"error", err,
		)
		return fmt.Errorf("extract entities: %w", err)
	}

	if len(ext.Entities) == 0 && len(ext.Relations) == 0 {
		return nil
	}

	if err := w.KGStore.IngestExtraction(ctx, ext); err != nil {
		return fmt.Errorf("ingest extraction: %w", err)
	}

	if err := w.KGStore.DedupAfterExtraction(ctx, w.AgentName); err != nil {
		return fmt.Errorf("dedup after extraction: %w", err)
	}

	return nil
}
