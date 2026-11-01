package cli

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	docx "github.com/mmonterroca/docxgo/v2"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/attachments"
	"github.com/oniharnantyo/buddies/internal/services"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/stretchr/testify/require"
)

// TestAttachmentEval is the env-gated live eval for the chat-attachment
// harness (the headless sibling of the GUI live smokes). It runs real model
// turns against a cloned config — one case per carriage path — and scores
// the replies, so harness gaps (carriage, pointer notes, the uploads mirror,
// the read_file hand-off) surface as scored failures with diagnostics
// instead of silent degradation:
//
//	HOME=$TMP BUDDIES_ATTACHMENT_EVAL=1 go test ./internal/cli -run TestAttachmentEval -v -count=1
//
// Cases, pinned to glm-5.3-flash (override with
// BUDDIES_ATTACHMENT_EVAL_MODEL):
//
//   - csv-inline: text lane — the CSV rides the message inline; the total
//     must come back with no tool round-trip.
//   - json-filelane: catch-all file lane — the bytes are withheld, the
//     pointer note points at the session uploads mirror, and the answer
//     requires a read_file round-trip.
//   - png-image: image lane — glm-5.3-flash carries no imageInput row on the
//     live config, so the image degrades to the pointer note; the eval
//     records whether the model can still answer through the filesystem
//     route (on the first live run the agent decoded the pixels with shell
//   - python3, so the route works). The fixture name is color-neutral so
//     a pass cannot come from guessing the name.
//   - pdf-lane: PDF lane — glm-5.3-flash also carries pdfInput=0, so the
//     pointer note is expected and read_file's PDF extraction is the route.
//   - docx-office: office lane — bytes withheld by design; read_file's
//     structured view is the route (fixture built by the repo's own docxgo
//     engine, so a parse failure names a real harness gap, not a bad file).
//   - multi-attach: mixed lanes in one turn — one inline file plus one
//     pointered file; exercises multi-part user-message assembly and a
//     read_file round-trip inside the same turn.
//   - attachment-only: an empty user text with one inline attachment — the
//     file's content is the only input the model receives.
func TestAttachmentEval(t *testing.T) {
	if os.Getenv("BUDDIES_ATTACHMENT_EVAL") == "" {
		t.Skip("set BUDDIES_ATTACHMENT_EVAL=1 (and HOME to a cloned config) to run the live attachment eval")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	app, err := Assemble(ctx, services.Noop{}, services.NoBrowser{})
	require.NoError(t, err)
	defer func() { require.NoError(t, app.Close()) }()

	agentList, err := app.Store.Agents().List(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, agentList, "cloned config has no agents")
	agent := agentList[0]
	for _, a := range agentList {
		if a.Name == "Scout" {
			agent = a
		}
	}
	wantModel := strings.ToLower(os.Getenv("BUDDIES_ATTACHMENT_EVAL_MODEL"))
	if wantModel == "" {
		wantModel = "glm-5.3-flash"
	}
	require.True(t, strings.Contains(strings.ToLower(agent.Model), wantModel),
		"eval must run on %s; %s uses %s (available: scout/jono rows of the cloned config)",
		wantModel, agent.Name, agent.Model)
	require.True(t, agentToolsInclude(agent.Tools, "read_file"),
		"%s lacks read_file; the pointered-lane cases would be unfair", agent.Name)
	t.Logf("eval agent: %s (model %s / provider %s)", agent.Name, agent.Model, agent.ProviderID)

	attStore := attachments.NewStore(app.Layout.Attachments, app.Store.Attachments())

	var orange []byte
	{
		img := image.NewRGBA(image.Rect(0, 0, 64, 64))
		c := color.RGBA{R: 0xFF, G: 0xA5, B: 0x00, A: 0xFF}
		for y := 0; y < 64; y++ {
			for x := 0; x < 64; x++ {
				img.Set(x, y, c)
			}
		}
		var buf bytes.Buffer
		require.NoError(t, png.Encode(&buf, img))
		orange = buf.Bytes()
	}

	expensesCSV := []byte("item,category,amount\ncoffee,food,12.50\nbus,transport,30.00\nbook,leisure,45.00\n")
	deployJSON := []byte("{\n  \"service\": \"payments\",\n  \"release_train\": \"cobalt\",\n  \"region\": \"ap-southeast-1\"\n}\n")

	cases := []struct {
		name   string
		files  []evalFile
		lane   string
		prompt string
		checks []string
	}{
		{
			name: "csv-inline",
			files: []evalFile{
				{"expenses.csv", "text/csv", expensesCSV},
			},
			lane:   "text lane — bytes ride the message inline, no tool round-trip needed",
			prompt: "The attached expenses.csv lists expenses. Reply with ONLY the total of the amount column, nothing else.",
			checks: []string{"87.5"},
		},
		{
			name: "json-filelane",
			files: []evalFile{
				{"deploy.json", "application/json", deployJSON},
			},
			lane:   "file lane — bytes withheld, pointer note to the session uploads mirror, read_file required",
			prompt: "The attached deploy.json holds the release train name. Reply with ONLY that name, nothing else.",
			checks: []string{"cobalt"},
		},
		{
			name: "png-image",
			files: []evalFile{
				{"img-0001.png", "image/png", orange},
			},
			lane:   "image lane — model lacks imageInput so a pointer note is expected; the filesystem route is the only path",
			prompt: "The attached img-0001.png is a single solid color. Reply with ONLY the color's common name, nothing else.",
			checks: []string{"orange"},
		},
		{
			name: "pdf-lane",
			files: []evalFile{
				{"report.pdf", "application/pdf", evalPDF()},
			},
			lane:   "pdf lane — model lacks pdfInput so a pointer note is expected; read_file's PDF extraction is the route",
			prompt: "The attached report.pdf contains an access code. Reply with ONLY that code, nothing else.",
			checks: []string{"maroon"},
		},
		{
			name: "docx-office",
			files: []evalFile{
				{"brief.docx", attachments.MimeDOCX, evalDOCX(t)},
			},
			lane:   "office lane — bytes withheld by design; read_file's structured view is the route",
			prompt: "The attached brief.docx names a vessel. Reply with ONLY the vessel name, nothing else.",
			checks: []string{"kestrel"},
		},
		{
			name: "multi-attach",
			files: []evalFile{
				{"expenses.csv", "text/csv", expensesCSV},
				{"deploy.json", "application/json", deployJSON},
			},
			lane:   "mixed lanes in one turn — csv rides inline while json is pointered; multi-part assembly + read_file",
			prompt: "Two files are attached: expenses.csv and deploy.json. Reply with ONLY the csv's amount-column total, then a comma and a space, then the json's release train name. Nothing else.",
			checks: []string{"87.5", "cobalt"},
		},
		{
			name: "attachment-only",
			files: []evalFile{
				{"note.txt", "text/plain", []byte("Reply with just the word BANANA.")},
			},
			lane:   "attachment-only send — empty user text; the inline .txt content is the only model input",
			prompt: "",
			checks: []string{"banana"},
		},
	}

	var failures []string
	for _, tc := range cases {
		sess, err := app.Runtime.CreateSession(ctx, agent.ID)
		require.NoError(t, err)
		keys := make([]string, 0, len(tc.files))
		for _, f := range tc.files {
			att, err := attStore.Save(ctx, sess.ID, f.name, f.mime, f.data)
			require.NoError(t, err, "fixture %s rejected by the lane caps", f.name)
			keys = append(keys, att.Key)
		}

		start := time.Now()
		turn, err := app.Runtime.Submit(ctx, agents.SendRequest{SessionID: sess.ID,
			Message: tc.prompt, AttachmentKeys: keys})
		require.NoError(t, err)
		require.NoError(t, turn.Wait())
		elapsed := time.Since(start)

		transcript, err := app.Services.Chat.Transcript(ctx, sess.ID)
		require.NoError(t, err)
		var reply, turnErr string
		for _, ev := range transcript {
			switch ev.Kind {
			case sessions.KindAssistantMessage:
				reply += ev.Text
			case sessions.KindTurnError:
				turnErr += ev.Text
			}
		}

		// Harness-health probe (logged, non-scoring): the uploads mirror is
		// best-effort by design, but its absence explains pointer-note cases.
		var missingMirror []string
		for _, f := range tc.files {
			if _, err := os.Stat(filepath.Join(app.Layout.Workspaces, agent.Slug,
				"sessions", sess.ID, "uploads", f.name)); err != nil {
				missingMirror = append(missingMirror, f.name)
			}
		}

		failed := turnErr != ""
		if !failed {
			lower := strings.ToLower(reply)
			for _, want := range tc.checks {
				if !strings.Contains(lower, want) {
					failed = true
				}
			}
		}
		status := "PASS"
		if failed {
			status = "FAIL"
			failures = append(failures, tc.name)
		}

		t.Logf("[%s] %s (%s)", tc.name, status, elapsed.Round(time.Millisecond))
		t.Logf("    lane: %s", tc.lane)
		if len(missingMirror) > 0 {
			t.Logf("    uploads mirror: MISSING %v", missingMirror)
		} else {
			t.Logf("    uploads mirror: present at sessions/%s/uploads", sess.ID)
		}
		if turnErr != "" {
			t.Logf("    turn error: %s", turnErr)
		}
		t.Logf("    reply: %.300s", reply)
	}

	t.Logf("=== attachment eval: %d/%d cases passed (model %s / provider %s) ===",
		len(cases)-len(failures), len(cases), agent.Model, agent.ProviderID)
	for _, name := range failures {
		t.Errorf("attachment eval case %q failed — see the diagnostics above", name)
	}
}

// evalFile is one fixture riding an eval case: stored name, mime (lane
// selector), and bytes.
type evalFile struct {
	name string
	mime string
	data []byte
}

// evalPDF returns a one-page PDF built by hand — object offsets, stream
// length, and the xref table are computed, so pdfcpu's extraction accepts
// it. The page carries one sentence with an unguessable code.
func evalPDF() []byte {
	stream := "BT /F1 24 Tf 72 700 Td (The access code is MAROON.) Tj ET"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs)+1)
	for i, body := range objs {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xrefStart := buf.Len()
	buf.WriteString("xref\n0 6\n0000000000 65535 f \n")
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefStart)
	return buf.Bytes()
}

// evalDOCX returns a minimal vessel brief built by the repo's own docxgo
// engine — the same bytes read_file's structured view parses in production,
// so a parse failure is a harness finding, not a bad fixture.
func evalDOCX(t *testing.T) []byte {
	t.Helper()
	doc := docx.NewDocument()
	para, err := doc.AddParagraph()
	require.NoError(t, err)
	run, err := para.AddRun()
	require.NoError(t, err)
	require.NoError(t, run.SetText("Vessel brief. Ship name: Kestrel."))
	var buf bytes.Buffer
	_, err = doc.WriteTo(&buf)
	require.NoError(t, err)
	return buf.Bytes()
}

// agentToolsInclude reports whether the agent's tool allowlist names tool.
// An empty allowlist means every built-in tool (spec agent-tools).
func agentToolsInclude(tools []string, tool string) bool {
	if len(tools) == 0 {
		return true
	}
	for _, t := range tools {
		if t == tool {
			return true
		}
	}
	return false
}
