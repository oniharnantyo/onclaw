package channels

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

func TestLocalProjectSpace_Ensure(t *testing.T) {
	ctx := context.Background()
	st := fake.New()
	ws := &domain.Workspace{Slug: "acme", Name: "Acme"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	dataDir := t.TempDir()
	space := NewLocalProjectSpace(dataDir, st.Workspaces())

	dir, err := space.Ensure(ws.ID, "incidents")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	want := filepath.Join(domain.WorkspaceRoot(dataDir), "acme", "projects", "incidents")
	if dir != want {
		t.Fatalf("Ensure = %q, want %q", dir, want)
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("expected created directory at %s: %v", dir, err)
	}

	// Idempotent: a second Ensure succeeds and keeps existing content.
	if err := os.WriteFile(filepath.Join(dir, "PLAN.md"), []byte("# tracker"), 0o644); err != nil {
		t.Fatalf("seed PLAN.md: %v", err)
	}
	again, err := space.Ensure(ws.ID, "incidents")
	if err != nil || again != want {
		t.Fatalf("second Ensure = %q, %v", again, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "PLAN.md")); err != nil {
		t.Fatalf("Ensure must not wipe the directory: %v", err)
	}
}

func TestLocalProjectSpace_EnsureRejectsBadSlug(t *testing.T) {
	ctx := context.Background()
	st := fake.New()
	ws := &domain.Workspace{Slug: "acme", Name: "Acme"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	space := NewLocalProjectSpace(t.TempDir(), st.Workspaces())

	for _, slug := range []string{"", "../evil", "UpperCase", "spaces out", "slash/ed"} {
		if _, err := space.Ensure(ws.ID, slug); err == nil {
			t.Fatalf("Ensure must reject slug %q", slug)
		}
	}
	// Nothing may be created outside the layout.
	root := domain.WorkspaceRoot(space.dataDir)
	entries, _ := os.ReadDir(filepath.Join(root, "acme", "projects"))
	if len(entries) != 0 {
		t.Fatalf("rejected slugs must not create directories, got %v", entries)
	}
}

func TestLocalProjectSpace_EnsureUnknownWorkspace(t *testing.T) {
	st := fake.New()
	space := NewLocalProjectSpace(t.TempDir(), st.Workspaces())
	if _, err := space.Ensure("no-such-workspace", "incidents"); err == nil {
		t.Fatal("Ensure must fail for an unknown workspace")
	}
}

func TestLocalProjectSpace_Remove(t *testing.T) {
	ctx := context.Background()
	st := fake.New()
	ws := &domain.Workspace{Slug: "acme", Name: "Acme"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	space := NewLocalProjectSpace(t.TempDir(), st.Workspaces())

	dir, err := space.Ensure(ws.ID, "incidents")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "spec.md"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Removing the channel removes its project directory.
	if err := space.Remove(ws.ID, "incidents"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("project directory must be gone, stat err = %v", err)
	}

	// Removing a missing directory is a no-op.
	if err := space.Remove(ws.ID, "incidents"); err != nil {
		t.Fatalf("Remove of a missing directory must be a no-op, got %v", err)
	}

	// A bad slug is rejected before touching the filesystem.
	if err := space.Remove(ws.ID, "../evil"); err == nil || !strings.Contains(err.Error(), "slug") {
		t.Fatalf("Remove must validate the slug, got %v", err)
	}
}
