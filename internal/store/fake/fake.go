// Package fake implements an in-memory Store with transaction snapshot isolation.
package fake

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// fakeStore is an in-memory implementation of store.Store.
type fakeStore struct {
	mu sync.RWMutex

	users               map[string]*domain.User                 // key: ID
	usersByEmail        map[string]string                       // key: normalized email -> ID
	workspaces          map[string]*domain.Workspace            // key: ID
	workspacesBySlug    map[string]string                       // key: slug -> ID
	roles               map[string]*domain.Role                 // key: ID
	rolesByName         map[string]string                       // key: workspaceID + ":" + name -> ID
	members             map[string]*domain.Member               // key: workspaceID + ":" + userID -> Member
	providers           map[string]*domain.ProviderConfig       // key: ID
	agents              map[string]*domain.Agent                // key: ID
	agentsBySlug        map[string]string                       // key: workspaceID + ":" + slug -> ID
	userMemories        map[string]*domain.Memory               // key: workspaceID + ":" + userID -> Memory
	workspaceMemories   map[string]*domain.Memory               // key: workspaceID -> Memory
	agentDailyMemories  map[string]*domain.Memory               // key: workspaceID + ":" + agentID + ":" + date(2006-01-02) -> Memory
	sessionEvents       map[string][]domain.SessionEvent        // key: sessionID -> ordered events
	sessionCPData       map[string][]byte                       // key: checkpointID -> data
	apiKeys             map[string]*domain.WorkspaceAPIKey      // key: ID
	apiKeysByHash       map[string]string                       // key: SHA-256 hex hash -> ID
	toolSettings        map[string]*domain.WorkspaceToolSetting // key: workspaceID + ":" + toolKey -> Setting
	skills              map[string]*domain.WorkspaceSkill       // key: ID
	skillsByName        map[string]string                       // key: workspaceID + ":" + name -> ID
	wsMCPServers        map[string]*domain.WorkspaceMCPServer   // key: ID
	wsMCPServerNames    map[string]string                       // key: workspaceID + ":" + lower(name) -> ID
	agentMCPServers     map[string]*domain.AgentMCPServer       // key: ID
	agentMCPServerNames map[string]string                       // key: agentID + ":" + lower(name) -> ID
	hooks               *hookData                               // hook state: all three levels + execution audit log
}

// New creates a new in-memory fake store.
func New() store.Store {
	return newStore()
}

func newStore() *fakeStore {
	return &fakeStore{
		users:               make(map[string]*domain.User),
		usersByEmail:        make(map[string]string),
		workspaces:          make(map[string]*domain.Workspace),
		workspacesBySlug:    make(map[string]string),
		roles:               make(map[string]*domain.Role),
		rolesByName:         make(map[string]string),
		members:             make(map[string]*domain.Member),
		providers:           make(map[string]*domain.ProviderConfig),
		agents:              make(map[string]*domain.Agent),
		agentsBySlug:        make(map[string]string),
		userMemories:        make(map[string]*domain.Memory),
		workspaceMemories:   make(map[string]*domain.Memory),
		agentDailyMemories:  make(map[string]*domain.Memory),
		sessionEvents:       make(map[string][]domain.SessionEvent),
		sessionCPData:       make(map[string][]byte),
		apiKeys:             make(map[string]*domain.WorkspaceAPIKey),
		apiKeysByHash:       make(map[string]string),
		toolSettings:        make(map[string]*domain.WorkspaceToolSetting),
		skills:              make(map[string]*domain.WorkspaceSkill),
		skillsByName:        make(map[string]string),
		wsMCPServers:        make(map[string]*domain.WorkspaceMCPServer),
		wsMCPServerNames:    make(map[string]string),
		agentMCPServers:     make(map[string]*domain.AgentMCPServer),
		agentMCPServerNames: make(map[string]string),
		hooks:               newHookData(),
	}
}

// Users returns the UserStore sub-port.
func (s *fakeStore) Users() store.UserStore {
	return &userStore{s: s}
}

// Workspaces returns the WorkspaceStore sub-port.
func (s *fakeStore) Workspaces() store.WorkspaceStore {
	return &workspaceStore{s: s}
}

// Roles returns the RoleStore sub-port.
func (s *fakeStore) Roles() store.RoleStore {
	return &roleStore{s: s}
}

// Members returns the MemberStore sub-port.
func (s *fakeStore) Members() store.MemberStore {
	return &memberStore{s: s}
}

// Providers returns the ProviderStore sub-port.
func (s *fakeStore) Providers() store.ProviderStore {
	return &providerStore{s: s}
}

// Agents returns the AgentStore sub-port.
func (s *fakeStore) Agents() store.AgentStore {
	return &agentStore{s: s}
}

// Memories returns the MemoryStore sub-port.
func (s *fakeStore) Memories() store.MemoryStore {
	return &memoryStore{s: s}
}

// SessionEvents returns the SessionEventStore sub-port.
func (s *fakeStore) SessionEvents() store.SessionEventStore {
	return &sessionEventStore{s: s}
}

// SessionCheckpoints returns the SessionCheckpointStore sub-port.
func (s *fakeStore) SessionCheckpoints() store.SessionCheckpointStore {
	return &sessionCheckpointStore{s: s}
}

// APIKeys returns the WorkspaceAPIKeyStore sub-port.
func (s *fakeStore) APIKeys() store.WorkspaceAPIKeyStore {
	return &apiKeyStore{s: s}
}

// WorkspaceSkills returns the WorkspaceSkillStore sub-port.
func (s *fakeStore) WorkspaceSkills() store.WorkspaceSkillStore {
	return &workspaceSkillStore{s: s}
}

// ToolSettings returns the ToolSettingsStore sub-port.
func (s *fakeStore) ToolSettings() store.ToolSettingsStore {
	return &toolSettingStore{s: s}
}

// WorkspaceMCPServers returns the WorkspaceMCPServers sub-port.
func (s *fakeStore) WorkspaceMCPServers() store.WorkspaceMCPServers {
	return &workspaceMCPServerStore{s: s}
}

// AgentMCPServers returns the AgentMCPServers sub-port.
func (s *fakeStore) AgentMCPServers() store.AgentMCPServers {
	return &agentMCPServerStore{s: s}
}

// WithTx executes the given function in an isolated transaction.
// It snapshots state on entry and applies modifications only if fn returns nil.
func (s *fakeStore) WithTx(ctx context.Context, fn func(store.Store) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	txStore := s.clone()
	if err := fn(txStore); err != nil {
		return err
	}

	s.apply(txStore)
	return nil
}

// Close closes the store.
func (s *fakeStore) Close() error {
	return nil
}

func (s *fakeStore) clone() *fakeStore {
	cp := newStore()
	for id, u := range s.users {
		cp.users[id] = cloneUser(u)
	}
	for email, id := range s.usersByEmail {
		cp.usersByEmail[email] = id
	}
	for id, ws := range s.workspaces {
		cp.workspaces[id] = cloneWorkspace(ws)
	}
	for slug, id := range s.workspacesBySlug {
		cp.workspacesBySlug[slug] = id
	}
	for id, r := range s.roles {
		cp.roles[id] = cloneRole(r)
	}
	for key, id := range s.rolesByName {
		cp.rolesByName[key] = id
	}
	for key, m := range s.members {
		cp.members[key] = cloneMember(m)
	}
	for id, p := range s.providers {
		cp.providers[id] = cloneProvider(p)
	}
	for id, a := range s.agents {
		cp.agents[id] = cloneAgent(a)
	}
	for key, id := range s.agentsBySlug {
		cp.agentsBySlug[key] = id
	}
	for key, m := range s.userMemories {
		cp.userMemories[key] = cloneMemory(m)
	}
	for key, m := range s.workspaceMemories {
		cp.workspaceMemories[key] = cloneMemory(m)
	}
	for key, m := range s.agentDailyMemories {
		cp.agentDailyMemories[key] = cloneMemory(m)
	}
	for sid, evts := range s.sessionEvents {
		copied := make([]domain.SessionEvent, len(evts))
		copy(copied, evts)
		cp.sessionEvents[sid] = copied
	}
	for cid, data := range s.sessionCPData {
		copied := make([]byte, len(data))
		copy(copied, data)
		cp.sessionCPData[cid] = copied
	}
	for id, k := range s.apiKeys {
		cp.apiKeys[id] = cloneWorkspaceAPIKey(k)
	}
	for hash, id := range s.apiKeysByHash {
		cp.apiKeysByHash[hash] = id
	}
	for key, ts := range s.toolSettings {
		cp.toolSettings[key] = cloneWorkspaceToolSetting(ts)
	}
	for id, sk := range s.skills {
		cp.skills[id] = cloneWorkspaceSkill(sk)
	}
	for key, id := range s.skillsByName {
		cp.skillsByName[key] = id
	}
	for id, srv := range s.wsMCPServers {
		cp.wsMCPServers[id] = cloneWorkspaceMCPServer(srv)
	}
	for key, id := range s.wsMCPServerNames {
		cp.wsMCPServerNames[key] = id
	}
	for id, srv := range s.agentMCPServers {
		cp.agentMCPServers[id] = cloneAgentMCPServer(srv)
	}
	for key, id := range s.agentMCPServerNames {
		cp.agentMCPServerNames[key] = id
	}
	cp.hooks = s.hooks.clone()
	return cp
}

func (s *fakeStore) apply(other *fakeStore) {
	s.users = other.users
	s.usersByEmail = other.usersByEmail
	s.workspaces = other.workspaces
	s.workspacesBySlug = other.workspacesBySlug
	s.roles = other.roles
	s.rolesByName = other.rolesByName
	s.members = other.members
	s.providers = other.providers
	s.agents = other.agents
	s.agentsBySlug = other.agentsBySlug
	s.userMemories = other.userMemories
	s.workspaceMemories = other.workspaceMemories
	s.agentDailyMemories = other.agentDailyMemories
	s.sessionEvents = other.sessionEvents
	s.sessionCPData = other.sessionCPData
	s.apiKeys = other.apiKeys
	s.apiKeysByHash = other.apiKeysByHash
	s.toolSettings = other.toolSettings
	s.skills = other.skills
	s.skillsByName = other.skillsByName
	s.wsMCPServers = other.wsMCPServers
	s.wsMCPServerNames = other.wsMCPServerNames
	s.agentMCPServers = other.agentMCPServers
	s.agentMCPServerNames = other.agentMCPServerNames
	s.hooks = other.hooks
}

func cloneUser(u *domain.User) *domain.User {
	if u == nil {
		return nil
	}
	cp := *u
	if u.PasswordHash != nil {
		h := *u.PasswordHash
		cp.PasswordHash = &h
	}
	if u.AvatarKey != nil {
		k := *u.AvatarKey
		cp.AvatarKey = &k
	}
	if u.AvatarURL != nil {
		url := *u.AvatarURL
		cp.AvatarURL = &url
	}
	if u.DisabledAt != nil {
		t := *u.DisabledAt
		cp.DisabledAt = &t
	}
	return &cp
}

func cloneWorkspace(ws *domain.Workspace) *domain.Workspace {
	if ws == nil {
		return nil
	}
	cp := *ws
	if ws.DisabledAt != nil {
		t := *ws.DisabledAt
		cp.DisabledAt = &t
	}
	return &cp
}

func cloneRole(r *domain.Role) *domain.Role {
	if r == nil {
		return nil
	}
	cp := *r
	if r.Permissions != nil {
		cp.Permissions = make([]string, len(r.Permissions))
		copy(cp.Permissions, r.Permissions)
	}
	return &cp
}

func cloneMember(m *domain.Member) *domain.Member {
	if m == nil {
		return nil
	}
	cp := *m
	if m.Role != nil {
		cp.Role = cloneRole(m.Role)
	}
	return &cp
}

func cloneProvider(p *domain.ProviderConfig) *domain.ProviderConfig {
	if p == nil {
		return nil
	}
	cp := *p
	return &cp
}

func cloneAgent(a *domain.Agent) *domain.Agent {
	if a == nil {
		return nil
	}
	cp := *a
	if a.MaxTokens != nil {
		m := *a.MaxTokens
		cp.MaxTokens = &m
	}
	if a.Effort != nil {
		e := *a.Effort
		cp.Effort = &e
	}
	if a.ContextWindow != nil {
		cw := *a.ContextWindow
		cp.ContextWindow = &cw
	}
	if a.Tools != nil {
		cp.Tools = make([]string, len(a.Tools))
		copy(cp.Tools, a.Tools)
	}
	if a.EnabledMCPS != nil {
		cp.EnabledMCPS = make([]string, len(a.EnabledMCPS))
		copy(cp.EnabledMCPS, a.EnabledMCPS)
	}
	if a.Avatar != nil {
		cp.Avatar = make(json.RawMessage, len(a.Avatar))
		copy(cp.Avatar, a.Avatar)
	}
	if a.PromptsError != nil {
		pe := *a.PromptsError
		cp.PromptsError = &pe
	}
	if a.CreatedBy != nil {
		cb := *a.CreatedBy
		cp.CreatedBy = &cb
	}
	if a.UpdatedBy != nil {
		ub := *a.UpdatedBy
		cp.UpdatedBy = &ub
	}
	return &cp
}

func cloneMemory(m *domain.Memory) *domain.Memory {
	if m == nil {
		return nil
	}
	cp := *m
	return &cp
}

func cloneWorkspaceAPIKey(k *domain.WorkspaceAPIKey) *domain.WorkspaceAPIKey {
	if k == nil {
		return nil
	}
	cp := *k
	if k.RevokedAt != nil {
		t := *k.RevokedAt
		cp.RevokedAt = &t
	}
	return &cp
}

func cloneWorkspaceToolSetting(ts *domain.WorkspaceToolSetting) *domain.WorkspaceToolSetting {
	if ts == nil {
		return nil
	}
	cp := *ts
	if ts.Config != nil {
		cp.Config = make(map[string]any, len(ts.Config))
		for k, v := range ts.Config {
			cp.Config[k] = v
		}
	}
	return &cp
}

func cloneWorkspaceSkill(sk *domain.WorkspaceSkill) *domain.WorkspaceSkill {
	if sk == nil {
		return nil
	}
	cp := *sk
	cp.Dependencies.Tools = cloneStringSlice(sk.Dependencies.Tools)
	cp.Dependencies.Binaries = cloneStringSlice(sk.Dependencies.Binaries)
	cp.Dependencies.Python = cloneStringSlice(sk.Dependencies.Python)
	return &cp
}

func cloneStringSlice(src []string) []string {
	if src == nil {
		return nil
	}
	cp := make([]string, len(src))
	copy(cp, src)
	return cp
}

func cloneEnvRows(src []domain.EnvRow) []domain.EnvRow {
	if src == nil {
		return nil
	}
	cp := make([]domain.EnvRow, len(src))
	copy(cp, src)
	return cp
}

func cloneWorkspaceMCPServer(srv *domain.WorkspaceMCPServer) *domain.WorkspaceMCPServer {
	if srv == nil {
		return nil
	}
	cp := *srv
	cp.MCPConnection = cloneMCPConnection(srv.MCPConnection)
	return &cp
}

func cloneAgentMCPServer(srv *domain.AgentMCPServer) *domain.AgentMCPServer {
	if srv == nil {
		return nil
	}
	cp := *srv
	cp.MCPConnection = cloneMCPConnection(srv.MCPConnection)
	return &cp
}

func cloneMCPConnection(c domain.MCPConnection) domain.MCPConnection {
	c.Args = cloneStringSlice(c.Args)
	c.Env = cloneEnvRows(c.Env)
	c.Headers = cloneEnvRows(c.Headers)
	return c
}

// -------------------------------------------------------------------------
// UserStore implementation
// -------------------------------------------------------------------------

type userStore struct {
	s *fakeStore
}

func (us *userStore) Create(ctx context.Context, u *domain.User) error {
	if u == nil {
		return domain.ErrInvalid
	}
	email := domain.NormalizeEmail(u.Email)
	if email == "" {
		return fmt.Errorf("%w: user email cannot be empty", domain.ErrInvalid)
	}
	if err := domain.ValidateEmail(email); err != nil {
		return err
	}
	if u.Name == "" {
		return fmt.Errorf("%w: user name cannot be empty", domain.ErrInvalid)
	}

	us.s.mu.Lock()
	defer us.s.mu.Unlock()

	if _, exists := us.s.usersByEmail[email]; exists {
		return fmt.Errorf("%w: user with email %q already exists", domain.ErrConflict, email)
	}

	if u.ID != "" {
		if _, exists := us.s.users[u.ID]; exists {
			return fmt.Errorf("%w: user with id %q already exists", domain.ErrConflict, u.ID)
		}
	} else {
		u.ID = uuid.NewString()
	}

	now := time.Now().UTC()
	if u.CreatedAt.IsZero() {
		u.CreatedAt = now
	}
	if u.UpdatedAt.IsZero() {
		u.UpdatedAt = now
	}
	u.Email = email

	us.s.users[u.ID] = cloneUser(u)
	us.s.usersByEmail[email] = u.ID
	return nil
}

func (us *userStore) ByEmail(ctx context.Context, email string) (*domain.User, error) {
	email = domain.NormalizeEmail(email)

	us.s.mu.RLock()
	defer us.s.mu.RUnlock()

	id, exists := us.s.usersByEmail[email]
	if !exists {
		return nil, domain.ErrNotFound
	}
	u, exists := us.s.users[id]
	if !exists {
		return nil, domain.ErrNotFound
	}
	return cloneUser(u), nil
}

func (us *userStore) ByID(ctx context.Context, id string) (*domain.User, error) {
	us.s.mu.RLock()
	defer us.s.mu.RUnlock()

	u, exists := us.s.users[id]
	if !exists {
		return nil, domain.ErrNotFound
	}
	return cloneUser(u), nil
}

func (us *userStore) List(ctx context.Context) ([]domain.User, error) {
	us.s.mu.RLock()
	defer us.s.mu.RUnlock()

	users := make([]domain.User, 0, len(us.s.users))
	for _, u := range us.s.users {
		cloned := cloneUser(u)
		count := 0
		for _, m := range us.s.members {
			if m.UserID == u.ID {
				count++
			}
		}
		cloned.MembershipCount = count
		users = append(users, *cloned)
	}
	sort.Slice(users, func(i, j int) bool {
		if users[i].CreatedAt.Equal(users[j].CreatedAt) {
			return users[i].ID < users[j].ID
		}
		return users[i].CreatedAt.Before(users[j].CreatedAt)
	})
	return users, nil
}

func (us *userStore) SetDisabled(ctx context.Context, id string, at *time.Time) error {
	us.s.mu.Lock()
	defer us.s.mu.Unlock()

	u, exists := us.s.users[id]
	if !exists {
		return domain.ErrNotFound
	}

	if at != nil {
		t := at.UTC()
		u.DisabledAt = &t
	} else {
		u.DisabledAt = nil
	}
	u.UpdatedAt = time.Now().UTC()
	return nil
}

func (us *userStore) SetPasswordHash(ctx context.Context, id string, hash string) error {
	us.s.mu.Lock()
	defer us.s.mu.Unlock()

	u, exists := us.s.users[id]
	if !exists {
		return domain.ErrNotFound
	}

	h := hash
	u.PasswordHash = &h
	u.UpdatedAt = time.Now().UTC()
	return nil
}

func (us *userStore) Update(ctx context.Context, u *domain.User) error {
	if u == nil || u.ID == "" {
		return domain.ErrInvalid
	}

	us.s.mu.Lock()
	defer us.s.mu.Unlock()

	existing, exists := us.s.users[u.ID]
	if !exists {
		return domain.ErrNotFound
	}

	if u.Email != "" {
		email := domain.NormalizeEmail(u.Email)
		if email != existing.Email {
			if otherID, exists := us.s.usersByEmail[email]; exists && otherID != u.ID {
				return fmt.Errorf("%w: user with email %q already exists", domain.ErrConflict, email)
			}
			delete(us.s.usersByEmail, existing.Email)
			us.s.usersByEmail[email] = u.ID
			existing.Email = email
		}
	}

	if u.Name != "" {
		existing.Name = u.Name
	}
	existing.AvatarKey = u.AvatarKey
	existing.AvatarURL = u.AvatarURL
	existing.DisabledAt = u.DisabledAt
	if u.PasswordHash != nil {
		existing.PasswordHash = u.PasswordHash
	}
	existing.UpdatedAt = time.Now().UTC()

	*u = *cloneUser(existing)
	return nil
}

// -------------------------------------------------------------------------
// WorkspaceStore implementation
// -------------------------------------------------------------------------

type workspaceStore struct {
	s *fakeStore
}

func (ws *workspaceStore) Create(ctx context.Context, w *domain.Workspace) error {
	if w == nil {
		return domain.ErrInvalid
	}
	if w.Slug == "" {
		return fmt.Errorf("%w: workspace slug cannot be empty", domain.ErrInvalid)
	}
	if !w.IsMaster {
		if err := domain.ValidateSlug(w.Slug); err != nil {
			return err
		}
	}

	ws.s.mu.Lock()
	defer ws.s.mu.Unlock()

	if _, exists := ws.s.workspacesBySlug[w.Slug]; exists {
		return fmt.Errorf("%w: workspace with slug %q already exists", domain.ErrConflict, w.Slug)
	}

	if w.ID != "" {
		if _, exists := ws.s.workspaces[w.ID]; exists {
			return fmt.Errorf("%w: workspace with id %q already exists", domain.ErrConflict, w.ID)
		}
	} else {
		w.ID = uuid.NewString()
	}

	if w.Timezone == "" {
		w.Timezone = "UTC"
	}

	now := time.Now().UTC()
	if w.CreatedAt.IsZero() {
		w.CreatedAt = now
	}
	if w.UpdatedAt.IsZero() {
		w.UpdatedAt = now
	}

	ws.s.workspaces[w.ID] = cloneWorkspace(w)
	ws.s.workspacesBySlug[w.Slug] = w.ID
	return nil
}

func (ws *workspaceStore) BySlug(ctx context.Context, slug string) (*domain.Workspace, error) {
	ws.s.mu.RLock()
	defer ws.s.mu.RUnlock()

	id, exists := ws.s.workspacesBySlug[slug]
	if !exists {
		return nil, domain.ErrNotFound
	}
	w, exists := ws.s.workspaces[id]
	if !exists {
		return nil, domain.ErrNotFound
	}
	return cloneWorkspace(w), nil
}

func (ws *workspaceStore) ByID(ctx context.Context, id string) (*domain.Workspace, error) {
	ws.s.mu.RLock()
	defer ws.s.mu.RUnlock()

	w, exists := ws.s.workspaces[id]
	if !exists {
		return nil, domain.ErrNotFound
	}
	return cloneWorkspace(w), nil
}

func (ws *workspaceStore) Update(ctx context.Context, w *domain.Workspace) error {
	if w == nil || w.ID == "" {
		return domain.ErrInvalid
	}

	ws.s.mu.Lock()
	defer ws.s.mu.Unlock()

	existing, exists := ws.s.workspaces[w.ID]
	if !exists {
		return domain.ErrNotFound
	}

	if w.Name != "" {
		existing.Name = w.Name
	}
	existing.Description = w.Description
	if w.Timezone != "" {
		existing.Timezone = w.Timezone
	}
	existing.IsMaster = w.IsMaster
	existing.DisabledAt = w.DisabledAt
	existing.UpdatedAt = time.Now().UTC()

	*w = *cloneWorkspace(existing)
	return nil
}

func (ws *workspaceStore) ListForUser(ctx context.Context, userID string) ([]domain.Workspace, error) {
	ws.s.mu.RLock()
	defer ws.s.mu.RUnlock()

	workspaces := make([]domain.Workspace, 0)
	for _, m := range ws.s.members {
		if m.UserID == userID {
			if w, exists := ws.s.workspaces[m.WorkspaceID]; exists {
				workspaces = append(workspaces, *cloneWorkspace(w))
			}
		}
	}
	sort.Slice(workspaces, func(i, j int) bool {
		if workspaces[i].CreatedAt.Equal(workspaces[j].CreatedAt) {
			return workspaces[i].ID < workspaces[j].ID
		}
		return workspaces[i].CreatedAt.Before(workspaces[j].CreatedAt)
	})
	return workspaces, nil
}

func (ws *workspaceStore) ListAll(ctx context.Context) ([]domain.Workspace, error) {
	ws.s.mu.RLock()
	defer ws.s.mu.RUnlock()

	workspaces := make([]domain.Workspace, 0, len(ws.s.workspaces))
	for _, w := range ws.s.workspaces {
		workspaces = append(workspaces, *cloneWorkspace(w))
	}
	sort.Slice(workspaces, func(i, j int) bool {
		if workspaces[i].CreatedAt.Equal(workspaces[j].CreatedAt) {
			return workspaces[i].ID < workspaces[j].ID
		}
		return workspaces[i].CreatedAt.Before(workspaces[j].CreatedAt)
	})
	return workspaces, nil
}

// -------------------------------------------------------------------------
// RoleStore implementation
// -------------------------------------------------------------------------

type roleStore struct {
	s *fakeStore
}

func (rs *roleStore) Create(ctx context.Context, r *domain.Role) error {
	if r == nil || r.WorkspaceID == "" || r.Name == "" {
		return domain.ErrInvalid
	}

	rs.s.mu.Lock()
	defer rs.s.mu.Unlock()

	nameKey := r.WorkspaceID + ":" + r.Name
	if _, exists := rs.s.rolesByName[nameKey]; exists {
		return fmt.Errorf("%w: role %q already exists in workspace", domain.ErrConflict, r.Name)
	}

	if r.ID != "" {
		if _, exists := rs.s.roles[r.ID]; exists {
			return fmt.Errorf("%w: role with id %q already exists", domain.ErrConflict, r.ID)
		}
	} else {
		r.ID = uuid.NewString()
	}

	now := time.Now().UTC()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}

	rs.s.roles[r.ID] = cloneRole(r)
	rs.s.rolesByName[nameKey] = r.ID
	return nil
}

func (rs *roleStore) ByID(ctx context.Context, id string) (*domain.Role, error) {
	rs.s.mu.RLock()
	defer rs.s.mu.RUnlock()

	r, exists := rs.s.roles[id]
	if !exists {
		return nil, domain.ErrNotFound
	}
	return cloneRole(r), nil
}

func (rs *roleStore) FindByName(ctx context.Context, workspaceID, name string) (*domain.Role, error) {
	rs.s.mu.RLock()
	defer rs.s.mu.RUnlock()

	nameKey := workspaceID + ":" + name
	id, exists := rs.s.rolesByName[nameKey]
	if !exists {
		return nil, domain.ErrNotFound
	}
	r, exists := rs.s.roles[id]
	if !exists {
		return nil, domain.ErrNotFound
	}
	return cloneRole(r), nil
}

func (rs *roleStore) ListForWorkspace(ctx context.Context, workspaceID string) ([]domain.Role, error) {
	rs.s.mu.RLock()
	defer rs.s.mu.RUnlock()

	roles := make([]domain.Role, 0)
	for _, r := range rs.s.roles {
		if r.WorkspaceID == workspaceID {
			roles = append(roles, *cloneRole(r))
		}
	}
	sort.Slice(roles, func(i, j int) bool {
		if roles[i].CreatedAt.Equal(roles[j].CreatedAt) {
			return roles[i].ID < roles[j].ID
		}
		return roles[i].CreatedAt.Before(roles[j].CreatedAt)
	})
	return roles, nil
}

func (rs *roleStore) CountMembers(ctx context.Context, roleID string) (int, error) {
	rs.s.mu.RLock()
	defer rs.s.mu.RUnlock()

	count := 0
	for _, m := range rs.s.members {
		if m.RoleID == roleID {
			count++
		}
	}
	return count, nil
}

// -------------------------------------------------------------------------
// MemberStore implementation
// -------------------------------------------------------------------------

type memberStore struct {
	s *fakeStore
}

func (ms *memberStore) Add(ctx context.Context, m *domain.Member) error {
	if m == nil || m.WorkspaceID == "" || m.UserID == "" || m.RoleID == "" {
		return domain.ErrInvalid
	}

	ms.s.mu.Lock()
	defer ms.s.mu.Unlock()

	memberKey := m.WorkspaceID + ":" + m.UserID
	if _, exists := ms.s.members[memberKey]; exists {
		return fmt.Errorf("%w: user is already a member of workspace", domain.ErrConflict)
	}

	if _, exists := ms.s.workspaces[m.WorkspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}
	if _, exists := ms.s.users[m.UserID]; !exists {
		return fmt.Errorf("%w: user not found", domain.ErrNotFound)
	}
	r, exists := ms.s.roles[m.RoleID]
	if !exists {
		return fmt.Errorf("%w: role not found", domain.ErrNotFound)
	}
	if r.WorkspaceID != m.WorkspaceID {
		return fmt.Errorf("%w: role does not belong to workspace", domain.ErrInvalid)
	}

	now := time.Now().UTC()
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}

	ms.s.members[memberKey] = cloneMember(m)
	return nil
}

func (ms *memberStore) Get(ctx context.Context, workspaceID, userID string) (*domain.Member, error) {
	ms.s.mu.RLock()
	defer ms.s.mu.RUnlock()

	memberKey := workspaceID + ":" + userID
	m, exists := ms.s.members[memberKey]
	if !exists {
		return nil, domain.ErrNotFound
	}

	res := cloneMember(m)
	if r, exists := ms.s.roles[m.RoleID]; exists {
		res.Role = cloneRole(r)
	}
	return res, nil
}

func (ms *memberStore) ListForWorkspace(ctx context.Context, workspaceID string) ([]domain.Member, error) {
	ms.s.mu.RLock()
	defer ms.s.mu.RUnlock()

	members := make([]domain.Member, 0)
	for _, m := range ms.s.members {
		if m.WorkspaceID == workspaceID {
			res := cloneMember(m)
			if r, exists := ms.s.roles[m.RoleID]; exists {
				res.Role = cloneRole(r)
			}
			members = append(members, *res)
		}
	}
	sort.Slice(members, func(i, j int) bool {
		if members[i].CreatedAt.Equal(members[j].CreatedAt) {
			return members[i].UserID < members[j].UserID
		}
		return members[i].CreatedAt.Before(members[j].CreatedAt)
	})
	return members, nil
}

func (ms *memberStore) ListForUser(ctx context.Context, userID string) ([]domain.MemberView, error) {
	ms.s.mu.RLock()
	defer ms.s.mu.RUnlock()

	views := make([]domain.MemberView, 0)
	for _, m := range ms.s.members {
		if m.UserID == userID {
			ws := ms.s.workspaces[m.WorkspaceID]
			u := ms.s.users[m.UserID]
			r := ms.s.roles[m.RoleID]

			view := domain.MemberView{
				WorkspaceID: m.WorkspaceID,
				UserID:      m.UserID,
				RoleID:      m.RoleID,
				JoinedAt:    m.CreatedAt,
			}
			if ws != nil {
				view.WorkspaceSlug = ws.Slug
				view.WorkspaceName = ws.Name
				view.Workspace = cloneWorkspace(ws)
			}
			if u != nil {
				view.Email = u.Email
				view.Name = u.Name
				view.AvatarKey = u.AvatarKey
				view.AvatarURL = u.AvatarURL
				view.Invited = u.PasswordHash == nil || *u.PasswordHash == ""
			}
			if r != nil {
				view.RoleName = r.Name
				view.Role = cloneRole(r)
			}
			views = append(views, view)
		}
	}
	sort.Slice(views, func(i, j int) bool {
		if views[i].JoinedAt.Equal(views[j].JoinedAt) {
			return views[i].WorkspaceID < views[j].WorkspaceID
		}
		return views[i].JoinedAt.Before(views[j].JoinedAt)
	})
	return views, nil
}

func (ms *memberStore) UpdateRole(ctx context.Context, workspaceID, userID, roleID string) error {
	ms.s.mu.Lock()
	defer ms.s.mu.Unlock()

	memberKey := workspaceID + ":" + userID
	m, exists := ms.s.members[memberKey]
	if !exists {
		return domain.ErrNotFound
	}

	r, exists := ms.s.roles[roleID]
	if !exists {
		return domain.ErrNotFound
	}
	if r.WorkspaceID != workspaceID {
		return fmt.Errorf("%w: role does not belong to workspace", domain.ErrInvalid)
	}

	m.RoleID = roleID
	return nil
}

func (ms *memberStore) Remove(ctx context.Context, workspaceID, userID string) error {
	ms.s.mu.Lock()
	defer ms.s.mu.Unlock()

	memberKey := workspaceID + ":" + userID
	if _, exists := ms.s.members[memberKey]; !exists {
		return domain.ErrNotFound
	}

	delete(ms.s.members, memberKey)
	return nil
}

// -------------------------------------------------------------------------
// ProviderStore implementation
// -------------------------------------------------------------------------

type providerStore struct {
	s *fakeStore
}

func (ps *providerStore) Create(ctx context.Context, p *domain.ProviderConfig) error {
	if p == nil || p.WorkspaceID == "" || p.Type == "" || p.Name == "" {
		return domain.ErrInvalid
	}

	ps.s.mu.Lock()
	defer ps.s.mu.Unlock()

	if p.ID != "" {
		if _, exists := ps.s.providers[p.ID]; exists {
			return fmt.Errorf("%w: provider with id %q already exists", domain.ErrConflict, p.ID)
		}
	} else {
		p.ID = uuid.NewString()
	}

	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = now
	}

	ps.s.providers[p.ID] = cloneProvider(p)
	return nil
}

func (ps *providerStore) ByID(ctx context.Context, workspaceID, id string) (*domain.ProviderConfig, error) {
	if workspaceID == "" || id == "" {
		return nil, domain.ErrNotFound
	}

	ps.s.mu.RLock()
	defer ps.s.mu.RUnlock()

	p, exists := ps.s.providers[id]
	if !exists || p.WorkspaceID != workspaceID {
		return nil, domain.ErrNotFound
	}
	return cloneProvider(p), nil
}

func (ps *providerStore) ListForWorkspace(ctx context.Context, workspaceID string) ([]domain.ProviderConfig, error) {
	if workspaceID == "" {
		return []domain.ProviderConfig{}, nil
	}

	ps.s.mu.RLock()
	defer ps.s.mu.RUnlock()

	providers := make([]domain.ProviderConfig, 0)
	for _, p := range ps.s.providers {
		if p.WorkspaceID == workspaceID {
			providers = append(providers, *cloneProvider(p))
		}
	}
	sort.Slice(providers, func(i, j int) bool {
		if providers[i].CreatedAt.Equal(providers[j].CreatedAt) {
			return providers[i].ID < providers[j].ID
		}
		return providers[i].CreatedAt.Before(providers[j].CreatedAt)
	})
	return providers, nil
}

func (ps *providerStore) Update(ctx context.Context, p *domain.ProviderConfig) error {
	if p == nil || p.ID == "" || p.WorkspaceID == "" {
		return domain.ErrInvalid
	}

	ps.s.mu.Lock()
	defer ps.s.mu.Unlock()

	existing, exists := ps.s.providers[p.ID]
	if !exists || existing.WorkspaceID != p.WorkspaceID {
		return domain.ErrNotFound
	}

	if p.Type != "" {
		existing.Type = p.Type
	}
	if p.Name != "" {
		existing.Name = p.Name
	}
	existing.BaseURL = p.BaseURL
	existing.KeyCiphertext = p.KeyCiphertext
	existing.KeyHint = p.KeyHint
	existing.Enabled = p.Enabled
	existing.UpdatedAt = time.Now().UTC()

	*p = *cloneProvider(existing)
	return nil
}

func (ps *providerStore) Delete(ctx context.Context, workspaceID, id string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	ps.s.mu.Lock()
	defer ps.s.mu.Unlock()

	existing, exists := ps.s.providers[id]
	if !exists || existing.WorkspaceID != workspaceID {
		return domain.ErrNotFound
	}

	delete(ps.s.providers, id)
	return nil
}

// -------------------------------------------------------------------------
// AgentStore implementation
// -------------------------------------------------------------------------

type agentStore struct {
	s *fakeStore
}

func (as *agentStore) Create(ctx context.Context, a *domain.Agent) error {
	if a == nil || a.WorkspaceID == "" || a.Name == "" || a.Slug == "" || a.ProviderID == "" || a.Model == "" {
		return fmt.Errorf("%w: missing required agent fields", domain.ErrInvalid)
	}
	if err := domain.ValidateAgentSlug(a.Slug); err != nil {
		return err
	}
	if a.Autonomy != "" {
		if err := domain.ValidateAgentAutonomy(a.Autonomy); err != nil {
			return err
		}
	} else {
		a.Autonomy = domain.AutonomyApproval
	}
	if err := domain.ValidateAgentTemperature(a.Temperature); err != nil {
		return err
	}
	if a.MaxTokens != nil && *a.MaxTokens <= 0 {
		return fmt.Errorf("%w: max_tokens must be positive", domain.ErrInvalid)
	}
	if err := domain.ValidateAgentContextWindow(a.ContextWindow); err != nil {
		return err
	}
	if len(a.Avatar) > 0 {
		if err := domain.ValidateAgentAvatar(a.Avatar); err != nil {
			return err
		}
	} else {
		a.Avatar = json.RawMessage("{}")
	}
	if a.PromptsStatus == "" {
		a.PromptsStatus = domain.PromptsStatusGenerating
	}
	if a.Tools == nil {
		a.Tools = []string{}
	}
	if a.EnabledMCPS == nil {
		a.EnabledMCPS = []string{}
	}

	as.s.mu.Lock()
	defer as.s.mu.Unlock()

	if _, exists := as.s.workspaces[a.WorkspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}

	p, exists := as.s.providers[a.ProviderID]
	if !exists || p.WorkspaceID != a.WorkspaceID {
		return fmt.Errorf("%w: provider not found in workspace", domain.ErrNotFound)
	}

	slugKey := a.WorkspaceID + ":" + a.Slug
	if _, exists := as.s.agentsBySlug[slugKey]; exists {
		return fmt.Errorf("%w: agent with slug %q already exists in workspace", domain.ErrConflict, a.Slug)
	}

	if a.ID != "" {
		if _, exists := as.s.agents[a.ID]; exists {
			return fmt.Errorf("%w: agent with id %q already exists", domain.ErrConflict, a.ID)
		}
	} else {
		a.ID = uuid.NewString()
	}

	now := time.Now().UTC()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
	if a.UpdatedAt.IsZero() {
		a.UpdatedAt = now
	}

	as.s.agents[a.ID] = cloneAgent(a)
	as.s.agentsBySlug[slugKey] = a.ID
	return nil
}

func (as *agentStore) ByID(ctx context.Context, workspaceID, id string) (*domain.Agent, error) {
	if workspaceID == "" || id == "" {
		return nil, domain.ErrNotFound
	}

	as.s.mu.RLock()
	defer as.s.mu.RUnlock()

	a, exists := as.s.agents[id]
	if !exists || a.WorkspaceID != workspaceID {
		return nil, domain.ErrNotFound
	}
	return cloneAgent(a), nil
}

func (as *agentStore) BySlug(ctx context.Context, workspaceID, slug string) (*domain.Agent, error) {
	if workspaceID == "" || slug == "" {
		return nil, domain.ErrNotFound
	}

	as.s.mu.RLock()
	defer as.s.mu.RUnlock()

	slugKey := workspaceID + ":" + slug
	id, exists := as.s.agentsBySlug[slugKey]
	if !exists {
		return nil, domain.ErrNotFound
	}
	a, exists := as.s.agents[id]
	if !exists || a.WorkspaceID != workspaceID {
		return nil, domain.ErrNotFound
	}
	return cloneAgent(a), nil
}

func (as *agentStore) ListForWorkspace(ctx context.Context, workspaceID string) ([]domain.Agent, error) {
	if workspaceID == "" {
		return []domain.Agent{}, nil
	}

	as.s.mu.RLock()
	defer as.s.mu.RUnlock()

	agents := make([]domain.Agent, 0)
	for _, a := range as.s.agents {
		if a.WorkspaceID == workspaceID {
			agents = append(agents, *cloneAgent(a))
		}
	}
	sort.Slice(agents, func(i, j int) bool {
		if agents[i].CreatedAt.Equal(agents[j].CreatedAt) {
			return agents[i].ID > agents[j].ID
		}
		return agents[i].CreatedAt.After(agents[j].CreatedAt)
	})
	return agents, nil
}

func (as *agentStore) Update(ctx context.Context, a *domain.Agent) error {
	if a == nil || a.ID == "" || a.WorkspaceID == "" || a.Name == "" || a.Slug == "" || a.ProviderID == "" || a.Model == "" {
		return fmt.Errorf("%w: missing required agent fields", domain.ErrInvalid)
	}
	if err := domain.ValidateAgentSlug(a.Slug); err != nil {
		return err
	}
	if a.Autonomy != "" {
		if err := domain.ValidateAgentAutonomy(a.Autonomy); err != nil {
			return err
		}
	}
	if err := domain.ValidateAgentTemperature(a.Temperature); err != nil {
		return err
	}
	if a.MaxTokens != nil && *a.MaxTokens <= 0 {
		return fmt.Errorf("%w: max_tokens must be positive", domain.ErrInvalid)
	}
	if err := domain.ValidateAgentContextWindow(a.ContextWindow); err != nil {
		return err
	}
	if len(a.Avatar) > 0 {
		if err := domain.ValidateAgentAvatar(a.Avatar); err != nil {
			return err
		}
	} else {
		a.Avatar = json.RawMessage("{}")
	}

	as.s.mu.Lock()
	defer as.s.mu.Unlock()

	existing, exists := as.s.agents[a.ID]
	if !exists || existing.WorkspaceID != a.WorkspaceID {
		return domain.ErrNotFound
	}

	p, exists := as.s.providers[a.ProviderID]
	if !exists || p.WorkspaceID != a.WorkspaceID {
		return fmt.Errorf("%w: provider not found in workspace", domain.ErrNotFound)
	}

	if a.Slug != existing.Slug {
		newSlugKey := a.WorkspaceID + ":" + a.Slug
		if otherID, exists := as.s.agentsBySlug[newSlugKey]; exists && otherID != a.ID {
			return fmt.Errorf("%w: agent with slug %q already exists in workspace", domain.ErrConflict, a.Slug)
		}
		delete(as.s.agentsBySlug, existing.WorkspaceID+":"+existing.Slug)
		as.s.agentsBySlug[newSlugKey] = a.ID
		existing.Slug = a.Slug
	}

	existing.Name = a.Name
	existing.Role = a.Role
	existing.Description = a.Description
	existing.Brief = a.Brief
	existing.ProviderID = a.ProviderID
	existing.Model = a.Model
	existing.Temperature = a.Temperature
	existing.MaxTokens = a.MaxTokens
	existing.Effort = a.Effort
	if a.Autonomy != "" {
		existing.Autonomy = a.Autonomy
	}
	existing.ContextWindow = a.ContextWindow
	if a.Tools != nil {
		existing.Tools = make([]string, len(a.Tools))
		copy(existing.Tools, a.Tools)
	}
	if a.EnabledMCPS != nil {
		existing.EnabledMCPS = make([]string, len(a.EnabledMCPS))
		copy(existing.EnabledMCPS, a.EnabledMCPS)
	}
	if a.Avatar != nil {
		existing.Avatar = make(json.RawMessage, len(a.Avatar))
		copy(existing.Avatar, a.Avatar)
	}
	existing.UpdatedBy = a.UpdatedBy
	existing.UpdatedAt = time.Now().UTC()

	*a = *cloneAgent(existing)
	return nil
}

func (as *agentStore) Delete(ctx context.Context, workspaceID, id string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	as.s.mu.Lock()
	defer as.s.mu.Unlock()

	existing, exists := as.s.agents[id]
	if !exists || existing.WorkspaceID != workspaceID {
		return domain.ErrNotFound
	}

	delete(as.s.agents, id)
	delete(as.s.agentsBySlug, workspaceID+":"+existing.Slug)

	// Agent daily memories die with the agent (ON DELETE CASCADE via the
	// composite FK to agents).
	prefix := workspaceID + ":" + id + ":"
	for key := range as.s.agentDailyMemories {
		if strings.HasPrefix(key, prefix) {
			delete(as.s.agentDailyMemories, key)
		}
	}

	// Agent-private MCP servers die with the agent (ON DELETE CASCADE).
	for srvID, srv := range as.s.agentMCPServers {
		if srv.AgentID == id {
			delete(as.s.agentMCPServers, srvID)
			delete(as.s.agentMCPServerNames, srv.AgentID+":"+strings.ToLower(srv.Name))
		}
	}

	return nil
}

func (as *agentStore) CountByProvider(ctx context.Context, workspaceID, providerID string) (int, error) {
	if workspaceID == "" || providerID == "" {
		return 0, nil
	}

	as.s.mu.RLock()
	defer as.s.mu.RUnlock()

	count := 0
	for _, a := range as.s.agents {
		if a.WorkspaceID == workspaceID && a.ProviderID == providerID {
			count++
		}
	}
	return count, nil
}

func (as *agentStore) SetPromptState(ctx context.Context, workspaceID, id string, status domain.PromptsStatus, promptsErr *string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	as.s.mu.Lock()
	defer as.s.mu.Unlock()

	a, exists := as.s.agents[id]
	if !exists || a.WorkspaceID != workspaceID {
		return domain.ErrNotFound
	}

	a.PromptsStatus = status
	if promptsErr != nil {
		pe := *promptsErr
		a.PromptsError = &pe
	} else {
		a.PromptsError = nil
	}
	a.UpdatedAt = time.Now().UTC()
	return nil
}

func (as *agentStore) SweepGenerating(ctx context.Context, errMsg string) (int64, error) {
	as.s.mu.Lock()
	defer as.s.mu.Unlock()

	now := time.Now().UTC()
	var count int64
	for _, a := range as.s.agents {
		if a.PromptsStatus == domain.PromptsStatusGenerating {
			a.PromptsStatus = domain.PromptsStatusFailed
			pe := errMsg
			a.PromptsError = &pe
			a.UpdatedAt = now
			count++
		}
	}
	return count, nil
}

// -------------------------------------------------------------------------
// MemoryStore implementation
// -------------------------------------------------------------------------

type memoryStore struct {
	s *fakeStore
}

// memoryDayKey normalizes a date to its calendar day (in the caller's intended
// location) so Get/Upsert/Append always address the same key for one day.
func memoryDayKey(date time.Time) string {
	y, m, d := date.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
}

func (ms *memoryStore) UserMemory(ctx context.Context, workspaceID, userID string) (*domain.Memory, error) {
	if workspaceID == "" || userID == "" {
		return nil, nil
	}

	ms.s.mu.RLock()
	defer ms.s.mu.RUnlock()

	m, exists := ms.s.userMemories[workspaceID+":"+userID]
	if !exists {
		return nil, nil
	}
	return cloneMemory(m), nil
}

func (ms *memoryStore) UpsertUserMemory(ctx context.Context, workspaceID, userID, content string) error {
	if workspaceID == "" || userID == "" {
		return domain.ErrInvalid
	}
	if err := domain.ValidateMemoryContent(content); err != nil {
		return err
	}

	ms.s.mu.Lock()
	defer ms.s.mu.Unlock()

	if _, exists := ms.s.workspaces[workspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}
	if _, exists := ms.s.users[userID]; !exists {
		return fmt.Errorf("%w: user not found", domain.ErrNotFound)
	}

	ms.s.userMemories[workspaceID+":"+userID] = &domain.Memory{Content: content, UpdatedAt: time.Now().UTC()}
	return nil
}

func (ms *memoryStore) AppendUserMemory(ctx context.Context, workspaceID, userID, content string) error {
	if workspaceID == "" || userID == "" {
		return domain.ErrInvalid
	}
	if err := domain.ValidateMemoryContent(content); err != nil {
		return err
	}

	ms.s.mu.Lock()
	defer ms.s.mu.Unlock()

	if _, exists := ms.s.workspaces[workspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}
	if _, exists := ms.s.users[userID]; !exists {
		return fmt.Errorf("%w: user not found", domain.ErrNotFound)
	}

	key := workspaceID + ":" + userID
	existing, exists := ms.s.userMemories[key]
	current := ""
	if exists {
		current = existing.Content
	}
	if err := domain.ValidateMemoryAppend(current, content); err != nil {
		return err
	}
	ms.s.userMemories[key] = &domain.Memory{Content: current + content, UpdatedAt: time.Now().UTC()}
	return nil
}

func (ms *memoryStore) WorkspaceMemory(ctx context.Context, workspaceID string) (*domain.Memory, error) {
	if workspaceID == "" {
		return nil, nil
	}

	ms.s.mu.RLock()
	defer ms.s.mu.RUnlock()

	m, exists := ms.s.workspaceMemories[workspaceID]
	if !exists {
		return nil, nil
	}
	return cloneMemory(m), nil
}

func (ms *memoryStore) UpsertWorkspaceMemory(ctx context.Context, workspaceID, content string) error {
	if workspaceID == "" {
		return domain.ErrInvalid
	}
	if err := domain.ValidateMemoryContent(content); err != nil {
		return err
	}

	ms.s.mu.Lock()
	defer ms.s.mu.Unlock()

	if _, exists := ms.s.workspaces[workspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}

	ms.s.workspaceMemories[workspaceID] = &domain.Memory{Content: content, UpdatedAt: time.Now().UTC()}
	return nil
}

func (ms *memoryStore) AppendWorkspaceMemory(ctx context.Context, workspaceID, content string) error {
	if workspaceID == "" {
		return domain.ErrInvalid
	}
	if err := domain.ValidateMemoryContent(content); err != nil {
		return err
	}

	ms.s.mu.Lock()
	defer ms.s.mu.Unlock()

	if _, exists := ms.s.workspaces[workspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}

	existing, exists := ms.s.workspaceMemories[workspaceID]
	current := ""
	if exists {
		current = existing.Content
	}
	if err := domain.ValidateMemoryAppend(current, content); err != nil {
		return err
	}
	ms.s.workspaceMemories[workspaceID] = &domain.Memory{Content: current + content, UpdatedAt: time.Now().UTC()}
	return nil
}

func (ms *memoryStore) AgentDailyMemory(ctx context.Context, workspaceID, agentID string, date time.Time) (*domain.Memory, error) {
	if workspaceID == "" || agentID == "" || date.IsZero() {
		return nil, nil
	}

	ms.s.mu.RLock()
	defer ms.s.mu.RUnlock()

	m, exists := ms.s.agentDailyMemories[workspaceID+":"+agentID+":"+memoryDayKey(date)]
	if !exists {
		return nil, nil
	}
	return cloneMemory(m), nil
}

func (ms *memoryStore) UpsertAgentDailyMemory(ctx context.Context, workspaceID, agentID string, date time.Time, content string) error {
	if workspaceID == "" || agentID == "" || date.IsZero() {
		return domain.ErrInvalid
	}
	if err := domain.ValidateMemoryContent(content); err != nil {
		return err
	}

	ms.s.mu.Lock()
	defer ms.s.mu.Unlock()

	if _, exists := ms.s.workspaces[workspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}
	a, exists := ms.s.agents[agentID]
	if !exists || a.WorkspaceID != workspaceID {
		return fmt.Errorf("%w: agent not found in workspace", domain.ErrNotFound)
	}

	key := workspaceID + ":" + agentID + ":" + memoryDayKey(date)
	ms.s.agentDailyMemories[key] = &domain.Memory{Content: content, UpdatedAt: time.Now().UTC()}
	return nil
}

func (ms *memoryStore) AppendAgentDailyMemory(ctx context.Context, workspaceID, agentID string, date time.Time, content string) error {
	if workspaceID == "" || agentID == "" || date.IsZero() {
		return domain.ErrInvalid
	}
	if err := domain.ValidateMemoryContent(content); err != nil {
		return err
	}

	ms.s.mu.Lock()
	defer ms.s.mu.Unlock()

	if _, exists := ms.s.workspaces[workspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}
	a, exists := ms.s.agents[agentID]
	if !exists || a.WorkspaceID != workspaceID {
		return fmt.Errorf("%w: agent not found in workspace", domain.ErrNotFound)
	}

	key := workspaceID + ":" + agentID + ":" + memoryDayKey(date)
	existing, exists := ms.s.agentDailyMemories[key]
	current := ""
	if exists {
		current = existing.Content
	}
	if err := domain.ValidateMemoryAppend(current, content); err != nil {
		return err
	}
	ms.s.agentDailyMemories[key] = &domain.Memory{Content: current + content, UpdatedAt: time.Now().UTC()}
	return nil
}

// --- sessionEventStore ---

type sessionEventStore struct {
	s *fakeStore
}

// AppendEvents idempotently appends events to in-memory session event log.
func (se *sessionEventStore) AppendEvents(_ context.Context, workspaceID string, events []domain.SessionEvent) error {
	if len(events) == 0 {
		return nil
	}
	se.s.mu.Lock()
	defer se.s.mu.Unlock()

	for _, e := range events {
		if e.WorkspaceID == "" {
			e.WorkspaceID = workspaceID
		}
		existing := se.s.sessionEvents[e.SessionID]
		duplicate := false
		for _, ex := range existing {
			if ex.EventID == e.EventID {
				duplicate = true
				break
			}
		}
		if !duplicate {
			se.s.sessionEvents[e.SessionID] = append(existing, e)
		}
	}
	return nil
}

// LoadEvents retrieves events from the in-memory event log with cursor and kind filtering.
func (se *sessionEventStore) LoadEvents(_ context.Context, params store.LoadSessionEventsParams) ([]domain.SessionEvent, error) {
	if params.WorkspaceID == "" || params.SessionID == "" {
		return nil, fmt.Errorf("%w: workspace_id and session_id are required", domain.ErrInvalid)
	}
	se.s.mu.RLock()
	defer se.s.mu.RUnlock()

	all := se.s.sessionEvents[params.SessionID]

	// Filter by workspace scoping.
	var scoped []domain.SessionEvent
	for _, e := range all {
		if e.WorkspaceID == params.WorkspaceID {
			scoped = append(scoped, e)
		}
	}

	// Filter by kind.
	if len(params.Kinds) > 0 {
		kindSet := make(map[string]struct{}, len(params.Kinds))
		for _, k := range params.Kinds {
			kindSet[k] = struct{}{}
		}
		var filtered []domain.SessionEvent
		for _, e := range scoped {
			if _, ok := kindSet[e.Kind]; ok {
				filtered = append(filtered, e)
			}
		}
		scoped = filtered
	}

	// Sort by seq ascending.
	sort.Slice(scoped, func(i, j int) bool { return scoped[i].Seq < scoped[j].Seq })

	// Apply cursor filter.
	if params.AfterEventID != "" {
		var cursorSeq int64
		found := false
		for _, e := range scoped {
			if e.EventID == params.AfterEventID {
				cursorSeq = e.Seq
				found = true
				break
			}
		}
		if !found {
			return nil, domain.ErrNotFound
		}
		var after []domain.SessionEvent
		for _, e := range scoped {
			if params.Reverse && e.Seq < cursorSeq {
				after = append(after, e)
			} else if !params.Reverse && e.Seq > cursorSeq {
				after = append(after, e)
			}
		}
		scoped = after
	}

	// Reverse if requested.
	if params.Reverse {
		for i, j := 0, len(scoped)-1; i < j; i, j = i+1, j-1 {
			scoped[i], scoped[j] = scoped[j], scoped[i]
		}
	}

	// Apply limit.
	limit := params.Limit
	if limit <= 0 {
		limit = 100
	}
	if len(scoped) > limit {
		scoped = scoped[:limit]
	}
	return scoped, nil
}

// --- sessionCheckpointStore ---

type sessionCheckpointStore struct {
	s *fakeStore
}

// Get retrieves a checkpoint by ID. Returns (data, true, nil) or (nil, false, nil).
func (sc *sessionCheckpointStore) Get(_ context.Context, checkpointID string) ([]byte, bool, error) {
	if checkpointID == "" {
		return nil, false, domain.ErrInvalid
	}
	sc.s.mu.RLock()
	defer sc.s.mu.RUnlock()

	data, ok := sc.s.sessionCPData[checkpointID]
	if !ok {
		return nil, false, nil
	}
	copied := make([]byte, len(data))
	copy(copied, data)
	return copied, true, nil
}

// Set upserts a checkpoint by ID.
func (sc *sessionCheckpointStore) Set(_ context.Context, checkpointID string, data []byte) error {
	if checkpointID == "" {
		return domain.ErrInvalid
	}
	sc.s.mu.Lock()
	defer sc.s.mu.Unlock()

	copied := make([]byte, len(data))
	copy(copied, data)
	sc.s.sessionCPData[checkpointID] = copied
	return nil
}

// Delete removes a checkpoint by ID. Returns ErrNotFound if absent.
func (sc *sessionCheckpointStore) Delete(_ context.Context, checkpointID string) error {
	if checkpointID == "" {
		return domain.ErrNotFound
	}
	sc.s.mu.Lock()
	defer sc.s.mu.Unlock()

	if _, ok := sc.s.sessionCPData[checkpointID]; !ok {
		return domain.ErrNotFound
	}
	delete(sc.s.sessionCPData, checkpointID)
	return nil
}

// -------------------------------------------------------------------------
// WorkspaceAPIKeyStore implementation
// -------------------------------------------------------------------------

type apiKeyStore struct {
	s *fakeStore
}

func (ks *apiKeyStore) Create(ctx context.Context, k *domain.WorkspaceAPIKey) error {
	if k == nil || k.WorkspaceID == "" || k.Name == "" || k.KeyHash == "" {
		return domain.ErrInvalid
	}

	ks.s.mu.Lock()
	defer ks.s.mu.Unlock()

	if _, exists := ks.s.workspaces[k.WorkspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}

	if _, exists := ks.s.apiKeysByHash[k.KeyHash]; exists {
		return fmt.Errorf("%w: api key with hash %q already exists", domain.ErrConflict, k.KeyHash)
	}

	if k.ID != "" {
		if _, exists := ks.s.apiKeys[k.ID]; exists {
			return fmt.Errorf("%w: api key with id %q already exists", domain.ErrConflict, k.ID)
		}
	} else {
		k.ID = uuid.NewString()
	}

	now := time.Now().UTC()
	if k.CreatedAt.IsZero() {
		k.CreatedAt = now
	}

	ks.s.apiKeys[k.ID] = cloneWorkspaceAPIKey(k)
	ks.s.apiKeysByHash[k.KeyHash] = k.ID
	return nil
}

func (ks *apiKeyStore) List(ctx context.Context, workspaceID string) ([]domain.WorkspaceAPIKey, error) {
	if workspaceID == "" {
		return []domain.WorkspaceAPIKey{}, nil
	}

	ks.s.mu.RLock()
	defer ks.s.mu.RUnlock()

	keys := make([]domain.WorkspaceAPIKey, 0)
	for _, k := range ks.s.apiKeys {
		if k.WorkspaceID == workspaceID {
			keys = append(keys, *cloneWorkspaceAPIKey(k))
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].CreatedAt.Equal(keys[j].CreatedAt) {
			return keys[i].ID < keys[j].ID
		}
		return keys[i].CreatedAt.After(keys[j].CreatedAt)
	})
	return keys, nil
}

func (ks *apiKeyStore) Revoke(ctx context.Context, workspaceID, id string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	ks.s.mu.Lock()
	defer ks.s.mu.Unlock()

	k, exists := ks.s.apiKeys[id]
	if !exists || k.WorkspaceID != workspaceID || k.RevokedAt != nil {
		// Unknown id, a key belonging to another workspace, or already revoked.
		return domain.ErrNotFound
	}

	now := time.Now().UTC()
	k.RevokedAt = &now
	return nil
}

func (ks *apiKeyStore) LookupByHash(ctx context.Context, keyHash string) (*domain.WorkspaceAPIKey, error) {
	if keyHash == "" {
		return nil, domain.ErrNotFound
	}

	ks.s.mu.RLock()
	defer ks.s.mu.RUnlock()

	id, exists := ks.s.apiKeysByHash[keyHash]
	if !exists {
		return nil, domain.ErrNotFound
	}
	k, exists := ks.s.apiKeys[id]
	if !exists {
		return nil, domain.ErrNotFound
	}
	return cloneWorkspaceAPIKey(k), nil
}

// -------------------------------------------------------------------------
// ToolSettingsStore implementation
// -------------------------------------------------------------------------

type toolSettingStore struct {
	s *fakeStore
}

func (ts *toolSettingStore) Get(ctx context.Context, workspaceID, toolKey string) (*domain.WorkspaceToolSetting, error) {
	if workspaceID == "" || toolKey == "" {
		return nil, domain.ErrNotFound
	}

	ts.s.mu.RLock()
	defer ts.s.mu.RUnlock()

	setting, exists := ts.s.toolSettings[workspaceID+":"+toolKey]
	if !exists {
		return nil, domain.ErrNotFound
	}
	return cloneWorkspaceToolSetting(setting), nil
}

func (ts *toolSettingStore) Upsert(ctx context.Context, setting *domain.WorkspaceToolSetting) error {
	if setting == nil || setting.WorkspaceID == "" || setting.ToolKey == "" {
		return domain.ErrInvalid
	}

	ts.s.mu.Lock()
	defer ts.s.mu.Unlock()

	if _, exists := ts.s.workspaces[setting.WorkspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}

	now := time.Now().UTC()
	if setting.UpdatedAt.IsZero() {
		setting.UpdatedAt = now
	}

	ts.s.toolSettings[setting.WorkspaceID+":"+setting.ToolKey] = cloneWorkspaceToolSetting(setting)
	return nil
}

func (ts *toolSettingStore) List(ctx context.Context, workspaceID string) ([]domain.WorkspaceToolSetting, error) {
	if workspaceID == "" {
		return []domain.WorkspaceToolSetting{}, nil
	}

	ts.s.mu.RLock()
	defer ts.s.mu.RUnlock()

	settings := make([]domain.WorkspaceToolSetting, 0)
	for _, s := range ts.s.toolSettings {
		if s.WorkspaceID == workspaceID {
			settings = append(settings, *cloneWorkspaceToolSetting(s))
		}
	}
	sort.Slice(settings, func(i, j int) bool {
		return settings[i].ToolKey < settings[j].ToolKey
	})
	return settings, nil
}

// -------------------------------------------------------------------------
// WorkspaceSkillStore implementation
// -------------------------------------------------------------------------

type workspaceSkillStore struct {
	s *fakeStore
}

func (wss *workspaceSkillStore) Create(ctx context.Context, sk *domain.WorkspaceSkill) error {
	if sk == nil {
		return domain.ErrInvalid
	}
	if sk.Version == "" {
		sk.Version = domain.DefaultSkillVersion
	}
	if err := sk.Validate(); err != nil {
		return err
	}

	wss.s.mu.Lock()
	defer wss.s.mu.Unlock()

	if _, exists := wss.s.workspaces[sk.WorkspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}

	nameKey := sk.WorkspaceID + ":" + sk.Name
	if _, exists := wss.s.skillsByName[nameKey]; exists {
		return fmt.Errorf("%w: workspace skill %q already exists in workspace", domain.ErrConflict, sk.Name)
	}

	if sk.ID != "" {
		if _, exists := wss.s.skills[sk.ID]; exists {
			return fmt.Errorf("%w: workspace skill with id %q already exists", domain.ErrConflict, sk.ID)
		}
	} else {
		sk.ID = uuid.NewString()
	}

	now := time.Now().UTC()
	if sk.CreatedAt.IsZero() {
		sk.CreatedAt = now
	}
	if sk.UpdatedAt.IsZero() {
		sk.UpdatedAt = now
	}

	wss.s.skills[sk.ID] = cloneWorkspaceSkill(sk)
	wss.s.skillsByName[nameKey] = sk.ID
	return nil
}

func (wss *workspaceSkillStore) Get(ctx context.Context, workspaceID, id string) (*domain.WorkspaceSkill, error) {
	if workspaceID == "" || id == "" {
		return nil, domain.ErrNotFound
	}

	wss.s.mu.RLock()
	defer wss.s.mu.RUnlock()

	sk, exists := wss.s.skills[id]
	if !exists || sk.WorkspaceID != workspaceID {
		return nil, domain.ErrNotFound
	}
	return cloneWorkspaceSkill(sk), nil
}

func (wss *workspaceSkillStore) GetByName(ctx context.Context, workspaceID, name string) (*domain.WorkspaceSkill, error) {
	if workspaceID == "" || name == "" {
		return nil, domain.ErrNotFound
	}

	wss.s.mu.RLock()
	defer wss.s.mu.RUnlock()

	id, exists := wss.s.skillsByName[workspaceID+":"+name]
	if !exists {
		return nil, domain.ErrNotFound
	}
	sk, exists := wss.s.skills[id]
	if !exists || sk.WorkspaceID != workspaceID {
		return nil, domain.ErrNotFound
	}
	return cloneWorkspaceSkill(sk), nil
}

func (wss *workspaceSkillStore) List(ctx context.Context, workspaceID string) ([]domain.WorkspaceSkill, error) {
	if workspaceID == "" {
		return []domain.WorkspaceSkill{}, nil
	}

	wss.s.mu.RLock()
	defer wss.s.mu.RUnlock()

	skills := make([]domain.WorkspaceSkill, 0)
	for _, sk := range wss.s.skills {
		if sk.WorkspaceID == workspaceID {
			skills = append(skills, *cloneWorkspaceSkill(sk))
		}
	}
	sort.Slice(skills, func(i, j int) bool {
		return skills[i].Name < skills[j].Name
	})
	return skills, nil
}

func (wss *workspaceSkillStore) ListEnabled(ctx context.Context, workspaceSlug string) ([]string, error) {
	if workspaceSlug == "" {
		return nil, domain.ErrNotFound
	}

	wss.s.mu.RLock()
	defer wss.s.mu.RUnlock()

	var workspaceID string
	for id, w := range wss.s.workspaces {
		if w.Slug == workspaceSlug {
			workspaceID = id
			break
		}
	}
	if workspaceID == "" {
		return nil, domain.ErrNotFound
	}

	names := make([]string, 0)
	for _, sk := range wss.s.skills {
		if sk.WorkspaceID == workspaceID && sk.Enabled {
			names = append(names, sk.Name)
		}
	}
	sort.Strings(names)
	return names, nil
}

func (wss *workspaceSkillStore) Update(ctx context.Context, sk *domain.WorkspaceSkill) error {
	if sk == nil || sk.ID == "" || sk.WorkspaceID == "" {
		return domain.ErrInvalid
	}
	if sk.Version == "" {
		sk.Version = domain.DefaultSkillVersion
	}
	if err := sk.Validate(); err != nil {
		return err
	}

	wss.s.mu.Lock()
	defer wss.s.mu.Unlock()

	existing, exists := wss.s.skills[sk.ID]
	if !exists || existing.WorkspaceID != sk.WorkspaceID {
		return domain.ErrNotFound
	}

	if sk.Name != existing.Name {
		nameKey := sk.WorkspaceID + ":" + sk.Name
		if otherID, exists := wss.s.skillsByName[nameKey]; exists && otherID != sk.ID {
			return fmt.Errorf("%w: workspace skill %q already exists in workspace", domain.ErrConflict, sk.Name)
		}
		delete(wss.s.skillsByName, existing.WorkspaceID+":"+existing.Name)
		wss.s.skillsByName[nameKey] = sk.ID
	}

	existing.Name = sk.Name
	existing.Description = sk.Description
	existing.Version = sk.Version
	existing.Source = sk.Source
	existing.Dependencies = domain.SkillDependencies{
		Tools:    cloneStringSlice(sk.Dependencies.Tools),
		Binaries: cloneStringSlice(sk.Dependencies.Binaries),
		Python:   cloneStringSlice(sk.Dependencies.Python),
	}
	existing.UpdatedAt = time.Now().UTC()

	*sk = *cloneWorkspaceSkill(existing)
	return nil
}

func (wss *workspaceSkillStore) SetEnabled(ctx context.Context, workspaceID, id string, enabled bool) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	wss.s.mu.Lock()
	defer wss.s.mu.Unlock()

	sk, exists := wss.s.skills[id]
	if !exists || sk.WorkspaceID != workspaceID {
		return domain.ErrNotFound
	}

	sk.Enabled = enabled
	sk.UpdatedAt = time.Now().UTC()
	return nil
}

func (wss *workspaceSkillStore) Delete(ctx context.Context, workspaceID, id string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	wss.s.mu.Lock()
	defer wss.s.mu.Unlock()

	sk, exists := wss.s.skills[id]
	if !exists || sk.WorkspaceID != workspaceID {
		return domain.ErrNotFound
	}

	delete(wss.s.skills, id)
	delete(wss.s.skillsByName, workspaceID+":"+sk.Name)
	return nil
}

// -------------------------------------------------------------------------
// WorkspaceMCPServers implementation
// -------------------------------------------------------------------------

type workspaceMCPServerStore struct {
	s *fakeStore
}

func (mss *workspaceMCPServerStore) Create(ctx context.Context, srv *domain.WorkspaceMCPServer) error {
	if srv == nil {
		return domain.ErrInvalid
	}
	if err := srv.Validate(); err != nil {
		return err
	}

	mss.s.mu.Lock()
	defer mss.s.mu.Unlock()

	if _, exists := mss.s.workspaces[srv.WorkspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}

	nameKey := srv.WorkspaceID + ":" + strings.ToLower(srv.Name)
	if _, exists := mss.s.wsMCPServerNames[nameKey]; exists {
		return fmt.Errorf("%w: workspace mcp server %q already exists in workspace", domain.ErrMCPServerNameTaken, srv.Name)
	}

	if srv.ID != "" {
		if _, exists := mss.s.wsMCPServers[srv.ID]; exists {
			return fmt.Errorf("%w: workspace mcp server with id %q already exists", domain.ErrConflict, srv.ID)
		}
	} else {
		srv.ID = uuid.NewString()
	}

	now := time.Now().UTC()
	if srv.CreatedAt.IsZero() {
		srv.CreatedAt = now
	}
	if srv.UpdatedAt.IsZero() {
		srv.UpdatedAt = now
	}

	mss.s.wsMCPServers[srv.ID] = cloneWorkspaceMCPServer(srv)
	mss.s.wsMCPServerNames[nameKey] = srv.ID
	return nil
}

func (mss *workspaceMCPServerStore) Get(ctx context.Context, workspaceID, id string) (*domain.WorkspaceMCPServer, error) {
	if workspaceID == "" || id == "" {
		return nil, domain.ErrNotFound
	}

	mss.s.mu.RLock()
	defer mss.s.mu.RUnlock()

	srv, exists := mss.s.wsMCPServers[id]
	if !exists || srv.WorkspaceID != workspaceID {
		return nil, domain.ErrNotFound
	}
	return cloneWorkspaceMCPServer(srv), nil
}

func (mss *workspaceMCPServerStore) List(ctx context.Context, workspaceID string) ([]domain.WorkspaceMCPServer, error) {
	if workspaceID == "" {
		return []domain.WorkspaceMCPServer{}, nil
	}

	mss.s.mu.RLock()
	defer mss.s.mu.RUnlock()

	servers := make([]domain.WorkspaceMCPServer, 0)
	for _, srv := range mss.s.wsMCPServers {
		if srv.WorkspaceID == workspaceID {
			servers = append(servers, *cloneWorkspaceMCPServer(srv))
		}
	}
	sort.Slice(servers, func(i, j int) bool {
		if servers[i].CreatedAt.Equal(servers[j].CreatedAt) {
			return servers[i].ID < servers[j].ID
		}
		return servers[i].CreatedAt.Before(servers[j].CreatedAt)
	})
	return servers, nil
}

func (mss *workspaceMCPServerStore) Update(ctx context.Context, srv *domain.WorkspaceMCPServer) error {
	if srv == nil || srv.ID == "" || srv.WorkspaceID == "" {
		return domain.ErrInvalid
	}
	if err := srv.Validate(); err != nil {
		return err
	}

	mss.s.mu.Lock()
	defer mss.s.mu.Unlock()

	existing, exists := mss.s.wsMCPServers[srv.ID]
	if !exists || existing.WorkspaceID != srv.WorkspaceID {
		return domain.ErrNotFound
	}

	if !strings.EqualFold(srv.Name, existing.Name) {
		nameKey := srv.WorkspaceID + ":" + strings.ToLower(srv.Name)
		if otherID, exists := mss.s.wsMCPServerNames[nameKey]; exists && otherID != srv.ID {
			return fmt.Errorf("%w: workspace mcp server %q already exists in workspace", domain.ErrMCPServerNameTaken, srv.Name)
		}
		delete(mss.s.wsMCPServerNames, srv.WorkspaceID+":"+strings.ToLower(existing.Name))
		mss.s.wsMCPServerNames[nameKey] = srv.ID
	}

	existing.Name = srv.Name
	existing.MCPConnection = cloneMCPConnection(srv.MCPConnection)
	existing.Enabled = srv.Enabled
	existing.UpdatedAt = time.Now().UTC()

	*srv = *cloneWorkspaceMCPServer(existing)
	return nil
}

func (mss *workspaceMCPServerStore) Delete(ctx context.Context, workspaceID, id string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	mss.s.mu.Lock()
	defer mss.s.mu.Unlock()

	srv, exists := mss.s.wsMCPServers[id]
	if !exists || srv.WorkspaceID != workspaceID {
		return domain.ErrNotFound
	}

	delete(mss.s.wsMCPServers, id)
	delete(mss.s.wsMCPServerNames, workspaceID+":"+strings.ToLower(srv.Name))
	return nil
}

func (mss *workspaceMCPServerStore) SetStatus(ctx context.Context, workspaceID, id, status, statusError string, toolCount int) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	mss.s.mu.Lock()
	defer mss.s.mu.Unlock()

	srv, exists := mss.s.wsMCPServers[id]
	if !exists || srv.WorkspaceID != workspaceID {
		return domain.ErrNotFound
	}

	srv.Status = status
	srv.StatusError = statusError
	srv.ToolCount = toolCount
	srv.UpdatedAt = time.Now().UTC()
	return nil
}

// -------------------------------------------------------------------------
// AgentMCPServers implementation
// -------------------------------------------------------------------------

type agentMCPServerStore struct {
	s *fakeStore
}

func (mss *agentMCPServerStore) Create(ctx context.Context, srv *domain.AgentMCPServer) error {
	if srv == nil {
		return domain.ErrInvalid
	}
	if err := srv.Validate(); err != nil {
		return err
	}

	mss.s.mu.Lock()
	defer mss.s.mu.Unlock()

	if _, exists := mss.s.workspaces[srv.WorkspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}
	a, exists := mss.s.agents[srv.AgentID]
	if !exists || a.WorkspaceID != srv.WorkspaceID {
		return fmt.Errorf("%w: agent not found in workspace", domain.ErrNotFound)
	}

	nameKey := srv.AgentID + ":" + strings.ToLower(srv.Name)
	if _, exists := mss.s.agentMCPServerNames[nameKey]; exists {
		return fmt.Errorf("%w: agent mcp server %q already exists for agent", domain.ErrMCPServerNameTaken, srv.Name)
	}

	if srv.ID != "" {
		if _, exists := mss.s.agentMCPServers[srv.ID]; exists {
			return fmt.Errorf("%w: agent mcp server with id %q already exists", domain.ErrConflict, srv.ID)
		}
	} else {
		srv.ID = uuid.NewString()
	}

	now := time.Now().UTC()
	if srv.CreatedAt.IsZero() {
		srv.CreatedAt = now
	}
	if srv.UpdatedAt.IsZero() {
		srv.UpdatedAt = now
	}

	mss.s.agentMCPServers[srv.ID] = cloneAgentMCPServer(srv)
	mss.s.agentMCPServerNames[nameKey] = srv.ID
	return nil
}

func (mss *agentMCPServerStore) Get(ctx context.Context, agentID, id string) (*domain.AgentMCPServer, error) {
	if agentID == "" || id == "" {
		return nil, domain.ErrNotFound
	}

	mss.s.mu.RLock()
	defer mss.s.mu.RUnlock()

	srv, exists := mss.s.agentMCPServers[id]
	if !exists || srv.AgentID != agentID {
		return nil, domain.ErrNotFound
	}
	return cloneAgentMCPServer(srv), nil
}

func (mss *agentMCPServerStore) List(ctx context.Context, agentID string) ([]domain.AgentMCPServer, error) {
	if agentID == "" {
		return []domain.AgentMCPServer{}, nil
	}

	mss.s.mu.RLock()
	defer mss.s.mu.RUnlock()

	servers := make([]domain.AgentMCPServer, 0)
	for _, srv := range mss.s.agentMCPServers {
		if srv.AgentID == agentID {
			servers = append(servers, *cloneAgentMCPServer(srv))
		}
	}
	sort.Slice(servers, func(i, j int) bool {
		if servers[i].CreatedAt.Equal(servers[j].CreatedAt) {
			return servers[i].ID < servers[j].ID
		}
		return servers[i].CreatedAt.Before(servers[j].CreatedAt)
	})
	return servers, nil
}

func (mss *agentMCPServerStore) Update(ctx context.Context, srv *domain.AgentMCPServer) error {
	if srv == nil || srv.ID == "" || srv.WorkspaceID == "" || srv.AgentID == "" {
		return domain.ErrInvalid
	}
	if err := srv.Validate(); err != nil {
		return err
	}

	mss.s.mu.Lock()
	defer mss.s.mu.Unlock()

	existing, exists := mss.s.agentMCPServers[srv.ID]
	if !exists || existing.AgentID != srv.AgentID || existing.WorkspaceID != srv.WorkspaceID {
		return domain.ErrNotFound
	}

	if !strings.EqualFold(srv.Name, existing.Name) {
		nameKey := srv.AgentID + ":" + strings.ToLower(srv.Name)
		if otherID, exists := mss.s.agentMCPServerNames[nameKey]; exists && otherID != srv.ID {
			return fmt.Errorf("%w: agent mcp server %q already exists for agent", domain.ErrMCPServerNameTaken, srv.Name)
		}
		delete(mss.s.agentMCPServerNames, srv.AgentID+":"+strings.ToLower(existing.Name))
		mss.s.agentMCPServerNames[nameKey] = srv.ID
	}

	existing.Name = srv.Name
	existing.MCPConnection = cloneMCPConnection(srv.MCPConnection)
	existing.Enabled = srv.Enabled
	existing.UpdatedAt = time.Now().UTC()

	*srv = *cloneAgentMCPServer(existing)
	return nil
}

func (mss *agentMCPServerStore) Delete(ctx context.Context, agentID, id string) error {
	if agentID == "" || id == "" {
		return domain.ErrNotFound
	}

	mss.s.mu.Lock()
	defer mss.s.mu.Unlock()

	srv, exists := mss.s.agentMCPServers[id]
	if !exists || srv.AgentID != agentID {
		return domain.ErrNotFound
	}

	delete(mss.s.agentMCPServers, id)
	delete(mss.s.agentMCPServerNames, agentID+":"+strings.ToLower(srv.Name))
	return nil
}

func (mss *agentMCPServerStore) SetStatus(ctx context.Context, agentID, id, status, statusError string, toolCount int) error {
	if agentID == "" || id == "" {
		return domain.ErrNotFound
	}

	mss.s.mu.Lock()
	defer mss.s.mu.Unlock()

	srv, exists := mss.s.agentMCPServers[id]
	if !exists || srv.AgentID != agentID {
		return domain.ErrNotFound
	}

	srv.Status = status
	srv.StatusError = statusError
	srv.ToolCount = toolCount
	srv.UpdatedAt = time.Now().UTC()
	return nil
}
