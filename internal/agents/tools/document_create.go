package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/agents/backend"
)

// NameDocumentCreate is the dotted capability name registered in the tool
// registry (add-document-create-tool design.md D1): one verb serving xlsx,
// pdf, docx, and pptx through the document.* family seam.
const NameDocumentCreate = "document.create"

// defaultDocumentCreateTimeout is the bounded generation deadline applied to
// each document.create invocation (mirrors document.read's bounded
// conversion context): generators must honor context cancellation.
const defaultDocumentCreateTimeout = 60 * time.Second

// DocumentPublisher publishes an agent-created document into the workspace's
// blob storage and returns its capability URL (design.md D6). Implemented by
// the workspace storage resolver; the runner binds it per run.
type DocumentPublisher interface {
	PublishCreatedDocument(ctx context.Context, workspaceID, name, sourcePath string) (capabilityURL string, err error)
}

// PDFHTMLRenderer renders agent-authored HTML to PDF bytes via headless
// Chrome (design.md D4). A nil renderer encodes a deployment without Chrome:
// the HTML route returns a structured error pointing at the structured route.
type PDFHTMLRenderer interface {
	RenderHTMLToPDF(ctx context.Context, html string) ([]byte, error)
}

// DocumentCreateOption configures the document.create tool.
type DocumentCreateOption func(*documentCreateTool)

// WithDocumentCreateReadOnlyRoots configures extra read-only roots the
// template path may resolve against (e.g. drop-lane attachment run
// directories). Named distinctly from document.read's WithReadOnlyRoots
// because Go does not permit same-package overloads.
func WithDocumentCreateReadOnlyRoots(roots ...string) DocumentCreateOption {
	return func(t *documentCreateTool) {
		for _, r := range roots {
			if r == "" {
				continue
			}
			abs, err := filepath.Abs(r)
			if err != nil {
				continue
			}
			resolved, err := filepath.EvalSymlinks(abs)
			if err != nil {
				t.readOnlyRoots = append(t.readOnlyRoots, strings.TrimSuffix(filepath.Clean(abs), string(filepath.Separator)))
				continue
			}
			t.readOnlyRoots = append(t.readOnlyRoots, strings.TrimSuffix(resolved, string(filepath.Separator)))
		}
	}
}

// WithTimeout overrides the bounded generation deadline (default 60s).
func WithTimeout(d time.Duration) DocumentCreateOption {
	return func(t *documentCreateTool) {
		if d > 0 {
			t.timeout = d
		}
	}
}

// WithWorkspaceID sets the workspace identity stamped on publisher calls
// (design.md D6 delivery). The registration worker binds it at composition.
func WithWorkspaceID(id string) DocumentCreateOption {
	return func(t *documentCreateTool) {
		t.workspaceID = id
	}
}

type documentCreateTool struct {
	// agentDir is the resolved (symlink-free) jail root bound at construction;
	// output paths land inside it, template paths may also resolve into the
	// read-only roots (chat drop-lane mounts).
	agentDir      string
	readOnlyRoots []string
	publisher     DocumentPublisher
	htmlRenderer  PDFHTMLRenderer
	workspaceID   string
	timeout       time.Duration
}

// NewDocumentCreate constructs the document.create tool for one agent
// workspace. agentDir must exist and resolves symlinks to its canonical form,
// exactly like the fs jail backend. publisher and htmlRenderer implement the
// delivery (D6) and PDF HTML-route (D4) ports; a nil htmlRenderer encodes a
// deployment without headless Chrome (the HTML route then degrades with a
// structured error), a nil publisher omits the capability URL from results.
func NewDocumentCreate(agentDir string, publisher DocumentPublisher, htmlRenderer PDFHTMLRenderer, opts ...DocumentCreateOption) (tool.BaseTool, error) {
	if agentDir == "" {
		return nil, fmt.Errorf("agent workspace directory cannot be empty")
	}
	abs, err := filepath.Abs(agentDir)
	if err != nil {
		return nil, fmt.Errorf("resolve agent workspace path: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("stat agent workspace directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("agent workspace is not a directory: %s", abs)
	}
	resolvedRoot, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("resolve symlinks in agent workspace path: %w", err)
	}
	t := &documentCreateTool{
		agentDir:     strings.TrimSuffix(resolvedRoot, string(filepath.Separator)),
		timeout:      defaultDocumentCreateTimeout,
		publisher:    publisher,
		htmlRenderer: htmlRenderer,
	}
	for _, opt := range opts {
		opt(t)
	}
	return t, nil
}

// Info returns the tool schema surfaced to agentic models.
func (t *documentCreateTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: NameDocumentCreate,
		Desc: "Generate a new document (xlsx, pdf, docx, or pptx) inside the workspace at an absolute output path under " + backend.DefaultMountPoint + ". " +
			"Input follows the format: docx takes {\"markdown\"} (headings #/##/###, **bold**, *italic*, - bullets, | pipe tables |); " +
			"pptx takes {\"slides\":[{\"title\",\"bullets\",\"notes\"}]}; " +
			"xlsx takes {\"sheets\":[{\"name\",\"rows\",\"bold_first_row\"}]} or a simple {\"text\"} pipe table; " +
			"pdf takes {\"source\":\"structured\",\"invoice\":{seller,buyer,number,date,line_items,tax_rate,notes}} or {\"source\":\"html\",\"html\":...}. " +
			"An optional \"template\" path (a chat-attached file mount or workspace file) switches to template-fill: " +
			"xlsx fills named cells {\"cells\":{\"Sheet1!B3\":\"v\"}} and appends rows; docx/pptx replace {{placeholder}} text. " +
			"PDF templates are not supported (PDF is final-form) — use an xlsx or docx template, or the structured/HTML pdf routes. " +
			"Generation failures return an error result naming the document; the output path must stay inside the workspace.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"path": {
				Type:     schema.String,
				Desc:     "The output file path, absolute under " + backend.DefaultMountPoint + " (e.g. " + backend.DefaultMountPoint + "/invoice.xlsx). Existing files are overwritten.",
				Required: true,
			},
			"format": {
				Type:     schema.String,
				Desc:     "The document format: xlsx, pdf, docx, or pptx.",
				Required: true,
			},
			"template": {
				Type:     schema.String,
				Desc:     "Optional path to a template file (xlsx, docx, or pptx) to fill instead of generating from scratch. Not supported for pdf.",
				Required: false,
			},
			"data": {
				Type:     schema.Object,
				Desc:     "Format-specific structured data (see description). Required without a template; for template-fill, the placeholder/cell values.",
				Required: false,
			},
		}),
	}, nil
}

// documentCreateArgs is the deserialized tool-call argument shape (design.md
// D1): output path, format, optional template path, optional structured data.
type documentCreateArgs struct {
	Path     string          `json:"path"`
	Format   string          `json:"format"`
	Template string          `json:"template,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
}

// resolveOutput maps the user-provided output path against the jail's write
// contract (design.md D5, same rules as delete_file / write_file): mount-scoped
// paths under DefaultMountPoint map into the agent directory, raw host absolute
// paths are rejected, ".." is rejected outright, and every resolved component —
// including the final element if it already exists — must stay inside the jail.
// Unlike delete_file, the final component may not exist yet (the tool creates
// it), so the deepest existing parent is resolved and containment is checked
// there.
func (t *documentCreateTool) resolveOutput(userPath string) (resolved string, display string, err error) {
	if userPath == "" {
		return "", "", fmt.Errorf("path cannot be empty")
	}
	if strings.Contains(userPath, "..") {
		return "", "", fmt.Errorf("path escape attempt: %q contains \"..\"", userPath)
	}

	var rel string
	if userPath == backend.DefaultMountPoint {
		rel = "."
	} else if candidate := strings.TrimPrefix(userPath, backend.DefaultMountPoint+"/"); candidate != userPath && candidate != "" {
		rel = candidate
	} else if filepath.IsAbs(userPath) {
		return "", "", fmt.Errorf("absolute paths are not allowed: %q (write under %s instead)", userPath, backend.DefaultMountPoint)
	} else {
		rel = userPath
	}
	if rel == "." {
		return "", "", fmt.Errorf("cannot write the workspace root %s", backend.DefaultMountPoint)
	}

	fullPath := filepath.Join(t.agentDir, filepath.Clean(rel))
	parent, base := filepath.Split(fullPath)
	resolvedParent, resolveErr := filepath.EvalSymlinks(parent)
	if resolveErr != nil {
		if os.IsNotExist(resolveErr) {
			return "", "", fmt.Errorf("output directory does not exist: %s", displayPathFor(backend.DefaultMountPoint, rel))
		}
		return "", "", fmt.Errorf("resolve path: %w", resolveErr)
	}
	if !isWithinDeleteRoot(t.agentDir, resolvedParent) {
		return "", "", fmt.Errorf("path escapes jail: %q resolves to %q", userPath, resolvedParent)
	}
	resolved = filepath.Join(resolvedParent, base)

	// If the final component already exists (file or symlink), it must also
	// resolve inside the jail.
	if _, statErr := os.Lstat(resolved); statErr == nil {
		resolvedFinal, evalErr := filepath.EvalSymlinks(resolved)
		if evalErr != nil {
			if os.IsNotExist(evalErr) {
				// Dangling symlink: its target does not exist inside the jail,
				// so creating the file through it would write outside.
				return "", "", fmt.Errorf("path escapes jail: %q resolves outside the workspace", userPath)
			}
			return "", "", fmt.Errorf("resolve path: %w", evalErr)
		}
		if !isWithinDeleteRoot(t.agentDir, resolvedFinal) {
			return "", "", fmt.Errorf("path escapes jail: %q resolves to %q", userPath, resolvedFinal)
		}
		if fi, statErr := os.Stat(resolvedFinal); statErr == nil && fi.IsDir() {
			return "", "", fmt.Errorf("output path is a directory: %s", userPath)
		}
	}

	return resolved, backend.DefaultMountPoint + "/" + filepath.ToSlash(filepath.Clean(rel)), nil
}

// displayPathFor renders a mount-scoped display path (shared by error copy).
func displayPathFor(mount, rel string) string {
	return mount + "/" + filepath.ToSlash(filepath.Clean(rel))
}

// resolveTemplate maps the template path against the jail's read contract
// (same rules as document.read: agent directory plus read-only roots such as
// drop-lane attachment mounts). The template must already exist.
func (t *documentCreateTool) resolveTemplate(userPath string) (string, error) {
	if userPath == "" {
		return "", fmt.Errorf("template path cannot be empty")
	}
	if strings.Contains(userPath, "..") {
		return "", fmt.Errorf("path escape attempt: %q contains \"..\"", userPath)
	}

	var candidate string
	if userPath == backend.DefaultMountPoint {
		candidate = t.agentDir
	} else if rel := strings.TrimPrefix(userPath, backend.DefaultMountPoint+"/"); rel != userPath && rel != "" {
		candidate = filepath.Join(t.agentDir, filepath.Clean(rel))
	} else if filepath.IsAbs(userPath) {
		candidate = filepath.Clean(userPath)
	} else {
		candidate = filepath.Join(t.agentDir, filepath.Clean(userPath))
	}

	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no such template file: %s", userPath)
		}
		return "", fmt.Errorf("resolve path: %w", err)
	}
	if !t.isWithinAllowedRoots(resolved) {
		return "", fmt.Errorf("path is outside allowed directories: %q", userPath)
	}
	fi, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("stat template file: %w", err)
	}
	if fi.IsDir() {
		return "", fmt.Errorf("template path is a directory: %s", userPath)
	}
	return resolved, nil
}

func (t *documentCreateTool) isWithinAllowedRoots(path string) bool {
	cleaned := filepath.Clean(path)
	if isWithinRoot(t.agentDir, cleaned) {
		return true
	}
	for _, root := range t.readOnlyRoots {
		if isWithinRoot(root, cleaned) {
			return true
		}
	}
	return false
}

// InvokableRun satisfies tool.InvokableTool. Path validation failures and
// unknown formats return errors (the runtime's tool-error middleware turns
// them into JSON error results); per-document generation failures return
// structured error results naming the document so the run continues
// (design.md D1, spec: "the agent run SHALL NOT fail").
func (t *documentCreateTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args documentCreateArgs
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("document.create: %w", err)
	}

	format := strings.ToLower(strings.TrimSpace(args.Format))
	switch format {
	case "xlsx", "pdf", "docx", "pptx":
	default:
		return "", fmt.Errorf("document.create: unsupported format %q; supported formats: docx, pdf, pptx, xlsx", args.Format)
	}

	resolvedPath, displayPath, err := t.resolveOutput(args.Path)
	if err != nil {
		return "", fmt.Errorf("document.create: %w", err)
	}
	docName := filepath.Base(displayPath)

	// D3: PDF is excluded from template-fill — reject up front with guidance.
	if args.Template != "" && format == "pdf" {
		return documentCreateFailure(displayPath, docName, format,
			fmt.Sprintf("Cannot create %s from a template: PDF is a final-form format and cannot be filled. Use an xlsx or docx invoice template instead, or generate the PDF with data.source = \"structured\" (invoice data) or data.source = \"html\".", docName)), nil
	}

	var resolvedTemplate string
	if args.Template != "" {
		resolvedTemplate, err = t.resolveTemplate(args.Template)
		if err != nil {
			return "", fmt.Errorf("document.create: %w", err)
		}
		// A template whose format does not match the requested output is a
		// per-document failure, not a parameter error.
		if ext := strings.ToLower(filepath.Ext(resolvedTemplate)); ext != "."+format {
			return documentCreateFailure(displayPath, docName, format,
				fmt.Sprintf("Cannot create %s: the template %s is a %s file, but the requested format is %s. Provide a .%s template or omit the template to generate from scratch.", docName, args.Template, strings.TrimPrefix(ext, "."), format, format)), nil
		}
	}

	genCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	var genErr error
	switch format {
	case "xlsx":
		if resolvedTemplate != "" {
			genErr = runDocumentGenerator(func() error { return fillXLSXTemplate(genCtx, resolvedTemplate, args.Data, resolvedPath) })
		} else {
			genErr = runDocumentGenerator(func() error { return generateXLSX(genCtx, args.Data, resolvedPath) })
		}
	case "docx":
		if resolvedTemplate != "" {
			genErr = runDocumentGenerator(func() error { return fillDOCXTemplate(genCtx, resolvedTemplate, args.Data, resolvedPath) })
		} else {
			genErr = runDocumentGenerator(func() error { return generateDOCX(genCtx, args.Data, resolvedPath) })
		}
	case "pptx":
		if resolvedTemplate != "" {
			genErr = runDocumentGenerator(func() error { return fillPPTXTemplate(genCtx, resolvedTemplate, args.Data, resolvedPath) })
		} else {
			genErr = runDocumentGenerator(func() error { return generatePPTX(genCtx, args.Data, resolvedPath) })
		}
	case "pdf":
		genErr = runDocumentGenerator(func() error { return generatePDF(genCtx, t.htmlRenderer, args.Data, resolvedPath) })
	}

	if genErr != nil {
		if ctx.Err() != nil {
			// The run itself is being torn down; propagate as a real error.
			return "", fmt.Errorf("document.create: %w", genErr)
		}
		if errors.Is(genErr, context.DeadlineExceeded) {
			return documentCreateFailure(displayPath, docName, format,
				fmt.Sprintf("Cannot create %s: generation timed out after %v.", docName, t.timeout)), nil
		}
		return documentCreateFailure(displayPath, docName, format,
			fmt.Sprintf("Cannot create %s: %v", docName, genErr)), nil
	}

	// D6: deliver through the capability-URL machinery. A publisher failure
	// does not undo the creation — the file exists; the result simply carries
	// no download link.
	capabilityURL := ""
	if t.publisher != nil {
		if url, pubErr := t.publisher.PublishCreatedDocument(ctx, t.workspaceID, docName, resolvedPath); pubErr == nil {
			capabilityURL = url
		}
	}

	out, err := json.Marshal(map[string]any{
		"path":   displayPath,
		"name":   docName,
		"format": format,
		"url":    capabilityURL,
		"result": fmt.Sprintf("Created %s (%s)", docName, format),
	})
	if err != nil {
		return "", fmt.Errorf("document.create: encode result: %w", err)
	}
	return string(out), nil
}

// documentCreateFailure renders the structured per-document error result: a
// result the model reads and reacts to, never a run failure.
func documentCreateFailure(userPath, docName, format, msg string) string {
	out, err := json.Marshal(map[string]any{
		"path":   userPath,
		"name":   docName,
		"format": format,
		"error":  msg,
	})
	if err != nil {
		return msg
	}
	return string(out)
}

// runDocumentGenerator executes one generator under panic recovery —
// malformed input data or template structures can make third-party writers
// panic, and every such failure is a structured per-document error, not a
// run failure (mirrors document.read's convertDocument).
func runDocumentGenerator(gen func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("generation failed: %v", r)
		}
	}()
	return gen()
}
