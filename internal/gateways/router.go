package gateways

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// OriginTelegram is the ExecRequest.Origin value for gateway-submitted runs.
// The runner's origin normalizer currently coerces unknown values to
// OriginUser, so behavior is identical until the runner integration wave
// lands agents.OriginTelegram (integrate-telegram-gateway task 6.3); this
// constant is defined here, not in agents, to keep the gateway package
// dependency-shaped like an ordinary ingress client.
const OriginTelegram = "telegram"

// TurnPlan is one routed turn ready for submission: everything
// ExecRequest needs plus the chat coordinates the delivery side uses.
// Channel coordinates are deliberately absent — gateway turns never touch
// channels or work sessions (spec: "No channel artifacts").
type TurnPlan struct {
	GatewayID   string
	WorkspaceID string
	AgentID     string
	SessionID   string
	UserID      string
	Input       string
	Command     string
	Attachments []agents.AttachmentRef
	Kind        InboundKind
	// ChatID is the platform chat the turn came from (delivery target).
	ChatID string
	// MessageID is the inbound platform message (for reply threading and
	// dedup on the delivery side).
	MessageID string
}

// ReplyPlan is a plain-text chat reply produced by routing (pairing hints,
// command confirmations, refusals). Sent as-is — the service HTML-escapes it
// for the wire.
type ReplyPlan struct {
	Text string
}

// RouteResult is the outcome of routing one inbound message: at most one of
// Turn or Reply is set; both nil means the message is dropped silently.
type RouteResult struct {
	Turn  *TurnPlan
	Reply *ReplyPlan
}

// SessionUsageReader supplies the /usage command its context-meter numbers.
type SessionUsageReader interface {
	// LatestUsage reports the most recent turn's usage for the session, or
	// (nil, nil) when the session has no usage yet.
	LatestUsage(ctx context.Context, workspaceID, sessionID string) (*agents.UsagePayload, error)
}

// IngressStage is the optional attachment/voice normalization stage
// (ingress.go): it turns an inbound message's media into turn input
// adjustments and attachment refs, or produces a refusal (oversized file,
// voice without STT). The default stage passes text through untouched.
type IngressStage interface {
	// Enrich returns the turn input text, attachment refs, and — when the
	// message cannot participate in a turn — a refusal reply. An empty
	// refusal means the message is accepted.
	Enrich(ctx context.Context, workspaceID string, msg InboundMessage) (text string, refs []agents.AttachmentRef, refusal string, err error)
}

// Router resolves an inbound platform message onto a workspace agent
// session (design D2/D3/D6): DMs route to the sender's per-user default
// agent with fallback to the gateway default, groups to their single bound
// agent; unpaired senders are refused with a hint; the built-in gateway
// commands (/start, /new, /compact, /usage, /agent) are parsed here, and
// unknown commands ride through as ordinary turn input so agent-defined
// slash commands keep working.
type Router struct {
	gateways store.GatewayStore
	bindings store.GatewayBindings
	links    store.GatewayLinks
	agents   store.AgentStore
	members  store.MemberStore
	users    store.UserStore
	pairing  *PairingService
	usage    SessionUsageReader
	ingress  IngressStage
}

// RouterOption customizes a Router.
type RouterOption func(*Router)

// WithIngress attaches the attachment/voice ingress stage. Without it the
// router passes message text through untouched and ignores media.
func WithIngress(stage IngressStage) RouterOption {
	return func(r *Router) { r.ingress = stage }
}

// NewRouter builds the router. Every dependency is required and resolved by
// the composition root (injected dependencies are never nil); pairing and
// the usage reader are granular ports, never a store aggregate.
func NewRouter(
	gateways store.GatewayStore,
	bindings store.GatewayBindings,
	links store.GatewayLinks,
	agentsStore store.AgentStore,
	members store.MemberStore,
	users store.UserStore,
	pairing *PairingService,
	usage SessionUsageReader,
	opts ...RouterOption,
) *Router {
	r := &Router{
		gateways: gateways,
		bindings: bindings,
		links:    links,
		agents:   agentsStore,
		members:  members,
		users:    users,
		pairing:  pairing,
		usage:    usage,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Route resolves one inbound message against its gateway's workspace.
// The gateway config must be enabled (the service checks before calling).
func (r *Router) Route(ctx context.Context, gateway *domain.GatewayConfig, msg InboundMessage) (*RouteResult, error) {
	workspaceID := gateway.WorkspaceID

	// migrate_to_chat_id service message (design D10): rewrite the binding's
	// platform_chat_id and drop the message itself. Session keys are derived,
	// never stored, so they are not remapped — the old tg_group_<old>_ key is
	// abandoned and the next message naturally starts (or resumes) on the
	// new-id key.
	if msg.MigrateToChatID != "" {
		err := r.bindings.RemapBindingChat(ctx, workspaceID, msg.Platform, msg.ChatID, msg.MigrateToChatID)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return nil, fmt.Errorf("gateway router: remap binding %s %s → %s: %w", msg.ChatID, msg.Platform, msg.MigrateToChatID, err)
		}
		return &RouteResult{}, nil
	}

	// /start <token> is the unpaired sender's only way in: it must reach the
	// pairing service before the default-deny identity gate below.
	if cmd, args := parseCommand(msg.Text); cmd == "start" {
		return r.routeCommand(ctx, gateway, msg, nil, cmd, args)
	}

	// Default-deny identity: only paired members proceed. Keyed on the
	// immutable platform user id.
	link, err := r.links.GetUserLink(ctx, workspaceID, msg.Platform, msg.FromUserID)
	if err != nil {
		return nil, fmt.Errorf("gateway router: resolve link: %w", err)
	}
	if link == nil {
		return &RouteResult{Reply: &ReplyPlan{Text: UnpairedSenderHint()}}, nil
	}

	// A stale pairing (member removed from the workspace after pairing) must
	// not mint runs: verify the membership still exists.
	if _, err := r.members.Get(ctx, workspaceID, link.UserID); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return &RouteResult{Reply: &ReplyPlan{Text: UnpairedSenderHint()}}, nil
		}
		return nil, fmt.Errorf("gateway router: resolve member: %w", err)
	}

	if cmd, args := parseCommand(msg.Text); cmd != "" {
		res, err := r.routeCommand(ctx, gateway, msg, link, cmd, args)
		if err != nil || res != nil {
			return res, err
		}
		// Unknown command: fall through as an ordinary turn.
	}

	return r.routeTurn(ctx, gateway, msg, link)
}

// routeCommand handles the built-in gateway commands. It returns (nil, nil)
// when the command is not one of the gateway's own — the caller treats it as
// ordinary turn input.
func (r *Router) routeCommand(ctx context.Context, gateway *domain.GatewayConfig, msg InboundMessage, link *domain.UserLink, cmd string, args []string) (*RouteResult, error) {
	workspaceID := gateway.WorkspaceID

	switch cmd {
	case "start":
		// Pairing is a DM flow (the token binds the sender's identity).
		if msg.Kind != InboundDM {
			return &RouteResult{Reply: &ReplyPlan{Text: "Send /start <token> in a direct message to pair."}}, nil
		}
		if len(args) == 0 {
			return &RouteResult{Reply: &ReplyPlan{Text: UnpairedSenderHint()}}, nil
		}
		_, err := r.pairing.Pair(ctx, workspaceID, msg.Platform, msg.FromUserID, msg.FromUsername, args[0])
		if err != nil {
			// Unknown/expired/already-used tokens and malformed shapes all
			// refuse identically; a re-pair attempt surfaces its own conflict.
			return &RouteResult{Reply: &ReplyPlan{Text: "Pairing failed: the token is invalid, expired, or already used."}}, nil
		}
		return &RouteResult{Reply: &ReplyPlan{Text: fmt.Sprintf("Paired %s to this workspace. You can now chat with your workspace agents.", mentionName(msg.FromUsername))}}, nil

	case "new":
		// Archive the current session by minting the next suffix (design D3).
		// Accepted in DMs and bound groups from paired members.
		agentID, reply, err := r.resolveGroupOrDMAgent(ctx, gateway, msg, link)
		if err != nil || reply != nil {
			if reply != nil {
				return &RouteResult{Reply: reply}, nil
			}
			return nil, err
		}
		chatID := gatewayChatID(msg)
		suffix, err := r.bindings.BumpActiveSessionSuffix(ctx, msg.Platform, chatID, agentID)
		if err != nil {
			return nil, fmt.Errorf("gateway router: bump session suffix: %w", err)
		}
		return &RouteResult{Reply: &ReplyPlan{Text: fmt.Sprintf("Started a new session (previous transcript stays readable). Session key: %s", gatewaySessionKey(msg.Kind, msg, agentID, suffix))}}, nil

	case "compact":
		// Compaction-as-a-turn (design D3): the existing runner command turn,
		// executed on the current session key without bumping the suffix.
		plan, reply, err := r.buildTurnPlan(ctx, gateway, msg, link, "", agents.CommandCompact)
		if err != nil {
			return nil, err
		}
		if reply != nil {
			return &RouteResult{Reply: reply}, nil
		}
		return &RouteResult{Turn: plan}, nil

	case "usage":
		// DM-only context-meter readout.
		if msg.Kind != InboundDM {
			return &RouteResult{Reply: &ReplyPlan{Text: "/usage is available in direct messages."}}, nil
		}
		agentID, err := r.resolveDMAgent(ctx, gateway, link)
		if err != nil {
			return nil, err
		}
		if agentID == "" {
			return &RouteResult{Reply: &ReplyPlan{Text: noDefaultAgentText()}}, nil
		}
		sessionID := GatewayDMSessionKey(msg.FromUserID, agentID, r.currentSuffix(ctx, msg.Platform, msg.FromUserID, agentID))
		usage, err := r.usage.LatestUsage(ctx, workspaceID, sessionID)
		if err != nil {
			return nil, fmt.Errorf("gateway router: read usage: %w", err)
		}
		if usage == nil {
			return &RouteResult{Reply: &ReplyPlan{Text: "No usage recorded for this session yet."}}, nil
		}
		return &RouteResult{Reply: &ReplyPlan{Text: usageText(usage)}}, nil

	case "agent":
		// Per-user default agent choice (design D3), DM-only.
		if msg.Kind != InboundDM {
			return &RouteResult{Reply: &ReplyPlan{Text: "/agent is available in direct messages."}}, nil
		}
		if len(args) == 0 {
			return &RouteResult{Reply: &ReplyPlan{Text: "Usage: /agent <agent name>"}}, nil
		}
		name := strings.Join(args, " ")
		agent, err := r.agents.BySlug(ctx, workspaceID, name)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return &RouteResult{Reply: &ReplyPlan{Text: fmt.Sprintf("No agent named %q in this workspace.", name)}}, nil
			}
			return nil, fmt.Errorf("gateway router: resolve agent %q: %w", name, err)
		}
		agentID := agent.ID
		if err := r.links.SetUserLinkDefaultAgent(ctx, workspaceID, msg.Platform, msg.FromUserID, &agentID); err != nil {
			return nil, fmt.Errorf("gateway router: set default agent: %w", err)
		}
		return &RouteResult{Reply: &ReplyPlan{Text: fmt.Sprintf("Your default agent for direct messages is now %s.", agent.Name)}}, nil
	}

	return nil, nil
}

// routeTurn builds the ordinary model turn for a message: DM or group.
func (r *Router) routeTurn(ctx context.Context, gateway *domain.GatewayConfig, msg InboundMessage, link *domain.UserLink) (*RouteResult, error) {
	plan, reply, err := r.buildTurnPlan(ctx, gateway, msg, link, msg.Text, "")
	if err != nil {
		return nil, err
	}
	if reply != nil {
		return &RouteResult{Reply: reply}, nil
	}
	return &RouteResult{Turn: plan}, nil
}

// buildTurnPlan assembles the TurnPlan for one message: resolves the agent,
// enriches media through the ingress stage, prefixes group attribution, and
// derives the deterministic session key. A non-nil reply is a refusal.
func (r *Router) buildTurnPlan(ctx context.Context, gateway *domain.GatewayConfig, msg InboundMessage, link *domain.UserLink, text, command string) (*TurnPlan, *ReplyPlan, error) {
	workspaceID := gateway.WorkspaceID

	var agentID string
	switch msg.Kind {
	case InboundDM:
		var err error
		agentID, err = r.resolveDMAgent(ctx, gateway, link)
		if err != nil {
			return nil, nil, err
		}
		if agentID == "" {
			return nil, &ReplyPlan{Text: noDefaultAgentText()}, nil
		}
	case InboundGroup:
		binding, err := r.bindings.GetChatBinding(ctx, workspaceID, msg.Platform, msg.ChatID)
		if err != nil {
			return nil, nil, fmt.Errorf("gateway router: resolve binding: %w", err)
		}
		if binding == nil {
			// Not bound: the bot sits in an unbound group. Drop silently —
			// bindings are admin-created and confirmed in-chat.
			return nil, &ReplyPlan{}, nil
		}
		if _, err := r.agents.ByID(ctx, workspaceID, binding.AgentID); err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return nil, &ReplyPlan{Text: "This group is bound to an agent that no longer exists. Ask a workspace admin to rebind it."}, nil
			}
			return nil, nil, fmt.Errorf("gateway router: resolve bound agent: %w", err)
		}
		agentID = binding.AgentID
	default:
		return nil, &ReplyPlan{}, nil
	}

	// Attachment/voice ingress (design D8): the stage normalizes media into
	// refs or produces a fail-soft refusal. The default stage passes the
	// text through untouched.
	input := text
	var refs []agents.AttachmentRef
	if r.ingress != nil {
		enriched, stageRefs, refusal, err := r.ingress.Enrich(ctx, workspaceID, msg)
		if err != nil {
			return nil, nil, fmt.Errorf("gateway router: ingress: %w", err)
		}
		if refusal != "" {
			return nil, &ReplyPlan{Text: refusal}, nil
		}
		input = enriched
		refs = stageRefs
	}
	if strings.TrimSpace(input) == "" && command == "" && len(refs) == 0 {
		// Media-only message the ingress dropped: nothing to run.
		return nil, &ReplyPlan{}, nil
	}

	// Turn attribution in shared sessions (spec): group turns are prefixed
	// with the sender's display name and handle so the agent knows who is
	// speaking. DMs carry no prefix.
	if msg.Kind == InboundGroup {
		display := r.displayName(ctx, workspaceID, link, msg)
		input = fmt.Sprintf("[%s]: %s", display, input)
	}

	suffix := r.currentSuffix(ctx, msg.Platform, gatewayChatID(msg), agentID)
	return &TurnPlan{
		GatewayID:   gateway.ID,
		WorkspaceID: workspaceID,
		AgentID:     agentID,
		SessionID:   gatewaySessionKey(msg.Kind, msg, agentID, suffix),
		UserID:      link.UserID,
		Input:       input,
		Command:     command,
		Attachments: refs,
		Kind:        msg.Kind,
		ChatID:      msg.ChatID,
		MessageID:   msg.MessageID,
	}, nil, nil
}

// resolveDMAgent resolves the DM routing agent: the member's per-user choice
// when set, otherwise the gateway's configured default agent (spec: "DM uses
// member default agent" / "DM falls back to gateway default"). Empty means
// neither is configured.
func (r *Router) resolveDMAgent(ctx context.Context, gateway *domain.GatewayConfig, link *domain.UserLink) (string, error) {
	if link.DefaultAgentID != nil && *link.DefaultAgentID != "" {
		if _, err := r.agents.ByID(ctx, gateway.WorkspaceID, *link.DefaultAgentID); err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				// The per-user choice was deleted (SET NULL lag): fall through
				// to the gateway default rather than refusing.
				return r.gatewayDefaultAgent(gateway), nil
			}
			return "", fmt.Errorf("gateway router: resolve per-user agent: %w", err)
		}
		return *link.DefaultAgentID, nil
	}
	return r.gatewayDefaultAgent(gateway), nil
}

func (r *Router) gatewayDefaultAgent(gateway *domain.GatewayConfig) string {
	if gateway.DefaultAgentID != nil {
		return *gateway.DefaultAgentID
	}
	return ""
}

// resolveGroupOrDMAgent resolves the agent a command targets (used by /new,
// which must work in both DMs and bound groups). A non-nil reply is a
// refusal or an already-sent response.
func (r *Router) resolveGroupOrDMAgent(ctx context.Context, gateway *domain.GatewayConfig, msg InboundMessage, link *domain.UserLink) (string, *ReplyPlan, error) {
	switch msg.Kind {
	case InboundDM:
		agentID, err := r.resolveDMAgent(ctx, gateway, link)
		if err != nil {
			return "", nil, err
		}
		if agentID == "" {
			return "", &ReplyPlan{Text: noDefaultAgentText()}, nil
		}
		return agentID, nil, nil
	case InboundGroup:
		binding, err := r.bindings.GetChatBinding(ctx, gateway.WorkspaceID, msg.Platform, msg.ChatID)
		if err != nil {
			return "", nil, fmt.Errorf("gateway router: resolve binding: %w", err)
		}
		if binding == nil {
			return "", &ReplyPlan{}, nil
		}
		return binding.AgentID, nil, nil
	default:
		return "", &ReplyPlan{}, nil
	}
}

// displayName builds the group attribution prefix body: display name and
// handle, e.g. `Oni (@onih)` (spec: "Multi-speaker group").
func (r *Router) displayName(ctx context.Context, workspaceID string, link *domain.UserLink, msg InboundMessage) string {
	handle := msg.FromUsername
	if handle == "" {
		handle = link.PlatformUsername
	}
	if u, err := r.users.ByID(ctx, link.UserID); err == nil && u != nil && u.Name != "" {
		if handle != "" {
			return fmt.Sprintf("%s (@%s)", u.Name, handle)
		}
		return u.Name
	}
	if handle != "" {
		return "@" + handle
	}
	return link.PlatformUserID
}

func mentionName(username string) string {
	if username == "" {
		return "your Telegram account"
	}
	return "@" + username
}

func noDefaultAgentText() string {
	return "No default agent is configured for this gateway. Ask a workspace admin to set one in Settings → Gateways, or choose one with /agent <name>."
}

// currentSuffix reads the persisted active-session suffix (design D3): 0 for
// the base key, n for the "_<n>" key. Read errors degrade to 0 rather than
// dropping the message — a missing cursor row only ever means "base key".
func (r *Router) currentSuffix(ctx context.Context, platform, chatID, agentID string) int64 {
	suffix, err := r.bindings.ActiveSessionSuffix(ctx, platform, chatID, agentID)
	if err != nil || suffix < 0 {
		return 0
	}
	return suffix
}

// gatewayChatID is the chat coordinate of the session key: the platform user
// id for DMs (the DM chat id IS the user id), the chat id for groups.
func gatewayChatID(msg InboundMessage) string {
	if msg.Kind == InboundDM {
		return msg.FromUserID
	}
	return msg.ChatID
}

// GatewayDMSessionKey derives the deterministic DM session key (design D3):
// tg_dm_<telegram_user_id>_<agent_id>, with the "_<n>" suffix when n > 0.
func GatewayDMSessionKey(platformUserID, agentID string, suffix int64) string {
	return suffixedKey(fmt.Sprintf("%s%s_%s", domain.SessionPrefixGatewayDM, platformUserID, agentID), suffix)
}

// GatewayGroupSessionKey derives the deterministic group session key:
// tg_group_<chat_id>_<agent_id>, with the "_<n>" suffix when n > 0.
func GatewayGroupSessionKey(chatID, agentID string, suffix int64) string {
	return suffixedKey(fmt.Sprintf("%s%s_%s", domain.SessionPrefixGatewayGroup, chatID, agentID), suffix)
}

func gatewaySessionKey(kind InboundKind, msg InboundMessage, agentID string, suffix int64) string {
	if kind == InboundDM {
		return GatewayDMSessionKey(msg.FromUserID, agentID, suffix)
	}
	return GatewayGroupSessionKey(msg.ChatID, agentID, suffix)
}

func suffixedKey(base string, suffix int64) string {
	if suffix <= 0 {
		return base
	}
	return fmt.Sprintf("%s_%d", base, suffix)
}

// usageText renders the /usage reply from the session's last-turn numbers.
func usageText(u *agents.UsagePayload) string {
	return fmt.Sprintf(
		"Last turn: %d in / %d out tokens (total %d). Context at last call: %d tokens.",
		u.InputTokens, u.OutputTokens, u.TotalTokens, u.FinalInputTokens,
	)
}

// parseCommand splits a message's leading "/command arg1 arg2" — returning
// empty cmd for non-commands. Telegram group commands carry a bot-name
// suffix ("/new@mybot"); it is stripped so a bound bot only answers its own
// command.
func parseCommand(text string) (string, []string) {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "/") {
		return "", nil
	}
	fields := strings.Fields(trimmed)
	head := strings.TrimPrefix(fields[0], "/")
	if at := strings.IndexByte(head, '@'); at >= 0 {
		head = head[:at]
	}
	if head == "" {
		return "", nil
	}
	return strings.ToLower(head), fields[1:]
}
