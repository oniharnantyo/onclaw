package gateways

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"strings"
	"sync"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// Chat-reply copy for the orchestration paths this service owns (design
// D4/D7, add-whatsapp-gateway D3): the queued-turn acknowledgment, the
// queue-full refusal, the generic submission failure, the pending-approval
// refusal for button platforms, and its text-reply counterpart for
// platforms without buttons.
const (
	QueuedAckText   = "Queued — I'll run this right after the current turn finishes."
	QueueFullText   = "Too many messages are already waiting for this session. Please wait for the current turn to finish and send this again."
	TurnFailedText  = "The agent run could not be started. Please try again in a moment."
	PendingApproval = "An approval is pending on this session — use the Approve/Deny buttons on the card before sending new messages."
	// PendingApprovalTextReply is the notice on CanButton=false platforms,
	// where the decision itself arrives as the chat text APPROVE or DENY
	// (add-whatsapp-gateway design D3).
	PendingApprovalTextReply = "An approval is pending on this session — reply APPROVE or DENY to decide it before sending new messages."
)

// gatewayRegistration is one lifecycle-managed gateway: the workspace and
// platform coordinates captured when the adapter started, so inbound
// traffic can resolve its (always fresh) configuration.
type gatewayRegistration struct {
	workspaceID string
	platform    string
}

// Service is the platform-neutral gateway orchestrator: it implements
// InboundHandler for every registered adapter, routes messages through the
// Router, submits turns through the RunSubmitter with queue-on-busy through
// the BusyQueue, drains submitted runs through the Streamer, and hands
// approval interrupts and callbacks to the ApprovalBridge. Guards (loop
// guard, circuit breaker) close the loop (design D10).
type Service struct {
	router    *Router
	submitter RunSubmitter
	bridge    *ApprovalBridge
	gateways  store.GatewayStore
	queue     *BusyQueue
	loop      *LoopGuard
	breaker   *CircuitBreaker
	// outbox is the durable delivery record every drained turn's final
	// replies are committed to (design D9).
	outbox *Outbox

	// regMu guards the gateway registries, written by the lifecycle manager
	// on config changes and read on every inbound message and reply.
	regMu    sync.RWMutex
	adapters map[string]PlatformAdapter
	regs     map[string]gatewayRegistration

	// approvalMu guards approvalChats: the chat coordinates of the turn
	// each pending approval came from, so the resumed turn's stream can be
	// drained on the right chat when its card is answered (the bridge holds
	// the resume state; the service holds the drain coordinates).
	approvalMu    sync.Mutex
	approvalChats map[string]TurnPlan // session id → the plan that raised the interrupt
}

// ServiceOption customizes a Service.
type ServiceOption func(*Service)

// WithLoopGuard overrides the bot-loop guard (tests, tuned deployments).
func WithLoopGuard(g *LoopGuard) ServiceOption {
	return func(s *Service) { s.loop = g }
}

// WithCircuitBreaker overrides the gateway circuit breaker.
func WithCircuitBreaker(b *CircuitBreaker) ServiceOption {
	return func(s *Service) { s.breaker = b }
}

// WithBusyQueue overrides the busy queue. Tests drive it with their own
// callbacks; production uses the default instance wired to the service's
// own submission path (depth tuning via queue.WithQueueDepth).
func WithBusyQueue(q *BusyQueue) ServiceOption {
	return func(s *Service) { s.queue = q }
}

// NewService builds the gateway service. Every dependency is required: the
// composition root resolves the router, the run submitter, the approval
// bridge, the gateway store, and the delivery outbox before calling this
// (injected dependencies are never nil).
func NewService(
	router *Router,
	submitter RunSubmitter,
	bridge *ApprovalBridge,
	gateways store.GatewayStore,
	outbox *Outbox,
	opts ...ServiceOption,
) *Service {
	s := &Service{
		router:        router,
		submitter:     submitter,
		bridge:        bridge,
		gateways:      gateways,
		outbox:        outbox,
		loop:          NewLoopGuard(),
		breaker:       NewCircuitBreaker(),
		adapters:      make(map[string]PlatformAdapter),
		regs:          make(map[string]gatewayRegistration),
		approvalChats: make(map[string]TurnPlan),
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.queue == nil {
		s.queue = NewBusyQueue(s.resubmitQueued, s.sendQueuedNotice)
	}
	return s
}

// AttachGateway registers a started gateway: its adapter (for outbound
// replies and streaming) and its workspace/platform coordinates (for config
// resolution). Called by the lifecycle manager when a gateway starts;
// DetachGateway when it stops.
func (s *Service) AttachGateway(gatewayID, workspaceID, platform string, adapter PlatformAdapter) {
	s.regMu.Lock()
	defer s.regMu.Unlock()
	s.adapters[gatewayID] = adapter
	s.regs[gatewayID] = gatewayRegistration{workspaceID: workspaceID, platform: platform}
}

// DetachGateway removes a stopped gateway's registration and adapter.
func (s *Service) DetachGateway(gatewayID string) {
	s.regMu.Lock()
	defer s.regMu.Unlock()
	delete(s.adapters, gatewayID)
	delete(s.regs, gatewayID)
}

// Adapter returns the registered adapter for one gateway.
func (s *Service) Adapter(gatewayID string) (PlatformAdapter, bool) {
	s.regMu.RLock()
	defer s.regMu.RUnlock()
	a, ok := s.adapters[gatewayID]
	return a, ok
}

// AdapterForWorkspace resolves the attached adapter for a workspace's gateway
// on the given platform (task 6.2 composition: attachment ingress knows the
// workspace it ingests for but not the gateway id, and must always hit the
// workspace's own bot — Telegram file ids are bot-scoped).
func (s *Service) AdapterForWorkspace(workspaceID, platform string) (PlatformAdapter, bool) {
	s.regMu.RLock()
	defer s.regMu.RUnlock()
	for id, reg := range s.regs {
		if reg.workspaceID == workspaceID && reg.platform == platform {
			return s.adapters[id], true
		}
	}
	return nil, false
}

// SoleAttachedAdapter returns the only currently attached adapter, or an
// error when none or more than one is attached. The approval bridge delivers
// cards and refusals with chat coordinates only — no gateway identity — so
// its composition-side adapter delegate can route unambiguously only while a
// single gateway runs. Multi-gateway instances need gateway-aware resolution
// threaded through the bridge itself (reported as follow-up design work).
func (s *Service) SoleAttachedAdapter() (PlatformAdapter, error) {
	s.regMu.RLock()
	defer s.regMu.RUnlock()
	switch len(s.adapters) {
	case 1:
		for _, a := range s.adapters {
			return a, nil
		}
	case 0:
		return nil, fmt.Errorf("gateway adapter delegate: no gateway is attached")
	default:
		return nil, fmt.Errorf("gateway adapter delegate: %d gateways attached; approval-card delivery needs gateway-aware routing", len(s.adapters))
	}
	return nil, fmt.Errorf("gateway adapter delegate: unreachable")
}

// IngestionOpen reports whether the gateway accepts traffic (circuit breaker
// closed). There is no automatic reopen after a trip — only ResumeGateway.
func (s *Service) IngestionOpen(gatewayID string) bool {
	return s.breaker.Allow(gatewayID)
}

// ResumeGateway reopens ingestion after a breaker trip — the admin-only
// action (design D10), exposed to the handlers wave.
func (s *Service) ResumeGateway(gatewayID string) {
	s.breaker.Resume(gatewayID)
}

// RecordSendOutcome feeds the circuit breaker from the delivery side (the
// drain path records its sends here; outbox redelivery does the same).
func (s *Service) RecordSendOutcome(gatewayID string, err error) {
	if err == nil {
		s.breaker.RecordSuccess(gatewayID)
		return
	}
	if s.breaker.RecordFailure(gatewayID) {
		slog.Error("gateway circuit breaker tripped — ingestion paused until an admin resumes it",
			"gateway_id", gatewayID)
	}
}

// HandleMessage implements InboundHandler. It never blocks platform
// ingestion for long: routing and submission are quick, and the run's drain
// (streaming) happens on its own goroutine with a detached context.
func (s *Service) HandleMessage(ctx context.Context, gatewayID string, msg InboundMessage) {
	// Circuit breaker (design D10): an auto-paused gateway ingests nothing.
	if !s.breaker.Allow(gatewayID) {
		slog.Warn("gateway ingestion paused by circuit breaker, dropping message",
			"gateway_id", gatewayID, "chat_id", msg.ChatID)
		return
	}

	// Bot-loop guard (design D10): runaway bot-to-bot loops are dropped
	// after the threshold, with a cooldown.
	if !s.loop.Allow(msg) {
		slog.Warn("gateway bot-loop guard dropped message",
			"gateway_id", gatewayID, "chat_id", msg.ChatID)
		return
	}

	reg, ok := s.registration(gatewayID)
	if !ok {
		slog.Warn("gateway message for unregistered gateway", "gateway_id", gatewayID)
		return
	}

	// Fresh configuration on every message: an admin can disable a gateway,
	// switch its transport, or change its default agent between messages —
	// a stale snapshot would keep a disabled gateway talking.
	cfg, err := s.gateways.GetGateway(ctx, reg.workspaceID, gatewayID)
	if err != nil {
		slog.Error("gateway config lookup failed", "gateway_id", gatewayID, "err", err)
		return
	}
	if cfg == nil || !cfg.Enabled {
		// Disabled gateways are inert (spec: no run, no reply, config
		// preserved) — the lifecycle manager normally stops the adapter too;
		// this guard covers the race between disable and stop.
		return
	}

	res, err := s.router.Route(ctx, cfg, msg)
	if err != nil {
		slog.Error("gateway routing failed", "gateway_id", gatewayID, "chat_id", msg.ChatID, "err", err)
		s.reply(ctx, gatewayID, msg.ChatID, TurnFailedText)
		return
	}
	if res.Callback != nil {
		// A synthesized approval decision (add-whatsapp-gateway design D3):
		// the text-reply interception produces the same callback a button
		// press would have carried, so it rides the identical bridge path.
		s.HandleCallback(ctx, gatewayID, *res.Callback)
		return
	}
	if res.Reply != nil {
		s.reply(ctx, gatewayID, msg.ChatID, res.Reply.Text)
	}
	if res.Turn != nil {
		s.submitTurn(ctx, *res.Turn)
	}
}

// HandleCallback implements InboundHandler: inline-keyboard presses go to
// the approval bridge (design D7) after the same breaker gate as messages.
// A successful resume is drained like any other turn.
func (s *Service) HandleCallback(ctx context.Context, gatewayID string, cb Callback) {
	if !s.breaker.Allow(gatewayID) {
		slog.Warn("gateway ingestion paused by circuit breaker, dropping callback",
			"gateway_id", gatewayID)
		return
	}

	stream, err := s.bridge.HandleCallback(ctx, cb)
	if err != nil {
		if !errors.Is(err, ErrApprovalNotPending) && !errors.Is(err, ErrApprovalUnpaired) {
			slog.Error("gateway approval callback failed", "gateway_id", gatewayID, "err", err)
		}
		return
	}

	// The bridge cleared its pending state — find the turn coordinates this
	// approval came from and drain the resumed turn.
	interruptID, _, decoded := DecodeApprovalCallback(cb.Data)
	if !decoded {
		return
	}
	s.approvalMu.Lock()
	plan, ok := s.approvalChats[approvalDrainKey(cb.ChatID, interruptID)]
	delete(s.approvalChats, approvalDrainKey(cb.ChatID, interruptID))
	s.approvalMu.Unlock()
	if !ok {
		slog.Warn("gateway approval resumed without drain coordinates; transcript persists but live delivery is dropped",
			"gateway_id", gatewayID)
		return
	}
	plan.GatewayID = gatewayID
	s.drainTurn(ctx, plan, stream)
}

// approvalDrainKey keys the drain-coordinate lookup: the approval card's
// chat plus its interrupt id — exactly the pair the approval bridge matches
// callbacks by.
func approvalDrainKey(chatID, interruptID string) string {
	return chatID + "|" + interruptID
}

// registration returns the workspace/platform coordinates for a gateway id.
func (s *Service) registration(gatewayID string) (gatewayRegistration, bool) {
	s.regMu.RLock()
	defer s.regMu.RUnlock()
	reg, ok := s.regs[gatewayID]
	return reg, ok
}

// originFor maps a gateway platform to the run origin stamped on its
// ExecRequests (add-whatsapp-gateway design D10): WhatsApp runs carry
// OriginWhatsApp; everything else keeps OriginTelegram, the historical value.
func originFor(platform string) string {
	if platform == domain.GatewayPlatformWhatsApp {
		return OriginWhatsApp
	}
	return OriginTelegram
}

// submitTurn runs one routed turn: submit, queue on the busy guard, or
// surface a failure. It is also the busy queue's resubmit path.
func (s *Service) submitTurn(ctx context.Context, plan TurnPlan) {
	if plan.Command == "" && s.bridge.Pending(plan.SessionID) {
		// Pending approval blocks new turns (spec) — refuse earlier with the
		// friendly notice instead of hitting the runner's busy guard.
		s.reply(ctx, plan.GatewayID, plan.ChatID, PendingApproval)
		return
	}

	req := agents.ExecRequest{
		WorkspaceID: plan.WorkspaceID,
		AgentID:     plan.AgentID,
		SessionID:   plan.SessionID,
		UserID:      plan.UserID,
		Input:       plan.Input,
		Origin:      s.runOrigin(plan.GatewayID),
		Command:     plan.Command,
		Attachments: plan.Attachments,
	}
	stream, err := s.submitter.Run(ctx, req)
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			// The one-run-per-session guard rejected the submission (design
			// D4): queue the message and acknowledge it — never fail.
			if !s.queue.Enqueue(ctx, plan, QueuedAckText) {
				s.reply(ctx, plan.GatewayID, plan.ChatID, QueueFullText)
			}
			return
		}
		slog.Error("gateway run submission failed", "session_id", plan.SessionID, "err", err)
		s.reply(ctx, plan.GatewayID, plan.ChatID, TurnFailedText)
		return
	}

	s.drainTurn(ctx, plan, stream)
}

// runOrigin resolves the ExecRequest origin for one gateway id: the
// gateway's platform mapped through originFor (design D10). Unregistered
// gateways keep OriginTelegram, the historical value.
func (s *Service) runOrigin(gatewayID string) string {
	if reg, ok := s.registration(gatewayID); ok {
		return originFor(reg.platform)
	}
	return OriginTelegram
}

// resubmitQueued is the busy queue's resubmit callback: a queued turn goes
// through the normal submission path, so conflict re-queues it and success
// hands the stream to the drain.
func (s *Service) resubmitQueued(ctx context.Context, plan TurnPlan) error {
	req := agents.ExecRequest{
		WorkspaceID: plan.WorkspaceID,
		AgentID:     plan.AgentID,
		SessionID:   plan.SessionID,
		UserID:      plan.UserID,
		Input:       plan.Input,
		Origin:      s.runOrigin(plan.GatewayID),
		Command:     plan.Command,
		Attachments: plan.Attachments,
	}
	stream, err := s.submitter.Run(ctx, req)
	if err != nil {
		return err
	}
	s.drainTurn(ctx, plan, stream)
	return nil
}

// sendQueuedNotice is the busy queue's chat-notification callback.
func (s *Service) sendQueuedNotice(ctx context.Context, plan TurnPlan, text string) {
	s.reply(ctx, plan.GatewayID, plan.ChatID, text)
}

// drainTurn consumes one submitted run's stream on a detached context: the
// webhook request that carried the message must not bound the turn's live
// delivery. A drain that pauses on an approval interrupt presents the card
// and leaves the session blocked (no queue release) until the card is
// answered; every other outcome releases the next queued turn (design D4).
func (s *Service) drainTurn(ctx context.Context, plan TurnPlan, stream *agents.EventStream) {
	runCtx := context.WithoutCancel(ctx)
	req := agents.ExecRequest{
		WorkspaceID: plan.WorkspaceID,
		AgentID:     plan.AgentID,
		SessionID:   plan.SessionID,
		UserID:      plan.UserID,
		Input:       plan.Input,
		Origin:      s.runOrigin(plan.GatewayID),
		Command:     plan.Command,
		Attachments: plan.Attachments,
	}

	go func() {
		adapter, ok := s.Adapter(plan.GatewayID)
		if !ok {
			slog.Error("gateway drain with no attached adapter", "gateway_id", plan.GatewayID)
			return
		}
		session := StreamSession{
			GatewayID:   plan.GatewayID,
			WorkspaceID: plan.WorkspaceID,
			SessionID:   plan.SessionID,
			ChatID:      plan.ChatID,
		}
		// The render flavor follows the gateway's platform
		// (add-whatsapp-gateway design D9); unregistered gateways default to
		// Telegram, the historical wire format.
		flavor := TelegramFlavor
		if reg, ok := s.registration(plan.GatewayID); ok {
			flavor = platformRenderFlavor(reg.platform)
		}
		streamer := NewStreamer(adapter, s.outbox, WithRenderFlavor(flavor))
		res := streamer.Stream(runCtx, session, stream)

		if res.Err != nil {
			s.RecordSendOutcome(plan.GatewayID, res.Err)
			slog.Error("gateway turn delivery failed", "session_id", plan.SessionID, "err", res.Err)
		}

		if res.Approval != nil {
			if err := s.bridge.Present(runCtx, session, req, *res.Approval); err != nil {
				slog.Error("gateway approval card failed", "session_id", plan.SessionID, "err", err)
				// The turn is paused with nowhere to answer: still release
				// the queue? No — the run is live-but-paused; resubmitting
				// would hit the busy guard or interleave turns. Leave the
				// session blocked; the card path stays the only way forward.
				return
			}
			s.approvalMu.Lock()
			s.approvalChats[approvalDrainKey(plan.ChatID, res.Approval.InterruptID)] = plan
			s.approvalMu.Unlock()
			return
		}

		// Terminal outcome observed via the drain (design D4): release the
		// next queued turn.
		s.NotifyRunFinished(runCtx, plan.SessionID)
	}()
}

// NotifyRunFinished releases the next queued turn for the session whose run
// just reached a terminal outcome (design D4).
func (s *Service) NotifyRunFinished(ctx context.Context, sessionID string) {
	s.queue.NotifyRunFinished(ctx, sessionID)
}

// reply sends a plain-text chat reply through the gateway's adapter. The
// text is rendered in the gateway platform's flavor — escaped for the
// Telegram HTML wire, passed through for markdown flavors — and the flavor
// tag rides the send so a misrouted adapter refuses it. Empty text is a
// deliberate silent drop (e.g. unbound-group chatter).
func (s *Service) reply(ctx context.Context, gatewayID, chatID, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	adapter, ok := s.Adapter(gatewayID)
	if !ok {
		slog.Warn("gateway reply with no attached adapter", "gateway_id", gatewayID)
		return
	}
	flavor := TelegramFlavor
	if reg, ok := s.registration(gatewayID); ok {
		flavor = platformRenderFlavor(reg.platform)
	}
	body := text
	if flavor.Name == FlavorTelegramHTML {
		body = html.EscapeString(text)
	}
	_, err := adapter.SendMessage(ctx, chatID, body, flavor.Name, SendOptions{DisablePreview: true})
	s.RecordSendOutcome(gatewayID, err)
	if err != nil {
		slog.Error("gateway reply failed", "gateway_id", gatewayID, "chat_id", chatID, "err", err)
	}
}

// -------------------------------------------------------------------------
// SessionUsageReader default implementation
// -------------------------------------------------------------------------

// sessionUsageReader reads /usage's context-meter numbers from the durable
// session-event log: it aggregates the span.model_request_end events of the
// session's last turn (the same events agents.History projects its
// turn_completed usage from), tolerantly decoded from the ADK serializer's
// JSON.
type sessionUsageReader struct {
	events store.SessionEventStore
}

// NewSessionUsageReader builds the /usage reader over the session-event log.
func NewSessionUsageReader(events store.SessionEventStore) SessionUsageReader {
	return &sessionUsageReader{events: events}
}

// LatestUsage reports the last turn's aggregated usage, or (nil, nil) when
// the session has no model spans yet.
func (r *sessionUsageReader) LatestUsage(ctx context.Context, workspaceID, sessionID string) (*agents.UsagePayload, error) {
	rows, err := r.events.LoadEvents(ctx, store.LoadSessionEventsParams{
		WorkspaceID: workspaceID,
		SessionID:   sessionID,
		Kinds:       []string{"span.model_request_end"},
		Limit:       100,
		Reverse:     true,
	})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}

	// Rows come newest-first; the session's last turn is the newest row's
	// turn id. Aggregate that turn's spans oldest-first.
	lastTurn := rows[0].TurnID
	var usage agents.UsagePayload
	haveAny := false
	for i := len(rows) - 1; i >= 0; i-- {
		row := rows[i]
		if row.TurnID != lastTurn {
			continue
		}
		mu, ok := decodeSpanUsage(row.Payload)
		if !ok {
			continue
		}
		haveAny = true
		usage.InputTokens += mu.InputTokens
		usage.OutputTokens += mu.OutputTokens
		usage.TotalTokens += mu.TotalTokens
		// FinalInputTokens is the input size of the LAST call of the turn —
		// set, never summed (agents events.go contract).
		usage.FinalInputTokens = mu.InputTokens
	}
	if !haveAny {
		return nil, nil
	}
	return &usage, nil
}

// spanUsageJSON is the tolerant decode shape of a span.model_request_end
// payload: the ADK HumanReadableSerializer JSON carries the usage under
// span.model.usage, with the provider-agnostic numbers on input/output and
// the provider-raw totals under raw.
type spanUsageJSON struct {
	Span *struct {
		Model *struct {
			Usage *struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
				Raw          *struct {
					TotalTokens int `json:"total_tokens"`
				} `json:"raw"`
			} `json:"usage"`
		} `json:"model"`
	} `json:"span"`
}

func decodeSpanUsage(payload []byte) (agents.UsagePayload, bool) {
	var decoded spanUsageJSON
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return agents.UsagePayload{}, false
	}
	if decoded.Span == nil || decoded.Span.Model == nil || decoded.Span.Model.Usage == nil {
		return agents.UsagePayload{}, false
	}
	u := decoded.Span.Model.Usage
	out := agents.UsagePayload{
		InputTokens:  u.InputTokens,
		OutputTokens: u.OutputTokens,
	}
	if u.Raw != nil {
		out.TotalTokens = u.Raw.TotalTokens
	}
	return out, true
}
