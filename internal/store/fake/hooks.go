package fake

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// maxHookExecutionDetailChars caps the audit detail at write time, matching
// the postgres adapter (design.md D16).
const maxHookExecutionDetailChars = 256

// hookData is the hook area's state, grouped so the fakeStore struct carries a
// single field for the whole area (snapshot-cloned with everything else).
type hookData struct {
	instanceHooks      map[string]*domain.InstanceHook  // key: ID
	instanceHooksByKey map[string]string                // key: source + ":" + key -> ID
	workspaceHooks     map[string]*domain.WorkspaceHook // key: ID
	agentHooks         map[string]*domain.AgentHook     // key: ID
	hookExecutions     []domain.HookExecution           // append-only; newest inserts at the end
}

func newHookData() *hookData {
	return &hookData{
		instanceHooks:      make(map[string]*domain.InstanceHook),
		instanceHooksByKey: make(map[string]string),
		workspaceHooks:     make(map[string]*domain.WorkspaceHook),
		agentHooks:         make(map[string]*domain.AgentHook),
	}
}

func (d *hookData) clone() *hookData {
	cp := newHookData()
	for id, h := range d.instanceHooks {
		cp.instanceHooks[id] = cloneInstanceHook(h)
	}
	for key, id := range d.instanceHooksByKey {
		cp.instanceHooksByKey[key] = id
	}
	for id, h := range d.workspaceHooks {
		cp.workspaceHooks[id] = cloneWorkspaceHook(h)
	}
	for id, h := range d.agentHooks {
		cp.agentHooks[id] = cloneAgentHook(h)
	}
	cp.hookExecutions = make([]domain.HookExecution, len(d.hookExecutions))
	for i := range d.hookExecutions {
		cp.hookExecutions[i] = *cloneHookExecution(&d.hookExecutions[i])
	}
	return cp
}

// Hooks returns the HookStore sub-port.
func (s *fakeStore) Hooks() store.HookStore {
	return &hookStore{s: s}
}

// normalizeHookBase applies the schema's column defaults (on_failure 'allow',
// status 'ok') so empty enum fields round-trip identically in both adapters.
func normalizeHookBase(base *domain.HookBase) {
	if base.OnFailure == "" {
		base.OnFailure = domain.HookFailureAllow
	}
	if base.Status == "" {
		base.Status = domain.HookStatusOK
	}
}

// truncateHookDetail caps the audit detail at write time (rune-safe).
func truncateHookDetail(detail string) string {
	runes := []rune(detail)
	if len(runes) <= maxHookExecutionDetailChars {
		return detail
	}
	return string(runes[:maxHookExecutionDetailChars])
}

// nextHookPosition returns max(position)+1 across the scoped hooks so Create
// appends to the end of the level's list (D14: API create order).
func nextHookPosition(positions []int) int {
	max := -1
	for _, p := range positions {
		if p > max {
			max = p
		}
	}
	return max + 1
}

func instanceHookPositions(d *hookData) []int {
	out := make([]int, 0, len(d.instanceHooks))
	for _, h := range d.instanceHooks {
		out = append(out, h.Position)
	}
	return out
}

func workspaceHookPositions(d *hookData, workspaceID string) []int {
	out := make([]int, 0)
	for _, h := range d.workspaceHooks {
		if h.WorkspaceID == workspaceID {
			out = append(out, h.Position)
		}
	}
	return out
}

func agentHookPositions(d *hookData, workspaceID, agentID string) []int {
	out := make([]int, 0)
	for _, h := range d.agentHooks {
		if h.WorkspaceID == workspaceID && h.AgentID == agentID {
			out = append(out, h.Position)
		}
	}
	return out
}

// sortByListOrder is the shared D14 list ordering: position, then created_at,
// then id for stability.
func sortByListOrder[T any](items []T, position func(T) int, createdAt func(T) time.Time, id func(T) string) {
	sort.Slice(items, func(i, j int) bool {
		if position(items[i]) != position(items[j]) {
			return position(items[i]) < position(items[j])
		}
		if !createdAt(items[i]).Equal(createdAt(items[j])) {
			return createdAt(items[i]).Before(createdAt(items[j]))
		}
		return id(items[i]) < id(items[j])
	})
}

// detachHookExecutions mirrors the schema's ON DELETE SET NULL trigger
// (migration 000026): deleting a hook keeps its execution records with hook_id
// cleared and the denormalized name intact.
func (d *hookData) detachHookExecutions(hookID string) {
	for i := range d.hookExecutions {
		if d.hookExecutions[i].HookID != nil && *d.hookExecutions[i].HookID == hookID {
			d.hookExecutions[i].HookID = nil
		}
	}
}

func cloneInstanceHook(h *domain.InstanceHook) *domain.InstanceHook {
	if h == nil {
		return nil
	}
	cp := *h
	cp.HookBase = cloneHookBase(h.HookBase)
	return &cp
}

func cloneWorkspaceHook(h *domain.WorkspaceHook) *domain.WorkspaceHook {
	if h == nil {
		return nil
	}
	cp := *h
	cp.HookBase = cloneHookBase(h.HookBase)
	return &cp
}

func cloneAgentHook(h *domain.AgentHook) *domain.AgentHook {
	if h == nil {
		return nil
	}
	cp := *h
	cp.HookBase = cloneHookBase(h.HookBase)
	return &cp
}

func cloneHookBase(b domain.HookBase) domain.HookBase {
	cp := b
	if b.Config != nil {
		cp.Config = make(json.RawMessage, len(b.Config))
		copy(cp.Config, b.Config)
	}
	return cp
}

func cloneHookExecution(e *domain.HookExecution) *domain.HookExecution {
	if e == nil {
		return nil
	}
	cp := *e
	if e.HookID != nil {
		id := *e.HookID
		cp.HookID = &id
	}
	if e.ExitCode != nil {
		code := *e.ExitCode
		cp.ExitCode = &code
	}
	if e.HTTPStatus != nil {
		status := *e.HTTPStatus
		cp.HTTPStatus = &status
	}
	if e.TokenCount != nil {
		count := *e.TokenCount
		cp.TokenCount = &count
	}
	return &cp
}

// -------------------------------------------------------------------------
// HookStore implementation
// -------------------------------------------------------------------------

type hookStore struct {
	s *fakeStore
}

// -------------------------------------------------------------------------
// Instance level (workspace-unscoped by design; D13/D15)
// -------------------------------------------------------------------------

func (hs *hookStore) CreateInstanceHook(ctx context.Context, hook *domain.InstanceHook) error {
	if hook == nil {
		return domain.ErrInvalid
	}
	if hook.Source != domain.HookSourceManaged {
		return fmt.Errorf("%w: instance hooks are created with source %q only; builtin rows are owned by the sync pipeline", domain.ErrInvalid, domain.HookSourceManaged)
	}
	if err := hook.Validate(nil); err != nil {
		return err
	}

	hs.s.mu.Lock()
	defer hs.s.mu.Unlock()

	key := string(hook.Source) + ":" + hook.Key
	if _, exists := hs.s.hooks.instanceHooksByKey[key]; exists {
		return fmt.Errorf("%w: instance hook with key %q already exists", domain.ErrConflict, hook.Key)
	}
	if hook.ID != "" {
		if _, exists := hs.s.hooks.instanceHooks[hook.ID]; exists {
			return fmt.Errorf("%w: instance hook with id %q already exists", domain.ErrConflict, hook.ID)
		}
	} else {
		hook.ID = uuid.NewString()
	}

	now := time.Now().UTC()
	if hook.CreatedAt.IsZero() {
		hook.CreatedAt = now
	}
	if hook.UpdatedAt.IsZero() {
		hook.UpdatedAt = now
	}
	normalizeHookBase(&hook.HookBase)
	hook.Position = nextHookPosition(instanceHookPositions(hs.s.hooks))

	hs.s.hooks.instanceHooks[hook.ID] = cloneInstanceHook(hook)
	hs.s.hooks.instanceHooksByKey[key] = hook.ID
	return nil
}

func (hs *hookStore) ListInstanceHooks(ctx context.Context) ([]domain.InstanceHook, error) {
	hs.s.mu.RLock()
	defer hs.s.mu.RUnlock()

	hooks := make([]domain.InstanceHook, 0, len(hs.s.hooks.instanceHooks))
	for _, h := range hs.s.hooks.instanceHooks {
		hooks = append(hooks, *cloneInstanceHook(h))
	}
	sortByListOrder(hooks,
		func(h domain.InstanceHook) int { return h.Position },
		func(h domain.InstanceHook) time.Time { return h.CreatedAt },
		func(h domain.InstanceHook) string { return h.ID },
	)
	return hooks, nil
}

func (hs *hookStore) GetInstanceHook(ctx context.Context, id string) (*domain.InstanceHook, error) {
	if id == "" {
		return nil, nil
	}

	hs.s.mu.RLock()
	defer hs.s.mu.RUnlock()

	h, exists := hs.s.hooks.instanceHooks[id]
	if !exists {
		return nil, nil
	}
	return cloneInstanceHook(h), nil
}

func (hs *hookStore) UpdateInstanceHook(ctx context.Context, hook *domain.InstanceHook) error {
	if hook == nil || hook.ID == "" {
		return domain.ErrInvalid
	}
	if err := hook.Validate(nil); err != nil {
		return err
	}

	hs.s.mu.Lock()
	defer hs.s.mu.Unlock()

	existing, exists := hs.s.hooks.instanceHooks[hook.ID]
	if !exists {
		return domain.ErrNotFound
	}
	if existing.Source == domain.HookSourceBuiltin {
		return fmt.Errorf("%w: builtin instance hooks are read-only; ship a new version via the sync pipeline", domain.ErrInvalid)
	}

	// key/source/version and position are owned by other methods; created_at
	// is immutable.
	existing.Name = hook.Name
	existing.Event = hook.Event
	existing.Matcher = hook.Matcher
	existing.If = hook.If
	existing.HandlerType = hook.HandlerType
	existing.Config = cloneHookBase(hook.HookBase).Config
	existing.TimeoutMS = hook.TimeoutMS
	existing.OnFailure = hook.OnFailure
	existing.Enabled = hook.Enabled
	existing.Status = hook.Status
	existing.StatusError = hook.StatusError
	normalizeHookBase(&existing.HookBase)
	existing.UpdatedAt = time.Now().UTC()

	*hook = *cloneInstanceHook(existing)
	return nil
}

func (hs *hookStore) DeleteInstanceHook(ctx context.Context, id string) error {
	if id == "" {
		return domain.ErrNotFound
	}

	hs.s.mu.Lock()
	defer hs.s.mu.Unlock()

	existing, exists := hs.s.hooks.instanceHooks[id]
	if !exists {
		return domain.ErrNotFound
	}
	if existing.Source == domain.HookSourceBuiltin {
		return fmt.Errorf("%w: builtin instance hooks are read-only; they are removed via DeleteMissingBuiltinHooks", domain.ErrInvalid)
	}

	delete(hs.s.hooks.instanceHooks, id)
	delete(hs.s.hooks.instanceHooksByKey, string(existing.Source)+":"+existing.Key)
	hs.s.hooks.detachHookExecutions(id)
	return nil
}

func (hs *hookStore) RepositionInstanceHooks(ctx context.Context, ids []string) error {
	hs.s.mu.Lock()
	defer hs.s.mu.Unlock()

	now := time.Now().UTC()
	pos := 0
	for _, id := range ids {
		if h, ok := hs.s.hooks.instanceHooks[id]; ok {
			// Listed hooks are renumbered compactly; ids not present are
			// skipped and do not leave gaps.
			h.Position = pos
			h.UpdatedAt = now
			pos++
		}
	}
	return nil
}

func (hs *hookStore) UpsertBuiltinHook(ctx context.Context, hook *domain.InstanceHook) error {
	if hook == nil {
		return domain.ErrInvalid
	}
	if err := hook.Validate(nil); err != nil {
		return err
	}

	hs.s.mu.Lock()
	defer hs.s.mu.Unlock()

	if hook.ID == "" {
		hook.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	if hook.CreatedAt.IsZero() {
		hook.CreatedAt = now
	}
	if hook.UpdatedAt.IsZero() {
		hook.UpdatedAt = now
	}
	normalizeHookBase(&hook.HookBase)

	key := string(hook.Source) + ":" + hook.Key
	existingID, exists := hs.s.hooks.instanceHooksByKey[key]
	if !exists {
		stored := cloneInstanceHook(hook)
		hs.s.hooks.instanceHooks[stored.ID] = stored
		hs.s.hooks.instanceHooksByKey[key] = stored.ID
		return nil
	}

	existing := hs.s.hooks.instanceHooks[existingID]
	if existing.Version < hook.Version {
		// Version guard passed: apply the new definition. Identity fields
		// (id, key, source, created_at) persist; a new definition has no
		// delivery history, so the health flag resets.
		updated := cloneInstanceHook(hook)
		updated.ID = existing.ID
		updated.CreatedAt = existing.CreatedAt
		updated.UpdatedAt = now
		updated.Status = domain.HookStatusOK
		updated.StatusError = ""
		hs.s.hooks.instanceHooks[existing.ID] = updated
		*hook = *cloneInstanceHook(updated)
		return nil
	}

	// Same or newer stored version: silent no-op (never downgrade, D15);
	// surface the stored truth to the caller.
	*hook = *cloneInstanceHook(existing)
	return nil
}

func (hs *hookStore) DeleteMissingBuiltinHooks(ctx context.Context, keepKeys []string) error {
	hs.s.mu.Lock()
	defer hs.s.mu.Unlock()

	keep := make(map[string]struct{}, len(keepKeys))
	for _, key := range keepKeys {
		keep[key] = struct{}{}
	}
	for id, h := range hs.s.hooks.instanceHooks {
		if h.Source != domain.HookSourceBuiltin {
			continue
		}
		if _, ok := keep[h.Key]; ok {
			continue
		}
		delete(hs.s.hooks.instanceHooks, id)
		delete(hs.s.hooks.instanceHooksByKey, string(h.Source)+":"+h.Key)
		hs.s.hooks.detachHookExecutions(id)
	}
	return nil
}

// -------------------------------------------------------------------------
// Workspace level
// -------------------------------------------------------------------------

func (hs *hookStore) CreateWorkspaceHook(ctx context.Context, hook *domain.WorkspaceHook) error {
	if hook == nil || hook.WorkspaceID == "" {
		return domain.ErrInvalid
	}
	if err := hook.Validate(nil); err != nil {
		return err
	}

	hs.s.mu.Lock()
	defer hs.s.mu.Unlock()

	if _, exists := hs.s.workspaces[hook.WorkspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}
	for _, existing := range hs.s.hooks.workspaceHooks {
		if existing.WorkspaceID == hook.WorkspaceID && existing.Name == hook.Name {
			return fmt.Errorf("%w: workspace hook %q already exists in workspace", domain.ErrConflict, hook.Name)
		}
	}

	if hook.ID != "" {
		if _, exists := hs.s.hooks.workspaceHooks[hook.ID]; exists {
			return fmt.Errorf("%w: workspace hook with id %q already exists", domain.ErrConflict, hook.ID)
		}
	} else {
		hook.ID = uuid.NewString()
	}

	now := time.Now().UTC()
	if hook.CreatedAt.IsZero() {
		hook.CreatedAt = now
	}
	if hook.UpdatedAt.IsZero() {
		hook.UpdatedAt = now
	}
	normalizeHookBase(&hook.HookBase)
	hook.Position = nextHookPosition(workspaceHookPositions(hs.s.hooks, hook.WorkspaceID))

	hs.s.hooks.workspaceHooks[hook.ID] = cloneWorkspaceHook(hook)
	return nil
}

func (hs *hookStore) ListWorkspaceHooks(ctx context.Context, workspaceID string) ([]domain.WorkspaceHook, error) {
	if workspaceID == "" {
		return []domain.WorkspaceHook{}, nil
	}

	hs.s.mu.RLock()
	defer hs.s.mu.RUnlock()

	hooks := make([]domain.WorkspaceHook, 0)
	for _, h := range hs.s.hooks.workspaceHooks {
		if h.WorkspaceID != workspaceID {
			continue
		}
		hooks = append(hooks, *cloneWorkspaceHook(h))
	}
	sortByListOrder(hooks,
		func(h domain.WorkspaceHook) int { return h.Position },
		func(h domain.WorkspaceHook) time.Time { return h.CreatedAt },
		func(h domain.WorkspaceHook) string { return h.ID },
	)
	return hooks, nil
}

func (hs *hookStore) GetWorkspaceHook(ctx context.Context, workspaceID, id string) (*domain.WorkspaceHook, error) {
	if workspaceID == "" || id == "" {
		return nil, nil
	}

	hs.s.mu.RLock()
	defer hs.s.mu.RUnlock()

	h, exists := hs.s.hooks.workspaceHooks[id]
	if !exists || h.WorkspaceID != workspaceID {
		return nil, nil
	}
	return cloneWorkspaceHook(h), nil
}

func (hs *hookStore) UpdateWorkspaceHook(ctx context.Context, hook *domain.WorkspaceHook) error {
	if hook == nil || hook.ID == "" || hook.WorkspaceID == "" {
		return domain.ErrInvalid
	}
	if err := hook.Validate(nil); err != nil {
		return err
	}

	hs.s.mu.Lock()
	defer hs.s.mu.Unlock()

	existing, exists := hs.s.hooks.workspaceHooks[hook.ID]
	if !exists || existing.WorkspaceID != hook.WorkspaceID {
		return domain.ErrNotFound
	}
	if hook.Name != existing.Name {
		for _, other := range hs.s.hooks.workspaceHooks {
			if other.ID != hook.ID && other.WorkspaceID == hook.WorkspaceID && other.Name == hook.Name {
				return fmt.Errorf("%w: workspace hook %q already exists in workspace", domain.ErrConflict, hook.Name)
			}
		}
	}

	// position and created_at are owned by other methods and immutable here.
	existing.Name = hook.Name
	existing.Event = hook.Event
	existing.Matcher = hook.Matcher
	existing.If = hook.If
	existing.HandlerType = hook.HandlerType
	existing.Config = cloneHookBase(hook.HookBase).Config
	existing.TimeoutMS = hook.TimeoutMS
	existing.OnFailure = hook.OnFailure
	existing.Enabled = hook.Enabled
	existing.Status = hook.Status
	existing.StatusError = hook.StatusError
	normalizeHookBase(&existing.HookBase)
	existing.UpdatedAt = time.Now().UTC()

	*hook = *cloneWorkspaceHook(existing)
	return nil
}

func (hs *hookStore) DeleteWorkspaceHook(ctx context.Context, workspaceID, id string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	hs.s.mu.Lock()
	defer hs.s.mu.Unlock()

	existing, exists := hs.s.hooks.workspaceHooks[id]
	if !exists || existing.WorkspaceID != workspaceID {
		return domain.ErrNotFound
	}
	delete(hs.s.hooks.workspaceHooks, id)
	hs.s.hooks.detachHookExecutions(id)
	return nil
}

func (hs *hookStore) RepositionWorkspaceHooks(ctx context.Context, workspaceID string, ids []string) error {
	if workspaceID == "" {
		return nil
	}

	hs.s.mu.Lock()
	defer hs.s.mu.Unlock()

	now := time.Now().UTC()
	pos := 0
	for _, id := range ids {
		if h, ok := hs.s.hooks.workspaceHooks[id]; ok && h.WorkspaceID == workspaceID {
			h.Position = pos
			h.UpdatedAt = now
			pos++
		}
	}
	return nil
}

// -------------------------------------------------------------------------
// Agent level
// -------------------------------------------------------------------------

func (hs *hookStore) CreateAgentHook(ctx context.Context, hook *domain.AgentHook) error {
	if hook == nil || hook.WorkspaceID == "" || hook.AgentID == "" {
		return domain.ErrInvalid
	}
	if err := hook.Validate(nil); err != nil {
		return err
	}

	hs.s.mu.Lock()
	defer hs.s.mu.Unlock()

	if _, exists := hs.s.workspaces[hook.WorkspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}
	a, exists := hs.s.agents[hook.AgentID]
	if !exists || a.WorkspaceID != hook.WorkspaceID {
		return fmt.Errorf("%w: agent not found in workspace", domain.ErrNotFound)
	}
	for _, existing := range hs.s.hooks.agentHooks {
		if existing.AgentID == hook.AgentID && existing.Name == hook.Name {
			return fmt.Errorf("%w: agent hook %q already exists for agent", domain.ErrConflict, hook.Name)
		}
	}

	if hook.ID != "" {
		if _, exists := hs.s.hooks.agentHooks[hook.ID]; exists {
			return fmt.Errorf("%w: agent hook with id %q already exists", domain.ErrConflict, hook.ID)
		}
	} else {
		hook.ID = uuid.NewString()
	}

	now := time.Now().UTC()
	if hook.CreatedAt.IsZero() {
		hook.CreatedAt = now
	}
	if hook.UpdatedAt.IsZero() {
		hook.UpdatedAt = now
	}
	normalizeHookBase(&hook.HookBase)
	hook.Position = nextHookPosition(agentHookPositions(hs.s.hooks, hook.WorkspaceID, hook.AgentID))

	hs.s.hooks.agentHooks[hook.ID] = cloneAgentHook(hook)
	return nil
}

func (hs *hookStore) ListAgentHooks(ctx context.Context, workspaceID, agentID string) ([]domain.AgentHook, error) {
	if workspaceID == "" || agentID == "" {
		return []domain.AgentHook{}, nil
	}

	hs.s.mu.RLock()
	defer hs.s.mu.RUnlock()

	hooks := make([]domain.AgentHook, 0)
	for _, h := range hs.s.hooks.agentHooks {
		if h.WorkspaceID != workspaceID || h.AgentID != agentID {
			continue
		}
		hooks = append(hooks, *cloneAgentHook(h))
	}
	sortByListOrder(hooks,
		func(h domain.AgentHook) int { return h.Position },
		func(h domain.AgentHook) time.Time { return h.CreatedAt },
		func(h domain.AgentHook) string { return h.ID },
	)
	return hooks, nil
}

func (hs *hookStore) GetAgentHook(ctx context.Context, workspaceID, agentID, id string) (*domain.AgentHook, error) {
	if workspaceID == "" || agentID == "" || id == "" {
		return nil, nil
	}

	hs.s.mu.RLock()
	defer hs.s.mu.RUnlock()

	h, exists := hs.s.hooks.agentHooks[id]
	if !exists || h.WorkspaceID != workspaceID || h.AgentID != agentID {
		return nil, nil
	}
	return cloneAgentHook(h), nil
}

func (hs *hookStore) UpdateAgentHook(ctx context.Context, hook *domain.AgentHook) error {
	if hook == nil || hook.ID == "" || hook.WorkspaceID == "" || hook.AgentID == "" {
		return domain.ErrInvalid
	}
	if err := hook.Validate(nil); err != nil {
		return err
	}

	hs.s.mu.Lock()
	defer hs.s.mu.Unlock()

	existing, exists := hs.s.hooks.agentHooks[hook.ID]
	if !exists || existing.WorkspaceID != hook.WorkspaceID || existing.AgentID != hook.AgentID {
		return domain.ErrNotFound
	}
	if hook.Name != existing.Name {
		for _, other := range hs.s.hooks.agentHooks {
			if other.ID != hook.ID && other.AgentID == hook.AgentID && other.Name == hook.Name {
				return fmt.Errorf("%w: agent hook %q already exists for agent", domain.ErrConflict, hook.Name)
			}
		}
	}

	// position and created_at are owned by other methods and immutable here.
	existing.Name = hook.Name
	existing.Event = hook.Event
	existing.Matcher = hook.Matcher
	existing.If = hook.If
	existing.HandlerType = hook.HandlerType
	existing.Config = cloneHookBase(hook.HookBase).Config
	existing.TimeoutMS = hook.TimeoutMS
	existing.OnFailure = hook.OnFailure
	existing.Enabled = hook.Enabled
	existing.Status = hook.Status
	existing.StatusError = hook.StatusError
	normalizeHookBase(&existing.HookBase)
	existing.UpdatedAt = time.Now().UTC()

	*hook = *cloneAgentHook(existing)
	return nil
}

func (hs *hookStore) DeleteAgentHook(ctx context.Context, workspaceID, agentID, id string) error {
	if workspaceID == "" || agentID == "" || id == "" {
		return domain.ErrNotFound
	}

	hs.s.mu.Lock()
	defer hs.s.mu.Unlock()

	existing, exists := hs.s.hooks.agentHooks[id]
	if !exists || existing.WorkspaceID != workspaceID || existing.AgentID != agentID {
		return domain.ErrNotFound
	}
	delete(hs.s.hooks.agentHooks, id)
	hs.s.hooks.detachHookExecutions(id)
	return nil
}

func (hs *hookStore) RepositionAgentHooks(ctx context.Context, workspaceID, agentID string, ids []string) error {
	if workspaceID == "" || agentID == "" {
		return nil
	}

	hs.s.mu.Lock()
	defer hs.s.mu.Unlock()

	now := time.Now().UTC()
	pos := 0
	for _, id := range ids {
		if h, ok := hs.s.hooks.agentHooks[id]; ok && h.WorkspaceID == workspaceID && h.AgentID == agentID {
			h.Position = pos
			h.UpdatedAt = now
			pos++
		}
	}
	return nil
}

// -------------------------------------------------------------------------
// Health (per-delivery status; D16/D7)
// -------------------------------------------------------------------------

// SetHookDeliveryStatus updates one hook's health fields. The level selects
// the map, mirroring the postgres adapter's table selection; an unknown id is
// an idempotent no-op (the hook may have been deleted mid-run).
func (hs *hookStore) SetHookDeliveryStatus(ctx context.Context, level domain.HookLevel, hookID string, status domain.HookStatus, statusError string) error {
	if hookID == "" {
		return nil
	}

	hs.s.mu.Lock()
	defer hs.s.mu.Unlock()

	switch level {
	case domain.HookLevelInstance:
		if h, ok := hs.s.hooks.instanceHooks[hookID]; ok {
			h.Status = status
			h.StatusError = statusError
			h.UpdatedAt = time.Now().UTC()
		}
	case domain.HookLevelWorkspace:
		if h, ok := hs.s.hooks.workspaceHooks[hookID]; ok {
			h.Status = status
			h.StatusError = statusError
			h.UpdatedAt = time.Now().UTC()
		}
	case domain.HookLevelAgent:
		if h, ok := hs.s.hooks.agentHooks[hookID]; ok {
			h.Status = status
			h.StatusError = statusError
			h.UpdatedAt = time.Now().UTC()
		}
	default:
		return fmt.Errorf("%w: level: unknown level %q", domain.ErrInvalid, level)
	}
	// Unknown ids are an idempotent no-op, not an error.
	return nil
}

// -------------------------------------------------------------------------
// Executions (audit log; D16)
// -------------------------------------------------------------------------

func (hs *hookStore) RecordHookExecution(ctx context.Context, exec *domain.HookExecution) error {
	if exec == nil {
		return domain.ErrInvalid
	}

	hs.s.mu.Lock()
	defer hs.s.mu.Unlock()

	if exec.ID == "" {
		exec.ID = uuid.NewString()
	}
	if exec.CreatedAt.IsZero() {
		exec.CreatedAt = time.Now().UTC()
	}
	exec.Detail = truncateHookDetail(exec.Detail)

	hs.s.hooks.hookExecutions = append(hs.s.hooks.hookExecutions, *cloneHookExecution(exec))
	return nil
}

func (hs *hookStore) ListHookExecutions(ctx context.Context, workspaceID string, hookID *string, limit int) ([]domain.HookExecution, error) {
	if workspaceID == "" {
		return []domain.HookExecution{}, nil
	}

	hs.s.mu.RLock()
	defer hs.s.mu.RUnlock()

	// Iterate newest-insert-first so the stable sort below keeps the newest
	// insert ahead on equal created_at (approximating created_at DESC, id DESC).
	stored := hs.s.hooks.hookExecutions
	out := make([]domain.HookExecution, 0)
	for i := len(stored) - 1; i >= 0; i-- {
		e := cloneHookExecution(&stored[i])
		if e.WorkspaceID != workspaceID {
			continue
		}
		if hookID != nil && (e.HookID == nil || *e.HookID != *hookID) {
			continue
		}
		out = append(out, *e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
