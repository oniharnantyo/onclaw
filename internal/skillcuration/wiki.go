package skillcuration

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// The skill wiki (add-skill-curation-from-traces 4.1, design D3): pattern
// pages and an append-only log as plain files under the workspace's
// skillwiki/ directory — user-inspectable, never memory rows, never exposed
// to agent execution. Nothing is ever deleted: supersede writes a successor
// page and a pointer in the old one, making "never rolled back" a filesystem
// property.

// SkillWikiDirName is the workspace-directory name of the wiki, a sibling of
// the workspace's skills/ directory. It is the withholding invariant's path
// component (spec: "Wiki never reaches an agent"): the wiki lives beside —
// never inside — any agent workspace directory, so no compose source can
// reach it.
const SkillWikiDirName = "skillwiki"

const (
	wikiPatternsDirName = "patterns"
	wikiLogsFileName    = "logs.md"
	wikiDirPerm         = 0o755
	wikiFilePerm        = 0o644

	// statusActive / statusSuperseded are the page-file Status header values.
	statusActive    = "active"
	statusSupersede = "superseded"

	// supersededByHeader is the page-file header line naming a superseded
	// page's successor.
	supersededByHeader = "Superseded-By:"
	// evidenceRunsHeader is the page-file header line listing the cited
	// evidence-run session IDs, comma-separated.
	evidenceRunsHeader = "Evidence-Runs:"
	// statusHeader is the page-file header line carrying the page status.
	statusHeader = "Status:"

	// logTimeFormat is the logs.md line timestamp format (RFC3339 UTC).
	logTimeFormat = time.RFC3339
)

// PageSlugRegex is the pattern-page name rule: a DNS-label-shaped name
// (lowercase alphanumeric with interior hyphens, 1–63 chars). The regex
// admits no path separators, dots, or whitespace, so a validated slug cannot
// traverse — the wiki validates every page name it receives (maintainer
// output is model-generated and untrusted) before it ever touches a path.
var PageSlugRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// ErrPageNotFound is the missing-pattern-page sentinel
// (domain.ErrNotFound-wrapped so store-consistent checks work).
var ErrPageNotFound = fmt.Errorf("%w: pattern page", domain.ErrNotFound)

// ValidatePageSlug validates a pattern-page name: DNS-label shaped, so it is
// safe to join into the wiki's patterns/ directory (no traversal, no
// separators).
func ValidatePageSlug(slug string) error {
	if len(slug) < 1 || len(slug) > 63 {
		return fmt.Errorf("%w: page slug must be between 1 and 63 characters", domain.ErrInvalid)
	}
	if !PageSlugRegex.MatchString(slug) {
		return fmt.Errorf("%w: page slug must match DNS label format (lowercase alphanumeric with interior hyphens)", domain.ErrInvalid)
	}
	return nil
}

// PageStatus is a pattern page's lifecycle state read back from disk.
type PageStatus string

const (
	// PageActive is a live pattern page.
	PageActive PageStatus = "active"
	// PageSuperseded is a retired page kept readable with a pointer to its
	// successor (spec: "superseded pages SHALL remain readable with a
	// pointer to their successor").
	PageSuperseded PageStatus = "superseded"
)

// Page is one wiki pattern page: title, status, the evidence runs (session
// IDs) it was derived from, and the markdown body. Every page cites at least
// one evidence run — the wiki layer refuses to write a page without
// citations (spec: "each page citing the evidence runs it was derived from").
type Page struct {
	Slug  string
	Title string
	// Status is active, or superseded with SupersededBy naming the successor.
	Status       PageStatus
	SupersededBy string
	// EvidenceRuns are the cited evidence-run session IDs.
	EvidenceRuns []string
	// Body is the page's markdown (failure modes, strategies, workarounds).
	Body string
}

// Wiki is the file-backed pattern wiki for one workspace. All methods are
// safe for concurrent use; page writes are atomic (staged temp file + rename
// in the destination directory, the promptdocs write discipline) and the log
// appends under a mutex with O_APPEND — a crash can leave an operation
// unapplied, never a partial page or a truncated log.
//
// The directory is injected (the composition root derives it with
// domain.WorkspaceSkillWikiDir); the wiki creates it and self-heals deleted
// subdirectories on write (the promptdocs ensureDir precedent).
type Wiki struct {
	dir string
	// logMu serializes logs.md appends within the process; O_APPEND makes
	// each write atomic at the file level.
	logMu sync.Mutex
}

// NewWiki returns the wiki rooted at dir (…/skillwiki). The directory is
// created lazily by the first write; reads on a missing wiki are the empty
// wiki, not errors.
func NewWiki(dir string) *Wiki {
	return &Wiki{dir: dir}
}

// Dir returns the wiki's root directory.
func (w *Wiki) Dir() string { return w.dir }

// patternsDir is the wiki's pattern-page directory.
func (w *Wiki) patternsDir() string {
	return filepath.Join(w.dir, wikiPatternsDirName)
}

// pagePath joins a validated slug into the patterns directory. Callers must
// validate the slug (ValidatePageSlug) first — panic-free by contract, the
// path is only ever built from validated names.
func (w *Wiki) pagePath(slug string) string {
	return filepath.Join(w.patternsDir(), slug+".md")
}

// ensureDirs creates the wiki directory tree if missing. Every writer
// self-heals a deleted or never-created wiki (the promptdocs ensureDir
// precedent).
func (w *Wiki) ensureDirs() error {
	if err := os.MkdirAll(w.patternsDir(), wikiDirPerm); err != nil {
		return fmt.Errorf("skillcuration: create wiki patterns dir: %w", err)
	}
	return nil
}

// stagePage writes content to a temp file inside the page's directory and
// returns its path; commitPage renames it into place. Replicated from
// promptdocs' stage/commit discipline (the helpers are unexported there):
// readers never observe a partial page, and a crash between stage and
// rename leaves a dot-prefixed temp file that no reader treats as a page.
func (w *Wiki) stagePage(dir, name, content string) (string, error) {
	tmp, err := os.CreateTemp(dir, "."+name+".tmp-")
	if err != nil {
		return "", fmt.Errorf("skillcuration: create temp file for %s: %w", name, err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", fmt.Errorf("skillcuration: write temp file for %s: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return "", fmt.Errorf("skillcuration: close temp file for %s: %w", name, err)
	}
	if err := os.Chmod(tmpName, wikiFilePerm); err != nil {
		os.Remove(tmpName)
		return "", fmt.Errorf("skillcuration: chmod temp file for %s: %w", name, err)
	}
	return tmpName, nil
}

// commitPage renames a staged temp file over the target page.
func (w *Wiki) commitPage(dir, name, staged string) error {
	if err := os.Rename(staged, filepath.Join(dir, name)); err != nil {
		return fmt.Errorf("skillcuration: rename temp file for %s: %w", name, err)
	}
	return nil
}

// writePage atomically writes one page file (slug already validated).
func (w *Wiki) writePage(page Page) error {
	if err := w.ensureDirs(); err != nil {
		return err
	}
	dir := w.patternsDir()
	staged, err := w.stagePage(dir, page.Slug+".md", serializePage(page))
	if err != nil {
		return err
	}
	if err := w.commitPage(dir, page.Slug+".md", staged); err != nil {
		os.Remove(staged)
		return err
	}
	return nil
}

// validate checks a page's write shape: a valid slug, a title, non-empty
// evidence citations, and a coherent status (superseded names its successor).
func validatePage(page Page) error {
	if err := ValidatePageSlug(page.Slug); err != nil {
		return fmt.Errorf("skillcuration: page slug: %w", err)
	}
	if strings.TrimSpace(page.Title) == "" {
		return fmt.Errorf("%w: pattern page %s needs a title", domain.ErrInvalid, page.Slug)
	}
	if len(page.EvidenceRuns) == 0 {
		return fmt.Errorf("%w: pattern page %s must cite at least one evidence run", domain.ErrInvalid, page.Slug)
	}
	for _, run := range page.EvidenceRuns {
		if strings.TrimSpace(run) == "" {
			return fmt.Errorf("%w: pattern page %s cites an empty evidence run", domain.ErrInvalid, page.Slug)
		}
	}
	switch page.Status {
	case PageActive:
	case PageSuperseded:
		if err := ValidatePageSlug(page.SupersededBy); err != nil {
			return fmt.Errorf("%w: superseded page %s needs a valid successor slug", domain.ErrInvalid, page.Slug)
		}
		if page.SupersededBy == page.Slug {
			return fmt.Errorf("%w: page %s cannot supersede itself", domain.ErrInvalid, page.Slug)
		}
	default:
		return fmt.Errorf("%w: unknown pattern page status %q", domain.ErrInvalid, page.Status)
	}
	return nil
}

// Create writes a new active pattern page. The slug must not already exist —
// overwriting an existing page is Update's job, and a silent clobber would
// erase evidence the maintenance audit trail points at.
func (w *Wiki) Create(page Page) error {
	page.Status = PageActive
	page.SupersededBy = ""
	if err := validatePage(page); err != nil {
		return err
	}
	if _, err := w.Page(page.Slug); err == nil {
		return fmt.Errorf("%w: pattern page %s already exists", domain.ErrInvalid, page.Slug)
	} else if !errors.Is(err, ErrPageNotFound) {
		return err
	}
	if err := w.writePage(page); err != nil {
		return err
	}
	return w.AppendLog(LogEntry{Operation: "create", Page: page.Slug, EvidenceRuns: page.EvidenceRuns})
}

// Update rewrites an existing active page's content (title, citations, body).
// A superseded page is immutable — the maintenance grammar moves forward via
// Supersede, never by editing history.
func (w *Wiki) Update(page Page) error {
	page.Status = PageActive
	page.SupersededBy = ""
	if err := validatePage(page); err != nil {
		return err
	}
	current, err := w.Page(page.Slug)
	if err != nil {
		return err
	}
	if current.Status == PageSuperseded {
		return fmt.Errorf("%w: pattern page %s is superseded and cannot be updated", domain.ErrInvalid, page.Slug)
	}
	if err := w.writePage(page); err != nil {
		return err
	}
	return w.AppendLog(LogEntry{Operation: "update", Page: page.Slug, EvidenceRuns: page.EvidenceRuns})
}

// Supersede retires oldSlug by pointing it at the successor page and writing
// the successor (spec: "Stale pattern is superseded, not erased"). The old
// page remains readable with a Superseded-By pointer; nothing is removed.
//
// The operation is idempotent for crash recovery: the successor page is
// written first, then the pointer — a crash between the two leaves the
// successor present with the old page still active, and re-running
// Supersede completes the pointer write instead of failing.
func (w *Wiki) Supersede(oldSlug string, successor Page) error {
	if err := ValidatePageSlug(oldSlug); err != nil {
		return fmt.Errorf("skillcuration: superseded page slug: %w", err)
	}
	successor.Status = PageActive
	successor.SupersededBy = ""
	if err := validatePage(successor); err != nil {
		return err
	}
	if successor.Slug == oldSlug {
		return fmt.Errorf("%w: successor page %s cannot supersede itself", domain.ErrInvalid, successor.Slug)
	}
	old, err := w.Page(oldSlug)
	if err != nil {
		return err
	}
	if old.Status == PageSuperseded {
		return fmt.Errorf("%w: pattern page %s is already superseded", domain.ErrInvalid, oldSlug)
	}

	// 1. The successor page (must not exist — the strict grammar: supersede
	// writes fresh successors, Update rewrites actives, Merge collapses
	// duplicates). A crash between this write and the pointer write below
	// leaves both pages well-formed; the next maintenance cycle sees the
	// half state in its index and moves forward with a fresh operation.
	if _, err := w.Page(successor.Slug); err == nil {
		return fmt.Errorf("%w: successor page %s already exists", domain.ErrInvalid, successor.Slug)
	} else if !errors.Is(err, ErrPageNotFound) {
		return err
	}
	if err := w.writePage(successor); err != nil {
		return err
	}

	// 2. The pointer in the old page (atomic rewrite).
	old.Status = PageSuperseded
	old.SupersededBy = successor.Slug
	if err := w.writePage(*old); err != nil {
		return err
	}

	runs := append(append([]string{}, successor.EvidenceRuns...), old.EvidenceRuns...)
	return w.AppendLog(LogEntry{Operation: "supersede", Page: successor.Slug, Detail: oldSlug, EvidenceRuns: runs})
}

// Merge collapses source pages into one canonical page: the canonical page
// (into) is written with the merged content, and every other source is
// superseded with a pointer to it. The canonical slug may be one of the
// sources (it is updated in place, the others retired) or a fresh name (all
// sources retire; it must not exist). Nothing is deleted.
func (w *Wiki) Merge(sources []string, into Page) error {
	if len(sources) < 2 {
		return fmt.Errorf("%w: merge needs at least two source pages", domain.ErrInvalid)
	}
	into.Status = PageActive
	into.SupersededBy = ""
	if err := validatePage(into); err != nil {
		return err
	}

	seen := make(map[string]bool, len(sources))
	retired := make([]string, 0, len(sources))
	updatesCanonical := false
	for _, src := range sources {
		if err := ValidatePageSlug(src); err != nil {
			return fmt.Errorf("skillcuration: merge source slug: %w", err)
		}
		if src == into.Slug {
			updatesCanonical = true
			continue
		}
		if seen[src] {
			return fmt.Errorf("%w: merge lists page %s twice", domain.ErrInvalid, src)
		}
		seen[src] = true
		page, err := w.Page(src)
		if err != nil {
			return err
		}
		if page.Status == PageSuperseded {
			return fmt.Errorf("%w: merge source %s is already superseded", domain.ErrInvalid, src)
		}
		retired = append(retired, src)
	}

	if updatesCanonical {
		// The canonical page is a source: it must exist and be active; its
		// content becomes the merged body. Written directly (not via
		// Update) so the composite operation appends one merge log line.
		current, err := w.Page(into.Slug)
		if err != nil {
			return err
		}
		if current.Status == PageSuperseded {
			return fmt.Errorf("%w: merge target %s is already superseded", domain.ErrInvalid, into.Slug)
		}
	} else {
		// A fresh canonical name: it must not exist.
		if _, err := w.Page(into.Slug); err == nil {
			return fmt.Errorf("%w: merge target %s already exists", domain.ErrInvalid, into.Slug)
		} else if !errors.Is(err, ErrPageNotFound) {
			return err
		}
	}
	if err := w.writePage(into); err != nil {
		return err
	}

	for _, src := range retired {
		page, err := w.Page(src)
		if err != nil {
			return err
		}
		page.Status = PageSuperseded
		page.SupersededBy = into.Slug
		if err := w.writePage(*page); err != nil {
			return err
		}
	}

	runs := append([]string{}, into.EvidenceRuns...)
	for _, src := range retired {
		if page, err := w.Page(src); err == nil {
			runs = append(runs, page.EvidenceRuns...)
		}
	}
	return w.AppendLog(LogEntry{Operation: "merge", Page: into.Slug, Detail: strings.Join(retired, ","), EvidenceRuns: runs})
}

// Page reads one pattern page by slug; ErrPageNotFound when absent.
// Superseded pages are returned like any other — with Status superseded and
// the SupersededBy pointer set (spec: "superseded pages SHALL remain
// readable with a pointer to their successor").
func (w *Wiki) Page(slug string) (*Page, error) {
	if err := ValidatePageSlug(slug); err != nil {
		return nil, fmt.Errorf("skillcuration: page slug: %w", err)
	}
	data, err := os.ReadFile(w.pagePath(slug))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w %s", ErrPageNotFound, slug)
	}
	if err != nil {
		return nil, fmt.Errorf("skillcuration: read pattern page %s: %w", slug, err)
	}
	page, err := parsePage(slug, string(data))
	if err != nil {
		return nil, err
	}
	return page, nil
}

// List returns every pattern page — active and superseded — ordered by slug.
// The result is never nil. A missing wiki is the empty wiki. A malformed page
// file (external tampering — the wiki never writes one) fails the listing:
// the maintenance stage treats it as fail-soft deferral.
func (w *Wiki) List() ([]Page, error) {
	entries, err := os.ReadDir(w.patternsDir())
	if errors.Is(err, os.ErrNotExist) {
		return []Page{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("skillcuration: list wiki patterns: %w", err)
	}
	pages := make([]Page, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		// Dot-prefixed files are staged temp files from a crashed write —
		// never pages. Only top-level *.md files are pages.
		if entry.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".md") {
			continue
		}
		slug := strings.TrimSuffix(name, ".md")
		page, err := w.Page(slug)
		if err != nil {
			return nil, err
		}
		pages = append(pages, *page)
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i].Slug < pages[j].Slug })
	return pages, nil
}

// serializePage renders the strict on-disk page shape:
//
//	# Title
//
//	Status: active|superseded
//	Superseded-By: successor-slug      (superseded pages only)
//	Evidence-Runs: sess-a, sess-b
//
//	body markdown
func serializePage(page Page) string {
	var sb strings.Builder
	sb.WriteString("# ")
	sb.WriteString(strings.TrimSpace(page.Title))
	sb.WriteString("\n\n")
	sb.WriteString(statusHeader + " ")
	sb.WriteString(string(page.Status))
	sb.WriteString("\n")
	if page.Status == PageSuperseded {
		sb.WriteString(supersededByHeader + " ")
		sb.WriteString(page.SupersededBy)
		sb.WriteString("\n")
	}
	sb.WriteString(evidenceRunsHeader + " ")
	sb.WriteString(strings.Join(page.EvidenceRuns, ", "))
	sb.WriteString("\n\n")
	sb.WriteString(strings.TrimRight(page.Body, "\n"))
	sb.WriteString("\n")
	return sb.String()
}

// parsePage parses the strict page shape back. Malformed files are errors —
// the wiki only ever writes well-formed pages, so a malformed file is
// external tampering the maintenance stage fails soft on.
func parsePage(slug, raw string) (*Page, error) {
	lines := strings.Split(raw, "\n")

	// 1. Title: the first line must be an H1.
	first := strings.TrimSpace(lines[0])
	if !strings.HasPrefix(first, "# ") {
		return nil, fmt.Errorf("%w: page %s is missing its title heading", domain.ErrInvalid, slug)
	}
	title := strings.TrimSpace(strings.TrimPrefix(first, "# "))

	// 2. Header lines: Status / Superseded-By / Evidence-Runs, then the
	// blank separator opens the body. Blank lines before the header block
	// (the title separator) are skipped.
	page := &Page{Slug: slug, Title: title}
	var evidence []string
	sawStatus := false
	i := 1
	for ; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			if sawStatus {
				i++
				break
			}
			continue
		}
		switch {
		case strings.HasPrefix(line, statusHeader):
			status := strings.TrimSpace(strings.TrimPrefix(line, statusHeader))
			switch status {
			case statusActive:
				page.Status = PageActive
			case statusSupersede:
				page.Status = PageSuperseded
			default:
				return nil, fmt.Errorf("%w: page %s has unknown status %q", domain.ErrInvalid, slug, status)
			}
			sawStatus = true
		case strings.HasPrefix(line, supersededByHeader):
			page.SupersededBy = strings.TrimSpace(strings.TrimPrefix(line, supersededByHeader))
		case strings.HasPrefix(line, evidenceRunsHeader):
			list := strings.TrimSpace(strings.TrimPrefix(line, evidenceRunsHeader))
			if list != "" {
				for _, run := range strings.Split(list, ",") {
					if run = strings.TrimSpace(run); run != "" {
						evidence = append(evidence, run)
					}
				}
			}
		default:
			return nil, fmt.Errorf("%w: page %s has an unrecognized header line %q", domain.ErrInvalid, slug, line)
		}
	}
	if !sawStatus {
		return nil, fmt.Errorf("%w: page %s is missing its status header", domain.ErrInvalid, slug)
	}
	if len(evidence) == 0 {
		return nil, fmt.Errorf("%w: page %s cites no evidence runs", domain.ErrInvalid, slug)
	}
	page.EvidenceRuns = evidence

	// 3. Body: everything after the blank separator.
	page.Body = strings.TrimSpace(strings.Join(lines[i:], "\n"))
	if page.Body == "" {
		return nil, fmt.Errorf("%w: page %s has an empty body", domain.ErrInvalid, slug)
	}

	// Cross-checks the serializer guarantees.
	if page.Status == PageSuperseded && page.SupersededBy == "" {
		return nil, fmt.Errorf("%w: superseded page %s is missing its %s pointer", domain.ErrInvalid, slug, supersededByHeader)
	}
	if page.Status == PageActive && page.SupersededBy != "" {
		return nil, fmt.Errorf("%w: active page %s carries a %s pointer", domain.ErrInvalid, slug, supersededByHeader)
	}
	return page, nil
}

// LogEntry is one append-only wiki log line: what operation touched which
// page, citing the evidence runs behind it.
type LogEntry struct {
	// Operation is create | update | supersede | merge.
	Operation string
	// Page is the operation's target page slug.
	Page string
	// Detail carries the operation's secondary page(s): the superseded slug
	// or the comma-joined merge sources. Empty for create/update.
	Detail string
	// EvidenceRuns are the cited evidence-run session IDs.
	EvidenceRuns []string
	// At stamps the entry (zero stamps to now, UTC).
	At time.Time
}

// AppendLog appends one entry to the append-only logs.md. The file is never
// truncated: the append is a single O_APPEND write serialized by an
// in-process mutex, so concurrent operations interleave whole lines and a
// crash loses at most the in-flight line — never earlier history.
func (w *Wiki) AppendLog(entry LogEntry) error {
	at := entry.At
	if at.IsZero() {
		at = time.Now().UTC()
	}
	line := fmt.Sprintf("- %s %s %s", at.UTC().Format(logTimeFormat), entry.Operation, entry.Page)
	if entry.Detail != "" {
		line += " (" + entry.Detail + ")"
	}
	if len(entry.EvidenceRuns) > 0 {
		line += " evidence=" + strings.Join(entry.EvidenceRuns, ",")
	}
	line += "\n"

	w.logMu.Lock()
	defer w.logMu.Unlock()
	if err := os.MkdirAll(w.dir, wikiDirPerm); err != nil {
		return fmt.Errorf("skillcuration: create wiki dir: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(w.dir, wikiLogsFileName), os.O_APPEND|os.O_CREATE|os.O_WRONLY, wikiFilePerm)
	if err != nil {
		return fmt.Errorf("skillcuration: open wiki log: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(line); err != nil {
		return fmt.Errorf("skillcuration: append wiki log: %w", err)
	}
	return nil
}

// ReadLog returns the wiki log's raw lines, oldest first. A missing log is
// the empty log, not an error.
func (w *Wiki) ReadLog() ([]string, error) {
	data, err := os.ReadFile(filepath.Join(w.dir, wikiLogsFileName))
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("skillcuration: read wiki log: %w", err)
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines, nil
}
