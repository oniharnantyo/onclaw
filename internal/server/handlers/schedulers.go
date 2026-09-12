package handlers

// Scheduler REST surface (openspec change integrate-scheduler, design D11):
// workspace-scoped CRUD over domain.Scheduler, run-now dispatch, and the
// per-scheduler and workspace-wide run-history reads. Reads are gated by
// scheduler.read, mutations and run-now by scheduler.write (spec: Scheduler
// API and permissions); unknown ids and other workspaces' schedulers are 404
// indistinguishably. domain.ValidateScheduler is the single validator shared
// with the schedule agent tool (D11), and next_run_at is always derived by
// the store on write (create, edit-reschedules-from-now, pause/resume — D9),
// never set here.

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// DefaultSchedulerRunsLimit is the page size when the request carries no
// limit; MaxSchedulerRunsLimit caps one page. The web client asks for 100.
const (
	DefaultSchedulerRunsLimit = 100
	MaxSchedulerRunsLimit     = 500
)

// SchedulerRunNow is the run-now dispatch capability the run endpoint uses
// (integrate-scheduler D9): direct dispatch recording trigger manual, 409 on
// an in-flight run, 404 unknown. *scheduler.Service satisfies it.
type SchedulerRunNow interface {
	RunNow(ctx context.Context, workspaceID, schedulerID string) (*domain.SchedulerRun, error)
}

// ---------------------------------------------------------------------------
// Payload shapes
// ---------------------------------------------------------------------------

// schedulerCreateRequest is the create payload (the web SchedulerPayload /
// the schedule tool's shared fields). run_at rides as an RFC3339 string and
// is decoded before validation; delivery defaults to the run's own thread.
type schedulerCreateRequest struct {
	Name     string                    `json:"name"`
	AgentID  string                    `json:"agent_id"`
	Prompt   string                    `json:"prompt"`
	Kind     string                    `json:"kind"`
	Expr     string                    `json:"expr"`
	RunAt    *string                   `json:"run_at"`
	Delivery *domain.SchedulerDelivery `json:"delivery"`
	Enabled  *bool                     `json:"enabled"`
}

// schedulerPatchRequest is the partial update payload. Absent fields keep the
// stored value; an explicit null run_at behaves like absent (switching kind to
// recurring clears it at the validator; kind once without a stored or provided
// instant is rejected there).
type schedulerPatchRequest struct {
	Name     *string                   `json:"name"`
	AgentID  *string                   `json:"agent_id"`
	Prompt   *string                   `json:"prompt"`
	Kind     *string                   `json:"kind"`
	Expr     *string                   `json:"expr"`
	RunAt    *string                   `json:"run_at"`
	Delivery *domain.SchedulerDelivery `json:"delivery"`
	Enabled  *bool                     `json:"enabled"`
}

// ---------------------------------------------------------------------------
// Read views
// ---------------------------------------------------------------------------

// schedulerView is the read view the web lib expects: the raw row plus the
// server-derived human_label (D2 — never stored from input).
type schedulerView struct {
	domain.Scheduler
	HumanLabel string `json:"human_label"`
}

// schedulerViewOf projects one scheduler onto its read view. The label is the
// derived cron description for recurring schedules and the workspace-local
// target instant for one-shots — the same convention the schedule tool speaks.
func schedulerViewOf(s *domain.Scheduler, tz *time.Location) schedulerView {
	label := domain.HumanLabel(s.Expr)
	if s.Kind == domain.SchedulerKindOnce && s.RunAt != nil {
		label = s.RunAt.In(tz).Format(time.RFC3339)
	}
	return schedulerView{Scheduler: *s, HumanLabel: label}
}

// schedulerRunView is the run read view: the raw row plus the enrichment the
// runs screens use to open transcripts and label rows (agent_id resolves the
// run's agent; scheduler_name labels the workspace-wide feed). Optional, so
// omitted when unknown.
type schedulerRunView struct {
	domain.SchedulerRun
	AgentID       string `json:"agent_id,omitempty"`
	SchedulerName string `json:"scheduler_name,omitempty"`
	AgentName     string `json:"agent_name,omitempty"`
}

func schedulerRunViewOf(run domain.SchedulerRun, sched *domain.Scheduler) schedulerRunView {
	v := schedulerRunView{SchedulerRun: run}
	if sched != nil {
		v.AgentID = sched.AgentID
		v.SchedulerName = sched.Name
	}
	return v
}

// workspaceTimezone resolves the workspace's IANA timezone for derived-label
// presentation, falling back to UTC (mirrors the store adapters' guard —
// workspace timezones are IANA-validated at the domain layer).
func workspaceTimezone(ws *domain.Workspace) *time.Location {
	if loc, err := time.LoadLocation(ws.Timezone); err == nil {
		return loc
	}
	return time.UTC
}

// schedulerFieldFor maps a domain.ValidateScheduler error onto the payload
// field it names — a presentation hint over the domain's stable error strings
// (one validator, two surfaces; the field split stays a view concern).
func schedulerFieldFor(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "cron expression"):
		return "expr"
	case strings.Contains(msg, "run_at"):
		return "run_at"
	case strings.Contains(msg, "delivery type"):
		return "delivery.type"
	case strings.Contains(msg, "channel_id"):
		return "delivery.channel_id"
	case strings.Contains(msg, "name"):
		return "name"
	case strings.Contains(msg, "prompt"):
		return "prompt"
	case strings.Contains(msg, "kind"):
		return "kind"
	default:
		return ""
	}
}

// respondSchedulerValidation writes the fielded 422 envelope (the channels /
// tool-settings convention) for scheduler payloads.
func respondSchedulerValidation(c *gin.Context, err error) {
	detail := ErrorDetail{Field: schedulerFieldFor(err), Message: err.Error()}
	message := detail.Message
	AbortWithError(c, http.StatusUnprocessableEntity, CodeInvalidRequest, message, detail)
}

func bindSchedulerRequest(c *gin.Context, req any) bool {
	if err := c.ShouldBindJSON(req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return false
	}
	return true
}

// parseSchedulerRunAt decodes an RFC3339 one-shot instant from the payload;
// nil/absent yields a nil instant (requiredness is the validator's call).
func parseSchedulerRunAt(raw *string) (*time.Time, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, *raw)
	if err != nil {
		return nil, fmt.Errorf("%w: run_at %q must be an RFC3339 instant (e.g. 2026-09-12T09:00:00+07:00)", domain.ErrInvalid, *raw)
	}
	return &parsed, nil
}

// parseSchedulerRunsPage reads the limit/offset query pair for the run
// listings: default page size, hard cap, fielded 422 on garbage.
func parseSchedulerRunsPage(c *gin.Context) (limit, offset int, ok bool) {
	limit = DefaultSchedulerRunsLimit
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			AbortWithError(c, http.StatusUnprocessableEntity, CodeInvalidRequest,
				"limit must be a positive integer", ErrorDetail{Field: "limit", Message: "limit must be a positive integer"})
			return 0, 0, false
		}
		if n > MaxSchedulerRunsLimit {
			n = MaxSchedulerRunsLimit
		}
		limit = n
	}
	offset = 0
	if raw := c.Query("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			AbortWithError(c, http.StatusUnprocessableEntity, CodeInvalidRequest,
				"offset must be a non-negative integer", ErrorDetail{Field: "offset", Message: "offset must be a non-negative integer"})
			return 0, 0, false
		}
		offset = n
	}
	return limit, offset, true
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// schedulerHandlers serves the workspace scheduler surface (integrate-scheduler
// D11): CRUD, run-now, and the run-history reads. Dependencies are granular:
// the scheduler store for reads/writes and the run-now dispatch face (the
// scheduler service) for the run endpoint.
type schedulerHandlers struct {
	schedulers store.SchedulerStore
	runNow     SchedulerRunNow
}

// NewSchedulerHandlers creates a new schedulerHandlers instance.
func NewSchedulerHandlers(schedulers store.SchedulerStore, runNow SchedulerRunNow) *schedulerHandlers {
	return &schedulerHandlers{schedulers: schedulers, runNow: runNow}
}

// ListSchedulers returns the workspace's schedulers in creation order, each
// with its derived human label and next fire time (absent when paused).
func (h *schedulerHandlers) ListSchedulers(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	rows, err := h.schedulers.ListSchedulers(c.Request.Context(), ws.ID)
	if err != nil {
		RespondError(c, err)
		return
	}
	tz := workspaceTimezone(ws)
	items := make([]schedulerView, 0, len(rows))
	for i := range rows {
		items = append(items, schedulerViewOf(&rows[i], tz))
	}
	RespondOK(c, gin.H{"schedulers": items})
}

// CreateScheduler registers a standing order: payload → domain row → the
// shared validator → store write. The store fills timestamps, computes
// next_run_at from now under the workspace timezone, and enforces the
// per-agent name uniqueness (409) and workspace-scoped FK parity (404).
func (h *schedulerHandlers) CreateScheduler(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)

	var req schedulerCreateRequest
	if !bindSchedulerRequest(c, &req) {
		return
	}

	runAt, err := parseSchedulerRunAt(req.RunAt)
	if err != nil {
		respondSchedulerValidation(c, err)
		return
	}

	s := &domain.Scheduler{
		WorkspaceID: ws.ID,
		AgentID:     req.AgentID,
		CreatedBy:   &user.ID,
		Name:        req.Name,
		Prompt:      req.Prompt,
		Kind:        req.Kind,
		Expr:        req.Expr,
		RunAt:       runAt,
		Enabled:     true, // a new schedule is live unless the payload pauses it
	}
	if req.Delivery != nil {
		s.Delivery = *req.Delivery
	}
	if req.Enabled != nil {
		s.Enabled = *req.Enabled
	}

	if err := domain.ValidateScheduler(s, time.Now(), true); err != nil {
		respondSchedulerValidation(c, err)
		return
	}

	if err := h.schedulers.CreateScheduler(c.Request.Context(), ws.ID, s); err != nil {
		RespondError(c, err)
		return
	}
	RespondJSON(c, http.StatusCreated, gin.H{"scheduler": schedulerViewOf(s, workspaceTimezone(ws))})
}

// GetScheduler returns one scheduler by id under the workspace; a foreign
// workspace's id is indistinguishable from unknown (the store reads both as
// absent — 404).
func (h *schedulerHandlers) GetScheduler(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	s, ok := h.resolveScheduler(c, ws.ID)
	if !ok {
		return
	}
	RespondOK(c, gin.H{"scheduler": schedulerViewOf(s, workspaceTimezone(ws))})
}

// resolveScheduler loads the scheduler addressed by :id under the workspace,
// responding 404 (absent) or the store error. (nil, nil) is optional response
// data, not a nil dependency.
func (h *schedulerHandlers) resolveScheduler(c *gin.Context, workspaceID string) (*domain.Scheduler, bool) {
	id := c.Param("id")
	if id == "" {
		RespondError(c, domain.ErrNotFound)
		return nil, false
	}
	s, err := h.schedulers.GetScheduler(c.Request.Context(), workspaceID, id)
	if err != nil {
		RespondError(c, err)
		return nil, false
	}
	if s == nil {
		RespondError(c, domain.ErrNotFound)
		return nil, false
	}
	return s, true
}

// PatchScheduler overlays the provided fields onto the stored row and
// persists through the store, which revalidates and recomputes next_run_at
// from now under the new schedule (D9: edit reschedules from now; pause
// clears it; resume lands on the next future occurrence). Name conflicts are
// 409; an agent moved out of the workspace is 404.
func (h *schedulerHandlers) PatchScheduler(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	s, ok := h.resolveScheduler(c, ws.ID)
	if !ok {
		return
	}

	var req schedulerPatchRequest
	if !bindSchedulerRequest(c, &req) {
		return
	}

	changed := false
	if req.Name != nil && *req.Name != s.Name {
		s.Name = *req.Name
		changed = true
	}
	if req.AgentID != nil && *req.AgentID != s.AgentID {
		s.AgentID = *req.AgentID
		changed = true
	}
	if req.Prompt != nil && *req.Prompt != s.Prompt {
		s.Prompt = *req.Prompt
		changed = true
	}
	if req.Kind != nil && *req.Kind != s.Kind {
		s.Kind = *req.Kind
		changed = true
	}
	if req.Expr != nil && *req.Expr != s.Expr {
		s.Expr = *req.Expr
		changed = true
	}
	if req.RunAt != nil {
		runAt, err := parseSchedulerRunAt(req.RunAt)
		if err != nil {
			respondSchedulerValidation(c, err)
			return
		}
		if runAt == nil || s.RunAt == nil || !runAt.Equal(*s.RunAt) {
			s.RunAt = runAt
			changed = true
		}
	}
	if req.Delivery != nil {
		if req.Delivery.Type != s.Delivery.Type {
			s.Delivery.Type = req.Delivery.Type
			changed = true
		}
		if req.Delivery.ChannelID != "" && req.Delivery.ChannelID != s.Delivery.ChannelID {
			s.Delivery.ChannelID = req.Delivery.ChannelID
			changed = true
		}
	}
	if req.Enabled != nil && *req.Enabled != s.Enabled {
		s.Enabled = *req.Enabled
		changed = true
	}
	if !changed {
		RespondError(c, fmt.Errorf("%w: no fields to update", domain.ErrInvalid))
		return
	}

	if err := domain.ValidateScheduler(s, time.Now(), false); err != nil {
		respondSchedulerValidation(c, err)
		return
	}

	if err := h.schedulers.UpdateScheduler(c.Request.Context(), ws.ID, s); err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, gin.H{"scheduler": schedulerViewOf(s, workspaceTimezone(ws))})
}

// DeleteScheduler removes the standing order; its run records cascade in the
// store.
func (h *schedulerHandlers) DeleteScheduler(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	s, ok := h.resolveScheduler(c, ws.ID)
	if !ok {
		return
	}
	if err := h.schedulers.DeleteScheduler(c.Request.Context(), ws.ID, s.ID); err != nil {
		RespondError(c, err)
		return
	}
	RespondNoContent(c)
}

// RunSchedulerNow dispatches immediately (D9): trigger manual, regardless of
// the enabled flag, never re-enabling. The scheduler service answers 409
// while a run is in flight and 404 unknown — both ride the sentinel mapping.
func (h *schedulerHandlers) RunSchedulerNow(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	run, err := h.runNow.RunNow(c.Request.Context(), ws.ID, c.Param("id"))
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, gin.H{"run": schedulerRunViewOf(*run, nil)})
}

// ListSchedulerRuns returns one scheduler's run history, newest-first with
// the total across all pages (limit/offset passthrough after normalization).
func (h *schedulerHandlers) ListSchedulerRuns(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	limit, offset, ok := parseSchedulerRunsPage(c)
	if !ok {
		return
	}
	s, ok := h.resolveScheduler(c, ws.ID)
	if !ok {
		return
	}
	runs, total, err := h.schedulers.ListSchedulerRuns(c.Request.Context(), ws.ID, s.ID, limit, offset)
	if err != nil {
		RespondError(c, err)
		return
	}
	views := make([]schedulerRunView, 0, len(runs))
	for _, run := range runs {
		views = append(views, schedulerRunViewOf(run, s))
	}
	RespondOK(c, gin.H{"runs": views, "total": total})
}

// ListWorkspaceSchedulerRuns returns the workspace-wide run history,
// newest-first with the exact total. The store port lists runs per scheduler
// only, so the page merges each scheduler's top (limit+offset) rows — a
// global top-k row is always within its own scheduler's top-k, so the page
// and total are exact — and enriches rows with their scheduler's agent.
func (h *schedulerHandlers) ListWorkspaceSchedulerRuns(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	limit, offset, ok := parseSchedulerRunsPage(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()

	scheds, err := h.schedulers.ListSchedulers(ctx, ws.ID)
	if err != nil {
		RespondError(c, err)
		return
	}

	window := limit + offset
	total := 0
	type mergedRun struct {
		run   domain.SchedulerRun
		sched *domain.Scheduler
	}
	merged := make([]mergedRun, 0, window)
	for i := range scheds {
		rows, schedTotal, err := h.schedulers.ListSchedulerRuns(ctx, ws.ID, scheds[i].ID, window, 0)
		if err != nil {
			RespondError(c, err)
			return
		}
		total += schedTotal
		for _, run := range rows {
			merged = append(merged, mergedRun{run: run, sched: &scheds[i]})
		}
	}

	// Newest first, id descending — the stores' own listing tiebreak.
	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].run.StartedAt.Equal(merged[j].run.StartedAt) {
			return merged[i].run.ID > merged[j].run.ID
		}
		return merged[i].run.StartedAt.After(merged[j].run.StartedAt)
	})
	if offset < len(merged) {
		merged = merged[offset:]
	} else {
		merged = nil
	}
	if len(merged) > limit {
		merged = merged[:limit]
	}

	views := make([]schedulerRunView, 0, len(merged))
	for _, m := range merged {
		views = append(views, schedulerRunViewOf(m.run, m.sched))
	}
	RespondOK(c, gin.H{"runs": views, "total": total})
}
