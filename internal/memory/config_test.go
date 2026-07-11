package memory_test

import (
	"testing"

	"github.com/oniharnantyo/onclaw/internal/memory"
)

func TestAgentMemoryConfig_Resolve(t *testing.T) {
	// 1. With all overrides nil, should fall back to global settings
	emptyCfg := &memory.AgentMemoryConfig{}
	resolved := emptyCfg.Resolve(
		true,  // globalEnabled
		true,  // globalCurated
		true,  // globalEpisodic
		false, // globalKG
		"openai",
		"text-embedding-3-small",
		true,  // globalSecurityScan
		true,  // globalExtraction
		true,  // globalRetrieval
		true,  // globalDreaming
		false, // globalStagedWriteApproval
	)

	if !resolved.Enabled || !resolved.CuratedEnabled || !resolved.EpisodicEnabled || resolved.KGEnabled ||
		resolved.EmbeddingProvider != "openai" || resolved.EmbeddingModel != "text-embedding-3-small" ||
		!resolved.SecurityScanEnabled || !resolved.ExtractionEnabled || !resolved.RetrievalEnabled ||
		!resolved.DreamingEnabled || resolved.StagedWriteApproval {
		t.Errorf("default resolve fallback mismatch: %+v", resolved)
	}

	// 2. Explicit overrides should take precedence
	tVal := true
	fVal := false
	overrideCfg := &memory.AgentMemoryConfig{
		CuratedEnabled:      &fVal,
		EpisodicEnabled:     &fVal,
		KGEnabled:           &tVal,
		EmbeddingProvider:   "ollama",
		EmbeddingModel:      "nomic-embed-text",
		SecurityScanEnabled: &fVal,
		ExtractionEnabled:   &fVal,
		RetrievalEnabled:    &fVal,
		DreamingEnabled:     &fVal,
		StagedWriteApproval: &tVal,
	}

	resolved2 := overrideCfg.Resolve(
		true,
		true,
		true,
		false,
		"openai",
		"text-embedding-3-small",
		true,
		true,
		true,
		true,
		false,
	)

	if resolved2.CuratedEnabled != false ||
		resolved2.EpisodicEnabled != false ||
		resolved2.KGEnabled != true ||
		resolved2.EmbeddingProvider != "ollama" ||
		resolved2.EmbeddingModel != "nomic-embed-text" ||
		resolved2.SecurityScanEnabled != false ||
		resolved2.ExtractionEnabled != false ||
		resolved2.RetrievalEnabled != false ||
		resolved2.DreamingEnabled != false ||
		resolved2.StagedWriteApproval != true {
		t.Errorf("explicit resolve overrides mismatch: %+v", resolved2)
	}
}
