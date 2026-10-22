package services

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/store/fake"
)

func TestNewSettingsService_DefaultsAreNullObjects(t *testing.T) {
	svc := NewSettingsService(fake.New())

	// The no-op defaults keep every dependency non-nil without a host
	// (dependency non-nil contract): events are discarded and browser
	// opens fail with ErrNoBrowser.
	require.NotPanics(t, func() { svc.emitter.Emit("gateway:state", GatewayStateEvent{State: "running"}) })
	require.ErrorIs(t, svc.urlOpener.Open("https://example"), ErrNoBrowser)
}

func TestSettingsOptions_NilIgnoredKeepsDefaults(t *testing.T) {
	svc := NewSettingsService(fake.New(), WithEmitter(nil), WithURLOpener(nil))

	require.NotPanics(t, func() { svc.emitter.Emit("gateway:state") })
	require.ErrorIs(t, svc.urlOpener.Open("https://example"), ErrNoBrowser)
}

func TestSettingsOptions_SuppliedFakesInstalled(t *testing.T) {
	em := &recordingEmitter{}
	op := &recordingOpener{}
	svc := NewSettingsService(fake.New(), WithEmitter(em), WithURLOpener(op))

	svc.emitter.Emit(EventMCPOAuthCompleted, MCPOAuthCompletedEvent{ServerID: "s1", OK: true})
	require.NoError(t, svc.urlOpener.Open("https://example"))

	require.Len(t, em.recorded(), 1)
	require.Equal(t, []string{"https://example"}, op.urls)
}
