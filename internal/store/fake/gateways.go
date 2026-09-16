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

// -------------------------------------------------------------------------
// Gateway stores (integrate-telegram-gateway 1.2). Mirror the postgres
// adapter: one gateway per (workspace, platform), globally-unique chat
// bindings enforced across every workspace, single-use pairing tokens whose
// consumption is decided by a conditional write (the fake's atomicity seam
// is the store mutex; the postgres adapter's is the UPDATE ... WHERE guard),
// and outbox claims evaluated under the same lock (FOR UPDATE SKIP LOCKED
// precedent).
// -------------------------------------------------------------------------

func gatewayIdentityKey(workspaceID, platform, identity string) string {
	return workspaceID + ":" + platform + ":" + identity
}

func userLinkKey(platform, platformUserID, workspaceID string) string {
	return platform + ":" + platformUserID + ":" + workspaceID
}

func cloneGatewayConfig(g *domain.GatewayConfig) *domain.GatewayConfig {
	if g == nil {
		return nil
	}
	cp := *g
	return &cp
}

func cloneChatBinding(b *domain.ChatBinding) *domain.ChatBinding {
	if b == nil {
		return nil
	}
	cp := *b
	if b.CreatedBy != nil {
		cb := *b.CreatedBy
		cp.CreatedBy = &cb
	}
	return &cp
}

func cloneUserLink(l *domain.UserLink) *domain.UserLink {
	if l == nil {
		return nil
	}
	cp := *l
	return &cp
}

func clonePairingToken(t *domain.PairingToken) *domain.PairingToken {
	if t == nil {
		return nil
	}
	cp := *t
	if t.ConsumedAt != nil {
		c := *t.ConsumedAt
		cp.ConsumedAt = &c
	}
	return &cp
}

func cloneOutboxEntry(e *domain.OutboxEntry) *domain.OutboxEntry {
	if e == nil {
		return nil
	}
	cp := *e
	cp.Payload = append([]byte(nil), e.Payload...)
	return &cp
}

// -------------------------------------------------------------------------
// GatewayStore
// -------------------------------------------------------------------------

type gatewayStore struct {
	s *fakeStore
}

func (gs *gatewayStore) CreateGateway(ctx context.Context, workspaceID string, g *domain.GatewayConfig) error {
	if g == nil {
		return domain.ErrInvalid
	}
	now := time.Now().UTC()
	g.WorkspaceID = workspaceID
	if err := domain.ValidateGatewayConfig(g); err != nil {
		return err
	}

	gs.s.mu.Lock()
	defer gs.s.mu.Unlock()

	if _, exists := gs.s.workspaces[workspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}
	agent, exists := gs.s.agents[g.AgentID]
	if !exists || agent.WorkspaceID != workspaceID {
		return fmt.Errorf("%w: agent not found in workspace", domain.ErrNotFound)
	}

	identKey := gatewayIdentityKey(workspaceID, g.Platform, g.Identity)
	if _, exists := gs.s.gatewayIdentities[identKey]; exists {
		return fmt.Errorf("%w: gateway identity %s already exists on platform %s", domain.ErrConflict, g.Identity, g.Platform)
	}

	if g.ID == "" {
		g.ID = uuid.NewString()
	}
	if g.CreatedAt.IsZero() {
		g.CreatedAt = now
	}
	g.UpdatedAt = now
	gs.s.gateways[g.ID] = cloneGatewayConfig(g)
	gs.s.gatewayIdentities[identKey] = g.ID
	return nil
}

func (gs *gatewayStore) GetGateway(ctx context.Context, workspaceID, id string) (*domain.GatewayConfig, error) {
	if workspaceID == "" || id == "" {
		return nil, nil
	}

	gs.s.mu.RLock()
	defer gs.s.mu.RUnlock()

	g, exists := gs.s.gateways[id]
	if !exists || g.WorkspaceID != workspaceID {
		return nil, nil
	}
	return cloneGatewayConfig(g), nil
}

func (gs *gatewayStore) ListGateways(ctx context.Context, workspaceID string) ([]domain.GatewayConfig, error) {
	if workspaceID == "" {
		return []domain.GatewayConfig{}, nil
	}

	gs.s.mu.RLock()
	defer gs.s.mu.RUnlock()

	gateways := make([]domain.GatewayConfig, 0)
	for _, g := range gs.s.gateways {
		if g.WorkspaceID == workspaceID {
			gateways = append(gateways, *cloneGatewayConfig(g))
		}
	}
	sort.Slice(gateways, func(i, j int) bool {
		if gateways[i].CreatedAt.Equal(gateways[j].CreatedAt) {
			return gateways[i].ID < gateways[j].ID
		}
		return gateways[i].CreatedAt.Before(gateways[j].CreatedAt)
	})
	return gateways, nil
}

func (gs *gatewayStore) ListGatewaysByPlatform(ctx context.Context, workspaceID, platform string) ([]domain.GatewayConfig, error) {
	if workspaceID == "" || platform == "" {
		return []domain.GatewayConfig{}, nil
	}

	gs.s.mu.RLock()
	defer gs.s.mu.RUnlock()

	gateways := make([]domain.GatewayConfig, 0)
	for _, g := range gs.s.gateways {
		if g.WorkspaceID == workspaceID && g.Platform == platform {
			gateways = append(gateways, *cloneGatewayConfig(g))
		}
	}
	sort.Slice(gateways, func(i, j int) bool {
		if gateways[i].CreatedAt.Equal(gateways[j].CreatedAt) {
			return gateways[i].ID < gateways[j].ID
		}
		return gateways[i].CreatedAt.Before(gateways[j].CreatedAt)
	})
	return gateways, nil
}

func (gs *gatewayStore) UpdateGateway(ctx context.Context, workspaceID string, g *domain.GatewayConfig) error {
	if g == nil || g.ID == "" {
		return domain.ErrInvalid
	}
	now := time.Now().UTC()
	g.WorkspaceID = workspaceID
	if err := domain.ValidateGatewayConfig(g); err != nil {
		return err
	}

	gs.s.mu.Lock()
	defer gs.s.mu.Unlock()

	existing, exists := gs.s.gateways[g.ID]
	if !exists || existing.WorkspaceID != workspaceID {
		return domain.ErrNotFound
	}
	agent, exists := gs.s.agents[g.AgentID]
	if !exists || agent.WorkspaceID != workspaceID {
		return fmt.Errorf("%w: agent not found in workspace", domain.ErrNotFound)
	}

	oldIdentKey := gatewayIdentityKey(workspaceID, existing.Platform, existing.Identity)
	newIdentKey := gatewayIdentityKey(workspaceID, g.Platform, g.Identity)
	if oldIdentKey != newIdentKey {
		if otherID, exists := gs.s.gatewayIdentities[newIdentKey]; exists && otherID != g.ID {
			return fmt.Errorf("%w: gateway identity %s already exists on platform %s", domain.ErrConflict, g.Identity, g.Platform)
		}
		delete(gs.s.gatewayIdentities, oldIdentKey)
		gs.s.gatewayIdentities[newIdentKey] = g.ID
	}

	existing.Platform = g.Platform
	existing.Lane = g.Lane
	existing.Identity = g.Identity
	existing.AgentID = g.AgentID
	existing.BotTokenCiphertext = g.BotTokenCiphertext
	existing.BotUsername = g.BotUsername
	existing.Enabled = g.Enabled
	existing.Transport = g.Transport
	existing.WebhookURL = g.WebhookURL
	existing.UpdatedAt = now

	*g = *cloneGatewayConfig(existing)
	return nil
}

func (gs *gatewayStore) SetGatewayEnabled(ctx context.Context, workspaceID, id string, enabled bool) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	gs.s.mu.Lock()
	defer gs.s.mu.Unlock()

	g, exists := gs.s.gateways[id]
	if !exists || g.WorkspaceID != workspaceID {
		return domain.ErrNotFound
	}
	g.Enabled = enabled
	g.UpdatedAt = time.Now().UTC()
	return nil
}

func (gs *gatewayStore) DeleteGateway(ctx context.Context, workspaceID, id string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	gs.s.mu.Lock()
	defer gs.s.mu.Unlock()

	g, exists := gs.s.gateways[id]
	if !exists || g.WorkspaceID != workspaceID {
		return domain.ErrNotFound
	}
	identKey := gatewayIdentityKey(workspaceID, g.Platform, g.Identity)
	delete(gs.s.gatewayIdentities, identKey)
	delete(gs.s.gateways, id)

	// Cascade delete chat bindings associated with this gateway
	for bindingID, b := range gs.s.gatewayChatBindings {
		if b.GatewayID == id {
			delete(gs.s.gatewayChatBindings, bindingID)
			delete(gs.s.gatewayBindingsByChat, b.Platform+":"+b.PlatformChatID)
		}
	}

	return nil
}

// -------------------------------------------------------------------------
// GatewayBindings
// -------------------------------------------------------------------------

type gatewayBindingStore struct {
	s *fakeStore
}

func (gb *gatewayBindingStore) CreateChatBinding(ctx context.Context, workspaceID string, b *domain.ChatBinding) error {
	if b == nil {
		return domain.ErrInvalid
	}
	now := time.Now().UTC()
	b.WorkspaceID = workspaceID
	if err := domain.ValidateChatBinding(b); err != nil {
		return err
	}

	gb.s.mu.Lock()
	defer gb.s.mu.Unlock()

	if _, exists := gb.s.workspaces[workspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}
	agent, exists := gb.s.agents[b.AgentID]
	if !exists || agent.WorkspaceID != workspaceID {
		return fmt.Errorf("%w: agent not found in workspace", domain.ErrNotFound)
	}
	if b.CreatedBy != nil && *b.CreatedBy != "" {
		if _, exists := gb.s.users[*b.CreatedBy]; !exists {
			return fmt.Errorf("%w: user not found", domain.ErrNotFound)
		}
	}

	// UNIQUE (platform, platform_chat_id) is global: a chat bound in any
	// workspace — including this one — is a conflict (design D2).
	chatKey := b.Platform + ":" + b.PlatformChatID
	if _, taken := gb.s.gatewayBindingsByChat[chatKey]; taken {
		return fmt.Errorf("%w: %s chat %s is already bound to an agent", domain.ErrGatewayBindingConflict, b.Platform, b.PlatformChatID)
	}

	if b.ID == "" {
		b.ID = uuid.NewString()
	}
	if b.CreatedAt.IsZero() {
		b.CreatedAt = now
	}
	gb.s.gatewayChatBindings[b.ID] = cloneChatBinding(b)
	gb.s.gatewayBindingsByChat[chatKey] = b.ID
	return nil
}

func (gb *gatewayBindingStore) GetChatBinding(ctx context.Context, workspaceID, platform, platformChatID string) (*domain.ChatBinding, error) {
	if workspaceID == "" || platform == "" || platformChatID == "" {
		return nil, nil
	}

	gb.s.mu.RLock()
	defer gb.s.mu.RUnlock()

	id, exists := gb.s.gatewayBindingsByChat[platform+":"+platformChatID]
	if !exists {
		return nil, nil
	}
	b := gb.s.gatewayChatBindings[id]
	if b == nil || b.WorkspaceID != workspaceID {
		return nil, nil
	}
	return cloneChatBinding(b), nil
}

func (gb *gatewayBindingStore) ListChatBindings(ctx context.Context, workspaceID string) ([]domain.ChatBinding, error) {
	if workspaceID == "" {
		return []domain.ChatBinding{}, nil
	}

	gb.s.mu.RLock()
	defer gb.s.mu.RUnlock()

	bindings := make([]domain.ChatBinding, 0)
	for _, b := range gb.s.gatewayChatBindings {
		if b.WorkspaceID == workspaceID {
			bindings = append(bindings, *cloneChatBinding(b))
		}
	}
	sort.Slice(bindings, func(i, j int) bool {
		if bindings[i].CreatedAt.Equal(bindings[j].CreatedAt) {
			return bindings[i].ID < bindings[j].ID
		}
		return bindings[i].CreatedAt.Before(bindings[j].CreatedAt)
	})
	return bindings, nil
}

func (gb *gatewayBindingStore) RemapBindingChat(ctx context.Context, workspaceID, platform, oldChatID, newChatID string) error {
	if workspaceID == "" || platform == "" || oldChatID == "" || newChatID == "" {
		return domain.ErrNotFound
	}

	gb.s.mu.Lock()
	defer gb.s.mu.Unlock()

	id, exists := gb.s.gatewayBindingsByChat[platform+":"+oldChatID]
	if !exists {
		return domain.ErrNotFound
	}
	b := gb.s.gatewayChatBindings[id]
	if b == nil || b.WorkspaceID != workspaceID {
		return domain.ErrNotFound
	}

	newKey := platform + ":" + newChatID
	if _, taken := gb.s.gatewayBindingsByChat[newKey]; taken {
		return fmt.Errorf("%w: %s chat %s is already bound to an agent", domain.ErrGatewayBindingConflict, platform, newChatID)
	}
	delete(gb.s.gatewayBindingsByChat, platform+":"+oldChatID)
	b.PlatformChatID = newChatID
	gb.s.gatewayBindingsByChat[newKey] = id
	return nil
}

func (gb *gatewayBindingStore) DeleteChatBinding(ctx context.Context, workspaceID, id string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	gb.s.mu.Lock()
	defer gb.s.mu.Unlock()

	b, exists := gb.s.gatewayChatBindings[id]
	if !exists || b.WorkspaceID != workspaceID {
		return domain.ErrNotFound
	}
	delete(gb.s.gatewayChatBindings, id)
	delete(gb.s.gatewayBindingsByChat, b.Platform+":"+b.PlatformChatID)
	return nil
}

func (gb *gatewayBindingStore) ActiveSessionSuffix(ctx context.Context, platform, platformChatID, agentID string) (int64, error) {
	if platform == "" || platformChatID == "" || agentID == "" {
		return 0, nil
	}

	gb.s.mu.RLock()
	defer gb.s.mu.RUnlock()

	return gb.s.gatewayActiveSessions[platform+":"+platformChatID+":"+agentID], nil
}

func (gb *gatewayBindingStore) BumpActiveSessionSuffix(ctx context.Context, platform, platformChatID, agentID string) (int64, error) {
	if platform == "" || platformChatID == "" || agentID == "" {
		return 0, fmt.Errorf("%w: platform, chat id, and agent are required", domain.ErrInvalid)
	}

	gb.s.mu.Lock()
	defer gb.s.mu.Unlock()

	key := platform + ":" + platformChatID + ":" + agentID
	gb.s.gatewayActiveSessions[key]++
	return gb.s.gatewayActiveSessions[key], nil
}

// -------------------------------------------------------------------------
// GatewayLinks
// -------------------------------------------------------------------------

type gatewayLinkStore struct {
	s *fakeStore
}

func (gl *gatewayLinkStore) CreateUserLink(ctx context.Context, workspaceID string, l *domain.UserLink) error {
	if l == nil {
		return domain.ErrInvalid
	}
	now := time.Now().UTC()
	l.WorkspaceID = workspaceID
	if err := domain.ValidateUserLink(l); err != nil {
		return err
	}

	gl.s.mu.Lock()
	defer gl.s.mu.Unlock()

	if _, exists := gl.s.workspaces[workspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}
	if _, exists := gl.s.users[l.UserID]; !exists {
		return fmt.Errorf("%w: user not found", domain.ErrNotFound)
	}

	key := userLinkKey(l.Platform, l.PlatformUserID, workspaceID)
	if _, exists := gl.s.gatewayUserLinks[key]; exists {
		return fmt.Errorf("%w: platform identity %s is already paired in this workspace", domain.ErrConflict, l.PlatformUserID)
	}

	if l.CreatedAt.IsZero() {
		l.CreatedAt = now
	}
	gl.s.gatewayUserLinks[key] = cloneUserLink(l)
	return nil
}

func (gl *gatewayLinkStore) GetUserLink(ctx context.Context, workspaceID, platform, platformUserID string) (*domain.UserLink, error) {
	if workspaceID == "" || platform == "" || platformUserID == "" {
		return nil, nil
	}

	gl.s.mu.RLock()
	defer gl.s.mu.RUnlock()

	l, exists := gl.s.gatewayUserLinks[userLinkKey(platform, platformUserID, workspaceID)]
	if !exists {
		return nil, nil
	}
	return cloneUserLink(l), nil
}

func (gl *gatewayLinkStore) ListUserLinks(ctx context.Context, workspaceID string) ([]domain.UserLink, error) {
	if workspaceID == "" {
		return []domain.UserLink{}, nil
	}

	gl.s.mu.RLock()
	defer gl.s.mu.RUnlock()

	links := make([]domain.UserLink, 0)
	for _, l := range gl.s.gatewayUserLinks {
		if l.WorkspaceID == workspaceID {
			links = append(links, *cloneUserLink(l))
		}
	}
	sort.Slice(links, func(i, j int) bool {
		if links[i].CreatedAt.Equal(links[j].CreatedAt) {
			return links[i].PlatformUserID < links[j].PlatformUserID
		}
		return links[i].CreatedAt.Before(links[j].CreatedAt)
	})
	return links, nil
}

func (gl *gatewayLinkStore) ListUserLinksForMember(ctx context.Context, workspaceID, userID string) ([]domain.UserLink, error) {
	if workspaceID == "" || userID == "" {
		return []domain.UserLink{}, nil
	}

	gl.s.mu.RLock()
	defer gl.s.mu.RUnlock()

	links := make([]domain.UserLink, 0)
	for _, l := range gl.s.gatewayUserLinks {
		if l.WorkspaceID == workspaceID && l.UserID == userID {
			links = append(links, *cloneUserLink(l))
		}
	}
	sort.Slice(links, func(i, j int) bool {
		if links[i].CreatedAt.Equal(links[j].CreatedAt) {
			return links[i].PlatformUserID < links[j].PlatformUserID
		}
		return links[i].CreatedAt.Before(links[j].CreatedAt)
	})
	return links, nil
}

func (gl *gatewayLinkStore) DeleteUserLink(ctx context.Context, workspaceID, platform, platformUserID string) error {
	if workspaceID == "" || platform == "" || platformUserID == "" {
		return domain.ErrNotFound
	}

	gl.s.mu.Lock()
	defer gl.s.mu.Unlock()

	key := userLinkKey(platform, platformUserID, workspaceID)
	if _, exists := gl.s.gatewayUserLinks[key]; !exists {
		return domain.ErrNotFound
	}
	delete(gl.s.gatewayUserLinks, key)
	return nil
}

func (gl *gatewayLinkStore) CreatePairingToken(ctx context.Context, t *domain.PairingToken) error {
	if t == nil {
		return domain.ErrInvalid
	}
	now := time.Now().UTC()
	if err := domain.ValidatePairingToken(t, now, true); err != nil {
		return err
	}

	gl.s.mu.Lock()
	defer gl.s.mu.Unlock()

	if _, exists := gl.s.workspaces[t.WorkspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}
	if _, exists := gl.s.users[t.UserID]; !exists {
		return fmt.Errorf("%w: user not found", domain.ErrNotFound)
	}

	key := t.WorkspaceID + ":" + t.Token
	if _, exists := gl.s.gatewayPairingTokens[key]; exists {
		return fmt.Errorf("%w: pairing token already exists", domain.ErrConflict)
	}

	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	gl.s.gatewayPairingTokens[key] = clonePairingToken(t)
	return nil
}

func (gl *gatewayLinkStore) ConsumePairingToken(ctx context.Context, workspaceID, token string, now time.Time) (*domain.PairingToken, error) {
	if workspaceID == "" || token == "" {
		return nil, domain.ErrNotFound
	}

	gl.s.mu.Lock()
	defer gl.s.mu.Unlock()

	t, exists := gl.s.gatewayPairingTokens[workspaceID+":"+token]
	if !exists {
		return nil, domain.ErrNotFound
	}
	// The validity window matches the postgres adapter's conditional-write
	// guard: valid iff expires_at is strictly after now.
	if t.ConsumedAt != nil || !now.Before(t.ExpiresAt) {
		return nil, fmt.Errorf("%w: token used or expired at %s", domain.ErrPairingTokenExpired, t.ExpiresAt.Format(time.RFC3339))
	}

	// Single-use: the conditional write decides the winner, exactly like
	// the postgres adapter's UPDATE ... WHERE consumed_at IS NULL guard.
	consumed := now
	t.ConsumedAt = &consumed
	return clonePairingToken(t), nil
}

func (gl *gatewayLinkStore) RevokePairingToken(ctx context.Context, workspaceID, token string) error {
	if workspaceID == "" || token == "" {
		return domain.ErrNotFound
	}

	gl.s.mu.Lock()
	defer gl.s.mu.Unlock()

	key := workspaceID + ":" + token
	if _, exists := gl.s.gatewayPairingTokens[key]; !exists {
		return domain.ErrNotFound
	}
	delete(gl.s.gatewayPairingTokens, key)
	return nil
}

// -------------------------------------------------------------------------
// GatewayOutbox
// -------------------------------------------------------------------------

type gatewayOutboxStore struct {
	s *fakeStore
}

func (goStore *gatewayOutboxStore) Enqueue(ctx context.Context, e *domain.OutboxEntry) error {
	if e == nil {
		return domain.ErrInvalid
	}
	now := time.Now().UTC()
	if err := domain.ValidateOutboxEntry(e); err != nil {
		return err
	}

	goStore.s.mu.Lock()
	defer goStore.s.mu.Unlock()

	if _, exists := goStore.s.workspaces[e.WorkspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}

	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	e.Attempts = 0
	if e.DeliverAfter.IsZero() {
		e.DeliverAfter = now
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	goStore.s.gatewayOutbox[e.ID] = cloneOutboxEntry(e)
	return nil
}

func (goStore *gatewayOutboxStore) ClaimDue(ctx context.Context, now time.Time, limit int) ([]domain.OutboxEntry, error) {
	if limit <= 0 {
		return []domain.OutboxEntry{}, nil
	}

	goStore.s.mu.Lock()
	defer goStore.s.mu.Unlock()

	due := make([]*domain.OutboxEntry, 0)
	for _, e := range goStore.s.gatewayOutbox {
		if e.Status == domain.GatewayOutboxStatusPending && !e.DeliverAfter.After(now) {
			due = append(due, e)
		}
	}
	sort.Slice(due, func(i, j int) bool {
		if due[i].DeliverAfter.Equal(due[j].DeliverAfter) {
			return due[i].ID < due[j].ID
		}
		return due[i].DeliverAfter.Before(due[j].DeliverAfter)
	})
	if len(due) > limit {
		due = due[:limit]
	}

	claimed := make([]domain.OutboxEntry, 0, len(due))
	for _, e := range due {
		e.Attempts++
		claimed = append(claimed, *cloneOutboxEntry(e))
	}
	return claimed, nil
}

func (goStore *gatewayOutboxStore) MarkDelivered(ctx context.Context, workspaceID, id string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	goStore.s.mu.Lock()
	defer goStore.s.mu.Unlock()

	e, exists := goStore.s.gatewayOutbox[id]
	if !exists || e.WorkspaceID != workspaceID {
		return domain.ErrNotFound
	}
	e.Status = domain.GatewayOutboxStatusDelivered
	return nil
}

func (goStore *gatewayOutboxStore) Reschedule(ctx context.Context, workspaceID, id string, deliverAfter time.Time) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	goStore.s.mu.Lock()
	defer goStore.s.mu.Unlock()

	e, exists := goStore.s.gatewayOutbox[id]
	if !exists || e.WorkspaceID != workspaceID {
		return domain.ErrNotFound
	}
	e.Status = domain.GatewayOutboxStatusPending
	e.DeliverAfter = deliverAfter
	return nil
}

func (goStore *gatewayOutboxStore) MarkDead(ctx context.Context, workspaceID, id string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	goStore.s.mu.Lock()
	defer goStore.s.mu.Unlock()

	e, exists := goStore.s.gatewayOutbox[id]
	if !exists || e.WorkspaceID != workspaceID {
		return domain.ErrNotFound
	}
	e.Status = domain.GatewayOutboxStatusDead
	return nil
}

// CountDead counts the workspace gateway's dead entries by decoding each
// payload (the postgres adapter filters on payload->>'gateway_id'; the fake
// mirrors that with the JSON field, legacy html-shape rows included).
func (goStore *gatewayOutboxStore) CountDead(ctx context.Context, workspaceID, gatewayID string) (int64, error) {
	if workspaceID == "" || gatewayID == "" {
		return 0, nil
	}

	goStore.s.mu.Lock()
	defer goStore.s.mu.Unlock()

	var n int64
	for _, e := range goStore.s.gatewayOutbox {
		if e.WorkspaceID != workspaceID || e.Status != domain.GatewayOutboxStatusDead {
			continue
		}
		var probe struct {
			GatewayID string `json:"gateway_id"`
		}
		if err := json.Unmarshal(e.Payload, &probe); err != nil {
			continue
		}
		if probe.GatewayID == gatewayID {
			n++
		}
	}
	return n, nil
}

func (goStore *gatewayOutboxStore) PruneDelivered(ctx context.Context, before time.Time) (int64, error) {
	goStore.s.mu.Lock()
	defer goStore.s.mu.Unlock()

	var pruned int64
	for id, e := range goStore.s.gatewayOutbox {
		if e.Status == domain.GatewayOutboxStatusDelivered && e.CreatedAt.Before(before) {
			delete(goStore.s.gatewayOutbox, id)
			pruned++
		}
	}
	return pruned, nil
}

// Compile-time interface conformance (the fake must satisfy the same ports
// the postgres adapter does).
var (
	_ store.GatewayStore    = (*gatewayStore)(nil)
	_ store.GatewayBindings = (*gatewayBindingStore)(nil)
	_ store.GatewayLinks    = (*gatewayLinkStore)(nil)
	_ store.GatewayOutbox   = (*gatewayOutboxStore)(nil)
)
