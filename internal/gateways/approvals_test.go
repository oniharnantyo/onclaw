package gateways

import (
	"context"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

func newApprovalTestEnv(t *testing.T) (*ApprovalBridge, *testPlatformAdapter, *testRunSubmitter) {
	return newApprovalTestEnvWithCaps(t, true, true)
}

// newApprovalTestEnvWithCaps wires the bridge to an adapter in one
// capability-matrix cell (add-whatsapp-gateway design D2); the default env
// is the Telegram cell.
func newApprovalTestEnvWithCaps(t *testing.T, canEdit, canButton bool) (*ApprovalBridge, *testPlatformAdapter, *testRunSubmitter) {
	t.Helper()
	st := fake.New()
	ctx := context.Background()

	if err := st.Workspaces().Create(ctx, &domain.Workspace{ID: "ws1", Slug: "acme", Name: "Acme"}); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := st.Users().Create(ctx, &domain.User{ID: "user1", Email: "oni@example.com", Name: "Oni"}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	link := &domain.UserLink{
		Platform:       PlatformTelegram,
		PlatformUserID: "tg-111",
		UserID:         "user1",
	}
	if err := st.GatewayLinks().CreateUserLink(ctx, "ws1", link); err != nil {
		t.Fatalf("create link: %v", err)
	}

	adapter := newTestPlatformAdapterWithCaps(canEdit, canButton)
	submitter := &testRunSubmitter{resumeStream: agents.NewEventStream(8)}
	bridge := NewApprovalBridge(submitter, adapter, st.GatewayLinks())
	return bridge, adapter, submitter
}

func approvalTestSession() StreamSession {
	return StreamSession{GatewayID: "gw1", WorkspaceID: "ws1", SessionID: "sess1", ChatID: "chat1"}
}

func TestApprovalBridgePresentMarksPending(t *testing.T) {
	bridge, adapter, _ := newApprovalTestEnv(t)
	session := approvalTestSession()

	payload := agents.ApprovalPayload{InterruptID: "int-1", Command: "rm -rf /tmp/x"}
	if err := bridge.Present(context.Background(), session, testExecRequest(), payload); err != nil {
		t.Fatalf("Present: %v", err)
	}

	if !bridge.Pending(session.SessionID) {
		t.Fatalf("session should be pending")
	}
	cards := adapter.approvalCards()
	if len(cards) != 1 || cards[0].InterruptID != "int-1" {
		t.Fatalf("card not sent correctly: %#v", cards)
	}
}

func TestApprovalBridgeRefusesNewTurnsWhilePending(t *testing.T) {
	bridge, _, _ := newApprovalTestEnv(t)
	session := approvalTestSession()
	if bridge.Pending(session.SessionID) {
		t.Fatalf("fresh session must not be pending")
	}
	_ = bridge.Present(context.Background(), session, testExecRequest(), agents.ApprovalPayload{InterruptID: "int-1"})
	if !bridge.Pending(session.SessionID) {
		t.Fatalf("session should be pending after Present")
	}
}

func TestApprovalBridgeCallbackActorValidation(t *testing.T) {
	bridge, adapter, submitter := newApprovalTestEnv(t)
	ctx := context.Background()
	session := approvalTestSession()
	payload := agents.ApprovalPayload{InterruptID: "int-1", Command: "rm -rf /tmp/x"}
	if err := bridge.Present(ctx, session, testExecRequest(), payload); err != nil {
		t.Fatalf("Present: %v", err)
	}

	t.Run("unpaired actor refused", func(t *testing.T) {
		_, err := bridge.HandleCallback(ctx, Callback{
			Platform:   PlatformTelegram,
			ChatID:     "chat1",
			MessageID:  "card-1",
			FromUserID: "tg-999",
			Data:       EncodeApprovalCallback("int-1", true),
		})
		if err == nil || !strings.Contains(err.Error(), ErrApprovalUnpaired.Error()) {
			t.Fatalf("unpaired actor must be refused, got %v", err)
		}
		if !bridge.Pending(session.SessionID) {
			t.Fatalf("pending state must survive a refused callback")
		}
		if len(submitter.calls()) != 0 {
			t.Fatalf("unpaired callback must not resume")
		}
	})

	t.Run("unrecognized callback data refused", func(t *testing.T) {
		_, err := bridge.HandleCallback(ctx, Callback{
			Platform:   PlatformTelegram,
			ChatID:     "chat1",
			FromUserID: "tg-111",
			Data:       "something-else",
		})
		if err == nil || !strings.Contains(err.Error(), ErrApprovalNotPending.Error()) {
			t.Fatalf("forged callback data must be refused, got %v", err)
		}
	})

	t.Run("unknown interrupt refused", func(t *testing.T) {
		_, err := bridge.HandleCallback(ctx, Callback{
			Platform:   PlatformTelegram,
			ChatID:     "chat1",
			FromUserID: "tg-111",
			Data:       EncodeApprovalCallback("int-other", true),
		})
		if err == nil || !strings.Contains(err.Error(), ErrApprovalNotPending.Error()) {
			t.Fatalf("unknown interrupt must be refused, got %v", err)
		}
	})

	t.Run("paired actor approve resumes", func(t *testing.T) {
		stream, err := bridge.HandleCallback(ctx, Callback{
			Platform:   PlatformTelegram,
			ChatID:     "chat1",
			MessageID:  "card-1",
			FromUserID: "tg-111",
			Data:       EncodeApprovalCallback("int-1", true),
		})
		if err != nil {
			t.Fatalf("HandleCallback: %v", err)
		}
		if stream == nil {
			t.Fatalf("expected the resumed stream")
		}
		calls := submitter.calls()
		if len(calls) != 1 {
			t.Fatalf("expected exactly one resume, got %d", len(calls))
		}
		if !calls[0].Approved || calls[0].Approval.InterruptID != "int-1" {
			t.Fatalf("resume called with wrong decision: %#v", calls[0])
		}
		if calls[0].Req.SessionID != "sess1" || calls[0].Req.WorkspaceID != "ws1" {
			t.Fatalf("resume must reuse the original request: %#v", calls[0].Req)
		}
		if bridge.Pending(session.SessionID) {
			t.Fatalf("pending state must clear after the decision")
		}
		// The card records the decision and actor.
		edits := adapter.editedMessages()
		if len(edits) == 0 {
			t.Fatalf("card must be updated with the decision")
		}
		last := edits[len(edits)-1]
		if !strings.Contains(last.HTML, "approved") || !strings.Contains(last.HTML, "tg-111") {
			t.Fatalf("card decision not recorded: %q", last.HTML)
		}
	})

	t.Run("deny does not resume approval", func(t *testing.T) {
		if err := bridge.Present(ctx, session, testExecRequest(), agents.ApprovalPayload{InterruptID: "int-2"}); err != nil {
			t.Fatalf("Present: %v", err)
		}
		_, err := bridge.HandleCallback(ctx, Callback{
			Platform:   PlatformTelegram,
			ChatID:     "chat1",
			FromUserID: "tg-111",
			Data:       EncodeApprovalCallback("int-2", false),
		})
		if err != nil {
			t.Fatalf("HandleCallback deny: %v", err)
		}
		calls := submitter.calls()
		if len(calls) != 2 || calls[1].Approved {
			t.Fatalf("deny must resume with approved=false: %#v", calls)
		}
		if bridge.Pending(session.SessionID) {
			t.Fatalf("pending must clear after deny")
		}
	})
}

func TestApprovalBridgeResumeFailureKeepsPending(t *testing.T) {
	bridge, _, submitter := newApprovalTestEnv(t)
	submitter.resumeErr = context.DeadlineExceeded
	ctx := context.Background()
	session := approvalTestSession()
	if err := bridge.Present(ctx, session, testExecRequest(), agents.ApprovalPayload{InterruptID: "int-1"}); err != nil {
		t.Fatalf("Present: %v", err)
	}

	_, err := bridge.HandleCallback(ctx, Callback{
		Platform:   PlatformTelegram,
		ChatID:     "chat1",
		FromUserID: "tg-111",
		Data:       EncodeApprovalCallback("int-1", true),
	})
	if err == nil {
		t.Fatalf("resume failure must surface")
	}
	if !bridge.Pending(session.SessionID) {
		t.Fatalf("the card stays the only path forward: pending must be kept on resume failure")
	}
}

func TestApprovalCallbackEncoding(t *testing.T) {
	id, approved, ok := DecodeApprovalCallback(EncodeApprovalCallback("int-9", true))
	if !ok || id != "int-9" || !approved {
		t.Fatalf("approve roundtrip failed: %q %v %v", id, approved, ok)
	}
	id, approved, ok = DecodeApprovalCallback(EncodeApprovalCallback("int-9", false))
	if !ok || id != "int-9" || approved {
		t.Fatalf("deny roundtrip failed: %q %v %v", id, approved, ok)
	}
	if _, _, ok := DecodeApprovalCallback("bogus"); ok {
		t.Fatalf("bogus data must not decode")
	}
}

// testExecRequest is the turn request the approval resume must replay.
func testExecRequest() agents.ExecRequest {
	return agents.ExecRequest{
		WorkspaceID: "ws1",
		AgentID:     "agent1",
		SessionID:   "sess1",
		UserID:      "user1",
		Input:       "clean the tmp dir",
	}
}
