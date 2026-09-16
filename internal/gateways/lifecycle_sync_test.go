package gateways

import (
	"context"
	"errors"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// Manager.Sync platform-registry tests (add-whatsapp-gateway design D10,
// task 2.7): the manager reconciles every supported platform row
// independently, skips decryption for credential-free lanes, and rebuilds an
// adapter when the lane changes.

// recordingDecryptor counts decryptions and refuses empty envelopes —
// multi_device rows store no credential, so a decrypt attempt against one is
// a contract breach (design D5).
type recordingDecryptor struct {
	calls int
}

func (d *recordingDecryptor) DecryptGatewayToken(_ context.Context, _, envelope string) (string, error) {
	if envelope == "" {
		return "", errors.New("must not decrypt an empty envelope")
	}
	d.calls++
	return "plaintext-token", nil
}

// recordingFactory records AdapterSpecs and hands back capability-full fakes.
type recordingFactory struct {
	specs []AdapterSpec
}

func (f *recordingFactory) build(spec AdapterSpec) (PlatformAdapter, error) {
	f.specs = append(f.specs, spec)
	return newTestPlatformAdapter(), nil
}

func newSyncTestManager(t *testing.T, f *gwFixture) (*Manager, *recordingFactory, *recordingDecryptor) {
	t.Helper()
	svc := NewService(f.router, &fakeRunSubmitter{}, NewApprovalBridge(&fakeRunSubmitter{}, newTestPlatformAdapter(), f.st.GatewayLinks()), f.st.Gateways(), newTestOutbox(t))
	factory := &recordingFactory{}
	decryptor := &recordingDecryptor{}
	manager := NewManager(f.st.Gateways(), decryptor, factory.build, svc)
	return manager, factory, decryptor
}

func TestSyncReconcilesEveryPlatformRow(t *testing.T) {
	f := newFixture(t) // telegram gateway enabled by the fixture
	manager, factory, decryptor := newSyncTestManager(t, f)

	// A credential-free whatsapp multi_device gateway beside the Telegram one.
	if err := f.st.Gateways().CreateGateway(f.ctx, f.ws.ID, &domain.GatewayConfig{
		WorkspaceID: f.ws.ID,
		Platform:    domain.GatewayPlatformWhatsApp,
		Lane:        domain.GatewayLaneMultiDevice,
		Identity:    "self",
		AgentID:     f.atlas.ID,
		BotUsername: "self",
		Enabled:     true,
		Transport:   domain.GatewayTransportWebhook, // ignored on the md lane
		WebhookURL:  "https://example.com/hook",
	}); err != nil {
		t.Fatalf("create whatsapp gateway: %v", err)
	}

	if err := manager.Sync(f.ctx, f.ws.ID); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if len(factory.specs) != 2 {
		t.Fatalf("expected one adapter per enabled platform, specs: %#v", factory.specs)
	}
	byPlatform := map[string]AdapterSpec{}
	for _, spec := range factory.specs {
		byPlatform[spec.Platform] = spec
	}
	tg, ok := byPlatform[domain.GatewayPlatformTelegram]
	if !ok || tg.BotToken != "plaintext-token" {
		t.Fatalf("telegram spec missing or token not decrypted: %#v", tg)
	}
	wa, ok := byPlatform[domain.GatewayPlatformWhatsApp]
	if !ok {
		t.Fatalf("whatsapp adapter never built: %#v", factory.specs)
	}
	if wa.Lane != domain.GatewayLaneMultiDevice {
		t.Fatalf("whatsapp spec must carry the lane, got %q", wa.Lane)
	}
	if wa.BotToken != "" {
		t.Fatalf("multi_device lane is credential-free, got token %q", wa.BotToken)
	}
	if decryptor.calls != 1 {
		t.Fatalf("only the telegram envelope may be decrypted, calls=%d", decryptor.calls)
	}

	// Both adapters are attached to the service.
	if _, ok := svcAdapterFor(t, manager, f, domain.GatewayPlatformWhatsApp); !ok {
		t.Fatalf("whatsapp adapter not attached")
	}
}

func TestSyncRebuildsAdapterOnLaneChange(t *testing.T) {
	f := newFixture(t)
	manager, factory, _ := newSyncTestManager(t, f)

	waGateway := &domain.GatewayConfig{
		WorkspaceID: f.ws.ID,
		Platform:    domain.GatewayPlatformWhatsApp,
		Lane:        domain.GatewayLaneMultiDevice,
		Identity:    "self",
		AgentID:     f.atlas.ID,
		Enabled:     true,
	}
	if err := f.st.Gateways().CreateGateway(f.ctx, f.ws.ID, waGateway); err != nil {
		t.Fatalf("create md gateway: %v", err)
	}
	if err := manager.Sync(f.ctx, f.ws.ID); err != nil {
		t.Fatalf("Sync (md): %v", err)
	}

	// Switch the lane to cloud_api (token + webhook): the lane participates
	// in the reconciliation diff, so the adapter must be rebuilt.
	waGateway.Lane = domain.GatewayLaneCloudAPI
	waGateway.BotTokenCiphertext = "v1:bm9uY2U=:Y2lwaGVydGV4dA=="
	waGateway.Transport = domain.GatewayTransportWebhook
	waGateway.WebhookURL = "https://example.com/hook"
	if err := f.st.Gateways().UpdateGateway(f.ctx, f.ws.ID, waGateway); err != nil {
		t.Fatalf("update cloud gateway: %v", err)
	}
	if err := manager.Sync(f.ctx, f.ws.ID); err != nil {
		t.Fatalf("Sync (cloud): %v", err)
	}

	waSpecs := 0
	var last AdapterSpec
	for _, spec := range factory.specs {
		if spec.Platform == domain.GatewayPlatformWhatsApp {
			waSpecs++
			last = spec
		}
	}
	if waSpecs != 2 {
		t.Fatalf("lane change must rebuild the whatsapp adapter, builds=%d", waSpecs)
	}
	if last.Lane != domain.GatewayLaneCloudAPI || last.BotToken == "" {
		t.Fatalf("rebuilt spec must be the cloud lane with a decrypted token: %#v", last)
	}
}

func TestSyncStopsDisabledPlatformOnly(t *testing.T) {
	f := newFixture(t)
	manager, _, _ := newSyncTestManager(t, f)
	if err := manager.Sync(f.ctx, f.ws.ID); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Disabling the telegram gateway stops only that platform; a later Sync
	// is a no-op for the absent whatsapp row.
	if err := f.st.Gateways().SetGatewayEnabled(f.ctx, f.ws.ID, f.gateway.ID, false); err != nil {
		t.Fatalf("disable telegram: %v", err)
	}
	if err := manager.Sync(f.ctx, f.ws.ID); err != nil {
		t.Fatalf("Sync after disable: %v", err)
	}

	if _, ok := svcAdapterFor(t, manager, f, domain.GatewayPlatformTelegram); ok {
		t.Fatalf("disabled telegram gateway must be stopped")
	}
}

// svcAdapterFor resolves the service's attached adapter for one platform via
// the manager's service registry.
func svcAdapterFor(t *testing.T, m *Manager, f *gwFixture, platform string) (PlatformAdapter, bool) {
	t.Helper()
	return m.service.AdapterForWorkspace(f.ws.ID, platform)
}
