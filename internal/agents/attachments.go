package agents

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// attLaneDrop marks a drop-lane attachment (attachments design D8): the file
// bytes the runner materializes into a per-run read-only mount instead of
// inlining into the user message. Mirrors the domain lane value as a literal
// so the runner stays decoupled from the attachments domain constants.
const attLaneDrop = "drop"

// AttachmentBlobs resolves attachment records to their bytes and to the
// capability URL transcripts and demoted references carry, consulting the
// recorded backend per attachment (attachments design D16/D17); it also
// publishes agent-created documents into the workspace's blob storage
// (add-document-create-tool design.md D6, the DocumentPublisher seam).
// Implemented by the workspace storage resolver; the runner depends on this
// narrow interface only.
type AttachmentBlobs interface {
	OpenAttachment(ctx context.Context, workspaceID, attachmentID string) ([]byte, error)
	AttachmentURL(ctx context.Context, workspaceID, attachmentID string) (string, error)
	PublishCreatedDocument(ctx context.Context, workspaceID, name, sourcePath string) (capabilityURL string, err error)
}

// WithAttachmentBlobs supplies the attachment byte resolver drop-lane
// materialization downloads through (attachments design D17). Required for
// runs carrying drop-lane refs — such a run fails at materialization time on
// a runner without it (an error path, not a defaultable dependency); runs
// without attachments never consult it.
func WithAttachmentBlobs(blobs AttachmentBlobs) RunnerOption {
	return func(r *Runner) {
		if blobs != nil {
			r.attachmentBlobs = blobs
		}
	}
}

// dropLaneRoot is the instance-wide materialization root every per-run
// drop-lane directory lives under (attachments design D17); the startup
// sweep clears it wholesale.
func dropLaneRoot(onClawDir string) string {
	return filepath.Join(onClawDir, "tmp", "drop-lane")
}

// dropLaneRunDir is the run-scoped materialization directory for key:
// <onclawDir>/tmp/drop-lane/<workspace>/<agent>/<session>, the RunKey
// coordinates rendered as a filesystem path.
func (r *Runner) dropLaneRunDir(key RunKey) string {
	return filepath.Join(
		dropLaneRoot(r.onClawDir),
		dropLaneSeg(key.WorkspaceID),
		dropLaneSeg(key.AgentID),
		dropLaneSeg(key.SessionID),
	)
}

// dropLaneSeg flattens one RunKey coordinate into a filesystem-safe path
// segment: every rune outside the safe set becomes '_' so a crafted
// workspace/agent/session id cannot traverse out of the drop-lane root.
// Degenerate results ("", ".", "..") map to "_" — filepath.Join cleans them
// away, which would reposition the path.
func dropLaneSeg(s string) string {
	var b strings.Builder
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_', c == '.':
			b.WriteRune(c)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" || out == "." || out == ".." {
		return "_"
	}
	return out
}

// hasDropLaneAttachments reports whether req carries at least one drop-lane
// ref. Inline-lane refs are resolved by the message-construction layer (D9)
// and never reach materialization.
func hasDropLaneAttachments(req ExecRequest) bool {
	for _, ref := range req.Attachments {
		if ref.Lane == attLaneDrop {
			return true
		}
	}
	return false
}

// materializeDropLane downloads every drop-lane attachment of the run into
// its run-scoped directory at <runDir>/<attachmentID>/<name> (attachments
// design D17), before any model call — a download failure fails the run. The
// directory is mounted read-only into the jail by the caller; teardown
// removes it (teardownDropLane). Returns "" when the request carries no
// drop-lane refs: the nil/inline-only fast path with zero filesystem
// footprint.
func (r *Runner) materializeDropLane(ctx context.Context, req ExecRequest) (string, error) {
	if !hasDropLaneAttachments(req) {
		return "", nil
	}
	if r.attachmentBlobs == nil {
		return "", fmt.Errorf("drop-lane attachments: the attachment blob resolver is not configured (WithAttachmentBlobs)")
	}
	dir := r.dropLaneRunDir(runKeyOf(req))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create drop-lane run dir: %w", err)
	}
	for _, ref := range req.Attachments {
		if ref.Lane != attLaneDrop {
			continue
		}
		name := filepath.Base(ref.Name)
		if name == "" || name == "." || name == ".." || name == string(filepath.Separator) {
			return "", fmt.Errorf("drop-lane attachment %q: unusable file name %q", ref.ID, ref.Name)
		}
		blob, err := r.attachmentBlobs.OpenAttachment(ctx, req.WorkspaceID, ref.ID)
		if err != nil {
			return "", fmt.Errorf("open drop-lane attachment %q: %w", ref.ID, err)
		}
		attDir := filepath.Join(dir, dropLaneSeg(ref.ID))
		if err := os.MkdirAll(attDir, 0o755); err != nil {
			return "", fmt.Errorf("create drop-lane attachment dir %q: %w", ref.ID, err)
		}
		if err := os.WriteFile(filepath.Join(attDir, name), blob, 0o644); err != nil {
			return "", fmt.Errorf("write drop-lane attachment %q: %w", ref.ID, err)
		}
	}
	return dir, nil
}

// teardownDropLane removes the run's materialized drop-lane directory
// (attachments design D17, teardownBrowserSession precedent). Best-effort: a
// failure is logged, never fails the run — the startup sweep removes
// leftovers from crashed runs anyway.
func (r *Runner) teardownDropLane(req ExecRequest) {
	if !hasDropLaneAttachments(req) {
		return
	}
	dir := r.dropLaneRunDir(runKeyOf(req))
	if err := os.RemoveAll(dir); err != nil {
		slog.Warn("drop-lane: removing run dir failed (best-effort)",
			"dir", dir, "error", err)
	}
}

// sweepDropLane removes every leftover run-scoped drop-lane directory from
// crashed runs (attachments design D17: the startup sweep covers restart
// cleanup). Best-effort and logged; a fresh instance's missing root is a
// silent no-op.
func sweepDropLane(onClawDir string) {
	root := dropLaneRoot(onClawDir)
	entries, err := os.ReadDir(root)
	if err != nil {
		return // nothing materialized yet
	}
	for _, e := range entries {
		slog.Info("drop-lane: sweeping leftover run dir",
			"dir", filepath.Join(root, e.Name()))
	}
	if err := os.RemoveAll(root); err != nil {
		slog.Warn("drop-lane: startup sweep failed (best-effort)",
			"root", root, "error", err)
	}
}
