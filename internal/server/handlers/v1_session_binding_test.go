package handlers

// /v1 owner-scoped session binding tests (fix-role-permission-audit 3.3,
// design D3): metadata.onclaw_session and previous_response_id resolve only
// sessions whose session-index row is owned by the key's creating user.
// System-born sessions (chan_/sched_/hb_ — no index row) and foreign-user
// sessions fail with the standard not-found, indistinguishable from a
// foreign-workspace session. Metadata birth on first use is preserved; the
// chained path stays bind-only.

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// bindingEventsStub answers LoadEvents with a fixed event count: the
// persisted-events check in resolveSession.
type bindingEventsStub struct {
	store.SessionEventStore
	events int
	err    error
}

func (s bindingEventsStub) LoadEvents(context.Context, store.LoadSessionEventsParams) ([]domain.SessionEvent, error) {
	if s.err != nil {
		return nil, s.err
	}
	rows := make([]domain.SessionEvent, s.events)
	return rows, nil
}

// bindingIndexStub answers GetAgentSession with a fixed index row: the
// ownership check. A nil row is the system-session answer (no index row).
type bindingIndexStub struct {
	store.AgentSessionStore
	row *domain.AgentSession
	err error
}

func (s bindingIndexStub) GetAgentSession(context.Context, string, string, string) (*domain.AgentSession, error) {
	return s.row, s.err
}

func newBindingHandler(t *testing.T, events int, row *domain.AgentSession) *v1Handlers {
	t.Helper()
	h := &v1Handlers{}
	h.sessionEvents = bindingEventsStub{events: events}
	h.sessions = bindingIndexStub{row: row}
	return h
}

func bindingContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	return c
}

const (
	bindingWS   = "ws-1"
	bindingAG   = "agent-1"
	bindingUser = "user-alice"
)

// ownedRow is the index row of a session the key's creating user owns.
func ownedRow(sessionID string) *domain.AgentSession {
	return &domain.AgentSession{WorkspaceID: bindingWS, AgentID: bindingAG, UserID: bindingUser, SessionID: sessionID}
}

func foreignRow(sessionID string) *domain.AgentSession {
	return &domain.AgentSession{WorkspaceID: bindingWS, AgentID: bindingAG, UserID: "user-bob", SessionID: sessionID}
}

func TestResolveSession_MetadataOwnerScoped(t *testing.T) {
	const sid = "sess_mine"

	t.Run("owned existing session binds", func(t *testing.T) {
		h := newBindingHandler(t, 1, ownedRow(sid))
		got, err := h.resolveSession(bindingContext(t), bindingWS, bindingAG, bindingUser, map[string]string{"onclaw_session": sid}, "", "")
		if err != nil || got != sid {
			t.Fatalf("resolveSession = %q, %v; want %q, nil", got, err, sid)
		}
	})

	t.Run("another user's session fails not-found", func(t *testing.T) {
		h := newBindingHandler(t, 1, foreignRow(sid))
		_, err := h.resolveSession(bindingContext(t), bindingWS, bindingAG, bindingUser, map[string]string{"onclaw_session": sid}, "", "")
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("foreign-user metadata binding err = %v, want not-found", err)
		}
	})

	t.Run("system session id fails not-found", func(t *testing.T) {
		for _, sysID := range []string{"chan_abc_def", "sched_123", "hb_456", "tg_group_789"} {
			// System sessions have transcript events but no index row.
			h := newBindingHandler(t, 1, nil)
			_, err := h.resolveSession(bindingContext(t), bindingWS, bindingAG, bindingUser, map[string]string{"onclaw_session": sysID}, "", "")
			if !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("system session %q err = %v, want not-found", sysID, err)
			}
		}
	})

	t.Run("cross-workspace id births an independent local session", func(t *testing.T) {
		// The foreign workspace's session is unreadable here: no persisted
		// events in the key's workspace, so the ordinary-turn path births an
		// independent session under the same ID (cross-workspace isolation).
		h := newBindingHandler(t, 0, nil)
		got, err := h.resolveSession(bindingContext(t), bindingWS, bindingAG, bindingUser, map[string]string{"onclaw_session": "sess_otherws"}, "", "")
		if err != nil || got != "sess_otherws" {
			t.Fatalf("cross-workspace metadata binding = %q, %v; want birth", got, err)
		}
	})

	t.Run("unknown id births on first use", func(t *testing.T) {
		h := newBindingHandler(t, 0, nil)
		got, err := h.resolveSession(bindingContext(t), bindingWS, bindingAG, bindingUser, map[string]string{"onclaw_session": "sess_new"}, "", "")
		if err != nil || got != "sess_new" {
			t.Fatalf("birth-on-first-use = %q, %v; want sess_new, nil", got, err)
		}
	})

	t.Run("compact on unknown id stays not-found", func(t *testing.T) {
		h := newBindingHandler(t, 0, nil)
		_, err := h.resolveSession(bindingContext(t), bindingWS, bindingAG, bindingUser, map[string]string{"onclaw_session": "sess_new"}, "", agents.CommandCompact)
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("compact birth attempt err = %v, want not-found", err)
		}
	})

	t.Run("compact on a foreign session fails not-found", func(t *testing.T) {
		h := newBindingHandler(t, 1, foreignRow(sid))
		_, err := h.resolveSession(bindingContext(t), bindingWS, bindingAG, bindingUser, map[string]string{"onclaw_session": sid}, "", agents.CommandCompact)
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("compact foreign session err = %v, want not-found", err)
		}
	})

	t.Run("index read failure resolves unowned", func(t *testing.T) {
		h := newBindingHandler(t, 1, nil)
		h.sessions = bindingIndexStub{row: ownedRow(sid), err: errors.New("index down")}
		_, err := h.resolveSession(bindingContext(t), bindingWS, bindingAG, bindingUser, map[string]string{"onclaw_session": sid}, "", "")
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("index failure err = %v, want not-found", err)
		}
	})
}

func TestResolveSession_ChainedOwnerScoped(t *testing.T) {
	const sid = "sess_chain"

	t.Run("own session's response id chains", func(t *testing.T) {
		h := newBindingHandler(t, 1, ownedRow(sid))
		got, err := h.resolveSession(bindingContext(t), bindingWS, bindingAG, bindingUser, nil, "resp_"+sid+"_turn1", "")
		if err != nil || got != sid {
			t.Fatalf("chained bind = %q, %v; want %q, nil", got, err, sid)
		}
	})

	t.Run("another user's session's response id fails not-found", func(t *testing.T) {
		h := newBindingHandler(t, 1, foreignRow(sid))
		_, err := h.resolveSession(bindingContext(t), bindingWS, bindingAG, bindingUser, nil, "resp_"+sid+"_turn1", "")
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("foreign-user chained bind err = %v, want not-found", err)
		}
	})

	t.Run("system session's response id fails not-found", func(t *testing.T) {
		// Events persist for channel sessions, but no index row exists.
		h := newBindingHandler(t, 1, nil)
		_, err := h.resolveSession(bindingContext(t), bindingWS, bindingAG, bindingUser, nil, "resp_chan_abc_def_turn1", "")
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("system chained bind err = %v, want not-found", err)
		}
	})

	t.Run("cross-workspace response id fails not-found and never births", func(t *testing.T) {
		h := newBindingHandler(t, 0, nil)
		_, err := h.resolveSession(bindingContext(t), bindingWS, bindingAG, bindingUser, nil, "resp_sess_otherws_turn1", "")
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("cross-workspace chained bind err = %v, want not-found", err)
		}
	})

	t.Run("malformed response id fails invalid", func(t *testing.T) {
		h := newBindingHandler(t, 0, nil)
		_, err := h.resolveSession(bindingContext(t), bindingWS, bindingAG, bindingUser, nil, "not-a-response-id", "")
		if !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("malformed chained bind err = %v, want invalid", err)
		}
	})
}
