package server

// Connection webhook runtime (add-connection-webhooks 3.2): the
// composition-owned bundle that assembles the webhook management service
// and the delivery ingress around the runner — the GatewayRuntime pattern.
// Built by the CLI composition root and shared with the HTTP layer through
// RouterOptions; the router's nil fallback builds a fresh one for test
// assembly.
//
// Seams that live here because they belong to composition, not to the
// pipeline core:
//   - the RunnerPort adapter: *agents.Runner satisfies webhooks.RunnerPort
//     through this wrapper, which resolves the event run's mechanical acting
//     identity (the bound agent's creator, falling back to the first
//     workspace owner — the scheduler/heartbeat creator precedent; the
//     runner's ExecRequest requires a user id, but the service-authority
//     attribution rides Origin=service + the connection/event fields, which
//     is what persists and traces),
//   - channel-target delivery: the run's stream drains on a detached
//     context and the final assistant text posts into the bound channel
//     through the chokepoint's PostFromAgent — the scheduler's channel
//     delivery shape (thread targets need no delivery: the transcript is
//     the surface),
//   - the secret cipher over the instance master key and the bounded
//     delivery queue's lifecycle (Close rides shutdown).

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/webhooks"
)

// DefaultWebhookQueueDepth is the delivery backlog bound (design.md D6): a
// provider burst queues up to this many verified deliveries before the
// pipeline degrades to inline processing.
const DefaultWebhookQueueDepth = 1024

// webhookChannelPoster is the channel delivery seam (the scheduler
// ChannelPoster shape): the chokepoint satisfies it structurally.
type webhookChannelPoster interface {
	PostFromAgent(ctx context.Context, workspaceID, channelID, agentID, body string) (domain.ChannelMessage, error)
}

// WebhookRuntime bundles the webhook management service, the delivery
// ingress, and the bounded delivery queue.
type WebhookRuntime struct {
	Service *webhooks.Service
	Ingress *webhooks.Ingress
	Queue   *webhooks.Queue
}

// NewWebhookRuntime assembles the webhook runtime around the runner:
// granular stores, the key-bound secret cipher, the bounded delivery queue
// (design.md D6), and the run submitter adapter. Injected dependencies are
// never nil. The queue's processing closure reads the runtime's ingress —
// the dispatcher starts lazily on the first enqueue, always after this
// constructor returns.
func NewWebhookRuntime(
	connections store.Connections,
	connectionWebhooks store.ConnectionWebhookStore,
	agents store.AgentStore,
	channels store.ChannelStore,
	members store.MemberStore,
	roles store.RoleStore,
	runner *agents.Runner,
	poster webhookChannelPoster,
	encryptionKey []byte,
) *WebhookRuntime {
	rt := &WebhookRuntime{}
	rt.Queue = webhooks.NewQueue(DefaultWebhookQueueDepth, func(ctx context.Context, job *webhooks.Delivery) {
		rt.Ingress.ProcessDelivery(ctx, job)
	}, webhooks.WithPerConnectionLimit(webhooks.DefaultPerConnectionLimit))
	rt.Service = webhooks.NewService(
		connections,
		connectionWebhooks,
		agents,
		channels,
		webhooks.NewAESGCMCipher(encryptionKey),
	)
	rt.Ingress = webhooks.NewIngress(
		connectionWebhooks,
		connections,
		webhooks.NewAESGCMCipher(encryptionKey),
		rt.Queue,
		&webhookRunSubmitter{
			runner:  runner,
			agents:  agents,
			members: members,
			roles:   roles,
			poster:  poster,
		},
	)
	return rt
}

// Close stops the delivery queue and waits for in-flight processing — the
// shutdown seam the composition root calls after the run drain.
func (rt *WebhookRuntime) Close() {
	rt.Queue.Close()
}

// webhookRunSubmitter adapts *agents.Runner to webhooks.RunnerPort: it
// resolves the acting identity the runner mechanically requires (event runs
// carry no requesting user; the bound agent's creator acts, falling back to
// the first workspace owner), stamps the first-class service origin with the
// connection/event attribution, and owns the stream drain — including
// channel-target delivery of the final assistant text.
type webhookRunSubmitter struct {
	runner  *agents.Runner
	agents  store.AgentStore
	members store.MemberStore
	roles   store.RoleStore
	poster  webhookChannelPoster
}

// RunEvent implements webhooks.RunnerPort.
func (s *webhookRunSubmitter) RunEvent(ctx context.Context, req webhooks.RunRequest) error {
	userID, err := s.actingUser(ctx, req.WorkspaceID, req.AgentID)
	if err != nil {
		return fmt.Errorf("webhook run submitter: resolve acting identity: %w", err)
	}

	stream, err := s.runner.Run(ctx, agents.ExecRequest{
		WorkspaceID: req.WorkspaceID,
		AgentID:     req.AgentID,
		SessionID:   req.SessionID,
		UserID:      userID,
		// OriginService is the service-authority run's origin (contract §6):
		// first-class in the runner's origin normalizer, so traces and hook
		// events see "service" instead of a fabricated user attribution. The
		// acting user above satisfies ExecRequest's mechanical non-empty-user
		// requirement only; the connection and event name the real trigger.
		Origin:            webhooks.OriginService,
		ConnectionID:      req.ConnectionID,
		ConnectionService: req.ConnectionService,
		Event:             req.Event,
		Input:             req.Input,
	})
	if err != nil {
		return err
	}
	if req.TargetKind == domain.ConnectionWebhookTargetChannel {
		// The drain outlives the delivery context: the acked webhook must
		// not bound the run's tail (the gateway drainTurn posture).
		go s.drainToChannel(context.WithoutCancel(ctx), req, stream)
	}
	// Thread targets need no delivery: the transcript IS the surface, slow
	// consumers never stall a run, and every subscriber stream closes when
	// the run ends. Dropping the stream reference is the documented
	// fire-and-forget posture.
	return nil
}

// actingUser resolves the member the event run executes under: the bound
// agent's creator when still a workspace member, else the first workspace
// owner. Every runner turn requires a real member — the scheduler resolves
// its schedule creator through exactly this shape (preflight); a
// connection has no creator of its own.
func (s *webhookRunSubmitter) actingUser(ctx context.Context, workspaceID, agentID string) (string, error) {
	agent, err := s.agents.ByID(ctx, workspaceID, agentID)
	if err != nil {
		return "", fmt.Errorf("resolve bound agent: %w", err)
	}
	if agent.CreatedBy != nil && *agent.CreatedBy != "" {
		if _, err := s.members.Get(ctx, workspaceID, *agent.CreatedBy); err == nil {
			return *agent.CreatedBy, nil
		} else if !errors.Is(err, domain.ErrNotFound) {
			return "", fmt.Errorf("resolve creator membership: %w", err)
		}
	}

	members, err := s.members.ListForWorkspace(ctx, workspaceID)
	if err != nil {
		return "", fmt.Errorf("list workspace members: %w", err)
	}
	for _, m := range members {
		role, err := s.roles.ByID(ctx, m.RoleID)
		if err != nil {
			continue
		}
		if role != nil && role.IsOwner {
			return m.UserID, nil
		}
	}
	return "", errors.New("no acting member: the bound agent has no live creator and the workspace has no owner")
}

// drainToChannel consumes the run's stream to EOF on a detached context and
// posts the final assistant text into the bound channel — the scheduler's
// drainRun delivery shape (delivery failure is never a run failure; the
// transcript remains the source of truth).
func (s *webhookRunSubmitter) drainToChannel(ctx context.Context, req webhooks.RunRequest, stream *agents.EventStream) {
	finalText := ""
	for {
		ev, err := stream.Recv()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				slog.WarnContext(ctx, "webhook run drain failed",
					"workspace_id", req.WorkspaceID, "connection_id", req.ConnectionID,
					"event", req.Event, "session_id", req.SessionID, "error", err)
			}
			break
		}
		if ev == nil {
			continue
		}
		if ev.Kind == agents.TranscriptEventMessageCompleted && ev.Message != nil &&
			ev.Message.Role == "assistant" && len(ev.Message.ToolCalls) == 0 &&
			strings.TrimSpace(ev.Message.Content) != "" {
			finalText = ev.Message.Content
		}
	}
	if strings.TrimSpace(finalText) == "" {
		return
	}
	if _, err := s.poster.PostFromAgent(ctx, req.WorkspaceID, req.TargetID, req.AgentID, finalText); err != nil {
		slog.WarnContext(ctx, "webhook channel delivery failed (the run transcript stands)",
			"workspace_id", req.WorkspaceID, "connection_id", req.ConnectionID,
			"event", req.Event, "channel_id", req.TargetID, "error", err)
	}
}
