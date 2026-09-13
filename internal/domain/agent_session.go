package domain

import (
	"fmt"
	"strings"
	"time"
)

// Session-id binding prefixes. The web mints client-side ids ("sess_<uuid>");
// every other origin mints deterministic server-side ids carrying one of the
// registered binding prefixes. Registering a new prefix here (and only here)
// is the deliberate act that lets a store accept the shape — unknown
// "<word>_"-shaped ids are refused (integrate-telegram-gateway design D3,
// channel-session-leak fix).
const (
	SessionPrefixWeb          = "sess_"
	SessionPrefixChannel      = "chan_"
	SessionPrefixScheduler    = "sched_"
	SessionPrefixGatewayDM    = "tg_dm_"
	SessionPrefixGatewayGroup = "tg_group_"
)

// agentSessionBindingPrefixes is the registry of accepted prefixes.
var agentSessionBindingPrefixes = []string{
	SessionPrefixWeb,
	SessionPrefixChannel,
	SessionPrefixScheduler,
	SessionPrefixGatewayDM,
	SessionPrefixGatewayGroup,
}

// ValidateAgentSessionID validates a session id for the durable session
// index: it must either carry a registered binding prefix or be a plain id
// (no '_' separator — the client-minted "sess_<uuid>" shape is registered;
// bare uuids and legacy unprefixed ids contain none). An id whose first '_'
// token forms an unregistered prefix is refused with ErrInvalid: a typo'd or
// future prefix must never silently index rows the store later has to filter
// back out.
func ValidateAgentSessionID(sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("%w: session id is required", ErrInvalid)
	}
	for _, prefix := range agentSessionBindingPrefixes {
		if strings.HasPrefix(sessionID, prefix) {
			return nil
		}
	}
	if strings.Contains(sessionID, "_") {
		return fmt.Errorf("%w: session id %q carries an unregistered binding prefix", ErrInvalid, sessionID)
	}
	return nil
}

// IsPrivateIndexSessionID reports whether a session belongs in the per-user
// session index (the listing the web sidebar reads). Chat sessions — web
// ("sess_") and gateway direct messages ("tg_dm_", indexed under the paired
// member) — are listed. Automation and shared artifacts are not:
// channel sessions ("chan_"), scheduler run sessions ("sched_", which the
// runner also never upserts), and gateway group sessions ("tg_group_",
// shared across every member of the group — design D3) are excluded even if
// a row exists.
func IsPrivateIndexSessionID(sessionID string) bool {
	return !(strings.HasPrefix(sessionID, SessionPrefixChannel) ||
		strings.HasPrefix(sessionID, SessionPrefixScheduler) ||
		strings.HasPrefix(sessionID, SessionPrefixGatewayGroup))
}

// AgentSession is one row of the durable agent session index
// (agent-session-index design D1): a per-user record that an agent chat
// session exists, carrying its birth-derived title and last-activity time so
// sidebar history follows the account rather than the browser. Deletion is
// soft — DeletedAt hides the row from listings while the transcript events
// and checkpoints in session_events stay intact.
type AgentSession struct {
	ID           string     `json:"id"`
	WorkspaceID  string     `json:"workspace_id"`
	AgentID      string     `json:"agent_id"`
	UserID       string     `json:"user_id"`
	SessionID    string     `json:"session_id"`
	Title        string     `json:"title"`
	DeletedAt    *time.Time `json:"deleted_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	LastActiveAt time.Time  `json:"last_active_at"`
}

// AgentSessionUpsert is the write model for indexing one persistent run at
// run start (design D2): the client-minted session id and the input-derived
// title candidate. Title is honored only on birth — an upsert against a row
// whose title is already set never rewrites it (an empty Title stores the
// empty string on birth and leaves an existing title untouched on conflict).
type AgentSessionUpsert struct {
	SessionID string
	Title     string
}
