package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestValidateGatewayConfig_AcceptsLongPollingAndNormalizes(t *testing.T) {
	g := &GatewayConfig{
		Platform:           " telegram ",
		AgentID:            " agent_123 ",
		BotTokenCiphertext: "v1:bm9uY2U=:Y2lwaGVydGV4dA==",
		BotUsername:        " onclaw_bot ",
		Transport:          GatewayTransportLongPolling,
		WebhookURL:         "https://example.com/hook",
	}
	if err := ValidateGatewayConfig(g); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if g.Platform != GatewayPlatformTelegram {
		t.Fatalf("expected platform trimmed to telegram, got %q", g.Platform)
	}
	if g.AgentID != "agent_123" {
		t.Fatalf("expected agent_id trimmed to agent_123, got %q", g.AgentID)
	}
	if g.BotUsername != "onclaw_bot" {
		t.Fatalf("expected username trimmed, got %q", g.BotUsername)
	}
	if g.Identity != "onclaw_bot" {
		t.Fatalf("expected identity defaulted to bot username, got %q", g.Identity)
	}
	if g.WebhookURL != "" {
		t.Fatalf("expected long-polling to clear webhook_url, got %q", g.WebhookURL)
	}
}

func TestValidateGatewayConfig_WebhookRequiresHTTPSURL(t *testing.T) {
	for _, bad := range []string{"", "http://example.com/hook", "ftp://example.com", "example.com/hook"} {
		g := &GatewayConfig{
			Platform:           GatewayPlatformTelegram,
			AgentID:            "agent_1",
			BotTokenCiphertext: "v1:bm9uY2U=:Y2lwaGVydGV4dA==",
			Transport:          GatewayTransportWebhook,
			WebhookURL:         bad,
		}
		if err := ValidateGatewayConfig(g); !errors.Is(err, ErrInvalid) {
			t.Fatalf("expected ErrInvalid for invalid webhook url %q, got %v", bad, err)
		}
	}
	g := &GatewayConfig{
		Platform:           GatewayPlatformTelegram,
		AgentID:            "agent_1",
		BotTokenCiphertext: "v1:bm9uY2U=:Y2lwaGVydGV4dA==",
		Transport:          GatewayTransportWebhook,
		WebhookURL:         "https://example.com/hook",
	}
	if err := ValidateGatewayConfig(g); err != nil {
		t.Fatalf("unexpected error for valid webhook url: %v", err)
	}
}

func TestValidateGatewayConfig_RejectsPlaintextToken(t *testing.T) {
	for _, ct := range []string{"", "123456:ABC-DEF", "v1:only-two", "v2:a:b"} {
		g := &GatewayConfig{
			Platform:           GatewayPlatformTelegram,
			AgentID:            "agent_1",
			BotTokenCiphertext: ct,
			Transport:          GatewayTransportLongPolling,
		}
		if err := ValidateGatewayConfig(g); !errors.Is(err, ErrInvalid) {
			t.Fatalf("expected ErrInvalid for ciphertext %q, got %v", ct, err)
		}
	}
}

func TestValidateGatewayConfig_RequiresAgentID(t *testing.T) {
	for _, badAgent := range []string{"", "   "} {
		g := &GatewayConfig{
			Platform:           GatewayPlatformTelegram,
			AgentID:            badAgent,
			BotTokenCiphertext: "v1:bm9uY2U=:Y2lwaGVydGV4dA==",
			Transport:          GatewayTransportLongPolling,
		}
		if err := ValidateGatewayConfig(g); !errors.Is(err, ErrInvalid) {
			t.Fatalf("expected ErrInvalid for empty agent_id, got %v", err)
		}
	}
}

func TestValidateGatewayConfig_LaneRules(t *testing.T) {
	cloudToken := "v1:bm9uY2U=:Y2lwaGVydGV4dA=="

	t.Run("telegram ignores lane entirely", func(t *testing.T) {
		g := &GatewayConfig{
			Platform:           GatewayPlatformTelegram,
			AgentID:            "agent_1",
			Lane:               GatewayLaneCloudAPI,
			BotTokenCiphertext: cloudToken,
			Transport:          GatewayTransportLongPolling,
		}
		if err := ValidateGatewayConfig(g); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if g.Lane != "" {
			t.Fatalf("expected telegram row to normalize lane to empty, got %q", g.Lane)
		}
	})

	t.Run("whatsapp requires a lane", func(t *testing.T) {
		g := &GatewayConfig{
			Platform:           GatewayPlatformWhatsApp,
			AgentID:            "agent_1",
			BotTokenCiphertext: cloudToken,
			Transport:          GatewayTransportWebhook,
			WebhookURL:         "https://example.com/hook",
		}
		if err := ValidateGatewayConfig(g); !errors.Is(err, ErrInvalid) {
			t.Fatalf("expected ErrInvalid for missing whatsapp lane, got %v", err)
		}
	})

	t.Run("cloud_api requires webhook transport", func(t *testing.T) {
		g := &GatewayConfig{
			Platform:           GatewayPlatformWhatsApp,
			AgentID:            "agent_1",
			Lane:               GatewayLaneCloudAPI,
			BotTokenCiphertext: cloudToken,
			Transport:          GatewayTransportLongPolling,
		}
		if err := ValidateGatewayConfig(g); !errors.Is(err, ErrInvalid) {
			t.Fatalf("expected ErrInvalid for long-polling cloud_api, got %v", err)
		}
	})

	t.Run("cloud_api requires a credential envelope", func(t *testing.T) {
		g := &GatewayConfig{
			Platform:   GatewayPlatformWhatsApp,
			AgentID:    "agent_1",
			Lane:       GatewayLaneCloudAPI,
			Transport:  GatewayTransportWebhook,
			WebhookURL: "https://example.com/hook",
		}
		if err := ValidateGatewayConfig(g); !errors.Is(err, ErrInvalid) {
			t.Fatalf("expected ErrInvalid for missing cloud credential, got %v", err)
		}
	})

	t.Run("cloud_api valid", func(t *testing.T) {
		g := &GatewayConfig{
			Platform:           GatewayPlatformWhatsApp,
			AgentID:            "agent_1",
			Lane:               GatewayLaneCloudAPI,
			BotTokenCiphertext: cloudToken,
			Transport:          GatewayTransportWebhook,
			WebhookURL:         "https://example.com/hook",
		}
		if err := ValidateGatewayConfig(g); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("multi_device stores no token and ignores transport", func(t *testing.T) {
		g := &GatewayConfig{
			Platform:           GatewayPlatformWhatsApp,
			AgentID:            "agent_1",
			Lane:               GatewayLaneMultiDevice,
			BotTokenCiphertext: cloudToken, // must be refused
			Transport:          GatewayTransportWebhook,
			WebhookURL:         "https://example.com/hook",
		}
		if err := ValidateGatewayConfig(g); !errors.Is(err, ErrInvalid) {
			t.Fatalf("expected ErrInvalid for multi_device with a token, got %v", err)
		}

		g.BotTokenCiphertext = ""
		if err := ValidateGatewayConfig(g); err != nil {
			t.Fatalf("unexpected error for tokenless multi_device: %v", err)
		}
		if g.Transport != GatewayTransportLongPolling || g.WebhookURL != "" {
			t.Fatalf("expected transport normalized to the inert shape, got %q %q", g.Transport, g.WebhookURL)
		}
	})

	t.Run("unknown lane rejected", func(t *testing.T) {
		g := &GatewayConfig{
			Platform:           GatewayPlatformWhatsApp,
			AgentID:            "agent_1",
			Lane:               "baileys",
			BotTokenCiphertext: cloudToken,
			Transport:          GatewayTransportWebhook,
			WebhookURL:         "https://example.com/hook",
		}
		if err := ValidateGatewayConfig(g); !errors.Is(err, ErrInvalid) {
			t.Fatalf("expected ErrInvalid for unknown lane, got %v", err)
		}
	})

	t.Run("unknown platform rejected", func(t *testing.T) {
		g := &GatewayConfig{
			Platform:           "slack",
			AgentID:            "agent_1",
			BotTokenCiphertext: cloudToken,
			Transport:          GatewayTransportLongPolling,
		}
		if err := ValidateGatewayConfig(g); !errors.Is(err, ErrInvalid) {
			t.Fatalf("expected ErrInvalid for unknown platform, got %v", err)
		}
	})
}

func TestValidateGatewayConfig_IdentityPreservedOrInferred(t *testing.T) {
	t.Run("explicit identity preserved", func(t *testing.T) {
		g := &GatewayConfig{
			Platform:           GatewayPlatformTelegram,
			AgentID:            "agent_1",
			Identity:           " @my_bot ",
			BotUsername:        "my_bot",
			BotTokenCiphertext: "v1:bm9uY2U=:Y2lwaGVydGV4dA==",
			Transport:          GatewayTransportLongPolling,
		}
		if err := ValidateGatewayConfig(g); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if g.Identity != "@my_bot" {
			t.Fatalf("expected identity '@my_bot', got %q", g.Identity)
		}
	})

	t.Run("identity inferred from bot username if empty", func(t *testing.T) {
		g := &GatewayConfig{
			Platform:           GatewayPlatformTelegram,
			AgentID:            "agent_1",
			Identity:           "   ",
			BotUsername:        " my_bot ",
			BotTokenCiphertext: "v1:bm9uY2U=:Y2lwaGVydGV4dA==",
			Transport:          GatewayTransportLongPolling,
		}
		if err := ValidateGatewayConfig(g); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if g.Identity != "my_bot" {
			t.Fatalf("expected identity 'my_bot', got %q", g.Identity)
		}
	})
}

func TestValidateUserLink(t *testing.T) {
	link := &UserLink{Platform: " telegram ", PlatformUserID: " 593821092 ", WorkspaceID: "ws", UserID: "u", PlatformUsername: " onih "}
	if err := ValidateUserLink(link); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if link.PlatformUserID != "593821092" || link.PlatformUsername != "onih" {
		t.Fatalf("expected trimmed fields, got %+v", link)
	}

	for _, mutate := range []func(*UserLink){
		func(l *UserLink) { l.Platform = "" },
		func(l *UserLink) { l.PlatformUserID = "" },
		func(l *UserLink) { l.WorkspaceID = "" },
		func(l *UserLink) { l.UserID = "" },
	} {
		bad := &UserLink{Platform: "telegram", PlatformUserID: "1", WorkspaceID: "ws", UserID: "u"}
		mutate(bad)
		if err := ValidateUserLink(bad); !errors.Is(err, ErrInvalid) {
			t.Fatalf("expected ErrInvalid for %+v, got %v", bad, err)
		}
	}
}

func TestValidatePairingToken(t *testing.T) {
	now := time.Now()
	token := &PairingToken{
		Token:       strings.Repeat("a", 43),
		WorkspaceID: "ws",
		UserID:      "u",
		ExpiresAt:   now.Add(time.Hour),
	}
	if err := ValidatePairingToken(token, now, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expired at mint is refused when creating.
	token.ExpiresAt = now.Add(-time.Minute)
	if err := ValidatePairingToken(token, now, true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for past expiry, got %v", err)
	}

	// Carried-past expiry is accepted on update (unchanged rows re-saved).
	if err := ValidatePairingToken(token, now, false); err != nil {
		t.Fatalf("unexpected error for carried-past expiry: %v", err)
	}
}

func TestValidatePairingTokenShape(t *testing.T) {
	if err := ValidatePairingTokenShape(strings.Repeat("a", 43)); err != nil {
		t.Fatalf("unexpected error for valid shape: %v", err)
	}
	for _, bad := range []string{
		"",                                   // empty
		strings.Repeat("a", 31),              // too short
		strings.Repeat("a", 129),             // too long
		strings.Repeat("a", 20) + " no-good", // whitespace / separator
		strings.Repeat("a", 20) + "!",        // punctuation
	} {
		if err := ValidatePairingTokenShape(bad); !errors.Is(err, ErrInvalidPairingToken) || !errors.Is(err, ErrInvalid) {
			t.Fatalf("expected ErrInvalidPairingToken wrapping ErrInvalid for %q, got %v", bad, err)
		}
	}
}

func TestValidateChatBinding(t *testing.T) {
	b := &ChatBinding{GatewayID: " gw_1 ", Platform: " telegram ", PlatformChatID: " -1001234567890 ", AgentID: " agent "}
	if err := ValidateChatBinding(b); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.GatewayID != "gw_1" || b.PlatformChatID != "-1001234567890" || b.AgentID != "agent" {
		t.Fatalf("expected trimmed fields, got %+v", b)
	}

	for _, mutate := range []func(*ChatBinding){
		func(c *ChatBinding) { c.Platform = "" },
		func(c *ChatBinding) { c.PlatformChatID = "" },
		func(c *ChatBinding) { c.AgentID = "" },
	} {
		bad := &ChatBinding{GatewayID: "gw_1", Platform: "telegram", PlatformChatID: "1", AgentID: "a"}
		mutate(bad)
		if err := ValidateChatBinding(bad); !errors.Is(err, ErrInvalid) {
			t.Fatalf("expected ErrInvalid for %+v, got %v", bad, err)
		}
	}
}

func TestValidateOutboxEntry(t *testing.T) {
	e := &OutboxEntry{WorkspaceID: "ws", SessionID: "sess_1", Payload: []byte(`{"a":1}`)}
	if err := ValidateOutboxEntry(e); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.Status != GatewayOutboxStatusPending {
		t.Fatalf("expected status forced to pending, got %q", e.Status)
	}

	for _, mutate := range []func(*OutboxEntry){
		func(o *OutboxEntry) { o.WorkspaceID = "" },
		func(o *OutboxEntry) { o.SessionID = "" },
		func(o *OutboxEntry) { o.Payload = nil },
	} {
		bad := &OutboxEntry{WorkspaceID: "ws", SessionID: "s", Payload: []byte("{}")}
		mutate(bad)
		if err := ValidateOutboxEntry(bad); !errors.Is(err, ErrInvalid) {
			t.Fatalf("expected ErrInvalid for %+v, got %v", bad, err)
		}
	}
}

func TestGatewaySentinelsWrapBaseSentinels(t *testing.T) {
	if !errors.Is(ErrGatewayBindingConflict, ErrConflict) {
		t.Fatal("expected ErrGatewayBindingConflict to wrap ErrConflict")
	}
	if !errors.Is(ErrInvalidPairingToken, ErrInvalid) || !errors.Is(ErrPairingTokenExpired, ErrInvalid) {
		t.Fatal("expected pairing token sentinels to wrap ErrInvalid")
	}
}
