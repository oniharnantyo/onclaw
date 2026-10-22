/**
 * Backend event channel names, mirroring internal/services/events.go.
 * Keep in sync with the Go constants; the strings are composed here rather
 * than duplicated at each call site.
 */

import type {
  MCPOAuthCompletedEvent,
  SchedulesChangedEvent,
} from "../../bindings/github.com/oniharnantyo/buddies/internal/services/models";

/** Event carrying gateway lifecycle changes; payload is GatewayStateEvent. */
export const GATEWAY_STATE_EVENT = "gateway:state";

/**
 * Event announcing the end of an MCP server's OAuth authorization flow
 * (design D2); payload is MCPOAuthCompletedEvent.
 */
export const MCP_OAUTH_COMPLETED_EVENT = "mcp:oauth:completed";

/** The generated payload of MCP_OAUTH_COMPLETED_EVENT, re-exported so
 * callers keep one import surface beside the constant. */
export type { MCPOAuthCompletedEvent };

/**
 * Event announcing any schedules change (add-scheduler design D10): every
 * mutation, fire, and miss emits it; payload is SchedulesChangedEvent.
 */
export const SCHEDULES_CHANGED_EVENT = "schedules:changed";

/** The generated payload of SCHEDULES_CHANGED_EVENT, re-exported so
 * callers keep one import surface beside the constant. */
export type { SchedulesChangedEvent };

/**
 * Untyped signal emitted by the native application menu's Settings… item
 * (design D5): the Go side emits this string with no payload, so no event
 * type is registered for it.
 */
export const SETTINGS_OPEN_EVENT = "settings:open";

/** Event name carrying one session's live turn events; payload is sessions.Event. */
export function sessionEventName(sessionID: string): string {
  return `session:${sessionID}:event`;
}
