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
	"unicode"

	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// fakeStore is an in-memory implementation of store.Store.
type fakeStore struct {
	mu sync.RWMutex

	users                          map[string]*domain.User                   // key: ID
	usersByEmail                   map[string]string                         // key: normalized email -> ID
	workspaces                     map[string]*domain.Workspace              // key: ID
	workspacesBySlug               map[string]string                         // key: slug -> ID
	roles                          map[string]*domain.Role                   // key: ID
	rolesByName                    map[string]string                         // key: workspaceID + ":" + name -> ID
	members                        map[string]*domain.Member                 // key: workspaceID + ":" + userID -> Member
	providers                      map[string]*domain.ProviderConfig         // key: ID
	agents                         map[string]*domain.Agent                  // key: ID
	agentsBySlug                   map[string]string                         // key: workspaceID + ":" + slug -> ID
	userMemories                   map[string]*domain.Memory                 // key: workspaceID + ":" + userID -> Memory
	workspaceMemories              map[string]*domain.Memory                 // key: workspaceID -> Memory
	memoryEvents                   map[string]*domain.MemoryEvent            // key: ID
	memoryNotes                    map[string]*domain.MemoryNote             // key: ID
	sessionEvents                  map[string][]domain.SessionEvent          // key: sessionID -> ordered events
	sessionCPData                  map[string][]byte                         // key: checkpointID -> data
	apiKeys                        map[string]*domain.WorkspaceAPIKey        // key: ID
	apiKeysByHash                  map[string]string                         // key: SHA-256 hex hash -> ID
	toolSettings                   map[string]*domain.WorkspaceToolSetting   // key: workspaceID + ":" + toolKey -> Setting
	skills                         map[string]*domain.WorkspaceSkill         // key: ID
	skillsByName                   map[string]string                         // key: workspaceID + ":" + name -> ID
	wsMCPServers                   map[string]*domain.WorkspaceMCPServer     // key: ID
	wsMCPServerNames               map[string]string                         // key: workspaceID + ":" + lower(name) -> ID
	agentMCPServers                map[string]*domain.AgentMCPServer         // key: ID
	agentMCPServerNames            map[string]string                         // key: agentID + ":" + lower(name) -> ID
	mcptokens                      map[string]*domain.MCPToken               // key: workspaceID + ":" + agentID + ":" + serverID (agentID empty = workspace scope)
	connections                    map[string]*domain.Connection             // key: ID
	connectionsByService           map[string]string                         // key: workspaceID + ":" + service -> ID
	connectionWebhooks             map[string]*domain.ConnectionWebhook      // key: connection ID
	connectionDeliveries           map[string]map[string]time.Time           // key: connection ID -> delivery ID -> recorded at
	oauthApps                      map[string]*domain.InstanceOAuthApp       // key: provider
	hooks                          *hookData                                 // hook state: all three levels + execution audit log
	channels                       *channelData                              // channel state: rooms, membership roster, shared feed
	agentSessions                  map[string]*domain.AgentSession           // key: workspaceID + ":" + agentID + ":" + sessionID
	attachments                    map[string]*domain.Attachment             // key: ID
	attachmentsByStorageKey        map[string]string                         // key: capability storage key -> ID
	workspaceStorage               map[string]*domain.WorkspaceStorageConfig // key: workspaceID -> config
	schedulers                     map[string]*domain.Scheduler              // key: ID
	schedulerRuns                  map[string]*domain.SchedulerRun           // key: ID
	heartbeats                     map[string]*domain.Heartbeat              // key: ID
	heartbeatsByAgent              map[string]string                         // key: workspaceID + ":" + agentID -> heartbeat ID
	heartbeatRuns                  map[string]*domain.HeartbeatRun           // key: ID
	gateways                       map[string]*domain.GatewayConfig          // key: ID
	gatewayIdentities              map[string]string                         // key: workspaceID + ":" + platform + ":" + identity -> ID
	gatewayChatBindings            map[string]*domain.ChatBinding            // key: ID
	gatewayBindingsByChat          map[string]string                         // key: platform + ":" + platformChatID -> binding ID
	gatewayUserLinks               map[string]*domain.UserLink               // key: platform + ":" + platformUserID + ":" + workspaceID
	gatewayPairingTokens           map[string]*domain.PairingToken           // key: workspaceID + ":" + token
	gatewayActiveSessions          map[string]int64                          // key: platform + ":" + platformChatID + ":" + agentID -> suffix
	gatewayOutbox                  map[string]*domain.OutboxEntry            // key: ID
	memoryNoteEvidence             map[string]*domain.MemoryNoteEvidence     // key: noteID + ":" + sourceEventID
	memoryReports                  map[string]*domain.MemoryReport           // key: workspaceID
	memoryEmbeddings               map[string]*domain.MemoryEmbedding        // key: ID
	memoryEntities                 map[string]*domain.MemoryEntity           // key: ID
	memoryEntitiesByNorm           map[string]string                         // key: workspaceID + ":" + normalizedLabel -> ID
	memoryEntityEdges              map[string]*domain.MemoryEntityEdge       // key: ID
	agentTodos                     map[string]*fakeTodoRow                   // key: sessionID + ":" + itemKey
	referenceDocuments             map[string]*domain.ReferenceDocument      // key: ID
	referenceDocumentsByStorageKey map[string]string                         // key: capability storage key -> ID
	referenceDocumentSections      map[string][]domain.DocumentSection       // key: document ID -> ordered sections
}

// New creates a new in-memory fake store.
func New() store.Store {
	return newStore()
}

func newStore() *fakeStore {
	return &fakeStore{
		users:                          make(map[string]*domain.User),
		usersByEmail:                   make(map[string]string),
		workspaces:                     make(map[string]*domain.Workspace),
		workspacesBySlug:               make(map[string]string),
		roles:                          make(map[string]*domain.Role),
		rolesByName:                    make(map[string]string),
		members:                        make(map[string]*domain.Member),
		providers:                      make(map[string]*domain.ProviderConfig),
		agents:                         make(map[string]*domain.Agent),
		agentsBySlug:                   make(map[string]string),
		userMemories:                   make(map[string]*domain.Memory),
		workspaceMemories:              make(map[string]*domain.Memory),
		memoryEvents:                   make(map[string]*domain.MemoryEvent),
		memoryNotes:                    make(map[string]*domain.MemoryNote),
		sessionEvents:                  make(map[string][]domain.SessionEvent),
		sessionCPData:                  make(map[string][]byte),
		apiKeys:                        make(map[string]*domain.WorkspaceAPIKey),
		apiKeysByHash:                  make(map[string]string),
		toolSettings:                   make(map[string]*domain.WorkspaceToolSetting),
		skills:                         make(map[string]*domain.WorkspaceSkill),
		skillsByName:                   make(map[string]string),
		wsMCPServers:                   make(map[string]*domain.WorkspaceMCPServer),
		wsMCPServerNames:               make(map[string]string),
		agentMCPServers:                make(map[string]*domain.AgentMCPServer),
		agentMCPServerNames:            make(map[string]string),
		mcptokens:                      make(map[string]*domain.MCPToken),
		connections:                    make(map[string]*domain.Connection),
		connectionsByService:           make(map[string]string),
		connectionWebhooks:             make(map[string]*domain.ConnectionWebhook),
		connectionDeliveries:           make(map[string]map[string]time.Time),
		oauthApps:                      make(map[string]*domain.InstanceOAuthApp),
		hooks:                          newHookData(),
		channels:                       newChannelData(),
		agentSessions:                  make(map[string]*domain.AgentSession),
		attachments:                    make(map[string]*domain.Attachment),
		attachmentsByStorageKey:        make(map[string]string),
		workspaceStorage:               make(map[string]*domain.WorkspaceStorageConfig),
		schedulers:                     make(map[string]*domain.Scheduler),
		schedulerRuns:                  make(map[string]*domain.SchedulerRun),
		heartbeats:                     make(map[string]*domain.Heartbeat),
		heartbeatsByAgent:              make(map[string]string),
		heartbeatRuns:                  make(map[string]*domain.HeartbeatRun),
		gateways:                       make(map[string]*domain.GatewayConfig),
		gatewayIdentities:              make(map[string]string),
		gatewayChatBindings:            make(map[string]*domain.ChatBinding),
		gatewayBindingsByChat:          make(map[string]string),
		gatewayUserLinks:               make(map[string]*domain.UserLink),
		gatewayPairingTokens:           make(map[string]*domain.PairingToken),
		gatewayActiveSessions:          make(map[string]int64),
		gatewayOutbox:                  make(map[string]*domain.OutboxEntry),
		memoryNoteEvidence:             make(map[string]*domain.MemoryNoteEvidence),
		memoryReports:                  make(map[string]*domain.MemoryReport),
		memoryEmbeddings:               make(map[string]*domain.MemoryEmbedding),
		memoryEntities:                 make(map[string]*domain.MemoryEntity),
		memoryEntitiesByNorm:           make(map[string]string),
		memoryEntityEdges:              make(map[string]*domain.MemoryEntityEdge),
		agentTodos:                     make(map[string]*fakeTodoRow),
		referenceDocuments:             make(map[string]*domain.ReferenceDocument),
		referenceDocumentsByStorageKey: make(map[string]string),
		referenceDocumentSections:      make(map[string][]domain.DocumentSection),
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

// MemoryEvents returns the MemoryEventStore sub-port.
func (s *fakeStore) MemoryEvents() store.MemoryEventStore {
	return &memoryEventStore{s: s}
}

// MemoryNotes returns the MemoryNoteStore sub-port.
func (s *fakeStore) MemoryNotes() store.MemoryNoteStore {
	return &memoryNoteStore{s: s}
}

// MemoryReports returns the MemoryReportStore sub-port.
func (s *fakeStore) MemoryReports() store.MemoryReportStore {
	return &memoryReportStore{s: s}
}

// MemoryEmbeddings returns the MemoryEmbeddingStore sub-port.
func (s *fakeStore) MemoryEmbeddings() store.MemoryEmbeddingStore {
	return &memoryEmbeddingStore{s: s}
}

// MemoryEntities returns the MemoryEntityStore sub-port.
func (s *fakeStore) MemoryEntities() store.MemoryEntityStore {
	return &memoryEntityStore{s: s}
}

// Todos returns the TodoStore sub-port.
func (s *fakeStore) Todos() store.TodoStore {
	return &todoStore{s: s}
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

// MCPTokens returns the MCPTokens sub-port.
func (s *fakeStore) MCPTokens() store.MCPTokens {
	return &mcpTokenStore{s: s}
}

// Connections returns the Connections sub-port.
// Connections returns the Connections sub-port.
func (s *fakeStore) Connections() store.Connections {
	return &connectionStore{s: s}
}

// ConnectionWebhooks returns the ConnectionWebhookStore sub-port.
func (s *fakeStore) ConnectionWebhooks() store.ConnectionWebhookStore {
	return &connectionWebhookStore{s: s}
}

// OAuthApps returns the OAuthApps sub-port.
func (s *fakeStore) OAuthApps() store.OAuthApps {
	return &oauthAppStore{s: s}
}

// AgentSessions returns the AgentSessionStore sub-port.
func (s *fakeStore) AgentSessions() store.AgentSessionStore {
	return &agentSessionStore{s: s}
}

// Attachments returns the AttachmentStore sub-port.
func (s *fakeStore) Attachments() store.AttachmentStore {
	return &attachmentStore{s: s}
}

// WorkspaceStorage returns the WorkspaceStorageStore sub-port.
func (s *fakeStore) WorkspaceStorage() store.WorkspaceStorageStore {
	return &workspaceStorageStore{s: s}
}

// Schedulers returns the SchedulerStore sub-port.
func (s *fakeStore) Schedulers() store.SchedulerStore {
	return &schedulerStore{s: s}
}

// Heartbeats returns the HeartbeatStore sub-port.
func (s *fakeStore) Heartbeats() store.HeartbeatStore {
	return &heartbeatStore{s: s}
}

// Gateways returns the GatewayStore sub-port.
func (s *fakeStore) Gateways() store.GatewayStore {
	return &gatewayStore{s: s}
}

// GatewayBindings returns the GatewayBindings sub-port.
func (s *fakeStore) GatewayBindings() store.GatewayBindings {
	return &gatewayBindingStore{s: s}
}

// GatewayLinks returns the GatewayLinks sub-port.
func (s *fakeStore) GatewayLinks() store.GatewayLinks {
	return &gatewayLinkStore{s: s}
}

// GatewayOutbox returns the GatewayOutbox sub-port.
func (s *fakeStore) GatewayOutbox() store.GatewayOutbox {
	return &gatewayOutboxStore{s: s}
}

// ReferenceDocuments returns the ReferenceDocumentStore sub-port.
func (s *fakeStore) ReferenceDocuments() store.ReferenceDocumentStore {
	return &referenceDocumentStore{s: s}
}

// DocumentSections returns the DocumentSectionStore sub-port.
func (s *fakeStore) DocumentSections() store.DocumentSectionStore {
	return &documentSectionStore{s: s}
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
	for id, e := range s.memoryEvents {
		cp.memoryEvents[id] = cloneMemoryEvent(e)
	}
	for id, n := range s.memoryNotes {
		cp.memoryNotes[id] = cloneMemoryNote(n)
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
	for key, t := range s.mcptokens {
		cp.mcptokens[key] = cloneMCPToken(t)
	}
	for id, c := range s.connections {
		cp.connections[id] = cloneConnection(c)
	}
	for key, id := range s.connectionsByService {
		cp.connectionsByService[key] = id
	}
	for id, w := range s.connectionWebhooks {
		cp.connectionWebhooks[id] = cloneConnectionWebhook(w)
	}
	for cid, deliveries := range s.connectionDeliveries {
		copied := make(map[string]time.Time, len(deliveries))
		for id, at := range deliveries {
			copied[id] = at
		}
		cp.connectionDeliveries[cid] = copied
	}
	for provider, app := range s.oauthApps {
		cp.oauthApps[provider] = cloneInstanceOAuthApp(app)
	}
	cp.hooks = s.hooks.clone()
	cp.channels = s.channels.clone()
	for key, session := range s.agentSessions {
		cp.agentSessions[key] = cloneAgentSession(session)
	}
	for id, a := range s.attachments {
		cp.attachments[id] = cloneAttachment(a)
	}
	for key, id := range s.attachmentsByStorageKey {
		cp.attachmentsByStorageKey[key] = id
	}
	for wsID, cfg := range s.workspaceStorage {
		cp.workspaceStorage[wsID] = cloneWorkspaceStorageConfig(cfg)
	}
	for id, sched := range s.schedulers {
		cp.schedulers[id] = cloneScheduler(sched)
	}
	for id, run := range s.schedulerRuns {
		cp.schedulerRuns[id] = cloneSchedulerRun(run)
	}
	for id, hb := range s.heartbeats {
		cp.heartbeats[id] = cloneHeartbeat(hb)
	}
	for key, id := range s.heartbeatsByAgent {
		cp.heartbeatsByAgent[key] = id
	}
	for id, run := range s.heartbeatRuns {
		cp.heartbeatRuns[id] = cloneHeartbeatRun(run)
	}
	for id, g := range s.gateways {
		cp.gateways[id] = cloneGatewayConfig(g)
	}
	for key, id := range s.gatewayIdentities {
		cp.gatewayIdentities[key] = id
	}
	for id, b := range s.gatewayChatBindings {
		cp.gatewayChatBindings[id] = cloneChatBinding(b)
	}
	for chat, id := range s.gatewayBindingsByChat {
		cp.gatewayBindingsByChat[chat] = id
	}
	for key, l := range s.gatewayUserLinks {
		cp.gatewayUserLinks[key] = cloneUserLink(l)
	}
	for key, tok := range s.gatewayPairingTokens {
		cp.gatewayPairingTokens[key] = clonePairingToken(tok)
	}
	for key, suffix := range s.gatewayActiveSessions {
		cp.gatewayActiveSessions[key] = suffix
	}
	for id, e := range s.gatewayOutbox {
		cp.gatewayOutbox[id] = cloneOutboxEntry(e)
	}
	for key, ev := range s.memoryNoteEvidence {
		cp.memoryNoteEvidence[key] = cloneMemoryNoteEvidence(ev)
	}
	for key, r := range s.memoryReports {
		cp.memoryReports[key] = cloneMemoryReport(r)
	}
	for id, e := range s.memoryEmbeddings {
		cp.memoryEmbeddings[id] = cloneMemoryEmbedding(e)
	}
	for id, e := range s.memoryEntities {
		cp.memoryEntities[id] = cloneMemoryEntity(e)
	}
	for key, id := range s.memoryEntitiesByNorm {
		cp.memoryEntitiesByNorm[key] = id
	}
	for id, e := range s.memoryEntityEdges {
		cp.memoryEntityEdges[id] = cloneMemoryEntityEdge(e)
	}
	for key, row := range s.agentTodos {
		copied := *row
		cp.agentTodos[key] = &copied
	}
	for id, doc := range s.referenceDocuments {
		cp.referenceDocuments[id] = cloneReferenceDocument(doc)
	}
	for key, id := range s.referenceDocumentsByStorageKey {
		cp.referenceDocumentsByStorageKey[key] = id
	}
	for docID, sections := range s.referenceDocumentSections {
		cp.referenceDocumentSections[docID] = cloneDocumentSections(sections)
	}
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
	s.memoryEvents = other.memoryEvents
	s.memoryNotes = other.memoryNotes
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
	s.mcptokens = other.mcptokens
	s.connections = other.connections
	s.connectionsByService = other.connectionsByService
	s.connectionWebhooks = other.connectionWebhooks
	s.connectionDeliveries = other.connectionDeliveries
	s.oauthApps = other.oauthApps
	s.hooks = other.hooks
	s.channels = other.channels
	s.agentSessions = other.agentSessions
	s.attachments = other.attachments
	s.attachmentsByStorageKey = other.attachmentsByStorageKey
	s.workspaceStorage = other.workspaceStorage
	s.schedulers = other.schedulers
	s.schedulerRuns = other.schedulerRuns
	s.heartbeats = other.heartbeats
	s.heartbeatsByAgent = other.heartbeatsByAgent
	s.heartbeatRuns = other.heartbeatRuns
	s.gateways = other.gateways
	s.gatewayIdentities = other.gatewayIdentities
	s.gatewayChatBindings = other.gatewayChatBindings
	s.gatewayBindingsByChat = other.gatewayBindingsByChat
	s.gatewayUserLinks = other.gatewayUserLinks
	s.gatewayPairingTokens = other.gatewayPairingTokens
	s.gatewayActiveSessions = other.gatewayActiveSessions
	s.gatewayOutbox = other.gatewayOutbox
	s.memoryNoteEvidence = other.memoryNoteEvidence
	s.memoryReports = other.memoryReports
	s.memoryEmbeddings = other.memoryEmbeddings
	s.memoryEntities = other.memoryEntities
	s.memoryEntitiesByNorm = other.memoryEntitiesByNorm
	s.memoryEntityEdges = other.memoryEntityEdges
	s.agentTodos = other.agentTodos
	s.referenceDocuments = other.referenceDocuments
	s.referenceDocumentsByStorageKey = other.referenceDocumentsByStorageKey
	s.referenceDocumentSections = other.referenceDocumentSections
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
	if ws.DefaultModel != nil {
		pair := *ws.DefaultModel
		cp.DefaultModel = &pair
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
	if a.DisabledTools != nil {
		cp.DisabledTools = make([]string, len(a.DisabledTools))
		copy(cp.DisabledTools, a.DisabledTools)
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

func cloneConnection(c *domain.Connection) *domain.Connection {
	if c == nil {
		return nil
	}
	cp := *c
	// GrantedScopes always leaves the store as an array, never null (the
	// served-JSON normalization the recipe registry applies too).
	if cp.GrantedScopes == nil {
		cp.GrantedScopes = []string{}
	}
	return &cp
}

func cloneInstanceOAuthApp(a *domain.InstanceOAuthApp) *domain.InstanceOAuthApp {
	if a == nil {
		return nil
	}
	cp := *a
	return &cp
}

func cloneAgentSession(session *domain.AgentSession) *domain.AgentSession {
	if session == nil {
		return nil
	}
	cp := *session
	if session.DeletedAt != nil {
		t := *session.DeletedAt
		cp.DeletedAt = &t
	}
	return &cp
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
	if w.DefaultModel == nil {
		existing.DefaultModel = nil
	} else {
		pair := *w.DefaultModel
		existing.DefaultModel = &pair
	}
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
	existing.CatalogProvider = p.CatalogProvider
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
	if a == nil || a.WorkspaceID == "" || a.Name == "" || a.Slug == "" {
		return fmt.Errorf("%w: missing required agent fields", domain.ErrInvalid)
	}
	// Pair rule mirrors the postgres adapter: fully pinned or fully empty.
	if err := domain.ValidateDefaultModelPair(a.ProviderID, a.Model); err != nil {
		return err
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
	if a.DisabledTools == nil {
		a.DisabledTools = []string{}
	}
	if a.EnabledMCPS == nil {
		a.EnabledMCPS = []string{}
	}

	as.s.mu.Lock()
	defer as.s.mu.Unlock()

	if _, exists := as.s.workspaces[a.WorkspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}

	// An inherit agent (empty pair) references no provider; a pinned one must
	// name a provider of its own workspace.
	if a.ProviderID != "" {
		p, exists := as.s.providers[a.ProviderID]
		if !exists || p.WorkspaceID != a.WorkspaceID {
			return fmt.Errorf("%w: provider not found in workspace", domain.ErrNotFound)
		}
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
	if a == nil || a.ID == "" || a.WorkspaceID == "" || a.Name == "" || a.Slug == "" {
		return fmt.Errorf("%w: missing required agent fields", domain.ErrInvalid)
	}
	// Pair rule mirrors the postgres adapter: fully pinned or fully empty.
	if err := domain.ValidateDefaultModelPair(a.ProviderID, a.Model); err != nil {
		return err
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

	if a.ProviderID != "" {
		p, exists := as.s.providers[a.ProviderID]
		if !exists || p.WorkspaceID != a.WorkspaceID {
			return fmt.Errorf("%w: provider not found in workspace", domain.ErrNotFound)
		}
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
	if a.DisabledTools != nil {
		existing.DisabledTools = make([]string, len(a.DisabledTools))
		copy(existing.DisabledTools, a.DisabledTools)
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

	// Agent-private MCP servers die with the agent (ON DELETE CASCADE).
	for srvID, srv := range as.s.agentMCPServers {
		if srv.AgentID == id {
			delete(as.s.agentMCPServers, srvID)
			delete(as.s.agentMCPServerNames, srv.AgentID+":"+strings.ToLower(srv.Name))
		}
	}

	// The agent's OAuth token rows die with its private servers
	// (ON DELETE CASCADE, add-mcp-oauth-client design.md D4).
	purgeAgentMCPTokensLocked(as.s, workspaceID, id)

	// Schedulers and their run records die with the agent
	// (ON DELETE CASCADE).
	for schedID, sched := range as.s.schedulers {
		if sched.AgentID == id {
			delete(as.s.schedulers, schedID)
			for runID, run := range as.s.schedulerRuns {
				if run.SchedulerID == schedID {
					delete(as.s.schedulerRuns, runID)
				}
			}
		}
	}

	// The agent's heartbeat and its tick records die with the agent
	// (ON DELETE CASCADE, add-agent-heartbeat D1).
	for hbID, hb := range as.s.heartbeats {
		if hb.AgentID == id {
			delete(as.s.heartbeats, hbID)
			delete(as.s.heartbeatsByAgent, workspaceID+":"+id)
			for runID, run := range as.s.heartbeatRuns {
				if run.HeartbeatID == hbID {
					delete(as.s.heartbeatRuns, runID)
				}
			}
		}
	}

	// The agent's todo rows die with the agent (ON DELETE CASCADE,
	// adopt-assistant-ui-elements D5).
	for _, key := range todoRowsBelongingToAgentLocked(as.s.agentTodos, workspaceID, id) {
		delete(as.s.agentTodos, key)
	}

	// Reference-document agent joins die with the agent (ON DELETE CASCADE
	// on reference_document_agents, add-reference-documents tenancy
	// requirement) — the join row is removed, never the document.
	for _, doc := range as.s.referenceDocuments {
		if doc.WorkspaceID != workspaceID || len(doc.AgentIDs) == 0 {
			continue
		}
		filtered := make([]string, 0, len(doc.AgentIDs))
		for _, agentID := range doc.AgentIDs {
			if agentID != id {
				filtered = append(filtered, agentID)
			}
		}
		doc.AgentIDs = filtered
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

// CountInheriting counts the workspace's inherit agents (empty provider/model
// pair), mirroring the postgres adapter's provider_id IS NULL scan.
func (as *agentStore) CountInheriting(ctx context.Context, workspaceID string) (int, error) {
	if workspaceID == "" {
		return 0, nil
	}

	as.s.mu.RLock()
	defer as.s.mu.RUnlock()

	count := 0
	for _, a := range as.s.agents {
		if a.WorkspaceID == workspaceID && a.ProviderID == "" && a.Model == "" {
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

	// Sort by (seq, occurred_at, event_id) ascending — the same deterministic
	// total order as the postgres store (fix-session-event-ordering D2); the
	// reverse slice below is then the exact mirror of this order.
	sort.Slice(scoped, func(i, j int) bool {
		if scoped[i].Seq != scoped[j].Seq {
			return scoped[i].Seq < scoped[j].Seq
		}
		if !scoped[i].OccurredAt.Equal(scoped[j].OccurredAt) {
			return scoped[i].OccurredAt.Before(scoped[j].OccurredAt)
		}
		return scoped[i].EventID < scoped[j].EventID
	})

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

	// Apply limit. Limit <= 0 means "no limit": every matching event is
	// returned (same semantics as the postgres store).
	if params.Limit > 0 && len(scoped) > params.Limit {
		scoped = scoped[:params.Limit]
	}
	return scoped, nil
}

// EventsByIDs returns the workspace-scoped events with the given ids,
// ordered deterministically by (session_id, seq). Absent and
// foreign-workspace ids are simply absent; duplicate ids collapse.
func (se *sessionEventStore) EventsByIDs(_ context.Context, workspaceID string, eventIDs []string) ([]domain.SessionEvent, error) {
	if workspaceID == "" || len(eventIDs) == 0 {
		return []domain.SessionEvent{}, nil
	}
	wanted := make(map[string]struct{}, len(eventIDs))
	for _, id := range eventIDs {
		if id != "" {
			wanted[id] = struct{}{}
		}
	}

	se.s.mu.RLock()
	defer se.s.mu.RUnlock()

	var found []domain.SessionEvent
	for _, events := range se.s.sessionEvents {
		for _, e := range events {
			if e.WorkspaceID != workspaceID {
				continue
			}
			if _, ok := wanted[e.EventID]; !ok {
				continue
			}
			found = append(found, e)
		}
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].SessionID != found[j].SessionID {
			return found[i].SessionID < found[j].SessionID
		}
		if found[i].Seq != found[j].Seq {
			return found[i].Seq < found[j].Seq
		}
		return found[i].EventID < found[j].EventID
	})
	if found == nil {
		return []domain.SessionEvent{}, nil
	}
	return found, nil
}

// NextEventSeq returns the next append position for the session's event log:
// MAX(seq)+1 over its rows, or 0 when the log is empty.
func (se *sessionEventStore) NextEventSeq(_ context.Context, workspaceID, sessionID string) (int64, error) {
	se.s.mu.RLock()
	defer se.s.mu.RUnlock()

	var next int64
	for _, e := range se.s.sessionEvents[sessionID] {
		if e.WorkspaceID != workspaceID {
			continue
		}
		if e.Seq >= next {
			next = e.Seq + 1
		}
	}
	return next, nil
}

// EventExists reports whether an event with the given ID is already stored for
// the session. Absence is (false, nil), not an error.
func (se *sessionEventStore) EventExists(_ context.Context, workspaceID, sessionID, eventID string) (bool, error) {
	se.s.mu.RLock()
	defer se.s.mu.RUnlock()

	for _, e := range se.s.sessionEvents[sessionID] {
		if e.WorkspaceID == workspaceID && e.EventID == eventID {
			return true, nil
		}
	}
	return false, nil
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
	// The server's OAuth token row dies with it (ON DELETE CASCADE,
	// add-mcp-oauth-client design.md D4).
	purgeMCPTokenLocked(mss.s, workspaceID, "", id)
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
	// The server's OAuth token row dies with it (ON DELETE CASCADE,
	// add-mcp-oauth-client design.md D4).
	purgeMCPTokenLocked(mss.s, srv.WorkspaceID, agentID, id)
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

// -------------------------------------------------------------------------
// AgentSessionStore implementation (agent-session-index D1/D2). Mirrors the
// postgres adapter: unknown workspace / agent / user references fail with
// domain.ErrNotFound (the FK behavior), the birth-only title rule rides in
// the upsert, and ownership is fixed at birth — the conflict path never
// rewrites user_id.
// -------------------------------------------------------------------------

type agentSessionStore struct {
	s *fakeStore
}

// agentSessionKey is the fake's (workspace, agent, session) uniqueness key,
// mirroring the schema's UNIQUE triple.
func agentSessionKey(workspaceID, agentID, sessionID string) string {
	return workspaceID + ":" + agentID + ":" + sessionID
}

func (a *agentSessionStore) UpsertAgentSession(ctx context.Context, workspaceID, agentID, userID string, up domain.AgentSessionUpsert) error {
	if up.SessionID == "" {
		return domain.ErrInvalid
	}
	// Binding-prefix validation (integrate-telegram-gateway design D3,
	// channel-session-leak fix): only registered prefixes — including the
	// gateways' tg_dm_/tg_group_/wa_dm_ — may index rows; unknown
	// "<word>_"-shaped ids are refused.
	if err := domain.ValidateAgentSessionID(up.SessionID); err != nil {
		return err
	}

	a.s.mu.Lock()
	defer a.s.mu.Unlock()

	// FK parity: unknown references fail before any row is touched.
	if _, exists := a.s.workspaces[workspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}
	agent, exists := a.s.agents[agentID]
	if !exists || agent.WorkspaceID != workspaceID {
		return fmt.Errorf("%w: agent not found in workspace", domain.ErrNotFound)
	}
	if _, exists := a.s.users[userID]; !exists {
		return fmt.Errorf("%w: user not found", domain.ErrNotFound)
	}

	key := agentSessionKey(workspaceID, agentID, up.SessionID)
	now := time.Now().UTC()
	if existing, exists := a.s.agentSessions[key]; exists {
		// Conflict path (design D2): bump activity, revive soft-deleted rows,
		// and apply the birth-only title rule. user_id stays as born.
		existing.LastActiveAt = now
		existing.DeletedAt = nil
		if existing.Title == "" {
			existing.Title = up.Title
		}
		return nil
	}

	a.s.agentSessions[key] = &domain.AgentSession{
		ID:           uuid.NewString(),
		WorkspaceID:  workspaceID,
		AgentID:      agentID,
		UserID:       userID,
		SessionID:    up.SessionID,
		Title:        up.Title,
		CreatedAt:    now,
		LastActiveAt: now,
	}
	return nil
}

func (a *agentSessionStore) ListAgentSessions(ctx context.Context, workspaceID, agentID, userID string) ([]domain.AgentSession, error) {
	if workspaceID == "" || agentID == "" || userID == "" {
		return []domain.AgentSession{}, nil
	}

	a.s.mu.RLock()
	defer a.s.mu.RUnlock()

	sessions := make([]domain.AgentSession, 0)
	for _, session := range a.s.agentSessions {
		// Non-deleted only, scoped to the requesting user (privacy boundary),
		// and private-index only (design D3): channel, scheduler, and gateway
		// group sessions are shared/automation artifacts and never surface in
		// a per-user listing; gateway DM sessions do, under the paired member.
		if session.WorkspaceID == workspaceID && session.AgentID == agentID && session.UserID == userID && session.DeletedAt == nil && domain.IsPrivateIndexSessionID(session.SessionID) {
			sessions = append(sessions, *cloneAgentSession(session))
		}
	}
	// Most recently active first, id as the determinism tiebreak.
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].LastActiveAt.Equal(sessions[j].LastActiveAt) {
			return sessions[i].ID > sessions[j].ID
		}
		return sessions[i].LastActiveAt.After(sessions[j].LastActiveAt)
	})
	return sessions, nil
}

func (a *agentSessionStore) SoftDeleteAgentSession(ctx context.Context, workspaceID, agentID, userID, sessionID string) error {
	if workspaceID == "" || agentID == "" || userID == "" || sessionID == "" {
		return domain.ErrNotFound
	}

	a.s.mu.Lock()
	defer a.s.mu.Unlock()

	session, exists := a.s.agentSessions[agentSessionKey(workspaceID, agentID, sessionID)]
	if !exists || session.UserID != userID {
		// Foreign-owned and unknown are indistinguishable (no existence leak).
		return domain.ErrNotFound
	}
	// Re-deleting an already-deleted row is an accepted no-op: the listing
	// outcome is identical.
	now := time.Now().UTC()
	session.DeletedAt = &now
	return nil
}

// -------------------------------------------------------------------------
// MemoryEventStore / MemoryNoteStore implementation
// (integrate-agent-zero-memory 2.5). Mirrors the postgres adapter: every read
// applies the structural visibility predicate (domain.MemoryVisibleTo — the
// same rule the postgres WHERE clause encodes) under the store mutex, writes
// validate the birth tuple, owner shape, and ceiling through the domain
// layer, and supersede commits both rows under one lock hold — the fake's
// atomicity seam (the postgres adapter uses a guard CTE).
// -------------------------------------------------------------------------

type memoryEventStore struct {
	s *fakeStore
}

type memoryNoteStore struct {
	s *fakeStore
}

func cloneMemoryEvent(e *domain.MemoryEvent) *domain.MemoryEvent {
	if e == nil {
		return nil
	}
	cp := *e
	if e.UserID != nil {
		u := *e.UserID
		cp.UserID = &u
	}
	if e.Participants != nil {
		cp.Participants = make([]domain.MemoryParticipant, len(e.Participants))
		copy(cp.Participants, e.Participants)
	}
	if e.TombstonedAt != nil {
		t := *e.TombstonedAt
		cp.TombstonedAt = &t
	}
	return &cp
}

func cloneMemoryNote(n *domain.MemoryNote) *domain.MemoryNote {
	if n == nil {
		return nil
	}
	cp := *n
	if n.UserID != nil {
		u := *n.UserID
		cp.UserID = &u
	}
	if n.AgentID != nil {
		a := *n.AgentID
		cp.AgentID = &a
	}
	if n.Topic != nil {
		t := *n.Topic
		cp.Topic = &t
	}
	if n.ConflictFlag != nil {
		c := *n.ConflictFlag
		cp.ConflictFlag = &c
	}
	if n.Supersedes != nil {
		s := *n.Supersedes
		cp.Supersedes = &s
	}
	if n.SupersededBy != nil {
		s := *n.SupersededBy
		cp.SupersededBy = &s
	}
	if n.PromotedBy != nil {
		p := *n.PromotedBy
		cp.PromotedBy = &p
	}
	if n.PromotedAt != nil {
		t := *n.PromotedAt
		cp.PromotedAt = &t
	}
	if n.TombstonedAt != nil {
		t := *n.TombstonedAt
		cp.TombstonedAt = &t
	}
	return &cp
}

func cloneMemoryNoteEvidence(e *domain.MemoryNoteEvidence) *domain.MemoryNoteEvidence {
	if e == nil {
		return nil
	}
	cp := *e
	return &cp
}

func cloneMemoryReport(r *domain.MemoryReport) *domain.MemoryReport {
	if r == nil {
		return nil
	}
	cp := *r
	if r.Report != nil {
		cp.Report = make([]byte, len(r.Report))
		copy(cp.Report, r.Report)
	}
	return &cp
}

// memoryEventVisibleLocked applies the structural visibility predicate for an
// event: the producing agent owns agent-visibility rows. Callers hold the
// store lock.
func memoryEventVisibleLocked(e *domain.MemoryEvent, viewerUserID, servingAgentID string) bool {
	return domain.MemoryVisibleTo(e.Visibility, e.UserID, &e.AgentID, viewerUserID, servingAgentID)
}

// memoryNoteVisibleLocked applies the structural visibility predicate for a
// note. Callers hold the store lock.
func memoryNoteVisibleLocked(n *domain.MemoryNote, viewerUserID, servingAgentID string) bool {
	return domain.MemoryVisibleTo(n.Visibility, n.UserID, n.AgentID, viewerUserID, servingAgentID)
}

// memoryEventNewer reports whether a sorts after b by the read ordering
// shared with the postgres ORDER BY (event_time, learned_at, id).
func memoryEventNewer(a, b *domain.MemoryEvent) bool {
	if !a.EventTime.Equal(b.EventTime) {
		return a.EventTime.After(b.EventTime)
	}
	if !a.LearnedAt.Equal(b.LearnedAt) {
		return a.LearnedAt.After(b.LearnedAt)
	}
	return a.ID > b.ID
}

// memoryInWindow applies the [From, To) event_time bounds; zero fields are
// open ends (mirrors the postgres predicates).
func memoryInWindow(t time.Time, window *store.MemoryTimeWindow) bool {
	if window == nil {
		return true
	}
	if !window.From.IsZero() && t.Before(window.From) {
		return false
	}
	if !window.To.IsZero() && !t.Before(window.To) {
		return false
	}
	return true
}

// memorySearchTermCap bounds the term list, mirroring the postgres shaper's
// memoryQueryTermCap: a long turn text must not grow the predicate without
// limit, and membership parity keeps the fake aligned with the capped
// postgres match.
const memorySearchTermCap = 16

// memoryStopwords is the PostgreSQL 'english' text-search stopword list
// (snowball english.stop). to_tsquery('english', …) silently drops these
// words from the shaped tsquery, so the fake drops them too — stopword
// handling "rides the english text-search configuration" (design risk note,
// fix-memory-prefetch-matching) and membership parity holds on noisy
// natural-language queries.
var memoryStopwords = map[string]struct{}{
	"i": {}, "me": {}, "my": {}, "myself": {}, "we": {}, "our": {}, "ours": {}, "ourselves": {},
	"you": {}, "your": {}, "yours": {}, "yourself": {}, "yourselves": {},
	"he": {}, "him": {}, "his": {}, "himself": {}, "she": {}, "her": {}, "hers": {}, "herself": {},
	"it": {}, "its": {}, "itself": {},
	"they": {}, "them": {}, "their": {}, "theirs": {}, "themselves": {},
	"what": {}, "which": {}, "who": {}, "whom": {},
	"this": {}, "that": {}, "these": {}, "those": {},
	"am": {}, "is": {}, "are": {}, "was": {}, "were": {}, "be": {}, "been": {}, "being": {},
	"have": {}, "has": {}, "had": {}, "having": {},
	"do": {}, "does": {}, "did": {}, "doing": {},
	"a": {}, "an": {}, "the": {},
	"and": {}, "but": {}, "if": {}, "or": {}, "because": {},
	"as": {}, "until": {}, "while": {},
	"of": {}, "at": {}, "by": {}, "for": {}, "with": {}, "about": {}, "against": {},
	"between": {}, "into": {}, "through": {}, "during": {}, "before": {}, "after": {},
	"above": {}, "below": {}, "to": {}, "from": {}, "up": {}, "down": {}, "in": {}, "out": {},
	"on": {}, "off": {}, "over": {}, "under": {},
	"again": {}, "further": {}, "then": {}, "once": {},
	"here": {}, "there": {}, "when": {}, "where": {}, "why": {}, "how": {},
	"all": {}, "any": {}, "both": {}, "each": {}, "few": {}, "more": {}, "most": {},
	"other": {}, "some": {}, "such": {},
	"no": {}, "nor": {}, "not": {}, "only": {}, "own": {}, "same": {},
	"so": {}, "than": {}, "too": {}, "very": {},
	"s": {}, "t": {}, "can": {}, "will": {}, "just": {}, "don": {}, "should": {}, "now": {},
}

// memorySearchQuery splits a raw search query into the two matching legs of
// the any-term semantics (fix-memory-prefetch-matching D4): the lowercased
// whole-string needle (the exact-identifier leg, always matching) and the
// tokenized terms — lowercase, split on non-alphanumeric, empties and
// english stopwords dropped, capped at memorySearchTermCap — mirroring the
// effective to_tsquery('english') shape. A query whose terms all tokenize or
// stop away keeps only the needle leg, so a whitespace-only query can never
// widen into a match-everything.
func memorySearchQuery(queryText string) (needle string, terms []string) {
	if queryText == "" {
		return "", nil
	}
	needle = strings.ToLower(queryText)
	terms = strings.FieldsFunc(needle, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	kept := terms[:0]
	for _, term := range terms {
		if _, stop := memoryStopwords[term]; stop {
			continue
		}
		kept = append(kept, term)
		if len(kept) == memorySearchTermCap {
			break
		}
	}
	return needle, kept
}

// memorySearchMatch reports whether a lowercased haystack satisfies a search
// query and how many query terms it matched: any single term being a
// substring suffices, and the whole lowercased query as a literal substring
// always matches (verbatim identifiers). The count ranks results — more
// matched terms first; it is 0 for empty queries and needle-only hits.
func memorySearchMatch(haystack, needle string, terms []string) (matched bool, termCount int) {
	if needle == "" {
		return true, 0
	}
	for _, term := range terms {
		if strings.Contains(haystack, term) {
			termCount++
		}
	}
	if termCount > 0 || strings.Contains(haystack, needle) {
		return true, termCount
	}
	return false, 0
}

// validateMemoryNoteForWrite runs the shared write-path validation: scope,
// tier validity, owner shape, ceiling dominance (D4), and the provenance
// birth tuple (D5) — the same rules the SQL CHECK constraints pin.
func validateMemoryNoteForWrite(note *domain.MemoryNote, ceiling domain.MemoryVisibility) error {
	if note == nil || note.WorkspaceID == "" {
		return domain.ErrInvalid
	}
	if !domain.ValidMemoryVisibility(note.Visibility) {
		return fmt.Errorf("%w: unknown memory visibility %q", domain.ErrInvalid, note.Visibility)
	}
	if err := domain.ValidateMemoryNoteOwner(note.Visibility, note.UserID, note.AgentID); err != nil {
		return err
	}
	if err := domain.ValidateMemoryVisibilityWithin(ceiling, note.Visibility); err != nil {
		return err
	}
	return domain.ValidateMemoryProvenance(note.Origin, note.EventTime, note.LearnedAt, note.SourceEventID)
}

func (es *memoryEventStore) InsertEvent(ctx context.Context, event *domain.MemoryEvent) error {
	if event == nil || event.WorkspaceID == "" || event.AgentID == "" || event.SessionID == "" || event.TurnID == "" {
		return domain.ErrInvalid
	}
	if !domain.ValidMemoryVisibility(event.Visibility) {
		return fmt.Errorf("%w: unknown memory visibility %q", domain.ErrInvalid, event.Visibility)
	}
	if err := domain.ValidateMemoryEventOwner(event.Visibility, event.UserID); err != nil {
		return err
	}
	if err := domain.ValidateMemoryProvenance(event.Origin, event.EventTime, event.LearnedAt, event.SourceEventID); err != nil {
		return err
	}

	es.s.mu.Lock()
	defer es.s.mu.Unlock()

	if _, exists := es.s.workspaces[event.WorkspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}
	if _, exists := es.s.agents[event.AgentID]; !exists {
		return fmt.Errorf("%w: agent not found", domain.ErrNotFound)
	}
	if event.UserID != nil {
		if _, exists := es.s.users[*event.UserID]; !exists {
			return fmt.Errorf("%w: user not found", domain.ErrNotFound)
		}
	}
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	es.s.memoryEvents[event.ID] = cloneMemoryEvent(event)
	return nil
}

func (es *memoryEventStore) LatestEventForSession(ctx context.Context, workspaceID, sessionID string) (*domain.MemoryEvent, error) {
	if workspaceID == "" || sessionID == "" {
		return nil, nil
	}

	es.s.mu.RLock()
	defer es.s.mu.RUnlock()

	var latest *domain.MemoryEvent
	for _, e := range es.s.memoryEvents {
		if e.WorkspaceID != workspaceID || e.SessionID != sessionID || e.TombstonedAt != nil {
			continue
		}
		if latest == nil || memoryEventNewer(e, latest) {
			latest = e
		}
	}
	if latest == nil {
		return nil, nil
	}
	return cloneMemoryEvent(latest), nil
}

func (es *memoryEventStore) ListEventsForUI(ctx context.Context, workspaceID, viewerUserID, servingAgentID string, filters store.MemoryEventFilters) ([]domain.MemoryEvent, error) {
	return es.queryEvents(ctx, workspaceID, viewerUserID, servingAgentID, filters, "")
}

func (es *memoryEventStore) SearchEvents(ctx context.Context, workspaceID, viewerUserID, servingAgentID, query string, filters store.MemoryEventFilters) ([]domain.MemoryEvent, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("%w: search query is empty", domain.ErrInvalid)
	}
	return es.queryEvents(ctx, workspaceID, viewerUserID, servingAgentID, filters, query)
}

// queryEvents filters the visible set (structural scope first, then the
// optional filters, then the any-term lexical match standing in for the
// postgres hybrid match: any tokenized query term hitting
// Description+" "+Outcome, with the whole-string query kept as an
// always-match leg) and orders by descending matched-term count before
// newest first. Events and their matched-term counts ride one slice so the
// sort comparator can never read a count that a swap left behind.
func (es *memoryEventStore) queryEvents(ctx context.Context, workspaceID, viewerUserID, servingAgentID string, filters store.MemoryEventFilters, queryText string) ([]domain.MemoryEvent, error) {
	needle, terms := memorySearchQuery(queryText)

	es.s.mu.RLock()
	defer es.s.mu.RUnlock()

	type ranked struct {
		event domain.MemoryEvent
		count int
	}
	rankedEvents := make([]ranked, 0)
	for _, e := range es.s.memoryEvents {
		if e.WorkspaceID != workspaceID || e.TombstonedAt != nil {
			continue
		}
		if !memoryEventVisibleLocked(e, viewerUserID, servingAgentID) {
			continue
		}
		if filters.SessionID != "" && e.SessionID != filters.SessionID {
			continue
		}
		if filters.AgentID != "" && e.AgentID != filters.AgentID {
			continue
		}
		if filters.Visibility != "" && e.Visibility != filters.Visibility {
			continue
		}
		if !memoryInWindow(e.EventTime, filters.TimeWindow) {
			continue
		}
		matched, termCount := memorySearchMatch(strings.ToLower(e.Description+" "+e.Outcome), needle, terms)
		if !matched {
			continue
		}
		rankedEvents = append(rankedEvents, ranked{event: *cloneMemoryEvent(e), count: termCount})
	}
	// Most matched terms first, then newest first.
	sort.Slice(rankedEvents, func(i, j int) bool {
		if rankedEvents[i].count != rankedEvents[j].count {
			return rankedEvents[i].count > rankedEvents[j].count
		}
		return memoryEventNewer(&rankedEvents[i].event, &rankedEvents[j].event)
	})
	events := make([]domain.MemoryEvent, 0, len(rankedEvents))
	for _, r := range rankedEvents {
		events = append(events, r.event)
	}
	if filters.Limit > 0 && len(events) > filters.Limit {
		events = events[:filters.Limit]
	}
	return events, nil
}

// GetEventsByIDs returns the caller-visible live events with the given ids,
// newest first. The structural visible-set predicate applies exactly as on
// queryEvents; missing, foreign, tombstoned, and invisible ids are absent.
func (es *memoryEventStore) GetEventsByIDs(ctx context.Context, workspaceID, viewerUserID, servingAgentID string, ids []string) ([]domain.MemoryEvent, error) {
	wanted := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id != "" {
			wanted[id] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return []domain.MemoryEvent{}, nil
	}

	es.s.mu.RLock()
	defer es.s.mu.RUnlock()

	events := make([]domain.MemoryEvent, 0, len(wanted))
	for _, e := range es.s.memoryEvents {
		if e.WorkspaceID != workspaceID || e.TombstonedAt != nil {
			continue
		}
		if _, ok := wanted[e.ID]; !ok {
			continue
		}
		if !memoryEventVisibleLocked(e, viewerUserID, servingAgentID) {
			continue
		}
		events = append(events, *cloneMemoryEvent(e))
	}
	// Newest first, id as the determinism tiebreak (the ListEventsForUI
	// ordering without the term ranking).
	sort.Slice(events, func(i, j int) bool {
		return memoryEventNewer(&events[i], &events[j])
	})
	return events, nil
}

func (es *memoryEventStore) TombstoneEvent(ctx context.Context, workspaceID, id string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrInvalid
	}

	es.s.mu.Lock()
	defer es.s.mu.Unlock()

	e, exists := es.s.memoryEvents[id]
	if !exists || e.WorkspaceID != workspaceID || e.TombstonedAt != nil {
		// Absent and already-tombstoned are indistinguishable — no leak.
		return domain.ErrNotFound
	}
	now := time.Now().UTC()
	e.TombstonedAt = &now
	return nil
}

func (es *memoryEventStore) CountByVisibility(ctx context.Context, workspaceID, sessionID, turnID string) (map[domain.MemoryVisibility]int, error) {
	counts := make(map[domain.MemoryVisibility]int)
	if workspaceID == "" || sessionID == "" || turnID == "" {
		return counts, nil
	}

	es.s.mu.RLock()
	defer es.s.mu.RUnlock()

	for _, e := range es.s.memoryEvents {
		if e.WorkspaceID != workspaceID || e.SessionID != sessionID || e.TurnID != turnID || e.TombstonedAt != nil {
			continue
		}
		counts[e.Visibility]++
	}
	return counts, nil
}

// checkNoteRefsLocked mirrors the postgres FKs: the workspace must exist and
// any set owner must resolve. Callers hold the store lock.
func (ns *memoryNoteStore) checkNoteRefsLocked(n *domain.MemoryNote) error {
	if _, exists := ns.s.workspaces[n.WorkspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}
	if n.UserID != nil {
		if _, exists := ns.s.users[*n.UserID]; !exists {
			return fmt.Errorf("%w: user not found", domain.ErrNotFound)
		}
	}
	if n.AgentID != nil {
		if _, exists := ns.s.agents[*n.AgentID]; !exists {
			return fmt.Errorf("%w: agent not found", domain.ErrNotFound)
		}
	}
	return nil
}

func (ns *memoryNoteStore) InsertNote(ctx context.Context, note *domain.MemoryNote, ceiling domain.MemoryVisibility) error {
	if err := validateMemoryNoteForWrite(note, ceiling); err != nil {
		return err
	}

	ns.s.mu.Lock()
	defer ns.s.mu.Unlock()

	if err := ns.checkNoteRefsLocked(note); err != nil {
		return err
	}
	if note.ID == "" {
		note.ID = uuid.NewString()
	}
	ns.s.memoryNotes[note.ID] = cloneMemoryNote(note)
	return nil
}

func (ns *memoryNoteStore) SupersedeNote(ctx context.Context, workspaceID, oldID string, note *domain.MemoryNote, ceiling domain.MemoryVisibility) error {
	if workspaceID == "" || oldID == "" {
		return domain.ErrInvalid
	}
	if err := validateMemoryNoteForWrite(note, ceiling); err != nil {
		return err
	}

	ns.s.mu.Lock()
	defer ns.s.mu.Unlock()

	old, exists := ns.s.memoryNotes[oldID]
	if !exists || old.WorkspaceID != workspaceID || old.TombstonedAt != nil {
		// Hidden rows leak nothing — absent and tombstoned are one case.
		return domain.ErrNotFound
	}
	if old.SupersededBy != nil {
		return fmt.Errorf("%w: note %s already superseded", domain.ErrConflict, oldID)
	}
	if err := ns.checkNoteRefsLocked(note); err != nil {
		return err
	}
	// Both rows commit under one lock hold — the fake's atomicity seam.
	if note.ID == "" {
		note.ID = uuid.NewString()
	}
	note.Supersedes = &oldID
	ns.s.memoryNotes[note.ID] = cloneMemoryNote(note)
	old.SupersededBy = &note.ID
	return nil
}

func (ns *memoryNoteStore) TombstoneNote(ctx context.Context, workspaceID, id string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrInvalid
	}

	ns.s.mu.Lock()
	defer ns.s.mu.Unlock()

	n, exists := ns.s.memoryNotes[id]
	if !exists || n.WorkspaceID != workspaceID || n.TombstonedAt != nil {
		return domain.ErrNotFound
	}
	now := time.Now().UTC()
	n.TombstonedAt = &now
	return nil
}

func (ns *memoryNoteStore) PromoteNote(ctx context.Context, workspaceID, id, promotedByUserID string) error {
	if workspaceID == "" || id == "" || promotedByUserID == "" {
		return domain.ErrInvalid
	}

	ns.s.mu.Lock()
	defer ns.s.mu.Unlock()

	n, exists := ns.s.memoryNotes[id]
	if !exists || n.WorkspaceID != workspaceID || n.TombstonedAt != nil || n.SupersededBy != nil {
		return domain.ErrNotFound
	}
	if n.Visibility == domain.MemoryVisibilityShared {
		return fmt.Errorf("%w: note is already shared", domain.ErrConflict)
	}
	// The only widening path (D4): owner columns clear because shared rows
	// carry no owner, and the widening is audited.
	n.Visibility = domain.MemoryVisibilityShared
	n.UserID = nil
	n.AgentID = nil
	promotedBy := promotedByUserID
	n.PromotedBy = &promotedBy
	now := time.Now().UTC()
	n.PromotedAt = &now
	return nil
}

func (ns *memoryNoteStore) GetNote(ctx context.Context, workspaceID, viewerUserID, servingAgentID, id string) (*domain.MemoryNote, error) {
	if workspaceID == "" || id == "" {
		return nil, nil
	}

	ns.s.mu.RLock()
	defer ns.s.mu.RUnlock()

	n, exists := ns.s.memoryNotes[id]
	if !exists || n.WorkspaceID != workspaceID || n.TombstonedAt != nil ||
		!memoryNoteVisibleLocked(n, viewerUserID, servingAgentID) {
		return nil, nil
	}
	return cloneMemoryNote(n), nil
}

// GetNotesByIDs returns the caller-visible CURRENT notes (live, not
// superseded — the retrieval contract SearchNotes follows) with the given
// ids, newest-learned first. Missing, foreign, tombstoned, superseded, and
// invisible ids are simply absent from the result.
func (ns *memoryNoteStore) GetNotesByIDs(ctx context.Context, workspaceID, viewerUserID, servingAgentID string, ids []string) ([]domain.MemoryNote, error) {
	wanted := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id != "" {
			wanted[id] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return []domain.MemoryNote{}, nil
	}

	ns.s.mu.RLock()
	defer ns.s.mu.RUnlock()

	notes := make([]domain.MemoryNote, 0, len(wanted))
	for _, n := range ns.s.memoryNotes {
		if n.WorkspaceID != workspaceID || n.TombstonedAt != nil || n.SupersededBy != nil {
			continue
		}
		if _, ok := wanted[n.ID]; !ok {
			continue
		}
		if !memoryNoteVisibleLocked(n, viewerUserID, servingAgentID) {
			continue
		}
		notes = append(notes, *cloneMemoryNote(n))
	}
	sort.Slice(notes, func(i, j int) bool {
		if !notes[i].LearnedAt.Equal(notes[j].LearnedAt) {
			return notes[i].LearnedAt.After(notes[j].LearnedAt)
		}
		return notes[i].ID > notes[j].ID
	})
	return notes, nil
}

func (ns *memoryNoteStore) ListNotesForUI(ctx context.Context, workspaceID, viewerUserID, servingAgentID string, filters store.MemoryNoteFilters) ([]domain.MemoryNote, error) {
	return ns.queryNotes(ctx, workspaceID, viewerUserID, servingAgentID, filters, "", filters.History)
}

func (ns *memoryNoteStore) SearchNotes(ctx context.Context, workspaceID, viewerUserID, servingAgentID, query string, filters store.MemoryNoteFilters) ([]domain.MemoryNote, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("%w: search query is empty", domain.ErrInvalid)
	}
	// Retrieval is current-state only: a superseded note is dead for search.
	return ns.queryNotes(ctx, workspaceID, viewerUserID, servingAgentID, filters, query, false)
}

// queryNotes filters the visible set (structural scope first, then the
// optional filters, then the any-term lexical match standing in for the
// postgres hybrid match: any tokenized query term hitting the content, with
// the whole-string query kept as an always-match leg) and orders by
// descending matched-term count before pinned / newest-learned / id. Notes
// and their matched-term counts ride one slice so the sort comparator can
// never read a count that a swap left behind.
func (ns *memoryNoteStore) queryNotes(ctx context.Context, workspaceID, viewerUserID, servingAgentID string, filters store.MemoryNoteFilters, queryText string, includeSuperseded bool) ([]domain.MemoryNote, error) {
	needle, terms := memorySearchQuery(queryText)

	ns.s.mu.RLock()
	defer ns.s.mu.RUnlock()

	type ranked struct {
		note  domain.MemoryNote
		count int
	}
	rankedNotes := make([]ranked, 0)
	for _, n := range ns.s.memoryNotes {
		if n.WorkspaceID != workspaceID || n.TombstonedAt != nil {
			continue
		}
		if !includeSuperseded && n.SupersededBy != nil {
			continue
		}
		if !memoryNoteVisibleLocked(n, viewerUserID, servingAgentID) {
			continue
		}
		if filters.Visibility != "" && n.Visibility != filters.Visibility {
			continue
		}
		if filters.Topic != "" && (n.Topic == nil || *n.Topic != filters.Topic) {
			continue
		}
		if !memoryInWindow(n.EventTime, filters.TimeWindow) {
			continue
		}
		matched, termCount := memorySearchMatch(strings.ToLower(n.Content), needle, terms)
		if !matched {
			continue
		}
		rankedNotes = append(rankedNotes, ranked{note: *cloneMemoryNote(n), count: termCount})
	}
	// Most matched terms first, then pinned first, then newest-learned, id
	// as the determinism tiebreak.
	sort.Slice(rankedNotes, func(i, j int) bool {
		if rankedNotes[i].count != rankedNotes[j].count {
			return rankedNotes[i].count > rankedNotes[j].count
		}
		if rankedNotes[i].note.Pinned != rankedNotes[j].note.Pinned {
			return rankedNotes[i].note.Pinned
		}
		if !rankedNotes[i].note.LearnedAt.Equal(rankedNotes[j].note.LearnedAt) {
			return rankedNotes[i].note.LearnedAt.After(rankedNotes[j].note.LearnedAt)
		}
		return rankedNotes[i].note.ID > rankedNotes[j].note.ID
	})
	notes := make([]domain.MemoryNote, 0, len(rankedNotes))
	for _, r := range rankedNotes {
		notes = append(notes, r.note)
	}
	if filters.Limit > 0 && len(notes) > filters.Limit {
		notes = notes[:filters.Limit]
	}
	return notes, nil
}

// memoryNoteSimilarity is the fake's stand-in for pg_trgm's similarity():
// the Jaccard overlap of lowercased word tokens. Threshold semantics match
// the postgres adapter — strictly greater than.
func memoryNoteSimilarity(a, b string) float64 {
	tokens := func(s string) map[string]struct{} {
		set := make(map[string]struct{})
		for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		}) {
			set[w] = struct{}{}
		}
		return set
	}
	as, bs := tokens(a), tokens(b)
	if len(as) == 0 || len(bs) == 0 {
		return 0
	}
	shared := 0
	for w := range as {
		if _, ok := bs[w]; ok {
			shared++
		}
	}
	return float64(shared) / float64(len(as)+len(bs)-shared)
}

func (ns *memoryNoteStore) CountSimilar(ctx context.Context, workspaceID, viewerUserID, servingAgentID, content string, threshold float64) (int, error) {
	if workspaceID == "" || strings.TrimSpace(content) == "" {
		return 0, nil
	}

	ns.s.mu.RLock()
	defer ns.s.mu.RUnlock()

	count := 0
	for _, n := range ns.s.memoryNotes {
		if n.WorkspaceID != workspaceID || n.TombstonedAt != nil || n.SupersededBy != nil {
			continue
		}
		if !memoryNoteVisibleLocked(n, viewerUserID, servingAgentID) {
			continue
		}
		if memoryNoteSimilarity(content, n.Content) > threshold {
			count++
		}
	}
	return count, nil
}

func (ns *memoryNoteStore) CountByVisibility(ctx context.Context, workspaceID string) (map[domain.MemoryVisibility]int, error) {
	counts := make(map[domain.MemoryVisibility]int)
	if workspaceID == "" {
		return counts, nil
	}

	ns.s.mu.RLock()
	defer ns.s.mu.RUnlock()

	for _, n := range ns.s.memoryNotes {
		if n.WorkspaceID != workspaceID || n.TombstonedAt != nil {
			continue
		}
		counts[n.Visibility]++
	}
	return counts, nil
}

// SupersedeInto implements the consolidation merge primitive (D12): the
// canonical survivor keeps its row and the folded duplicate is pointed at
// it — both rows commit under one lock hold, the fake's atomicity seam (the
// postgres adapter uses a guard CTE). Guard order mirrors the postgres
// disambiguation: absent/tombstoned duplicates read ErrNotFound, an
// already-superseded duplicate reads ErrConflict, and an invalid survivor
// reads ErrNotFound.
func (ns *memoryNoteStore) SupersedeInto(ctx context.Context, workspaceID, oldID, intoID string) error {
	if workspaceID == "" || oldID == "" || intoID == "" {
		return domain.ErrInvalid
	}
	if oldID == intoID {
		return fmt.Errorf("%w: a note cannot be superseded into itself", domain.ErrInvalid)
	}

	ns.s.mu.Lock()
	defer ns.s.mu.Unlock()

	old, exists := ns.s.memoryNotes[oldID]
	if !exists || old.WorkspaceID != workspaceID {
		// Hidden rows leak nothing — absent and tombstoned are one case.
		return domain.ErrNotFound
	}
	if old.TombstonedAt != nil {
		return domain.ErrNotFound
	}
	if old.SupersededBy != nil {
		return fmt.Errorf("%w: note %s already superseded", domain.ErrConflict, oldID)
	}
	survivor, exists := ns.s.memoryNotes[intoID]
	if !exists || survivor.WorkspaceID != workspaceID || survivor.TombstonedAt != nil || survivor.SupersededBy != nil {
		return domain.ErrNotFound
	}
	pointer := intoID
	old.SupersededBy = &pointer
	return nil
}

// SetNoteTopic labels one live note for the consolidator's topic fold (D12).
func (ns *memoryNoteStore) SetNoteTopic(ctx context.Context, workspaceID, noteID, topic string) error {
	if workspaceID == "" || noteID == "" || strings.TrimSpace(topic) == "" {
		return domain.ErrInvalid
	}

	ns.s.mu.Lock()
	defer ns.s.mu.Unlock()

	n, exists := ns.s.memoryNotes[noteID]
	if !exists || n.WorkspaceID != workspaceID || n.TombstonedAt != nil || n.SupersededBy != nil {
		return domain.ErrNotFound
	}
	label := strings.TrimSpace(topic)
	n.Topic = &label
	return nil
}

// AddNoteEvidence links additional raw-event evidence on a note (D12's
// multi-evidence), idempotent per (note, source event): re-adding keeps the
// first link's added_at.
func (ns *memoryNoteStore) AddNoteEvidence(ctx context.Context, workspaceID, noteID string, sourceEventIDs []string) error {
	if workspaceID == "" || noteID == "" {
		return domain.ErrInvalid
	}

	ns.s.mu.Lock()
	defer ns.s.mu.Unlock()

	n, exists := ns.s.memoryNotes[noteID]
	if !exists || n.WorkspaceID != workspaceID || n.TombstonedAt != nil {
		return domain.ErrNotFound
	}
	now := time.Now().UTC()
	for _, sourceEventID := range sourceEventIDs {
		if sourceEventID == "" {
			continue
		}
		key := noteID + ":" + sourceEventID
		if _, exists := ns.s.memoryNoteEvidence[key]; exists {
			continue
		}
		ns.s.memoryNoteEvidence[key] = &domain.MemoryNoteEvidence{
			SourceEventID: sourceEventID,
			AddedAt:       now,
		}
	}
	return nil
}

// ListNoteEvidence returns the note's multi-evidence links, oldest link
// first. The workspace partition scopes the read; absent or hidden notes
// contribute an empty slice.
func (ns *memoryNoteStore) ListNoteEvidence(ctx context.Context, workspaceID, noteID string) ([]domain.MemoryNoteEvidence, error) {
	if workspaceID == "" || noteID == "" {
		return []domain.MemoryNoteEvidence{}, nil
	}

	ns.s.mu.RLock()
	defer ns.s.mu.RUnlock()

	evidence := make([]domain.MemoryNoteEvidence, 0)
	if n, exists := ns.s.memoryNotes[noteID]; !exists || n.WorkspaceID != workspaceID {
		return evidence, nil
	}
	for key, ev := range ns.s.memoryNoteEvidence {
		// The key is noteID + ":" + sourceEventID; the note id never carries
		// a colon (uuid), but source event ids are arbitrary text, so the
		// split lands on the last separator.
		sep := strings.LastIndex(key, ":")
		if sep < 0 || key[:sep] != noteID {
			continue
		}
		evidence = append(evidence, *cloneMemoryNoteEvidence(ev))
	}
	sort.Slice(evidence, func(i, j int) bool {
		if evidence[i].AddedAt.Equal(evidence[j].AddedAt) {
			return evidence[i].SourceEventID < evidence[j].SourceEventID
		}
		return evidence[i].AddedAt.Before(evidence[j].AddedAt)
	})
	return evidence, nil
}

// -------------------------------------------------------------------------
// MemoryReportStore implementation (integrate-agent-zero-memory D12): the
// last morning report per workspace, replaced on every pass. Absence is a
// normal state — Get returns (nil, nil) per the MemoryStore convention.
// -------------------------------------------------------------------------

type memoryReportStore struct {
	s *fakeStore
}

func (rs *memoryReportStore) Save(ctx context.Context, workspaceID string, report []byte, generatedAt time.Time) error {
	if workspaceID == "" || len(report) == 0 || generatedAt.IsZero() {
		return domain.ErrInvalid
	}

	rs.s.mu.Lock()
	defer rs.s.mu.Unlock()

	if _, exists := rs.s.workspaces[workspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}
	rs.s.memoryReports[workspaceID] = &domain.MemoryReport{
		WorkspaceID: workspaceID,
		Report:      append([]byte(nil), report...),
		GeneratedAt: generatedAt.UTC(),
	}
	return nil
}

func (rs *memoryReportStore) Get(ctx context.Context, workspaceID string) (*domain.MemoryReport, error) {
	if workspaceID == "" {
		return nil, nil
	}

	rs.s.mu.RLock()
	defer rs.s.mu.RUnlock()

	r, exists := rs.s.memoryReports[workspaceID]
	if !exists {
		return nil, nil
	}
	return cloneMemoryReport(r), nil
}
