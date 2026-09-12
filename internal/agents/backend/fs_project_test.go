package backend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	einofs "github.com/cloudwego/eino/adk/filesystem"
)

// newJailWithProject builds a jail whose primary root stands in for an agent
// dir and whose /project mount points at a real project directory
// (channel-teams D5).
func newJailWithProject(t *testing.T) (einofs.Backend, string, string) {
	t.Helper()
	primary := t.TempDir()
	project := t.TempDir()
	jail, err := NewFilesystemJailedWithMounts(primary,
		[]WritableMount{{Mount: ProjectMountPoint, Dir: project}})
	if err != nil {
		t.Fatalf("create jail with project mount: %v", err)
	}
	return jail, primary, project
}

func TestFilesystemJail_ProjectMountReadWrite(t *testing.T) {
	ctx := context.Background()

	t.Run("member agent writes then reads under /project", func(t *testing.T) {
		jail, _, project := newJailWithProject(t)

		if err := jail.Write(ctx, &einofs.WriteRequest{
			FilePath: ProjectMountPoint + "/PLAN.md",
			Content:  "# Plan\n\n- backend claims the queue",
		}); err != nil {
			t.Fatalf("write via /project mount: %v", err)
		}
		// The write landed in the shared host directory, not the agent dir.
		data, err := os.ReadFile(filepath.Join(project, "PLAN.md"))
		if err != nil {
			t.Fatalf("expected PLAN.md under the host project dir: %v", err)
		}
		if !strings.Contains(string(data), "backend claims the queue") {
			t.Fatalf("unexpected PLAN.md content: %q", data)
		}

		content, err := jail.Read(ctx, &einofs.ReadRequest{FilePath: ProjectMountPoint + "/PLAN.md"})
		if err != nil {
			t.Fatalf("read via /project mount: %v", err)
		}
		if !strings.Contains(content.Content, "backend claims the queue") {
			t.Fatalf("unexpected read content: %q", content.Content)
		}

		// Nested paths create their directories inside the project root.
		if err := jail.Write(ctx, &einofs.WriteRequest{
			FilePath: ProjectMountPoint + "/backend/notes.md",
			Content:  "claimed",
		}); err != nil {
			t.Fatalf("nested write via mount: %v", err)
		}
		if _, err := os.Stat(filepath.Join(project, "backend", "notes.md")); err != nil {
			t.Fatalf("expected nested file inside the project dir: %v", err)
		}
	})

	t.Run("project dir is readable by absolute path too", func(t *testing.T) {
		jail, _, project := newJailWithProject(t)
		target := filepath.Join(project, "spec.md")
		if err := os.WriteFile(target, []byte("shared spec"), 0o644); err != nil {
			t.Fatalf("seed: %v", err)
		}
		content, err := jail.Read(ctx, &einofs.ReadRequest{FilePath: target})
		if err != nil {
			t.Fatalf("absolute read under project dir: %v", err)
		}
		if content.Content != "shared spec" {
			t.Fatalf("content = %q", content.Content)
		}
	})

	t.Run("edit under /project works", func(t *testing.T) {
		jail, _, _ := newJailWithProject(t)
		if err := jail.Write(ctx, &einofs.WriteRequest{
			FilePath: ProjectMountPoint + "/PLAN.md", Content: "status: todo",
		}); err != nil {
			t.Fatalf("seed write: %v", err)
		}
		if err := jail.Edit(ctx, &einofs.EditRequest{
			FilePath: ProjectMountPoint + "/PLAN.md", OldString: "todo", NewString: "done",
		}); err != nil {
			t.Fatalf("edit via mount: %v", err)
		}
		content, err := jail.Read(ctx, &einofs.ReadRequest{FilePath: ProjectMountPoint + "/PLAN.md"})
		if err != nil || content.Content != "status: done" {
			t.Fatalf("edited content = %q, err %v", content, err)
		}
	})

	t.Run("mount .. escape rejected", func(t *testing.T) {
		jail, _, _ := newJailWithProject(t)
		if _, err := jail.Read(ctx, &einofs.ReadRequest{
			FilePath: ProjectMountPoint + "/../secret.txt",
		}); err == nil || !strings.Contains(err.Error(), "..") {
			t.Errorf("expected .. rejection, got: %v", err)
		}
		if err := jail.Write(ctx, &einofs.WriteRequest{
			FilePath: ProjectMountPoint + "/../escape.md", Content: "x",
		}); err == nil || !strings.Contains(err.Error(), "..") {
			t.Errorf("expected .. write rejection, got: %v", err)
		}
	})

	t.Run("symlink escape from /project rejected", func(t *testing.T) {
		jail, primary, project := newJailWithProject(t)

		// A symlink inside the project dir pointing into the agent dir:
		// reads may land in any jail root, so this one resolves — but a link
		// OUTSIDE every root must not.
		insideLink := filepath.Join(project, "agent_link")
		if err := os.Symlink(primary, insideLink); err != nil {
			t.Fatalf("symlink: %v", err)
		}

		outsideDir := t.TempDir()
		outsideFile := filepath.Join(outsideDir, "secret.txt")
		if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		escapeLink := filepath.Join(project, "escape_link")
		if err := os.Symlink(outsideFile, escapeLink); err != nil {
			t.Fatalf("symlink: %v", err)
		}

		if _, err := jail.Read(ctx, &einofs.ReadRequest{FilePath: escapeLink}); err == nil {
			t.Fatal("expected symlink escaping all roots to be rejected")
		}
		if _, err := jail.Read(ctx, &einofs.ReadRequest{
			FilePath: ProjectMountPoint + "/escape_link",
		}); err == nil {
			t.Fatal("expected mount-scoped symlink escape to be rejected")
		}
		// A write through an in-jail link must stay within the mount (the
		// link targets the primary root, not the project dir).
		if err := jail.Write(ctx, &einofs.WriteRequest{
			FilePath: ProjectMountPoint + "/agent_link/planted.md", Content: "x",
		}); err == nil {
			t.Fatal("mount write must stay contained in the project dir")
		}
		_ = insideLink
	})

	t.Run("raw absolute write still rejected", func(t *testing.T) {
		jail, _, project := newJailWithProject(t)
		if err := jail.Write(ctx, &einofs.WriteRequest{
			FilePath: filepath.Join(project, "direct.md"), Content: "x",
		}); err == nil || !strings.Contains(err.Error(), "absolute paths are not allowed") {
			t.Fatalf("expected absolute write rejection, got: %v", err)
		}
	})

	t.Run("prefix confusion rejected", func(t *testing.T) {
		jail, _, _ := newJailWithProject(t)
		if _, err := jail.Read(ctx, &einofs.ReadRequest{FilePath: ProjectMountPoint + "foo/x.md"}); err == nil {
			t.Fatal("expected /projectfoo to stay outside the mount")
		}
		if err := jail.Write(ctx, &einofs.WriteRequest{
			FilePath: "/tmp/onclaw-should-not-exist.md", Content: "x",
		}); err == nil {
			t.Fatal("expected absolute write rejection")
		}
	})

	t.Run("primary workspace behavior unchanged alongside the mount", func(t *testing.T) {
		jail, primary, _ := newJailWithProject(t)
		if err := jail.Write(ctx, &einofs.WriteRequest{
			FilePath: DefaultMountPoint + "/notes.md", Content: "mine",
		}); err != nil {
			t.Fatalf("primary mount write must keep working: %v", err)
		}
		if _, err := os.Stat(filepath.Join(primary, "notes.md")); err != nil {
			t.Fatalf("expected file under the primary root: %v", err)
		}
	})
}

// A jail without the mount — the non-member agent's composition — has no
// /project at all (channel-teams D5: agents outside the channel get none).
func TestFilesystemJail_NoProjectMountForNonMembers(t *testing.T) {
	ctx := context.Background()
	primary := t.TempDir()
	jail, err := NewFilesystemJailedWithRoots(primary)
	if err != nil {
		t.Fatalf("create jail: %v", err)
	}

	if _, err := jail.Read(ctx, &einofs.ReadRequest{FilePath: ProjectMountPoint + "/PLAN.md"}); err == nil {
		t.Fatal("expected /project reads to fail without the mount")
	}
	if err := jail.Write(ctx, &einofs.WriteRequest{
		FilePath: ProjectMountPoint + "/PLAN.md", Content: "x",
	}); err == nil {
		t.Fatal("expected /project writes to fail without the mount")
	}
}

func TestFilesystemJail_MountValidation(t *testing.T) {
	t.Run("missing mount dir is an error, not a skip", func(t *testing.T) {
		primary := t.TempDir()
		missing := filepath.Join(t.TempDir(), "projects", "ops")
		_, err := NewFilesystemJailedWithMounts(primary,
			[]WritableMount{{Mount: ProjectMountPoint, Dir: missing}})
		if err == nil {
			t.Fatal("mounted roots are load-bearing; absence must fail construction")
		}
	})

	t.Run("relative prefix rejected", func(t *testing.T) {
		primary := t.TempDir()
		_, err := NewFilesystemJailedWithMounts(primary,
			[]WritableMount{{Mount: "project", Dir: t.TempDir()}})
		if err == nil {
			t.Fatal("relative mount prefixes must be rejected")
		}
	})

	t.Run("shadowing the primary mount rejected", func(t *testing.T) {
		primary := t.TempDir()
		_, err := NewFilesystemJailedWithMounts(primary,
			[]WritableMount{{Mount: DefaultMountPoint, Dir: t.TempDir()}})
		if err == nil {
			t.Fatal("a mount shadowing /workspace must be rejected")
		}
	})

	t.Run("overlapping mounts rejected", func(t *testing.T) {
		primary := t.TempDir()
		_, err := NewFilesystemJailedWithMounts(primary, []WritableMount{
			{Mount: "/project", Dir: t.TempDir()},
			{Mount: "/project/nested", Dir: t.TempDir()},
		})
		if err == nil {
			t.Fatal("nested mount prefixes must be rejected")
		}
	})

	t.Run("read-only extra root stays read-only beside the mount", func(t *testing.T) {
		primary := t.TempDir()
		skills := t.TempDir()
		project := t.TempDir()
		jail, err := NewFilesystemJailedWithMounts(primary,
			[]WritableMount{{Mount: ProjectMountPoint, Dir: project}}, skills)
		if err != nil {
			t.Fatalf("create jail: %v", err)
		}
		target := filepath.Join(skills, "s.txt")
		if err := os.WriteFile(target, []byte("original"), 0o644); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if err := jail.Write(context.Background(), &einofs.WriteRequest{FilePath: target, Content: "tampered"}); err == nil {
			t.Fatal("extra root must remain read-only")
		}
	})
}
