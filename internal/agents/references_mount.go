package agents

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/references"
)

// ReferencesLibrary is the runner-side port over the workspace's
// reference-document library (add-reference-documents D6/D7/D8): the
// run-visibility lens the compose-time manifest and the references mount are
// built from, the visibility-filtered section search document.search binds
// to, and the mount's write half. The references service implements it
// structurally; it also satisfies the registry's DocumentTools port, so one
// wiring feeds the tools and the run assembly. Nil (unwired) keeps every
// references capability off — no mount, no manifest, no document.search (the
// memory-search posture; the composition root wires it).
type ReferencesLibrary interface {
	VisibleDocuments(ctx context.Context, workspaceID string, scope domain.DocumentRunScope) ([]domain.ReferenceDocument, error)
	SearchDocuments(ctx context.Context, workspaceID string, scope domain.DocumentRunScope, query string, limit int) ([]domain.DocumentSectionHit, error)
	// TableOfContents projects one document's distinct top-level headings in
	// ordinal order — the manifest entry's TOC segment (add-reference-
	// documents 5.1/D6). A read error degrades that entry's TOC, never the run.
	TableOfContents(ctx context.Context, workspaceID, docID string, topN int) ([]string, error)
	WriteMount(ctx context.Context, dir string, docs []domain.ReferenceDocument) error
}

// WithReferences supplies the reference-document library the run assembly
// consumes (add-reference-documents 4.3/5.1): the run-scoped references/
// mount materializes from its visibility lens, the compose-time manifest
// renders from the same lens, and document.search resolves its searcher from
// the same value. Unset, the deployment surfaces no references capability at
// all — runs compose exactly as before.
func WithReferences(lib ReferencesLibrary) RunnerOption {
	return func(r *Runner) {
		if lib != nil {
			r.references = lib
		}
	}
}

// referencesRunScope builds the run-side half of the D7 visibility predicate
// from the request: the serving agent, plus — only when the run executes
// inside a team room — that room. A channel session is a chan_-prefixed
// session binding carrying the ChannelID; scheduler- and heartbeat-origin
// runs resolve visibility by agent bindings ONLY (the D7 scheduler rule):
// even a request that carries a delivery channel composes
// IsChannelSession=false, so a channel-tied document is never visible to an
// unattended run. composeAgent's channel-docs guard is the same posture.
func referencesRunScope(req ExecRequest) domain.DocumentRunScope {
	scope := domain.DocumentRunScope{AgentID: req.AgentID}
	origin := normalizeOrigin(req.Origin)
	isChannelSession := req.ChannelID != "" &&
		strings.HasPrefix(req.SessionID, domain.SessionPrefixChannel) &&
		origin != OriginScheduler &&
		origin != OriginHeartbeat
	if isChannelSession {
		scope.ChannelID = req.ChannelID
		scope.IsChannelSession = true
	}
	return scope
}

// referencesMountRoot is the instance-wide materialization root every
// per-run references mount lives under (D8) — the drop-lane root's sibling
// (attachments design D17); the startup sweep clears it wholesale.
func referencesMountRoot(onClawDir string) string {
	return filepath.Join(onClawDir, "tmp", "references-mount")
}

// referencesRunDir is the run-scoped mount directory for key:
// <onclawDir>/tmp/references-mount/<workspace>/<agent>/<session>/references —
// the RunKey coordinates rendered as a filesystem path (the drop-lane
// precedent) with the mount directory named references/ as the leaf, so the
// jail sees the mount exactly at its documented path.
func (r *Runner) referencesRunDir(key RunKey) string {
	return filepath.Join(
		referencesMountRoot(r.onClawDir),
		dropLaneSeg(key.WorkspaceID),
		dropLaneSeg(key.AgentID),
		dropLaneSeg(key.SessionID),
		references.MountDirName,
	)
}

// materializeReferencesMount composes the run's visible documents into its
// run-scoped references/ mount (add-reference-documents 4.3, D8): the
// visibility lens decides membership exactly as document.search's
// WHERE-clause does (D7 — visibility enforced twice, identically), making
// path-level visibility a jail property rather than a runtime check. Runs
// before tool resolution and composition, so a failure fails the run before
// any model call — the drop-lane posture (attachments design D17): a
// half-materialized mount would break the manifest's promise, never fail
// silently. Returns the mount directory and the visible documents the
// manifest renders from; ("", nil, nil) when the library is unwired or
// nothing is visible — the skip-silently paths.
func (r *Runner) materializeReferencesMount(ctx context.Context, req ExecRequest, scope domain.DocumentRunScope) (string, []domain.ReferenceDocument, error) {
	if r.references == nil {
		return "", nil, nil
	}
	docs, err := r.references.VisibleDocuments(ctx, req.WorkspaceID, scope)
	if err != nil {
		return "", nil, fmt.Errorf("resolve visible reference documents: %w", err)
	}
	if len(docs) == 0 {
		return "", nil, nil
	}
	dir := r.referencesRunDir(runKeyOf(req))
	if err := r.references.WriteMount(ctx, dir, docs); err != nil {
		return "", nil, fmt.Errorf("write references mount: %w", err)
	}
	return dir, docs, nil
}

// teardownReferencesMount removes the run's materialized references mount
// (D8; the teardownDropLane precedent). Best-effort: a failure is logged,
// never fails the run — the startup sweep removes leftovers from crashed
// runs anyway.
func (r *Runner) teardownReferencesMount(req ExecRequest) {
	if r.references == nil {
		return
	}
	dir := r.referencesRunDir(runKeyOf(req))
	if err := os.RemoveAll(dir); err != nil {
		slog.Warn("references mount: removing run dir failed (best-effort)",
			"dir", dir, "error", err)
	}
}

// sweepReferencesMount removes every leftover run-scoped references mount
// from crashed runs (D8; the sweepDropLane precedent: the startup sweep
// covers restart cleanup). Best-effort and logged; a fresh instance's
// missing root is a silent no-op.
func sweepReferencesMount(onClawDir string) {
	root := referencesMountRoot(onClawDir)
	entries, err := os.ReadDir(root)
	if err != nil {
		return // nothing materialized yet
	}
	for _, e := range entries {
		slog.Info("references mount: sweeping leftover run dir",
			"dir", filepath.Join(root, e.Name()))
	}
	if err := os.RemoveAll(root); err != nil {
		slog.Warn("references mount: startup sweep failed (best-effort)",
			"root", root, "error", err)
	}
}
