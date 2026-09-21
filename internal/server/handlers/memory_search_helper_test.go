package handlers_test

import (
	"github.com/oniharnantyo/onclaw/internal/memory"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// newTestMemorySearcher builds the fused searcher over st with the
// composition root's production wiring (wave3 task 3.3): the provider-backed
// embedding lane. These test worlds configure no embedding model, so the
// vector channel degrades to lexical-only exactly as production does without
// one (wave3 D4).
func newTestMemorySearcher(st store.Store) *memory.Searcher {
	return memory.NewSearcher(
		st.MemoryNotes(),
		st.MemoryEvents(),
		st.MemoryEmbeddings(),
		st.MemoryEntities(),
		st.SessionEvents(),
		memory.NewProviderEmbedder(st.Providers(), st.ToolSettings(), v1aEncKey, providers.NewRegistry()),
	)
}
