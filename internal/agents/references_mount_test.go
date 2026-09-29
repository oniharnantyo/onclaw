package agents

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/components/tool"
	"github.com/go-pdf/fpdf"
	"github.com/oniharnantyo/onclaw/internal/agents/backend"
	"github.com/oniharnantyo/onclaw/internal/agents/tools"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/references"
	"github.com/oniharnantyo/onclaw/internal/storage/resolver"
	storagefake "github.com/oniharnantyo/onclaw/internal/storage/fake"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// toolInvoker is the InvokableRun seam every document-family tool exposes.
type toolInvoker interface {
	InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error)
}

// ---------------------------------------------------------------------------
// Harness: the real references.Service over the fake stores, seeded with the
// ids the run-assembly tests attach against (the references-package harness
// shape, local to the agents tests).
// ---------------------------------------------------------------------------

const refRunbookMD = "# Setup\nInstall steps go here.\n\n# Webhooks\nSigning requires the secret token.\n"

// pdfBytesForTest builds a PDF with one optional text line per page (the
// references-package fixture builder, local to the agents tests).
func pdfBytesForTest(t *testing.T, pages ...string) []byte {
	t.Helper()
	doc := fpdf.New("P", "mm", "A4", "")
	for _, text := range pages {
		doc.AddPage()
		if text != "" {
			doc.SetFont("helvetica", "", 14)
			doc.Text(10, 20, text)
		}
	}
	var buf bytes.Buffer
	if err := doc.Output(&buf); err != nil {
		t.Fatalf("build pdf fixture: %v", err)
	}
	return buf.Bytes()
}

type referencesRunHarness struct {
	ctx         context.Context
	svc         *references.Service
	st          store.Store
	wsID        string
	userID      string
	atlasID     string
	beaconID    string
	incidentsID string
}

func newReferencesRunHarness(t *testing.T, slug string) referencesRunHarness {
	t.Helper()
	s := storefake.New()
	stor := storagefake.New()
	// NewService takes the workspace storage resolver (route-reference-
	// documents-through-workspace-storage D1); no workspace storage configs
	// are seeded here, so it resolves the instance default exactly as the
	// raw driver did.
	wsStorage := resolver.New(stor, s.WorkspaceStorage(), s.Attachments(), []byte(testEncryptionKey), t.TempDir())
	ctx := context.Background()

	ws := &domain.Workspace{Slug: slug, Name: "WS " + slug}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	user := &domain.User{Email: slug + "-uploader@example.com", Name: "Uploader"}
	if err := s.Users().Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	atlas := &domain.Agent{WorkspaceID: ws.ID, Slug: "atlas", Name: "Atlas"}
	if err := s.Agents().Create(ctx, atlas); err != nil {
		t.Fatalf("create agent atlas: %v", err)
	}
	beacon := &domain.Agent{WorkspaceID: ws.ID, Slug: "beacon", Name: "Beacon"}
	if err := s.Agents().Create(ctx, beacon); err != nil {
		t.Fatalf("create agent beacon: %v", err)
	}
	incidents := &domain.Channel{WorkspaceID: ws.ID, Slug: "incidents", Name: "#incidents"}
	if err := s.Channels().CreateChannel(ctx, incidents); err != nil {
		t.Fatalf("create channel incidents: %v", err)
	}

	return referencesRunHarness{
		ctx:         ctx,
		svc:         references.NewService(s, wsStorage),
		st:          s,
		wsID:        ws.ID,
		userID:      user.ID,
		atlasID:     atlas.ID,
		beaconID:    beacon.ID,
		incidentsID: incidents.ID,
	}
}

// uploadDoc uploads one markdown document, failing the test on error.
func (h referencesRunHarness) uploadDoc(t *testing.T, name string, agentIDs []string, channelIDs []string) domain.ReferenceDocument {
	t.Helper()
	return h.uploadRaw(t, name, []byte(refRunbookMD), agentIDs, channelIDs)
}

// uploadRaw uploads one document with explicit bytes, failing the test on
// error.
func (h referencesRunHarness) uploadRaw(t *testing.T, name string, data []byte, agentIDs []string, channelIDs []string) domain.ReferenceDocument {
	t.Helper()
	doc, err := h.svc.Upload(h.ctx, h.wsID, h.userID, references.UploadInput{
		Filename: name,
		Data:     data,
		AgentIDs: agentIDs, ChannelIDs: channelIDs,
	})
	if err != nil {
		t.Fatalf("upload %q: %v", name, err)
	}
	return doc
}

// ---------------------------------------------------------------------------
// D7 scope resolution (the scheduler rule)
// ---------------------------------------------------------------------------

func TestReferencesRunScope(t *testing.T) {
	atlas := "agent-atlas"
	incidents := "chan-incidents"
	for _, tc := range []struct {
		name               string
		req                ExecRequest
		wantAgent          string
		wantChannelID      string
		wantChannelSession bool
	}{
		{
			name:      "direct chat: agent scope only",
			req:       ExecRequest{AgentID: atlas, SessionID: "sess_web1"},
			wantAgent: atlas,
		},
		{
			name:               "channel run: chan_ session + room",
			req:                ExecRequest{AgentID: atlas, SessionID: "chan_s1", ChannelID: incidents, Origin: OriginChannel},
			wantAgent:          atlas,
			wantChannelID:      incidents,
			wantChannelSession: true,
		},
		{
			name:      "channel id without a chan_ session binding is not a channel session",
			req:       ExecRequest{AgentID: atlas, SessionID: "sess_web2", ChannelID: incidents},
			wantAgent: atlas,
		},
		{
			name:      "scheduled run with a channel delivery target resolves by agent only (D7)",
			req:       ExecRequest{AgentID: atlas, SessionID: "sched_r1", ChannelID: incidents, Origin: OriginScheduler},
			wantAgent: atlas,
		},
		{
			name:      "heartbeat run never composes a channel scope",
			req:       ExecRequest{AgentID: atlas, SessionID: "hb_atlas", ChannelID: incidents, Origin: OriginHeartbeat},
			wantAgent: atlas,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := referencesRunScope(tc.req)
			if got.AgentID != tc.wantAgent {
				t.Errorf("AgentID = %q, want %q", got.AgentID, tc.wantAgent)
			}
			if got.IsChannelSession != tc.wantChannelSession {
				t.Errorf("IsChannelSession = %v, want %v", got.IsChannelSession, tc.wantChannelSession)
			}
			if got.ChannelID != tc.wantChannelID {
				t.Errorf("ChannelID = %q, want %q", got.ChannelID, tc.wantChannelID)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 4.3: the run-scoped references mount
// ---------------------------------------------------------------------------

// TestMaterializeReferencesMount_VisibleDocsOnly pins mount composition: the
// mount materializes exactly the run-visible documents under
// <tmp>/references-mount/<ws>/<agent>/<session>/references, and not-visible
// documents are unresolvable through it.
func TestMaterializeReferencesMount_VisibleDocsOnly(t *testing.T) {
	h := newReferencesRunHarness(t, "refs-mount")
	atlasDoc := h.uploadDoc(t, "atlas-runbook.md", []string{h.atlasID}, nil)
	channelDoc := h.uploadDoc(t, "channel-runbook.md", nil, []string{h.incidentsID})

	r := &Runner{onClawDir: t.TempDir(), references: h.svc}
	req := ExecRequest{
		WorkspaceID: h.wsID, AgentID: h.atlasID, SessionID: "sess_mount1",
	}
	dir, docs, err := r.materializeReferencesMount(context.Background(), req, referencesRunScope(req))
	if err != nil {
		t.Fatalf("materializeReferencesMount: %v", err)
	}
	// Only the atlas-attached document is visible to this direct-chat scope.
	if len(docs) != 1 || docs[0].ID != atlasDoc.ID {
		t.Fatalf("visible docs = %v, want exactly the atlas document (%s)", docIDs(docs), atlasDoc.ID)
	}
	wantDir := filepath.Join(referencesMountRoot(r.onClawDir),
		dropLaneSeg(h.wsID), dropLaneSeg(h.atlasID), dropLaneSeg("sess_mount1"), references.MountDirName)
	if dir != wantDir {
		t.Fatalf("mount dir = %q, want %q", dir, wantDir)
	}

	// The visible document resolves; the channel-attached one does not.
	if data, err := os.ReadFile(filepath.Join(dir, "atlas-runbook.md")); err != nil || string(data) != refRunbookMD {
		t.Errorf("mounted atlas-runbook.md = %q, %v; want the stored bytes as-is", string(data), err)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.Base(channelDoc.Name))); !os.IsNotExist(err) {
		t.Errorf("not-visible %s surfaced through the mount: %v", channelDoc.Name, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read mount: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("mount holds %d files, want exactly the visible document", len(entries))
	}
}

// TestMaterializeReferencesMount_SkipPaths pins the silent skips: unwired
// library and zero visible documents materialize nothing.
func TestMaterializeReferencesMount_SkipPaths(t *testing.T) {
	h := newReferencesRunHarness(t, "refs-skip")
	req := ExecRequest{WorkspaceID: h.wsID, AgentID: h.atlasID, SessionID: "sess_skip"}

	unwired := &Runner{onClawDir: t.TempDir()}
	dir, docs, err := unwired.materializeReferencesMount(context.Background(), req, referencesRunScope(req))
	if err != nil || dir != "" || docs != nil {
		t.Fatalf("unwired = (%q, %v, %v), want silent skip", dir, docs, err)
	}

	wired := &Runner{onClawDir: t.TempDir(), references: h.svc}
	// Nothing uploaded yet: zero visible docs skip silently.
	dir, docs, err = wired.materializeReferencesMount(context.Background(), req, referencesRunScope(req))
	if err != nil || dir != "" || docs != nil {
		t.Fatalf("zero visible = (%q, %v, %v), want silent skip", dir, docs, err)
	}
	if _, err := os.Stat(referencesMountRoot(wired.onClawDir)); !os.IsNotExist(err) {
		t.Errorf("skip paths must leave no filesystem footprint: %v", err)
	}
}

// TestTeardownAndSweepReferencesMount pins the drop-lane lifecycle parity:
// teardown removes the run dir, the sweep clears the whole root.
func TestTeardownAndSweepReferencesMount(t *testing.T) {
	h := newReferencesRunHarness(t, "refs-teardown")
	h.uploadDoc(t, "runbook.md", []string{h.atlasID}, nil)

	r := &Runner{onClawDir: t.TempDir(), references: h.svc}
	req := ExecRequest{WorkspaceID: h.wsID, AgentID: h.atlasID, SessionID: "sess_td"}
	dir, _, err := r.materializeReferencesMount(context.Background(), req, referencesRunScope(req))
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("mount missing after materialize: %v", err)
	}

	// Teardown removes the run dir; unwired runners are no-ops.
	r.teardownReferencesMount(req)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("mount survived teardown: %v", err)
	}
	unwired := &Runner{onClawDir: t.TempDir()}
	unwired.teardownReferencesMount(req) // must not panic

	// The sweep clears the root wholesale (crashed-run cleanup).
	if _, _, err := r.materializeReferencesMount(context.Background(), req, referencesRunScope(req)); err != nil {
		t.Fatalf("re-materialize: %v", err)
	}
	sweepReferencesMount(r.onClawDir)
	if _, err := os.Stat(referencesMountRoot(r.onClawDir)); !os.IsNotExist(err) {
		t.Errorf("sweep left the references-mount root behind: %v", err)
	}
}

// TestReferencesMountJail pins the D8 jail property against the real tool
// jail behavior: document.read resolves the mounted file through the
// read-only root, document.create refuses to WRITE into the subtree, and
// delete_file refuses to remove from it.
func TestReferencesMountJail(t *testing.T) {
	h := newReferencesRunHarness(t, "refs-jail")
	// document.read converts registered formats only (pdf among them), so the
	// fixture is a PDF.
	h.uploadRaw(t, "runbook.pdf", pdfBytesForTest(t, "Webhooks signing requires the secret token."), []string{h.atlasID}, nil)

	r := &Runner{onClawDir: t.TempDir(), references: h.svc}
	agentDir := t.TempDir()
	req := ExecRequest{WorkspaceID: h.wsID, AgentID: h.atlasID, SessionID: "sess_jail"}
	dir, _, err := r.materializeReferencesMount(context.Background(), req, referencesRunScope(req))
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}

	ctx := context.Background()

	// document.read: the read-only root resolves the mounted document.
	reader, err := tools.NewDocumentRead(agentDir, tools.WithReadOnlyRoots(dir))
	if err != nil {
		t.Fatalf("build document.read: %v", err)
	}
	out, err := reader.(toolInvoker).InvokableRun(ctx, `{"path":"`+filepath.Join(dir, "runbook.pdf")+`"}`)
	if err != nil {
		t.Fatalf("document.read over the mount: %v", err)
	}
	if !strings.Contains(out, "Webhooks") {
		t.Errorf("document.read over the mount lost the content: %q", out)
	}

	// document.create: the subtree is read-only — an output path inside the
	// mount escapes the write jail.
	creator, err := tools.NewDocumentCreate(agentDir, nil, nil, tools.WithDocumentCreateReadOnlyRoots(dir))
	if err != nil {
		t.Fatalf("build document.create: %v", err)
	}
	if _, err := creator.(toolInvoker).InvokableRun(ctx, `{"path":"`+filepath.Join(dir, "evil.pdf")+`","format":"pdf"}`); err == nil {
		t.Errorf("document.create wrote into the read-only references mount")
	}

	// delete_file: jailed to the agent dir — the mount is untouchable.
	deleter, err := tools.NewDeleteFile(agentDir)
	if err != nil {
		t.Fatalf("build delete_file: %v", err)
	}
	if _, err := deleter.(toolInvoker).InvokableRun(ctx, `{"path":"`+filepath.Join(dir, "runbook.pdf")+`"}`); err == nil {
		t.Errorf("delete_file removed from the read-only references mount")
	}
	if _, err := os.Stat(filepath.Join(dir, "runbook.pdf")); err != nil {
		t.Errorf("mounted document did not survive the write/delete attempts: %v", err)
	}
}

// TestReferencesMountFileToolsLane pins the file-tools visibility fix
// (fix-reference-document-retrieval 3.3, design D6 — session-events finding,
// 2026-09-28 run): with the mount materialized and linked into the
// workspace, the glob/ls lane — the fs middleware's jailed backend, fed the
// same read-only roots document.read gets — lists the mounted document at
// its documented path (`ls /workspace/references`) and finds it from a bare
// workspace-wide probe (`glob **/<name>` with no path argument). That exact
// run probed both forms first — glob answered "No files found" and ls
// "…/agents/personal-assistant/references: no such file or directory" —
// because the lane resolves /workspace onto the agent dir, where no
// references/ exists, and then defected to the shell. The subtree stays
// read-only (the link target is outside the agent dir), and the stale link
// is retired once the mount is gone.
func TestReferencesMountFileToolsLane(t *testing.T) {
	h := newReferencesRunHarness(t, "refs-filetools")
	h.uploadDoc(t, "runbook.md", []string{h.atlasID}, nil)

	r := &Runner{onClawDir: t.TempDir(), references: h.svc}
	agentDir := t.TempDir()
	req := ExecRequest{WorkspaceID: h.wsID, AgentID: h.atlasID, SessionID: "sess_filetools"}
	dir, _, err := r.materializeReferencesMount(context.Background(), req, referencesRunScope(req))
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if err := linkWorkspaceReferencesMount(agentDir, dir, referencesMountRoot(r.onClawDir)); err != nil {
		t.Fatalf("link mount into workspace: %v", err)
	}

	// The same jail shape the run composes: primary root = agent dir, the
	// read-only roots = the mount (skills/drop dirs are absent here).
	jail, err := backend.NewFilesystemJailedWithRoots(agentDir, dir)
	if err != nil {
		t.Fatalf("build jail: %v", err)
	}
	ctx := context.Background()

	// ls on the documented path lists the mounted document.
	entries, err := jail.LsInfo(ctx, &filesystem.LsInfoRequest{Path: "/workspace/references"})
	if err != nil {
		t.Fatalf("ls /workspace/references: %v", err)
	}
	if len(entries) != 1 || entries[0].Path != "runbook.md" {
		t.Fatalf("ls /workspace/references = %+v, want exactly runbook.md", entries)
	}

	// The bare workspace-wide probe finds the mounted document (the
	// regression's exact glob form).
	matches, err := jail.GlobInfo(ctx, &filesystem.GlobInfoRequest{Pattern: "**/runbook.md"})
	if err != nil {
		t.Fatalf("glob **/runbook.md: %v", err)
	}
	if len(matches) != 1 || matches[0].Path != "references/runbook.md" {
		t.Fatalf("glob **/runbook.md = %+v, want exactly references/runbook.md", matches)
	}

	// Read-only semantics survive the link: writes through the workspace
	// path are jailed out, while the agent dir itself stays writable.
	if err := jail.Write(ctx, &filesystem.WriteRequest{FilePath: "/workspace/references/evil.md", Content: "x"}); err == nil {
		t.Errorf("write through /workspace/references was not jailed out")
	}
	if _, err := os.Stat(filepath.Join(dir, "evil.md")); !os.IsNotExist(err) {
		t.Errorf("wrote into the read-only references mount: %v", err)
	}
	if err := jail.Write(ctx, &filesystem.WriteRequest{FilePath: "/workspace/notes.md", Content: "ok"}); err != nil {
		t.Errorf("write into the agent workspace itself failed: %v", err)
	}

	// Lifecycle parity: once the mount is gone, the stale link is retired so
	// the workspace never advertises a dead references/ entry.
	r.teardownReferencesMount(req)
	unlinkStaleWorkspaceReferencesMount(agentDir, referencesMountRoot(r.onClawDir))
	if _, err := os.Lstat(filepath.Join(agentDir, references.MountDirName)); !os.IsNotExist(err) {
		t.Errorf("stale references link survived cleanup: %v", err)
	}
}

// TestLinkWorkspaceReferencesMountLifecycle pins the link rules the run
// assembly relies on: fresh link, idempotent re-link, retarget to a new
// run's mount, and never clobbering a non-symlink occupant at the documented
// path (an agent-created references/ file or directory keeps both its
// identity and its contents).
func TestLinkWorkspaceReferencesMountLifecycle(t *testing.T) {
	mountA := filepath.Join(t.TempDir(), "references")
	if err := os.MkdirAll(mountA, 0o755); err != nil {
		t.Fatalf("mkdir mountA: %v", err)
	}
	mountBRoot := t.TempDir()
	mountB := filepath.Join(mountBRoot, "references")
	if err := os.MkdirAll(mountB, 0o755); err != nil {
		t.Fatalf("mkdir mountB: %v", err)
	}

	agentDir := t.TempDir()
	link := filepath.Join(agentDir, references.MountDirName)
	if err := linkWorkspaceReferencesMount(agentDir, mountA, filepath.Dir(mountA)); err != nil {
		t.Fatalf("link fresh: %v", err)
	}
	if target, err := os.Readlink(link); err != nil || target != mountA {
		t.Fatalf("fresh link = %q, %v; want %q", target, err, mountA)
	}
	// Idempotent: the same target links cleanly.
	if err := linkWorkspaceReferencesMount(agentDir, mountA, filepath.Dir(mountA)); err != nil {
		t.Fatalf("idempotent link: %v", err)
	}
	// Retarget: a fresh run's mount replaces the previous run's link.
	if err := linkWorkspaceReferencesMount(agentDir, mountB, mountBRoot); err != nil {
		t.Fatalf("retarget: %v", err)
	}
	if target, err := os.Readlink(link); err != nil || target != mountB {
		t.Fatalf("retargeted link = %q, %v; want %q", target, err, mountB)
	}

	// Occupant: an agent-owned references/ directory is never clobbered —
	// the link is skipped (warn) and the directory survives untouched.
	occupied := t.TempDir()
	occupant := filepath.Join(occupied, references.MountDirName)
	if err := os.MkdirAll(occupant, 0o755); err != nil {
		t.Fatalf("mkdir occupant: %v", err)
	}
	if err := os.WriteFile(filepath.Join(occupant, "agent-notes.md"), []byte("mine"), 0o644); err != nil {
		t.Fatalf("seed occupant: %v", err)
	}
	if err := linkWorkspaceReferencesMount(occupied, mountB, mountBRoot); err != nil {
		t.Fatalf("occupied path must not fail the run: %v", err)
	}
	if target, err := os.Readlink(occupant); err == nil {
		t.Errorf("occupant was replaced by a symlink to %q", target)
	}
	if data, err := os.ReadFile(filepath.Join(occupant, "agent-notes.md")); err != nil || string(data) != "mine" {
		t.Errorf("occupant contents disturbed: %q, %v", string(data), err)
	}

	// unlink hygiene: a live platform link stays (another run's mount may
	// still be serving it), a foreign symlink and a real directory are never
	// touched, and a missing link is a silent no-op.
	unlinkStaleWorkspaceReferencesMount(agentDir, mountBRoot) // mountB alive → link stays
	if target, err := os.Readlink(link); err != nil || target != mountB {
		t.Errorf("live link was removed: %q, %v", target, err)
	}
	foreign := t.TempDir()
	foreignLink := filepath.Join(foreign, references.MountDirName)
	if err := os.Symlink(t.TempDir(), foreignLink); err != nil {
		t.Fatalf("seed foreign link: %v", err)
	}
	unlinkStaleWorkspaceReferencesMount(foreign, mountBRoot)
	if _, err := os.Lstat(foreignLink); err != nil {
		t.Errorf("foreign symlink was removed: %v", err)
	}
	unlinkStaleWorkspaceReferencesMount(occupied, mountBRoot)
	if _, err := os.Stat(filepath.Join(occupant, "agent-notes.md")); err != nil {
		t.Errorf("occupant directory was removed by hygiene: %v", err)
	}
	unlinkStaleWorkspaceReferencesMount(t.TempDir(), mountBRoot) // absent link: no-op, no panic
}
