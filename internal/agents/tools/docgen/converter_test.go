package docgen

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	picoloom "github.com/alnah/picoloom/v2"
	gopptx "github.com/kenny-not-dead/gopptx"
	docx "github.com/mmonterroca/docxgo/v2"
	"github.com/mmonterroca/docxgo/v2/domain"
	"github.com/stretchr/testify/require"
)

// fakeEngine stands in for picoloom's converter so lifecycle tests never
// launch Chrome. It records calls and, when Delay is set, slows conversions
// to expose unsynchronized overlap.
type fakeEngine struct {
	mu        sync.Mutex
	converts  int
	closes    int
	active    int
	maxActive int
	delay     time.Duration
	convertFn func(ctx context.Context, input picoloom.Input) (*picoloom.ConvertResult, error)
}

func (f *fakeEngine) Convert(ctx context.Context, input picoloom.Input) (*picoloom.ConvertResult, error) {
	f.mu.Lock()
	f.converts++
	f.active++
	if f.active > f.maxActive {
		f.maxActive = f.active
	}
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.active--
		f.mu.Unlock()
	}()
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	if f.convertFn != nil {
		return f.convertFn(ctx, input)
	}
	return &picoloom.ConvertResult{PDF: []byte("%PDF-fake")}, nil
}

func (f *fakeEngine) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closes++
	return nil
}

func (f *fakeEngine) stats() (converts, closes, maxActive int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.converts, f.closes, f.maxActive
}

// engineRecorder is a newFunc seam capturing every engine built and the
// theme it was built for.
type engineRecorder struct {
	mu      sync.Mutex
	themes  []string
	engines []*fakeEngine
	fn      func(theme string, e *fakeEngine) (pdfEngine, error)
}

func (r *engineRecorder) newFunc(theme string) (pdfEngine, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := &fakeEngine{}
	if r.fn != nil {
		eng, err := r.fn(theme, e)
		if err != nil {
			return nil, err
		}
		r.themes = append(r.themes, theme)
		r.engines = append(r.engines, e)
		return eng, nil
	}
	r.themes = append(r.themes, theme)
	r.engines = append(r.engines, e)
	return e, nil
}

func (r *engineRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.engines)
}

func (r *engineRecorder) lastTheme() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.themes[len(r.themes)-1]
}

// newRecordingConverter builds a Converter wired to the recorder's stubs.
func newRecordingConverter(rec *engineRecorder) *Converter {
	c := NewConverter()
	c.newFunc = rec.newFunc
	return c
}

func TestConverter_EngineCreatedLazilyOnFirstUse(t *testing.T) {
	rec := &engineRecorder{}
	c := newRecordingConverter(rec)
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	require.Zero(t, rec.count(), "construction must not build an engine (no Chrome until first use)")

	dest := filepath.Join(t.TempDir(), "out.pdf")
	res, err := c.Convert(context.Background(), "# Hello", dest, nil)
	require.NoError(t, err)
	require.Equal(t, 1, rec.count(), "first conversion builds the engine")
	require.Equal(t, "", rec.lastTheme())
	require.Equal(t, dest, res.Path)
	require.Equal(t, int64(len("%PDF-fake")), res.SizeBytes)
	require.FileExists(t, dest)

	_, err = c.Convert(context.Background(), "# Hello", dest, nil)
	require.NoError(t, err)
	require.Equal(t, 1, rec.count(), "the second conversion reuses the live engine")

	converts, _, _ := rec.engines[0].stats()
	require.Equal(t, 2, converts)
}

func TestConverter_ThemeSwapRebuildsEngine(t *testing.T) {
	rec := &engineRecorder{}
	c := newRecordingConverter(rec)
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	dest := filepath.Join(t.TempDir(), "out.pdf")

	_, err := c.Convert(context.Background(), "md", dest, &Preset{Theme: "technical"})
	require.NoError(t, err)
	require.Equal(t, "technical", rec.lastTheme())

	_, err = c.Convert(context.Background(), "md", dest, &Preset{Theme: "corporate"})
	require.NoError(t, err)
	require.Equal(t, 2, rec.count(), "a new theme rebuilds the engine (picoloom resolves styles at construction)")
	_, closes, _ := rec.engines[0].stats()
	require.Equal(t, 1, closes, "the swapped-out engine is closed")

	// The empty theme and the explicit "default" theme share one engine.
	_, err = c.Convert(context.Background(), "md", dest, nil)
	require.NoError(t, err)
	_, err = c.Convert(context.Background(), "md", dest, &Preset{Theme: "default"})
	require.NoError(t, err)
	require.Equal(t, 3, rec.count(), "nil preset and theme \"default\" are the same engine key")
	require.Equal(t, "", rec.lastTheme())
}

func TestConverter_ConversionsAreSerialized(t *testing.T) {
	rec := &engineRecorder{}
	c := newRecordingConverter(rec)
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	dest := filepath.Join(t.TempDir(), "out.pdf")

	const concurrent = 2
	errs := make(chan error, concurrent)
	for i := range concurrent {
		go func() {
			<-time.After(time.Duration(i)*10*time.Millisecond + 5*time.Millisecond)
			_, err := c.Convert(context.Background(), "md", dest, nil)
			errs <- err
		}()
	}
	for range concurrent {
		require.NoError(t, <-errs)
	}

	_, _, maxActive := rec.engines[0].stats()
	require.Equal(t, 1, maxActive, "conversions must run one at a time (design D3)")
	require.Equal(t, 1, rec.count(), "concurrent callers must not race engine creation")
}

func TestConverter_CloseLifecycle(t *testing.T) {
	t.Run("close before any use", func(t *testing.T) {
		c := NewConverter()
		require.NoError(t, c.Close(), "closing an unused converter is a no-op")
	})

	t.Run("close is idempotent and closes the engine once", func(t *testing.T) {
		rec := &engineRecorder{}
		c := newRecordingConverter(rec)
		dest := filepath.Join(t.TempDir(), "out.pdf")
		_, err := c.Convert(context.Background(), "md", dest, nil)
		require.NoError(t, err)

		require.NoError(t, c.Close())
		require.NoError(t, c.Close(), "second close must not error")
		_, closes, _ := rec.engines[0].stats()
		require.Equal(t, 1, closes)
	})

	t.Run("convert after close", func(t *testing.T) {
		c := NewConverter()
		require.NoError(t, c.Close())
		_, err := c.Convert(context.Background(), "md", filepath.Join(t.TempDir(), "out.pdf"), nil)
		require.ErrorContains(t, err, "converter is closed")
	})
}

func TestConverter_InputValidation(t *testing.T) {
	rec := &engineRecorder{}
	c := newRecordingConverter(rec)
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	dest := filepath.Join(t.TempDir(), "out.pdf")

	_, err := c.Convert(context.Background(), "", dest, nil)
	require.ErrorContains(t, err, "markdown is empty")

	_, err = c.Convert(context.Background(), "md", "", nil)
	require.ErrorContains(t, err, "destination path is required")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.Convert(ctx, "md", dest, nil)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, rec.count(), "validation and cancellation must precede engine creation")
}

func TestConverter_MissingChromeErrorAndRelaunch(t *testing.T) {
	browserDead := false
	rec := &engineRecorder{
		fn: func(theme string, e *fakeEngine) (pdfEngine, error) {
			if browserDead {
				e.convertFn = func(_ context.Context, _ picoloom.Input) (*picoloom.ConvertResult, error) {
					// Mirror picoloom's wrapping: Convert wraps the engine's
					// ErrBrowserConnect in a conversion error.
					return nil, fmt.Errorf("converting to PDF: %w",
						fmt.Errorf("%w: executable file not found", picoloom.ErrBrowserConnect))
				}
				return e, nil
			}
			return e, nil
		},
	}
	c := newRecordingConverter(rec)
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	dest := filepath.Join(t.TempDir(), "out.pdf")

	browserDead = true
	_, err := c.Convert(context.Background(), "md", dest, nil)
	require.ErrorContains(t, err, "start chromium (set CHROME_PATH to skip rod's lookup/download)")

	browserDead = false
	res, err := c.Convert(context.Background(), "md", dest, nil)
	require.NoError(t, err, "the next conversion relaunches lazily, like the browser tool")
	require.Equal(t, int64(len("%PDF-fake")), res.SizeBytes)
	require.Equal(t, 2, rec.count(), "the poisoned engine must have been dropped")
	_, closes, _ := rec.engines[0].stats()
	require.Equal(t, 1, closes, "the dropped engine is closed")
}

func TestConverter_OtherEngineErrorsPassThrough(t *testing.T) {
	rec := &engineRecorder{
		fn: func(theme string, e *fakeEngine) (pdfEngine, error) {
			e.convertFn = func(_ context.Context, _ picoloom.Input) (*picoloom.ConvertResult, error) {
				return nil, errors.New("markdown conversion failed")
			}
			return e, nil
		},
	}
	c := newRecordingConverter(rec)
	t.Cleanup(func() { require.NoError(t, c.Close()) })

	_, err := c.Convert(context.Background(), "md", filepath.Join(t.TempDir(), "out.pdf"), nil)
	require.ErrorContains(t, err, "docgen: convert")
	require.ErrorContains(t, err, "markdown conversion failed")
	require.Equal(t, 1, rec.count(), "non-browser errors keep the engine alive")
}

func TestBridgeChromePath(t *testing.T) {
	t.Run("forwards CHROME_PATH when ROD_BROWSER_BIN is unset", func(t *testing.T) {
		t.Setenv("CHROME_PATH", "/opt/custom/chrome")
		require.NoError(t, os.Unsetenv("ROD_BROWSER_BIN"))
		t.Cleanup(func() { _ = os.Unsetenv("ROD_BROWSER_BIN") })

		bridgeChromePath()
		require.Equal(t, "/opt/custom/chrome", os.Getenv("ROD_BROWSER_BIN"))
	})

	t.Run("existing ROD_BROWSER_BIN wins", func(t *testing.T) {
		t.Setenv("CHROME_PATH", "/opt/custom/chrome")
		t.Setenv("ROD_BROWSER_BIN", "/already/set")

		bridgeChromePath()
		require.Equal(t, "/already/set", os.Getenv("ROD_BROWSER_BIN"))
	})

	t.Run("no CHROME_PATH leaves ROD_BROWSER_BIN alone", func(t *testing.T) {
		require.NoError(t, os.Unsetenv("CHROME_PATH"))
		t.Cleanup(func() { _ = os.Unsetenv("CHROME_PATH") })
		require.NoError(t, os.Unsetenv("ROD_BROWSER_BIN"))
		t.Cleanup(func() { _ = os.Unsetenv("ROD_BROWSER_BIN") })

		bridgeChromePath()
		require.Empty(t, os.Getenv("ROD_BROWSER_BIN"))
	})
}

func TestNewPicoloomEngine(t *testing.T) {
	// Real library construction needs no Chrome: it only resolves embedded
	// assets and parses templates.
	_, err := newPicoloomEngine("")
	require.NoError(t, err)

	for _, theme := range Themes {
		_, err := newPicoloomEngine(theme)
		require.NoError(t, err, "every v1 theme must be a picoloom style: %s", theme)
	}

	_, err = newPicoloomEngine("not-a-style")
	require.Error(t, err, "an unknown theme must fail at engine creation with a readable error")
}

// TestConverter_Live drives a real markdown-to-PDF conversion through
// headless Chromium; it needs a browser binary (CHROME_PATH or one rod can
// find) and is skipped unless BUDDIES_BROWSER_LIVE_TEST=1, so
// `go test ./...` never needs a browser — the same gate the browser tool's
// live test uses.
func TestConverter_Live(t *testing.T) {
	if os.Getenv("BUDDIES_BROWSER_LIVE_TEST") != "1" {
		t.Skip("set BUDDIES_BROWSER_LIVE_TEST=1 (and CHROME_PATH if no Chromium is installed) to run the live smoke test")
	}
	dest := filepath.Join(t.TempDir(), "report.pdf")

	c := NewConverter()
	defer func() { require.NoError(t, c.Close()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	res, err := c.Convert(ctx, "# Title\n\nBody paragraph.\n\n## Section\n\nMore text.", dest, &Preset{
		Theme: "technical",
		Page:  &PageSetup{Size: "a4", Orientation: "portrait"},
		TOC:   &TOCSettings{},
	})
	require.NoError(t, err)
	require.Equal(t, dest, res.Path)
	require.Greater(t, res.SizeBytes, int64(0))

	data, err := os.ReadFile(dest)
	require.NoError(t, err)
	require.Len(t, data, int(res.SizeBytes), "result size must match the file on disk")
	require.Equal(t, "%PDF", string(data[:4]), "the destination must hold a real PDF")
}

// TestConverter_OfficeEntryPoints covers the DOCX and PPTX entry points'
// shared plumbing (add-document-editing task 4.2): the guards, the closed
// check, the serialized write, and — through the real renderers — a file on
// disk that re-reads as the right kind of document.
func TestConverter_OfficeEntryPoints(t *testing.T) {
	t.Run("guards match the PDF path", func(t *testing.T) {
		c := NewConverter()
		t.Cleanup(func() { _ = c.Close() })
		_, err := c.ConvertDOCX(context.Background(), "", "/tmp/never.docx", nil, nil)
		require.ErrorContains(t, err, "markdown is empty")
		_, err = c.ConvertPPTX(context.Background(), "# H", "", nil, []byte("x"))
		require.ErrorContains(t, err, "destination path is required")
		_, err = c.ConvertPPTX(context.Background(), "# H", "/tmp/never.pptx", nil, nil)
		require.ErrorContains(t, err, "needs a template preset")
	})

	t.Run("closed converter refuses office output", func(t *testing.T) {
		c := NewConverter()
		require.NoError(t, c.Close())
		_, err := c.ConvertDOCX(context.Background(), "# H", "/tmp/never.docx", nil, nil)
		require.ErrorContains(t, err, "converter is closed")
		_, err = c.ConvertPPTX(context.Background(), "# H", "/tmp/never.pptx", nil, []byte("x"))
		require.ErrorContains(t, err, "converter is closed")
	})

	t.Run("renders land on disk and re-read as office documents", func(t *testing.T) {
		dir := t.TempDir()
		c := NewConverter()
		t.Cleanup(func() { _ = c.Close() })

		docxPath := filepath.Join(dir, "letter.docx")
		res, err := c.ConvertDOCX(context.Background(), "# Q3 Report\n\nBody.", docxPath,
			&DOCXPreset{Flavor: PresetFlavorConfig, Fonts: &DOCXFonts{BodyFamily: "Georgia"}}, nil)
		require.NoError(t, err)
		require.Equal(t, docxPath, res.Path)
		require.Greater(t, res.SizeBytes, int64(0))
		doc, err := docx.OpenDocumentFromBytes(mustRead(t, docxPath))
		require.NoError(t, err)
		require.Equal(t, []string{"Q3 Report", "Body."}, docxTexts(t, doc))

		pptxPath := filepath.Join(dir, "deck.pptx")
		skeleton := gopptxSkel(t)
		res, err = c.ConvertPPTX(context.Background(), "# Q3 Review", pptxPath,
			&PPTXPreset{Flavor: PresetFlavorTemplate}, skeleton)
		require.NoError(t, err)
		require.Equal(t, pptxPath, res.Path)
		require.Greater(t, res.SizeBytes, int64(0))
		deck, err := gopptx.OpenReader(bytes.NewReader(mustRead(t, pptxPath)))
		require.NoError(t, err)
		t.Cleanup(func() { _ = deck.Close() })
		require.Equal(t, []int{256}, deck.GetSlideList())
	})
}

// gopptxSkel authors a one-token skeleton, the same fixture pattern the
// pptx engine tests use.
func gopptxSkel(t *testing.T) []byte {
	t.Helper()
	f := gopptx.NewFile()
	body := gopptx.DecodeTextBody{
		BodyProperties: &gopptx.DecodeBodyProperties{},
		Paragraph: []gopptx.DecodeParagraph{
			{Runs: []gopptx.DecodeRuns{{Text: "{{title}}"}}},
		},
	}
	_, err := f.CreateShape(256, gopptx.DecodeShapeProperties{}, body)
	require.NoError(t, err)
	var buf bytes.Buffer
	_, err = f.WriteTo(&buf)
	require.NoError(t, err)
	return buf.Bytes()
}

// docxTexts re-reads the generated DOCX and returns the paragraph texts.
func docxTexts(t *testing.T, doc domain.Document) []string {
	t.Helper()
	var texts []string
	for _, p := range doc.Paragraphs() {
		texts = append(texts, p.Text())
	}
	return texts
}

// mustRead is the disk read every round-trip assertion rides.
func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}
