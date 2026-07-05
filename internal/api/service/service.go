package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/oniharnantyo/onclaw/internal/llm"
	"github.com/oniharnantyo/onclaw/internal/memory"
	"github.com/oniharnantyo/onclaw/internal/skill"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// Service encapsulates the business logic of the application.
type Service struct {
	mgr       *llm.Service
	kv        store.KVStore
	conv      store.ConversationStore
	resolve   ResolveAndAssembleFunc
	installer *skill.Installer
	log       *slog.Logger
	hookStore store.HookStore
	execStore store.HookExecutionStore
	mcpStore  store.MCPServerStore
	reloadMCP func()
	testMCP   func(ctx context.Context, srv *store.MCPServer) ([]string, error)

	toolRegistryStore    store.ToolRegistryStore
	toolGroupConfigStore store.ToolGroupConfigStore

	stagedWriteStore memory.StagedWriteStore
	workspacePath    string
}

// SetStagedWriteStore sets the staged write store for memory approval flows.
func (s *Service) SetStagedWriteStore(sts memory.StagedWriteStore) {
	s.stagedWriteStore = sts
}

// SetWorkspacePath sets the base workspace path for reading dream sweep files.
func (s *Service) SetWorkspacePath(wp string) {
	s.workspacePath = wp
}

// New returns a new Service instance.
func New(
	mgr *llm.Service,
	kv store.KVStore,
	conv store.ConversationStore,
	resolve ResolveAndAssembleFunc,
	installer *skill.Installer,
	log *slog.Logger,
	hookStore store.HookStore,
	execStore store.HookExecutionStore,
	mcpStore store.MCPServerStore,
	reloadMCP func(),
	testMCP func(ctx context.Context, srv *store.MCPServer) ([]string, error),
	toolRegistryStore store.ToolRegistryStore,
	toolGroupConfigStore store.ToolGroupConfigStore,
) *Service {
	if reloadMCP == nil {
		reloadMCP = func() {}
	}
	if testMCP == nil {
		testMCP = func(ctx context.Context, srv *store.MCPServer) ([]string, error) {
			return nil, fmt.Errorf("testMCP not implemented")
		}
	}
	return &Service{
		mgr:                  mgr,
		kv:                   kv,
		conv:                 conv,
		resolve:              resolve,
		installer:            installer,
		log:                  log,
		hookStore:            hookStore,
		execStore:            execStore,
		mcpStore:             mcpStore,
		reloadMCP:            reloadMCP,
		testMCP:              testMCP,
		toolRegistryStore:    toolRegistryStore,
		toolGroupConfigStore: toolGroupConfigStore,
	}
}
