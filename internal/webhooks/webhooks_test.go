package webhooks_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
	"github.com/oniharnantyo/onclaw/internal/webhooks"
)

// testKey is the instance master key for the test cipher (32 bytes, the
// AES-256-GCM requirement).
var testKey = []byte("01234567890123456789012345678901")

// seed is one seeded webhook fixture: a workspace, its github connection,
// a bound agent (created by the workspace's actor id), and a channel.
type seed struct {
	WS          *domain.Workspace
	Connection  *domain.Connection
	Agent       *domain.Agent
	Channel     *domain.Channel
	ActorID     string
	connections store.Connections
	webhooks    store.ConnectionWebhookStore
	agents      store.AgentStore
	channels    store.ChannelStore
}

// seedWorkspace seeds a workspace + github connection + agent + channel in
// the fake store — everything a webhook enablement and delivery need.
func seedWorkspace(t *testing.T, slug string) *seed {
	t.Helper()
	s := fake.New()
	ctx := context.Background()

	actorID := uuid.NewString()
	ws := &domain.Workspace{Slug: slug, Name: "WS " + slug}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	conn := &domain.Connection{WorkspaceID: ws.ID, Service: "github", AccessLevel: domain.ConnectionAccessReadOnly}
	if err := s.Connections().Create(ctx, conn); err != nil {
		t.Fatalf("create connection: %v", err)
	}
	agent := &domain.Agent{WorkspaceID: ws.ID, Name: "Atlas", Slug: "atlas", CreatedBy: &actorID}
	if err := s.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	channel := &domain.Channel{WorkspaceID: ws.ID, Name: "incidents", Slug: "incidents"}
	if err := s.Channels().CreateChannel(ctx, channel); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	return &seed{
		WS:          ws,
		Connection:  conn,
		Agent:       agent,
		Channel:     channel,
		ActorID:     actorID,
		connections: s.Connections(),
		webhooks:    s.ConnectionWebhooks(),
		agents:      s.Agents(),
		channels:    s.Channels(),
	}
}

// newService builds a management service over the seed's stores with the
// test cipher.
func (sd *seed) newService(t *testing.T) *webhooks.Service {
	t.Helper()
	return webhooks.NewService(sd.connections, sd.webhooks, sd.agents, sd.channels, webhooks.NewAESGCMCipher(testKey))
}

// enable is the happy-path enablement: bind the seed's agent to the seed's
// channel with the given events.
func (sd *seed) enable(t *testing.T, svc *webhooks.Service, events []string) string {
	t.Helper()
	state, secret, err := svc.Enable(context.Background(), sd.WS.ID, sd.Connection.ID, webhooks.EnableRequest{
		AgentID:    sd.Agent.ID,
		TargetKind: domain.ConnectionWebhookTargetChannel,
		TargetID:   sd.Channel.ID,
		Events:     events,
	})
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if state == nil || !state.Enabled {
		t.Fatalf("expected an enabled state, got %+v", state)
	}
	return secret
}
