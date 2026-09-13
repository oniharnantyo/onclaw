package observability

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/cloudwego/eino-ext/callbacks/langfuse"
	"github.com/google/uuid"
)

// Run origins mirrored from internal/agents (ExecRequest.Origin). This
// package cannot import internal/agents — the runner imports observability —
// so the value set and the unknown-maps-to-"user" normalization are pinned
// here too; agents.normalizeOrigin remains the source of truth.
const (
	OriginUser      = "user"
	OriginScheduler = "scheduler"
	OriginChannel   = "channel"
	OriginTelegram  = "telegram"
)

// Trace attribution tag prefixes and metadata keys (D2): every exported trace
// carries tags "origin:<origin>", "agent:<agent id>", "ws:<workspace id>" and
// metadata {turn_id, workspace_id, agent_id, origin}.
const (
	tagOriginPrefix    = "origin:"
	tagAgentPrefix     = "agent:"
	tagWorkspacePrefix = "ws:"

	metadataKeyTurnID      = "turn_id"
	metadataKeyWorkspaceID = "workspace_id"
	metadataKeyAgentID     = "agent_id"
	metadataKeyOrigin      = "origin"
)

// maxTraceNameRunes bounds the interactive trace name: the turn input's first
// line, truncated (D2).
const maxTraceNameRunes = 80

// traceIDNamespaceSeed namespaces the deterministic run→trace id mapping so
// ids never collide with uuid v5 values minted for other purposes.
const traceIDNamespaceSeed = "urn:onclaw:langfuse:trace:run:"

// TraceContext carries the per-turn coordinates the trace is attributed with
// (D2), sourced from the runner's ExecRequest: session and user identity,
// workspace/agent scope, the turn id (which pins the trace id), the run
// origin, the turn input (interactive trace name), and the schedule name
// (scheduler-fire trace name).
type TraceContext struct {
	SessionID    string
	UserID       string
	WorkspaceID  string
	AgentID      string
	TurnID       string
	Origin       string
	Input        string
	ScheduleName string
}

// ApplyTraceContext stamps the turn's trace context onto the run context via
// langfuse.SetTrace (D2): session id, user id, origin/agent/workspace tags,
// turn/workspace/agent/origin metadata, and the trace name (schedule name for
// scheduler fires, otherwise the input's first line truncated). The trace id
// is pinned to TraceIDForRun(TurnID) so retried attempts of the same turn
// land inside one trace and the deterministic sampler (D5) keys on the run.
// Empty optional coordinates are omitted rather than exported as blanks.
//
// Call this for every traced turn once the handler is configured — including
// sampled-out turns: the pinned trace id makes the upstream sampler drop the
// whole turn's events atomically, while persistence gates on SampledIn.
func ApplyTraceContext(ctx context.Context, tc TraceContext) context.Context {
	opts := []langfuse.TraceOption{
		langfuse.WithTags(TraceTags(tc)...),
		langfuse.WithMetadata(TraceMetadata(tc)),
	}
	if id := TraceIDForRun(tc.TurnID); id != "" {
		opts = append(opts, langfuse.WithID(id))
	}
	if name := TraceName(tc.Origin, tc.Input, tc.ScheduleName); name != "" {
		opts = append(opts, langfuse.WithName(name))
	}
	if tc.SessionID != "" {
		opts = append(opts, langfuse.WithSessionID(tc.SessionID))
	}
	if tc.UserID != "" {
		opts = append(opts, langfuse.WithUserID(tc.UserID))
	}
	return langfuse.SetTrace(ctx, opts...)
}

// TraceIDForRun maps a run id (the runner's per-turn uuid) onto the Langfuse
// trace id deterministically: a uuid v5 in a dedicated namespace. The mapping
// is pure, so the same turn resolves to the same trace id in every process
// and on every retry (spec: a retried turn's attempts stay inside one trace),
// and the exported trace carries exactly the id the caller computed. An empty
// run id yields "" — callers must not persist or pin an empty id.
func TraceIDForRun(runID string) string {
	if runID == "" {
		return ""
	}
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(traceIDNamespaceSeed+runID)).String()
}

// SampledIn reports whether a trace with the given id is exported at the
// configured sample rate (D5). It mirrors the upstream consumer's
// deterministic sampler exactly — SHA-256 of the trace id, first 8 hex digits
// normalized by 0xFFFFFFFF, kept when strictly below the rate — so the local
// persistence decision and the upstream export decision agree for every id:
// a run whose trace id fails SampledIn exports nothing (its whole pinned
// event set is dropped) and must not persist a trace id. Rates outside (0,1)
// sample everything in, as does an empty id, matching upstream.
func SampledIn(traceID string, sampleRate float64) bool {
	if sampleRate <= 0 || sampleRate >= 1 || traceID == "" {
		return true
	}
	sum := sha256.Sum256([]byte(traceID))
	hashInt, err := strconv.ParseInt(hex.EncodeToString(sum[:])[:8], 16, 64)
	if err != nil {
		return true
	}
	return float64(hashInt)/float64(0xFFFFFFFF) < sampleRate
}

// TraceTags builds the attribution tags (D2): "origin:<origin>" always,
// "agent:<id>" and "ws:<id>" when the coordinate is present.
func TraceTags(tc TraceContext) []string {
	tags := []string{tagOriginPrefix + NormalizeOrigin(tc.Origin)}
	if tc.AgentID != "" {
		tags = append(tags, tagAgentPrefix+tc.AgentID)
	}
	if tc.WorkspaceID != "" {
		tags = append(tags, tagWorkspacePrefix+tc.WorkspaceID)
	}
	return tags
}

// TraceMetadata builds the trace metadata (D2): {turn_id, workspace_id,
// agent_id, origin}, absent keys omitted rather than exported blank.
func TraceMetadata(tc TraceContext) map[string]string {
	metadata := map[string]string{metadataKeyOrigin: NormalizeOrigin(tc.Origin)}
	if tc.TurnID != "" {
		metadata[metadataKeyTurnID] = tc.TurnID
	}
	if tc.WorkspaceID != "" {
		metadata[metadataKeyWorkspaceID] = tc.WorkspaceID
	}
	if tc.AgentID != "" {
		metadata[metadataKeyAgentID] = tc.AgentID
	}
	return metadata
}

// TraceName picks the trace name (D2): the schedule name for scheduler fires,
// otherwise the turn input's first line truncated to maxTraceNameRunes. An
// empty result means the caller has nothing to name the trace with and the
// option is omitted.
func TraceName(origin, input, scheduleName string) string {
	if NormalizeOrigin(origin) == OriginScheduler {
		if name := strings.TrimSpace(scheduleName); name != "" {
			return truncateRunes(name, maxTraceNameRunes)
		}
	}
	return FirstLine(input, maxTraceNameRunes)
}

// FirstLine returns the first line of s, truncated to at most max runes with
// a "..." tail. Empty input yields "".
func FirstLine(s string, max int) string {
	line := s
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		line = s[:i]
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	return truncateRunes(line, max)
}

// truncateRunes cuts s to at most max runes, appending "..." when truncated.
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	const tail = "..."
	runes := []rune(s)
	keep := max - len(tail)
	if keep < 1 {
		return string(runes[:max])
	}
	return string(runes[:keep]) + tail
}

// NormalizeOrigin maps a request origin onto the fixed origin value set:
// documented values pass through, anything else (including empty) is
// user-initiated — mirroring internal/agents normalizeOrigin.
func NormalizeOrigin(origin string) string {
	switch origin {
	case OriginScheduler, OriginChannel, OriginTelegram:
		return origin
	default:
		return OriginUser
	}
}
