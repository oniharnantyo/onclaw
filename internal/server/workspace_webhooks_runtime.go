package server

// Standalone workspace webhook runtime (add-workspace-webhooks tasks 5.1/5.2,
// design.md D6): the composition-owned bundle that assembles the standalone
// management service and the endpoint-ingress pipeline around the runner —
// the WebhookRuntime pattern (one pipeline, two ingress addresses, two
// submitter bindings). Built by the CLI composition root and shared with the
// HTTP layer through RouterOptions; the router's nil fallback builds a fresh
// one for test assembly.
//
// Seams that live here because they belong to composition, not to the
// pipeline core:
//   - the StandaloneRunnerPort adapter: *agents.Runner satisfies the port
//     through this wrapper, which resolves the event run's mechanical acting
//     identity (the bound agent's creator, falling back to the first
//     workspace owner — the connection-lane submitter's exact posture) and
//     stamps the run's service-authority attribution with the endpoint's
//     id and name (ExecRequest.WebhookID/WebhookName, D6),
//   - the detached run drain (add-webhook-channel-visibility D5): every
//     run's tap is consumed to EOF on a detached context, finishing the
//     delivery-log row's run telemetry (status, duration, tokens — the
//     scheduler drain's semantics) for all targets, and posting the final
//     assistant text into the bound channel through the chokepoint's
//     PostFromAgent for channel targets (thread targets need no delivery:
//     the transcript is the surface),
//   - the secret cipher over the instance master key, the per-endpoint rate
//     limiter, and the standalone-lane queue's lifecycle (Close rides
//     shutdown).

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/webhooks"
)

// WorkspaceWebhookRuntime bundles the standalone webhook management service,
// the endpoint ingress, and the standalone delivery queue.
type WorkspaceWebhookRuntime struct {
	Service *webhooks.StandaloneService
	Ingress *webhooks.StandaloneIngress
	Queue   *webhooks.Queue
}

// NewWorkspaceWebhookRuntime assembles the standalone webhook runtime around
// the runner: granular stores, the key-bound secret cipher, the per-endpoint
// rate limiter, its own bounded queue whose process func is the standalone
// processor (design.md D6 — the connection lane's queue is untouched), and
// the run submitter adapter. Injected dependencies are never nil. The queue's
// processing closure reads the runtime's ingress — the dispatcher starts
// lazily on the first enqueue, always after this constructor returns. The
// connection-lane Service doubles as the D2 read-in's StateForView seam.
func NewWorkspaceWebhookRuntime(
	workspaceWebhooks store.WorkspaceWebhookStore,
	connections store.Connections,
	connectionViews webhooks.ConnectionWebhookViewSource,
	agentsStore store.AgentStore,
	members store.MemberStore,
	roles store.RoleStore,
	runner webhookExecRunner,
	poster webhookChannelPoster,
	encryptionKey []byte,
) *WorkspaceWebhookRuntime {
	rt := &WorkspaceWebhookRuntime{}
	cipher := webhooks.NewAESGCMCipher(encryptionKey)
	rt.Service = webhooks.NewStandaloneService(workspaceWebhooks, cipher, connections, connectionViews)
	rt.Queue = webhooks.NewQueue(DefaultWebhookQueueDepth, func(ctx context.Context, job *webhooks.Delivery) {
		rt.Ingress.ProcessDelivery(ctx, job)
	}, webhooks.WithPerConnectionLimit(webhooks.DefaultPerConnectionLimit))
	limiter := webhooks.NewRateLimiter(webhooks.StandaloneRateLimit, webhooks.StandaloneRateWindow)
	rt.Ingress = webhooks.NewStandaloneIngress(
		workspaceWebhooks,
		cipher,
		limiter,
		rt.Queue,
		&workspaceWebhookRunSubmitter{
			runner:   runner,
			agents:   agentsStore,
			members:  members,
			roles:    roles,
			poster:   poster,
			webhooks: workspaceWebhooks,
		},
	)
	return rt
}

// Close stops the standalone delivery queue and waits for in-flight
// processing — the shutdown seam the composition root calls after the run
// drain.
func (rt *WorkspaceWebhookRuntime) Close() {
	rt.Queue.Close()
}

// webhookExecRunner is the run seam the standalone submitter needs: the
// runner's Run signature (*agents.Runner satisfies it structurally, the
// chokepoint's RunSubmitter shape). The narrow interface is the test seam —
// a fake records the ExecRequest the attribution assertions read.
type webhookExecRunner interface {
	Run(ctx context.Context, req agents.ExecRequest) (*agents.EventStream, error)
}

// workspaceWebhookRunSubmitter adapts the runner to webhooks.StandaloneRunnerPort
// (design.md D6): it resolves the acting identity the runner mechanically
// requires (event runs carry no requesting user; the bound agent's creator
// acts, falling back to the first workspace owner), stamps the first-class
// service origin with the endpoint's attribution (ExecRequest.WebhookID/
// WebhookName — the connection lane leaves them empty), and owns the stream
// drain for every target: run telemetry finished onto the delivery-log row
// (design.md D5) plus channel-target delivery of the final assistant text.
type workspaceWebhookRunSubmitter struct {
	runner   webhookExecRunner
	agents   store.AgentStore
	members  store.MemberStore
	roles    store.RoleStore
	poster   webhookChannelPoster
	webhooks store.WorkspaceWebhookStore
}

// RunWebhookEvent implements webhooks.StandaloneRunnerPort. The returned run
// id — the delivery log's link to the run (design.md D8) — is the run's
// transcript session: the runner mints turn ids internally and its Run
// returns only the stream, so the session id is the stable handle (the
// scheduler's run-records posture: SchedulerRun links its run via
// SessionID).
func (s *workspaceWebhookRunSubmitter) RunWebhookEvent(ctx context.Context, req webhooks.StandaloneRunRequest) (string, error) {
	userID, err := s.actingUser(ctx, req.WorkspaceID, req.AgentID)
	if err != nil {
		return "", fmt.Errorf("workspace webhook run submitter: resolve acting identity: %w", err)
	}

	stream, err := s.runner.Run(ctx, agents.ExecRequest{
		WorkspaceID: req.WorkspaceID,
		AgentID:     req.AgentID,
		SessionID:   req.SessionID,
		UserID:      userID,
		// OriginService is the service-authority run's origin: first-class in
		// the runner's origin normalizer, so traces and hook events see
		// "service" instead of a fabricated user attribution. The acting user
		// satisfies ExecRequest's mechanical non-empty-user requirement only;
		// the endpoint id/name and event name the real trigger.
		Origin:      webhooks.OriginService,
		WebhookID:   req.WebhookID,
		WebhookName: req.WebhookName,
		Event:       req.Event,
		Input:       req.Input,
	})
	if err != nil {
		return "", err
	}
	// Every run drains on a detached context (design.md D5): the drain
	// finishes the delivery-log row's run telemetry for all targets and
	// posts the final assistant text for channel targets. The acked webhook
	// must not bound the run's tail (the connection-lane drain posture);
	// the tap is best-effort with drop-new semantics, so the drain can
	// never stall the run.
	go s.drainRun(context.WithoutCancel(ctx), req, stream)
	return req.SessionID, nil
}

// actingUser resolves the member the event run executes under: the bound
// agent's creator when still a workspace member, else the first workspace
// owner — the connection-lane submitter's exact resolution (the scheduler's
// creator precedent; an endpoint has no creator of its own).
func (s *workspaceWebhookRunSubmitter) actingUser(ctx context.Context, workspaceID, agentID string) (string, error) {
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

// drainRun consumes one run's event tap to EOF on a detached context and
// finishes what the run owed (design.md D5): the delivery-log row's run
// telemetry — status, duration, tokens, derived with the scheduler drain's
// exact semantics — for every target, plus the final assistant text posted
// into the bound channel for channel targets. Telemetry and delivery
// failures are never run failures; the transcript remains the source of
// truth.
func (s *workspaceWebhookRunSubmitter) drainRun(ctx context.Context, req webhooks.StandaloneRunRequest, stream *agents.EventStream) {
	started := time.Now()

	finalText := ""
	tokens := 0
	status := ""
	for {
		ev, err := stream.Recv()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				slog.WarnContext(ctx, "standalone webhook run drain failed",
					"workspace_id", req.WorkspaceID, "webhook_id", req.WebhookID,
					"event", req.Event, "session_id", req.SessionID, "error", err)
			}
			break
		}
		if ev == nil {
			continue
		}
		if ev.Usage != nil {
			// Provider usage accumulates over the run; the last frame is
			// the total (the scheduler drain's accounting).
			tokens = ev.Usage.TotalTokens
		}
		if ev.Kind == agents.TranscriptEventMessageCompleted && ev.Message != nil &&
			ev.Message.Role == "assistant" && len(ev.Message.ToolCalls) == 0 &&
			strings.TrimSpace(ev.Message.Content) != "" {
			finalText = ev.Message.Content
		}
		switch ev.Kind {
		case agents.TranscriptEventTurnCompleted:
			status = domain.WorkspaceWebhookRunStatusCompleted
		case agents.TranscriptEventError:
			status = domain.WorkspaceWebhookRunStatusFailed
		case agents.TranscriptEventCancelled:
			status = domain.WorkspaceWebhookRunStatusCancelled
		}
	}
	if status == "" {
		// The tap ended without a terminal event: success-by-default
		// matches the fanout (approval interrupts cannot reach unattended
		// runs; the scheduler drain's same default).
		status = domain.WorkspaceWebhookRunStatusCompleted
	}

	// The row finish is best-effort (design.md D5): a telemetry write
	// failure never surfaces — the structured log carries it instead.
	if err := s.webhooks.FinishDeliveryRun(ctx, req.WorkspaceID, req.SessionID, status, time.Since(started).Milliseconds(), tokens); err != nil {
		slog.WarnContext(ctx, "standalone webhook run telemetry finish failed (best-effort)",
			"workspace_id", req.WorkspaceID, "webhook_id", req.WebhookID,
			"session_id", req.SessionID, "error", err)
	}

	if req.TargetKind != domain.ConnectionWebhookTargetChannel || strings.TrimSpace(finalText) == "" {
		return
	}
	if _, err := s.poster.PostFromAgent(ctx, req.WorkspaceID, req.TargetID, req.AgentID, finalText); err != nil {
		slog.WarnContext(ctx, "standalone webhook channel delivery failed (the run transcript stands)",
			"workspace_id", req.WorkspaceID, "webhook_id", req.WebhookID,
			"event", req.Event, "channel_id", req.TargetID, "error", err)
	}
}
