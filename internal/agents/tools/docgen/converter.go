package docgen

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	picoloom "github.com/alnah/picoloom/v2"
)

// missingChromeMessage mirrors the browser tool's launch-failure wording:
// go-rod shares ~/.cache/rod between both tools and picoloom drives rod
// directly, so the same remedy (setting CHROME_PATH) applies to a missing
// browser binary.
const missingChromeMessage = "start chromium (set CHROME_PATH to skip rod's lookup/download)"

// Result reports what a conversion produced; the create_document tool
// surfaces these fields on its result card.
type Result struct {
	Path      string // absolute destination path the PDF was written to
	SizeBytes int64  // bytes of the generated PDF
}

// pdfEngine is the slice of picoloom.Converter the lifecycle drives. The
// seam keeps headless Chrome out of unit tests: picoloom launches Chrome on
// the first conversion, so the lifecycle tests run against stub engines.
type pdfEngine interface {
	Convert(ctx context.Context, input picoloom.Input) (*picoloom.ConvertResult, error)
	Close() error
}

// Converter is the manager-held document converter (design D3): created
// lazily on first use (so resolving turns never spawn Chrome), serialized
// behind a mutex (one document at a time on a desktop), and closed on
// shutdown. The caller bounds each conversion with its context — the tool
// applies the 60 s timeout — and cancellation is honored throughout.
//
// picoloom resolves its style at converter construction, so a preset theme
// change swaps the underlying engine (closing the old one); the next
// conversion pays Chrome startup once for the new theme.
type Converter struct {
	mu      sync.Mutex
	engine  pdfEngine // nil until first use; nil again after a failed launch
	theme   string    // theme the live engine was built for ("" = library default)
	closed  bool
	newFunc func(theme string) (pdfEngine, error) // seam for tests
}

// NewConverter constructs the manager-held converter. It never launches
// Chrome and never fails: engine creation is deferred to the first Convert
// and its errors surface there, per the lazy-lifecycle design.
func NewConverter() *Converter {
	return &Converter{newFunc: newPicoloomEngine}
}

// Convert renders markdown to PDF at destPath using the preset's styling; a
// nil preset means picoloom's built-in defaults. The PDF overwrites
// destPath, mirroring the regenerate-on-same-name flow. A browser-level
// failure (missing or dead Chromium) is dropped and rebuilt on the next
// call, the way the browser tool relaunches a profile.
func (c *Converter) Convert(ctx context.Context, markdown, destPath string, preset *Preset) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, fmt.Errorf("docgen: %w", err)
	}
	if markdown == "" {
		return Result{}, errors.New("docgen: markdown is empty")
	}
	if destPath == "" {
		return Result{}, errors.New("docgen: destination path is required")
	}

	theme := engineTheme(preset)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return Result{}, errors.New("docgen: converter is closed")
	}
	engine, err := c.engineFor(theme)
	if err != nil {
		return Result{}, err
	}

	input := mapPreset(preset)
	input.Markdown = markdown
	res, err := engine.Convert(ctx, input)
	if err != nil {
		if errors.Is(err, picoloom.ErrBrowserConnect) {
			c.dropEngine()
			return Result{}, fmt.Errorf("%s: %w", missingChromeMessage, err)
		}
		return Result{}, fmt.Errorf("docgen: convert: %w", err)
	}
	if err := os.WriteFile(destPath, res.PDF, 0o644); err != nil {
		return Result{}, fmt.Errorf("docgen: write PDF: %w", err)
	}
	return Result{Path: destPath, SizeBytes: int64(len(res.PDF))}, nil
}

// Close releases the live engine (its headless Chrome). It is safe to call
// more than once and on a converter that never converted.
func (c *Converter) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	if c.engine == nil {
		return nil
	}
	err := c.engine.Close()
	c.engine = nil
	return err
}

// engineFor returns the live engine, creating it lazily on first use and
// rebuilding it when the requested theme differs from the live one. The
// caller holds mu.
func (c *Converter) engineFor(theme string) (pdfEngine, error) {
	if c.engine != nil && c.theme == theme {
		return c.engine, nil
	}
	if c.engine != nil {
		// Best-effort teardown: the swap replaces the engine either way, and
		// a failed close of a dying Chrome must not block the new one.
		_ = c.engine.Close()
	}
	engine, err := c.newFunc(theme)
	if err != nil {
		c.engine = nil
		c.theme = ""
		return nil, fmt.Errorf("docgen: create converter: %w", err)
	}
	c.engine = engine
	c.theme = theme
	return engine, nil
}

// dropEngine discards the live engine after a browser-level failure so the
// next conversion starts from a fresh launch. The caller holds mu.
func (c *Converter) dropEngine() {
	if c.engine != nil {
		_ = c.engine.Close()
	}
	c.engine = nil
	c.theme = ""
}

// engineTheme normalizes the preset theme into the engine key: the empty
// theme and the explicit "default" theme share one engine because both mean
// picoloom's built-in default style.
func engineTheme(p *Preset) string {
	if p == nil || p.Theme == "" || p.Theme == "default" {
		return ""
	}
	return p.Theme
}

// newPicoloomEngine builds the real picoloom converter for a theme (the
// empty theme keeps the library's default style). No Chrome is launched
// here — picoloom connects on its first conversion.
func newPicoloomEngine(theme string) (pdfEngine, error) {
	bridgeChromePath()
	var opts []picoloom.Option
	if theme != "" {
		opts = append(opts, picoloom.WithStyle(theme))
	}
	return picoloom.NewConverter(opts...)
}

// bridgeChromePath forwards CHROME_PATH to ROD_BROWSER_BIN when the latter
// is unset: rod picks its browser binary from ROD_BROWSER_BIN, and picoloom
// drives rod directly, so the browser tool's CHROME_PATH override must be
// carried over for both tools to honor the same environment variable.
func bridgeChromePath() {
	chrome := os.Getenv("CHROME_PATH")
	if chrome == "" || os.Getenv("ROD_BROWSER_BIN") != "" {
		return
	}
	_ = os.Setenv("ROD_BROWSER_BIN", chrome)
}
