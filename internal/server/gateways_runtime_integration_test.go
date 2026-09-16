//go:build integration

package server

// Integration regression for the multi-device lane's device-store isolation
// (add-whatsapp-gateway design D6 as amended): whatsmeow's sqlstore
// self-upgrade probes table existence through information_schema unqualified
// — database-wide — while resolving whatsmeow_version via search_path, so a
// per-gateway SCHEMA dies with 42P01 the moment any other schema holds
// whatsmeow_* tables (second gateway, or gateway recreate). The fix scopes
// each gateway to its own DEVICE DATABASE (see mdDatabaseName for the
// design-letter deviation). This test drives the real adapter start path —
// buildRealDevice runs the actual dbutil upgrade — against the live
// PostgreSQL instance.

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/oniharnantyo/onclaw/internal/gateways/adapters/whatsappmd"
)

// mdTestBaseDSN resolves the integration DSN (the setupTestSchema pattern).
func mdTestBaseDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		dsn = "postgres://localhost:5432/postgres?sslmode=disable"
	}
	return dsn
}

func mdTestConnect(t *testing.T, ctx context.Context, dsn string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Skipf("skipping integration test: database connection failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

func mdTestRandomHex(t *testing.T) string {
	t.Helper()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("failed to read random bytes: %v", err)
	}
	return hex.EncodeToString(b)
}

func TestIntegration_MdDevicePool_PerGatewayDatabases(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	dsn := mdTestBaseDSN(t)
	conn := mdTestConnect(t, ctx, dsn)

	// The pre-fix legacy state: another schema in the app database holding
	// whatsmeow tables (left by the old per-gateway-schema design or by a
	// previous smoke run). It must be irrelevant to new gateways.
	legacySchema := "whatsmeow_legacy_" + mdTestRandomHex(t)
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+legacySchema); err != nil {
		t.Fatalf("failed to create legacy schema: %v", err)
	}
	if _, err := conn.Exec(ctx,
		"CREATE TABLE "+legacySchema+".whatsmeow_version (version INTEGER, compat INTEGER)"); err != nil {
		t.Fatalf("failed to create legacy whatsmeow_version: %v", err)
	}
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+legacySchema+" CASCADE")
	})

	rt := &GatewayRuntime{databaseURL: dsn, mdDBs: make(map[string]*sql.DB)}

	// Three gateway identities: A and B simulate two workspaces (or two
	// concurrent md gateways on one instance); C simulates the
	// delete-and-recreate flow (a brand-new gateway id after logout).
	gatewayIDs := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		for _, gwID := range gatewayIDs {
			if db, ok := rt.mdDBs[gwID]; ok {
				_ = db.Close()
			}
			dropConn, err := pgx.Connect(cleanupCtx, dsn)
			if err != nil {
				return
			}
			// WITH (FORCE) evicts any straggler connection so the device
			// databases never accumulate across test runs.
			_, _ = dropConn.Exec(cleanupCtx,
				"DROP DATABASE IF EXISTS "+mdDatabaseName(gwID)+" WITH (FORCE)")
			_ = dropConn.Close(cleanupCtx)
		}
	})

	startAdapter := func(gwID string) {
		t.Helper()
		db, err := rt.mdDevicePool(gwID)
		if err != nil {
			t.Fatalf("gateway %s: mdDevicePool: %v", gwID, err)
		}
		a, err := whatsappmd.NewAdapter(gwID, db, nopInboundHandler{})
		if err != nil {
			t.Fatalf("gateway %s: new adapter: %v", gwID, err)
		}
		if err := a.Start(ctx); err != nil {
			t.Fatalf("gateway %s: adapter start (runs the whatsmeow store upgrade): %v", gwID, err)
		}
		t.Cleanup(func() { _ = a.Stop(context.Background()) })
	}

	// 1. Virgin instance: the first md gateway provisions and upgrades its
	// device database.
	startAdapter(gatewayIDs[0])

	// 2. THE regression: the second md gateway starts while the first one's
	// whatsmeow tables exist. The per-gateway-schema design failed here with
	// `relation "whatsmeow_version" does not exist (SQLSTATE 42P01)`.
	startAdapter(gatewayIDs[1])

	// 3. Recreate-after-logout: a third, brand-new gateway id starts cleanly.
	startAdapter(gatewayIDs[2])

	// 4. Restart survival: a boot re-open returns the cached pool as-is (no
	// drop/recreate — device sessions must survive restarts) and the device
	// databases still exist.
	for i, gwID := range gatewayIDs {
		db, err := rt.mdDevicePool(gwID)
		if err != nil {
			t.Fatalf("gateway %d: re-open: %v", i, err)
		}
		if db != rt.mdDBs[gwID] {
			t.Fatalf("gateway %d: expected the cached pool to be reused, got a new one", i)
		}
		var exists bool
		if err := conn.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`,
			mdDatabaseName(gwID),
		).Scan(&exists); err != nil {
			t.Fatalf("gateway %d: probe device database: %v", i, err)
		}
		if !exists {
			t.Fatalf("gateway %d: expected device database %s to survive, it is gone", i, mdDatabaseName(gwID))
		}
	}

	// 5. The upgrade really ran in the gateway's own database: the
	// whatsmeow_device table lives there.
	for _, gwID := range gatewayIDs {
		db := rt.mdDBs[gwID]
		var upgraded bool
		if err := db.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'whatsmeow_device')`,
		).Scan(&upgraded); err != nil {
			t.Fatalf("gateway %s: probe upgraded tables: %v", gwID, err)
		}
		if !upgraded {
			t.Fatalf("gateway %s: expected whatsmeow_device in its device database after the upgrade", gwID)
		}
	}
}
