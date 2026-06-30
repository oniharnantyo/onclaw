package service

import (
	"log/slog"

	"github.com/oniharnantyo/onclaw/internal/llm"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// Service encapsulates the business logic of the application.
type Service struct {
	mgr     *llm.Service
	kv      store.KVStore
	conv    store.ConversationStore
	resolve ResolveAndAssembleFunc
	log     *slog.Logger
}

// New returns a new Service instance.
func New(mgr *llm.Service, kv store.KVStore, conv store.ConversationStore, resolve ResolveAndAssembleFunc, log *slog.Logger) *Service {
	return &Service{
		mgr:     mgr,
		kv:      kv,
		conv:    conv,
		resolve: resolve,
		log:     log,
	}
}
