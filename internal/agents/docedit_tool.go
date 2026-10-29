package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"

	"github.com/oniharnantyo/buddies/internal/agents/backend"
	"github.com/oniharnantyo/buddies/internal/agents/tools/documents"
	"github.com/oniharnantyo/buddies/internal/agents/tools/pdfops"
)

// editDocumentToolDescription is the model-facing description of the
// edit_document tool; the settings surfaces render the catalog entry's
// shorter one (builtin_tools.go).
const editDocumentToolDescription = "Edit documents in your workspace. Office files (.docx, .xlsx, .pptx) are revised IN PLACE with targeted operations — only what an operation addresses changes, and every other paragraph, cell, slide, style, and embedded object is preserved. " +
	"Supply path (a workspace file) and ops, a JSON array of operations addressed exactly as read_file printed them: " +
	"DOCX — replace_paragraph {index,text}, insert_paragraph {afterIndex,text,style}, delete_paragraph {index}, fill_placeholders {values} at ¶n addresses; " +
	"XLSX — set_cell {sheet,cell,text}, clear_cell {sheet,cell}, insert_row {sheet,index}, delete_row {sheet,index} at Sheet!Cell addresses; " +
	"PPTX — replace_shape_text {slide,shape,text}, fill_placeholders {values} at slide n, shape m addresses. " +
	"PDF files (.pdf) take structural operations in the same ops array — inputs are never modified, every operation writes an output file, so write outputs under artifacts/ (e.g. /workspace/artifacts/merged.pdf) to surface them as artifacts: " +
	"merge {type,inputs,output}, extract {type,input,pages,output}, rotate {type,input,pages,angle,output}, stamp {type,input,text,output,pages?,position?,font_size?,rotation?,opacity?,on_top?}, fill_form {type,input,fields,output}, rewrite_text {type,input}. " +
	"Page selections use pdfcpu syntax (\"1-3\", \"even\", \"2-8:odd\"). " +
	"Read a file with read_file first to see its addresses. " +
	"PDF text is never rewritten: rewrite_text answers with the regeneration path (create_document). " +
	"Rejected operations (unknown target, malformed operation) and processing failures are reported in the result's error field; they do not fail the call."

// resolveWorkspaceRoot canonicalizes the workspace directory the workspace-
// scoped document tools root at, mirroring the delete tool's construction:
// the root resolves symlinks to its canonical form so the jail's containment
// checks compare resolved paths on both sides.
func resolveWorkspaceRoot(workspace, toolName string) (string, error) {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return "", fmt.Errorf("%s: resolve workspace path: %w", toolName, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("%s: resolve workspace: %w", toolName, err)
	}
	return strings.TrimSuffix(resolved, string(filepath.Separator)), nil
}

// resolveWorkspacePath maps a model-supplied workspace path onto the host
// filesystem, mirroring the fs jail's read and write validation
// (fsJailedBackend.resolvePath / resolveWritablePath) for the document tools:
// mount-scoped paths under DefaultMountPoint and bare relative paths resolve
// inside the workspace, raw host absolute paths and any ".." are rejected,
// and symlinks must resolve inside the jail. mustExist requires the target to
// exist (inputs); a not-yet-existing target resolves through its first
// existing parent (outputs), validated like the jail's resolveUnder.
// workspace is the resolved (symlink-free) workspace root. Returns the
// resolved host path and its mount-scoped display form.
func resolveWorkspacePath(workspace, userPath string, mustExist bool) (resolved, display string, err error) {
	fail := func(format string, args ...any) (string, string, error) {
		return "", "", fmt.Errorf(format, args...)
	}
	if userPath == "" {
		return fail("path cannot be empty")
	}
	// Reject obvious escape attempts.
	if strings.Contains(userPath, "..") {
		return fail("path escape attempt: %q contains %q", userPath, "..")
	}
	rel := userPath
	switch {
	case userPath == backend.DefaultMountPoint:
		return fail("%s is the workspace root, not a file inside it", backend.DefaultMountPoint)
	case strings.HasPrefix(userPath, backend.DefaultMountPoint+"/"):
		rel = strings.TrimPrefix(userPath, backend.DefaultMountPoint+"/")
	case filepath.IsAbs(userPath):
		return fail("absolute paths are not allowed: %q (address files under %s instead)", userPath, backend.DefaultMountPoint)
	}
	if filepath.Clean(rel) == "." {
		return fail("%s addresses the workspace root, not a file inside it", backend.DefaultMountPoint)
	}
	full := filepath.Join(workspace, filepath.Clean(rel))
	resolved, err = filepath.EvalSymlinks(full)
	if err != nil && !os.IsNotExist(err) {
		return "", "", fmt.Errorf("resolve path: %w", err)
	}
	if os.IsNotExist(err) {
		if mustExist {
			return fail("no such file: %s", mountDisplay(rel))
		}
		// Not-yet-created target: the first existing parent must be inside
		// the jail (the jail's resolveUnder walk).
		parent := filepath.Dir(full)
		for {
			resolvedParent, perr := filepath.EvalSymlinks(parent)
			if os.IsNotExist(perr) && filepath.Dir(parent) != parent {
				parent = filepath.Dir(parent)
				continue
			}
			if perr != nil {
				return "", "", fmt.Errorf("resolve path: %w", perr)
			}
			if !isWithinWorkspace(workspace, resolvedParent) {
				return fail("parent directory is outside jail: %q", userPath)
			}
			break
		}
		resolved = full
	} else if !isWithinWorkspace(workspace, resolved) {
		return fail("path escapes jail: %q resolves to %q", userPath, resolved)
	}
	return resolved, mountDisplay(rel), nil
}

// mountDisplay renders a workspace-relative path in its mount-scoped display
// form — the address the model is told its workspace lives at.
func mountDisplay(rel string) string {
	return backend.DefaultMountPoint + "/" + filepath.ToSlash(filepath.Clean(rel))
}

// newEditDocumentTool builds the edit_document invokable tool for one agent
// workspace (add-document-editing task 5.1, designs D5 and D7): targeted
// operations apply against workspace files — office documents revised in
// place through the documents package's engines, PDFs restructured through
// the pdfops engine — dispatched by the file name's extension. Rejected
// operations and processing failures come back as readable {ok:false} tool
// results so the model can react and the turn continues; a Go error is
// returned only for infrastructure failures (an unwritable destination).
func newEditDocumentTool(workspace string) (tool.BaseTool, error) {
	if workspace == "" {
		return nil, errors.New("edit_document: no workspace configured")
	}
	root, err := resolveWorkspaceRoot(workspace, "edit_document")
	if err != nil {
		return nil, err
	}
	st := &editDocumentTool{
		root: root,
		docx: documents.NewDocxEngine(),
		xlsx: documents.NewXlsxEngine(),
		pptx: documents.NewPptxEngine(),
		pdf:  pdfops.NewEngine(),
	}
	return utils.InferTool(ToolEditDocument, editDocumentToolDescription, st.run)
}

// editDocumentTool is the edit_document tool's closed-over dependencies: the
// resolved workspace root every path resolves inside, and the four edit
// engines dispatched by extension.
type editDocumentTool struct {
	root string
	docx *documents.DocxEngine
	xlsx *documents.XlsxEngine
	pptx *documents.PptxEngine
	pdf  pdfops.Engine
}

// editOp is one operation of the tool's ops array: the documents package's
// targeted-edit operation (embedded — its fields carry the office ops), plus
// the fields the PDF structural operations need. Which fields matter depends
// on Type and the file's extension.
type editOp struct {
	documents.Op

	// Inputs and Input name the operation's source PDFs (merge takes at
	// least two); Output is the workspace path the result PDF is written to.
	Inputs []string `json:"inputs,omitempty"`
	Input  string   `json:"input,omitempty"`
	Output string   `json:"output,omitempty"`

	// Pages is the page selection in pdfcpu syntax ("" means all pages for
	// rotate and stamp; extract requires one).
	Pages string `json:"pages,omitempty"`
	// Angle is rotate's clockwise rotation in degrees, a multiple of 90.
	Angle int `json:"angle,omitempty"`
	// Position, FontSize, Rotation, Opacity, and OnTop shape where and how
	// stamp's text (the embedded Op's Text) is drawn; absent options keep
	// the defaults.
	Position string  `json:"position,omitempty"`
	FontSize float64 `json:"font_size,omitempty"`
	Rotation int     `json:"rotation,omitempty"`
	Opacity  float64 `json:"opacity,omitempty"`
	OnTop    bool    `json:"on_top,omitempty"`
	// Fields maps form field names to values for fill_form.
	Fields map[string]any `json:"fields,omitempty"`
}

// editDocumentArgs is the tool's whole input: the workspace file to edit and
// the operations, following the documents package's op schema for office
// files (the addresses read_file's views print) and the pdfops package's op
// vocabulary for PDFs.
type editDocumentArgs struct {
	// Path is the workspace file to edit, absolute under /workspace (a bare
	// relative path addresses the same file).
	Path string `json:"path"`
	// Ops are the operations, applied in order — each addresses the file as
	// left by the previous one. PDF operations are independent: each names
	// its own inputs and output.
	Ops []editOp `json:"ops"`
}

// editDocumentOutput is the tool's whole output: ok=false with Error for
// every rejected operation or unreadable document (the turn continues); on
// success the edited file's workspace path for office edits, or the applied
// operation, its explanation, and the written PDF's workspace path and page
// count for PDF structural operations.
type editDocumentOutput struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Path  string `json:"path,omitempty"`

	Op         string `json:"op,omitempty"`
	Message    string `json:"message,omitempty"`
	OutputPath string `json:"output_path,omitempty"`
	PageCount  int    `json:"page_count,omitempty"`
}

// run executes one edit_document tool call.
func (t *editDocumentTool) run(ctx context.Context, in editDocumentArgs) (editDocumentOutput, error) {
	resolved, display, err := resolveWorkspacePath(t.root, in.Path, true)
	if err != nil {
		return editDocumentOutput{OK: false, Error: fmt.Sprintf("path: %s", err.Error())}, nil
	}
	if len(in.Ops) == 0 {
		return editDocumentOutput{OK: false,
			Error: "ops: at least one operation is required (read_file prints the addresses the operations target)",
		}, nil
	}

	switch strings.ToLower(filepath.Ext(display)) {
	case ".docx", ".xlsx", ".pptx":
		return t.runOffice(ctx, resolved, display, in.Ops)
	case ".pdf":
		return t.runPDF(ctx, in.Ops)
	default:
		return editDocumentOutput{OK: false,
			Error: fmt.Sprintf("%s is not an editable document — edit_document applies to .docx, .xlsx, .pptx, and .pdf files", display),
		}, nil
	}
}

// runOffice applies targeted operations to an office document in place: the
// engine reads the file, applies the operations, and writes the result back
// to the same path.
func (t *editDocumentTool) runOffice(ctx context.Context, resolved, display string, ops []editOp) (editDocumentOutput, error) {
	officeOps := make([]documents.Op, 0, len(ops))
	for _, op := range ops {
		officeOps = append(officeOps, op.Op)
	}
	var res documents.Result
	var err error
	switch strings.ToLower(filepath.Ext(display)) {
	case ".docx":
		res, err = t.docx.Edit(ctx, resolved, resolved, officeOps)
	case ".xlsx":
		res, err = t.xlsx.Edit(ctx, resolved, resolved, officeOps)
	default:
		res, err = t.pptx.Edit(ctx, resolved, resolved, officeOps)
	}
	if err != nil {
		// Infrastructure only (an unwritable destination): the documents
		// package's convention.
		return editDocumentOutput{}, fmt.Errorf("edit_document: %w", err)
	}
	if !res.OK {
		return editDocumentOutput{OK: false, Error: res.Error, Path: display}, nil
	}
	return editDocumentOutput{OK: true, Path: display}, nil
}

// runPDF applies structural operations to a PDF, each in order: every
// operation names its own inputs and output, all resolved through the jail
// before the engine sees them, and the result reports the last one applied.
func (t *editDocumentTool) runPDF(ctx context.Context, ops []editOp) (editDocumentOutput, error) {
	var last pdfops.Result
	outputDisplay := ""
	for _, op := range ops {
		built, outDisplay, failure, err := t.buildPDFOp(op)
		if err != nil {
			return editDocumentOutput{}, fmt.Errorf("edit_document: %w", err)
		}
		if failure != "" {
			return editDocumentOutput{OK: false, Op: op.Type, Error: failure}, nil
		}
		res, err := t.pdf.Apply(ctx, built)
		if err != nil {
			// Infrastructure only (an unwritable output, vanished input):
			// the pdfops package's convention.
			return editDocumentOutput{}, fmt.Errorf("edit_document: %w", err)
		}
		if !res.OK {
			return editDocumentOutput{OK: false, Op: res.Op, Error: res.Message}, nil
		}
		last = res
		outputDisplay = outDisplay
	}
	return editDocumentOutput{
		OK:         true,
		Op:         last.Op,
		Message:    last.Message,
		OutputPath: outputDisplay,
		PageCount:  last.PageCount,
	}, nil
}

// buildPDFOp maps one of the model's PDF operations onto a pdfops operation.
// A readable failure string is a rejected request; a Go error is a
// path-resolution infrastructure failure.
func (t *editDocumentTool) buildPDFOp(op editOp) (built pdfops.Op, outputDisplay, failure string, err error) {
	resolveInput := func(userPath string) (string, string) {
		resolved, display, rerr := resolveWorkspacePath(t.root, userPath, true)
		if rerr != nil {
			failure = fmt.Sprintf("input: %s", rerr.Error())
			return "", ""
		}
		return resolved, display
	}
	resolveOutput := func(userPath string) string {
		resolved, display, rerr := resolveWorkspacePath(t.root, userPath, false)
		if rerr != nil {
			failure = fmt.Sprintf("output: %s", rerr.Error())
			return ""
		}
		// The output's directory is created on demand — the first call
		// targeting artifacts/ brings it into being, exactly like
		// create_document (0700 matches the workspaces package's private
		// dir mode). A failure is a readable result, not fatal.
		if mkErr := os.MkdirAll(filepath.Dir(resolved), docArtifactsDirPerm); mkErr != nil {
			failure = fmt.Sprintf("creating the output directory failed: %s", mkErr.Error())
			return ""
		}
		outputDisplay = display
		return resolved
	}

	switch op.Type {
	case pdfops.OpMerge:
		if len(op.Inputs) < 2 {
			return nil, "", "merge needs at least two input PDFs to concatenate", nil
		}
		inputs := make([]string, 0, len(op.Inputs))
		for _, p := range op.Inputs {
			resolved, _ := resolveInput(p)
			if failure != "" {
				return nil, "", failure, nil
			}
			inputs = append(inputs, resolved)
		}
		if op.Output == "" {
			return nil, "", "merge needs an output path — write it under artifacts/ so it surfaces as an artifact", nil
		}
		output := resolveOutput(op.Output)
		if failure != "" {
			return nil, "", failure, nil
		}
		return pdfops.Merge{Inputs: inputs, Output: output}, outputDisplay, "", nil

	case pdfops.OpExtract:
		input, _ := resolveInput(op.Input)
		if failure != "" {
			return nil, "", failure, nil
		}
		if op.Output == "" {
			return nil, "", "extract needs an output path — write it under artifacts/ so it surfaces as an artifact", nil
		}
		output := resolveOutput(op.Output)
		if failure != "" {
			return nil, "", failure, nil
		}
		return pdfops.Extract{Input: input, Pages: op.Pages, Output: output}, outputDisplay, "", nil

	case pdfops.OpRotate:
		input, _ := resolveInput(op.Input)
		if failure != "" {
			return nil, "", failure, nil
		}
		if op.Output == "" {
			return nil, "", "rotate needs an output path — write it under artifacts/ so it surfaces as an artifact", nil
		}
		output := resolveOutput(op.Output)
		if failure != "" {
			return nil, "", failure, nil
		}
		return pdfops.Rotate{Input: input, Pages: op.Pages, Angle: op.Angle, Output: output}, outputDisplay, "", nil

	case pdfops.OpStamp:
		input, _ := resolveInput(op.Input)
		if failure != "" {
			return nil, "", failure, nil
		}
		if op.Output == "" {
			return nil, "", "stamp needs an output path — write it under artifacts/ so it surfaces as an artifact", nil
		}
		output := resolveOutput(op.Output)
		if failure != "" {
			return nil, "", failure, nil
		}
		return pdfops.Stamp{
			Input: input,
			Text:  op.Text,
			Options: pdfops.StampOptions{
				Position: op.Position,
				FontSize: op.FontSize,
				Rotation: op.Rotation,
				Opacity:  op.Opacity,
				OnTop:    op.OnTop,
			},
			Pages:  op.Pages,
			Output: output,
		}, outputDisplay, "", nil

	case pdfops.OpFillForm:
		input, _ := resolveInput(op.Input)
		if failure != "" {
			return nil, "", failure, nil
		}
		if op.Output == "" {
			return nil, "", "fill_form needs an output path — write it under artifacts/ so it surfaces as an artifact", nil
		}
		output := resolveOutput(op.Output)
		if failure != "" {
			return nil, "", failure, nil
		}
		return pdfops.FillForm{Input: input, Fields: op.Fields, Output: output}, outputDisplay, "", nil

	case pdfops.OpRewriteText:
		// The redirect passes through: the engine's readable result names
		// create_document (spec document-editing, design D7). The engine
		// answers the bare redirect when no input is named.
		input := ""
		if op.Input != "" {
			input, _ = resolveInput(op.Input)
			if failure != "" {
				return nil, "", failure, nil
			}
		}
		return pdfops.RewriteText{Input: input}, "", "", nil

	default:
		return nil, "", fmt.Sprintf("unknown operation %q for a PDF — supported: merge, extract, rotate, stamp, fill_form, rewrite_text", op.Type), nil
	}
}
