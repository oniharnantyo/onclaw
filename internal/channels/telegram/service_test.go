package telegram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDefaultBotFactoryHonorsEnvBase pins the BUDDIES_TELEGRAM_API_BASE
// override: the default factory points the client at the given root (the
// headless smoke test's seam).
func TestDefaultBotFactoryHonorsEnvBase(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"id":900,"username":"buddies_bot"}}`))
	}))
	t.Cleanup(srv.Close)

	t.Setenv(EnvAPIBase, srv.URL+"/")
	client, err := defaultBotFactory("tok", "")
	require.NoError(t, err)

	me, err := client.GetMe(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(900), me.ID)
	require.Equal(t, "/bot"+"tok"+"/getMe", gotPath)
}

// TestSetStateDeduplicates pins the sink contract: unchanged transitions do
// not re-emit (the 90s heartbeat would otherwise spam the pane).
func TestSetStateDeduplicates(t *testing.T) {
	fx := newFixture(t)

	fx.svc.setState(StateConnected, "")
	fx.svc.setState(StateConnected, "")
	fx.svc.setState(StateDegraded, "boom")
	fx.svc.setState(StateDegraded, "boom")
	fx.svc.setState(StateDegraded, "boom again")

	states := fx.recordedStates()
	require.Equal(t, []stateRec{
		{"telegram", StateConnected, ""},
		{"telegram", StateDegraded, "boom"},
		{"telegram", StateDegraded, "boom again"},
	}, states)
}
