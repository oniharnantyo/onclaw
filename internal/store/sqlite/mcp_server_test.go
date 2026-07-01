package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/store"
)

func TestMCPServerStore(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	ms := NewMCPServerStore(db)

	// Test adding invalid server (empty name)
	invalidSrv := &store.MCPServer{Name: ""}
	if err := ms.AddServer(ctx, invalidSrv); err == nil {
		t.Error("expected error adding empty server name, got nil")
	}

	// Test adding valid server
	srv := &store.MCPServer{
		Name:      "test-mcp",
		Transport: "stdio",
		Command:   "node",
		Args:      `["arg1", "arg2"]`,
		Env:       `{"KEY": "VAL"}`,
		URL:       "",
		Enabled:   1,
	}

	if err := ms.AddServer(ctx, srv); err != nil {
		t.Fatalf("failed to AddServer: %v", err)
	}

	// Test getting server
	gotSrv, err := ms.GetServer(ctx, srv.Name)
	if err != nil {
		t.Fatalf("failed to GetServer: %v", err)
	}

	// Verify fields match
	if gotSrv.Name != srv.Name ||
		gotSrv.Transport != srv.Transport ||
		gotSrv.Command != srv.Command ||
		gotSrv.Args != srv.Args ||
		gotSrv.Env != srv.Env ||
		gotSrv.URL != srv.URL ||
		gotSrv.Enabled != srv.Enabled {
		t.Errorf("server fields mismatch. got: %+v, want: %+v", gotSrv, srv)
	}

	// Verify timestamps
	if gotSrv.CreatedAt == "" || gotSrv.UpdatedAt == "" {
		t.Error("expected CreatedAt and UpdatedAt to be set, got empty strings")
	}

	// Test getting non-existent server returns sql.ErrNoRows
	_, err = ms.GetServer(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error getting nonexistent server, got nil")
	} else if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}

	// Test listing servers
	list, err := ms.ListServers(ctx)
	if err != nil {
		t.Fatalf("failed to ListServers: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected list length 1, got %d", len(list))
	}
	if list[0].Name != srv.Name {
		t.Errorf("expected server name %s, got %s", srv.Name, list[0].Name)
	}

	// Test updating server
	srv.Transport = "http"
	srv.URL = "http://localhost:8080/mcp"
	srv.Command = ""
	srv.Args = "[]"
	srv.Env = "{}"
	if err := ms.UpdateServer(ctx, srv); err != nil {
		t.Fatalf("failed to UpdateServer: %v", err)
	}

	gotSrv, err = ms.GetServer(ctx, srv.Name)
	if err != nil {
		t.Fatalf("failed to GetServer after update: %v", err)
	}
	if gotSrv.Transport != "http" || gotSrv.URL != "http://localhost:8080/mcp" || gotSrv.Command != "" {
		t.Errorf("updated fields mismatch. got: %+v", gotSrv)
	}

	// Test adding duplicate server fails
	if err := ms.AddServer(ctx, srv); err == nil {
		t.Error("expected error when adding duplicate server, got nil")
	}

	// Test removing server
	if err := ms.RemoveServer(ctx, srv.Name); err != nil {
		t.Fatalf("failed to RemoveServer: %v", err)
	}

	// Verify server was removed
	_, err = ms.GetServer(ctx, srv.Name)
	if err == nil {
		t.Error("expected error getting removed server, got nil")
	}

	// Test removing non-existent server
	if err := ms.RemoveServer(ctx, "nonexistent"); err == nil {
		t.Error("expected RemoveServer to return error for nonexistent server")
	} else if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows for removing nonexistent server, got %v", err)
	}
}
