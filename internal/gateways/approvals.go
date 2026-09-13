package gateways

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// Approval-bridge sentinels. They are distinct from the pairing service's
// errors: the bridge reports why a *callback* could not resume a turn.
var (
	// ErrApprovalPending reports that the session already has an unanswered
	// approval card — new turns are refused while it is pending (design D7).
	ErrApprovalPending = errors.New("an approval is pending on this session; answer the approval card first")
	// ErrApprovalNotPending reports a callback that does not address any
	// unanswered approval card (stale, repeated, or forged callback data).
	ErrApprovalNotPending = errors.New("no pending approval matches this callback")
	// ErrApprovalUnpaired reports a callback from a platform identity that is
	// not a paired workspace member (design D7: only paired members may
	// approve).
	ErrApprovalUnpaired = errors.New("only paired members may answer approval requests")
)

// Callback decision encoding. The card's buttons are minted by the adapter,
// but the platform-neutral contract is fixed here: callback data for the two
// buttons is EncodeApprovalCallback(interruptID, true|false), and any other
// value is refused. The encoding needs no nonce because a callback is only
// honored while its interrupt is the chat's pending card.
const (
	approvalCallbackApprove = "approve:"
	approvalCallbackDeny    = "deny:"
)

// EncodeApprovalCallback returns the inline-keyboard callback data for one
// decision on an interrupt. Adapters must use it verbatim as the button's
// callback_data.
func EncodeApprovalCallback(interruptID string, approved bool) string {
	if approved {
		return approvalCallbackApprove + interruptID
	}
	return approvalCallbackDeny + interruptID
}

// DecodeApprovalCallback parses callback data minted by EncodeApprovalCallback.
func DecodeApprovalCallback(data string) (interruptID string, approved bool, ok bool) {
	switch {
	case strings.HasPrefix(data, approvalCallbackApprove):
		return strings.TrimPrefix(data, approvalCallbackApprove), true, true
	case strings.HasPrefix(data, approvalCallbackDeny):
		return strings.TrimPrefix(data, approvalCallbackDeny), false, true
	default:
		return "", false, false
	}
}

// pendingApproval is one unanswered approval card.
type pendingApproval struct {
	workspaceID   string
	chatID        string
	req           agents.ExecRequest
	payload       agents.ApprovalPayload
	cardMessageID string
}

// ApprovalBridge turns approval interrupts into inline-keyboard cards and
// button presses back into resumed turns (design D7). While a card is
// unanswered for a session, new turns on that session are refused — the
// runner serializes turns per session anyway; the bridge just refuses
// earlier with a friendlier message.
type ApprovalBridge struct {
	submitter RunSubmitter
	adapter   PlatformAdapter
	links     store.GatewayLinks

	mu      sync.Mutex
	pending map[string]pendingApproval // key: session id
}

// NewApprovalBridge creates an ApprovalBridge over the runner seam, the
// platform adapter, and the workspace pairing store.
func NewApprovalBridge(submitter RunSubmitter, adapter PlatformAdapter, links store.GatewayLinks) *ApprovalBridge {
	return &ApprovalBridge{
		submitter: submitter,
		adapter:   adapter,
		links:     links,
		pending:   make(map[string]pendingApproval),
	}
}

// Present renders the approval card for a turn the streamer paused and marks
// the session pending. The card's message id is kept so the decision can be
// recorded on it later.
func (b *ApprovalBridge) Present(ctx context.Context, session StreamSession, req agents.ExecRequest, approval agents.ApprovalPayload) error {
	cardID, err := b.adapter.SendApprovalCard(ctx, session.ChatID, approval)
	if err != nil {
		return fmt.Errorf("gateway approvals: send card: %w", err)
	}
	b.mu.Lock()
	b.pending[session.SessionID] = pendingApproval{
		workspaceID:   session.WorkspaceID,
		chatID:        session.ChatID,
		req:           req,
		payload:       approval,
		cardMessageID: cardID,
	}
	b.mu.Unlock()
	return nil
}

// Pending reports whether the session has an unanswered approval card.
func (b *ApprovalBridge) Pending(sessionID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.pending[sessionID]
	return ok
}

// HandleCallback validates an approval button press and, when it is
// legitimate, resumes the interrupted turn (the same path the web UI uses).
// The actor must be a paired member of the workspace the card belongs to,
// and the callback data must address the chat's pending interrupt. On
// success the card is updated to record the decision and actor, the pending
// state is cleared, and the resumed turn's stream is returned for the caller
// to drain.
func (b *ApprovalBridge) HandleCallback(ctx context.Context, cb Callback) (*agents.EventStream, error) {
	interruptID, approved, ok := DecodeApprovalCallback(cb.Data)
	if !ok {
		return nil, fmt.Errorf("%w: unrecognized callback data", ErrApprovalNotPending)
	}

	b.mu.Lock()
	var found *pendingApproval
	var sessionKey string
	for key, p := range b.pending {
		if p.chatID == cb.ChatID && p.payload.InterruptID == interruptID {
			p := p
			found = &p
			sessionKey = key
			break
		}
	}
	b.mu.Unlock()

	if found == nil {
		return nil, ErrApprovalNotPending
	}

	// Default-deny: only paired members of the card's workspace may answer.
	link, err := b.links.GetUserLink(ctx, found.workspaceID, cb.Platform, cb.FromUserID)
	if err != nil || link == nil {
		slog.Warn("gateway approvals: callback from unpaired identity refused",
			"chat", cb.ChatID, "platform_user", cb.FromUserID, "error", err)
		b.refuse(ctx, cb, "Only paired members can answer approval requests.")
		return nil, ErrApprovalUnpaired
	}

	stream, err := b.submitter.Resume(ctx, found.req, found.payload, approved)
	if err != nil {
		// The interrupt stays pending: it may still be resumable, and the
		// card remains the only path forward.
		slog.Error("gateway approvals: resume failed", "interrupt", interruptID, "error", err)
		b.editCard(ctx, cb.ChatID, found.cardMessageID, approvalCardHTML(approved, cb, true))
		return nil, fmt.Errorf("gateway approvals: resume: %w", err)
	}

	b.mu.Lock()
	delete(b.pending, sessionKey)
	b.mu.Unlock()
	b.editCard(ctx, cb.ChatID, found.cardMessageID, approvalCardHTML(approved, cb, false))
	return stream, nil
}

// refuse posts a refusal notice (best-effort).
func (b *ApprovalBridge) refuse(ctx context.Context, cb Callback, text string) {
	if _, err := b.adapter.SendMessage(ctx, cb.ChatID, RenderTelegramHTML(text), SendOptions{DisablePreview: true}); err != nil {
		slog.Warn("gateway approvals: refusal notice failed", "chat", cb.ChatID, "error", err)
	}
}

// editCard rewrites the approval card message (best-effort).
func (b *ApprovalBridge) editCard(ctx context.Context, chatID, messageID, html string) {
	if err := b.adapter.EditMessage(ctx, chatID, messageID, html); err != nil {
		slog.Warn("gateway approvals: card update failed", "chat", chatID, "error", err)
	}
}

// actorLabel names the callback actor from the platform user id (Callback
// carries no display name; the id is the identity anyway — design D6).
func actorLabel(cb Callback) string {
	if cb.FromUserID == "" {
		return "a paired member"
	}
	return "member linked to platform user " + cb.FromUserID
}

// approvalCardHTML renders the card's post-decision body: the command, the
// decision, and the actor, with a resume-failure caveat when needed.
func approvalCardHTML(approved bool, cb Callback, resumeFailed bool) string {
	var sb strings.Builder
	sb.WriteString("Shell approval ")
	if approved {
		sb.WriteString("✅ approved")
	} else {
		sb.WriteString("⛔ denied")
	}
	sb.WriteString(" by " + actorLabel(cb))
	if resumeFailed {
		sb.WriteString(" — resume failed; the card stays open, try again")
	}
	sb.WriteString(".")
	return RenderTelegramHTML(sb.String())
}
