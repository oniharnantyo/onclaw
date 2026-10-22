package services

import "fmt"

// Event names forwarded to the frontend. Constant names are registered with
// the Wails binding generator (application.RegisterEvent in the desktop
// main) so the frontend gets typed event definitions; the per-session names
// are composed with SessionEventName.
const (
	// EventGatewayState carries gateway lifecycle changes. The payload is a
	// GatewayStateEvent.
	EventGatewayState = "gateway:state"
	// EventMCPOAuthCompleted announces the end of an MCP server's OAuth
	// authorization flow (design D2). The payload is an
	// MCPOAuthCompletedEvent.
	EventMCPOAuthCompleted = "mcp:oauth:completed"
	// EventSchedulesChanged announces a schedule mutation, fire, or miss
	// (add-scheduler design D10) so the frontend refetches instead of
	// polling. The payload is a SchedulesChangedEvent.
	EventSchedulesChanged = "schedules:changed"
)

// GatewayStateEvent is the payload of EventGatewayState. State mirrors
// gateway.State's String form ("stopped", "running", "paused") so the
// gateway package's types stay out of the binding surface.
type GatewayStateEvent struct {
	State string `json:"state"`
}

// MCPOAuthCompletedEvent is the payload of EventMCPOAuthCompleted. Error
// carries the failure reason when OK is false.
type MCPOAuthCompletedEvent struct {
	ServerID string `json:"serverId"`
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
}

// SchedulesChangedEvent is the payload of EventSchedulesChanged. SessionID is
// set only when a run just created a session; fields not applicable to the
// change are empty strings (add-scheduler design D10).
type SchedulesChangedEvent struct {
	ScheduleID string `json:"scheduleId"`
	SessionID  string `json:"sessionId"`
	AgentID    string `json:"agentId"`
}

// SessionEventName returns the event name carrying one session's live turn
// events: "session:<id>:event". The payload is a sessions.Event. The name is
// scoped per session (design D7) so the frontend subscribes only to the
// session it displays.
func SessionEventName(sessionID string) string {
	return fmt.Sprintf("session:%s:event", sessionID)
}
