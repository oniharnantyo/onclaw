package agents

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents/backend"
	"github.com/oniharnantyo/buddies/internal/attachments"
	"github.com/oniharnantyo/buddies/internal/knowledge"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// knowledgeToolFixture wires the save_document tool over one knowledge store
// and one attachment store sharing the fake store's registry rows.
type knowledgeToolFixture struct {
	st   *fake.Store
	know *knowledge.Store
	atts *attachments.Store
}

func newKnowledgeToolFixture(t *testing.T) *knowledgeToolFixture {
	t.Helper()
	st := fake.New()
	return &knowledgeToolFixture{
		st:   st,
		know: knowledge.NewStore(t.TempDir(), st.Documents()),
		atts: attachments.NewStore(t.TempDir(), st.Attachments()),
	}
}

// runSaveDocumentTool runs one save_document call through the InvokableTool
// surface the constructor returns as BaseTool and decodes the result.
func runSaveDocumentTool(t *testing.T, base tool.BaseTool, args string) SaveDocumentOutput {
	t.Helper()
	invokable, ok := base.(tool.InvokableTool)
	require.True(t, ok, "save_document tool %T does not implement InvokableTool", base)
	out, err := invokable.InvokableRun(t.Context(), args)
	require.NoError(t, err, "gate rejections must be results, not errors: args %s", args)
	var res SaveDocumentOutput
	require.NoError(t, json.Unmarshal([]byte(out), &res), "result %q", out)
	return res
}

// TestSaveDocumentTool_Info pins the generated schema: every field is a
// scalar, and only the content-source fields are optional-by-validation —
// the inference must not require either source (the handler validates the
// exactly-one rule).
func TestSaveDocumentTool_Info(t *testing.T) {
	f := newKnowledgeToolFixture(t)
	base, err := newSaveDocumentTool(f.know, f.atts, "agent-1", "scout")
	require.NoError(t, err)
	info, err := base.Info(t.Context())
	require.NoError(t, err)
	require.Equal(t, ToolSaveDocument, info.Name)
	require.NotEmpty(t, info.Desc)

	b, err := json.Marshal(info)
	require.NoError(t, err)
	var decoded struct {
		JSONSchema struct {
			Properties map[string]struct {
				Type string `json:"type"`
			} `json:"properties"`
			Required []string `json:"required"`
		} `json:"json_schema"`
	}
	require.NoError(t, json.Unmarshal(b, &decoded))
	props := decoded.JSONSchema.Properties
	for _, name := range []string{"attachment_key", "text", "filename", "description"} {
		require.Contains(t, props, name)
	}
	require.Empty(t, decoded.JSONSchema.Required, "no required field: the exactly-one rule is the handler's")
}

// TestSaveDocumentTool_InlineText pins the inline-text path: the bytes land
// at agents/<slug>/ under an agent source, and the result reports the
// mount-scoped path the filesystem tools read the document at.
func TestSaveDocumentTool_InlineText(t *testing.T) {
	f := newKnowledgeToolFixture(t)
	base, err := newSaveDocumentTool(f.know, f.atts, "agent-1", "scout")
	require.NoError(t, err)

	res := runSaveDocumentTool(t, base, `{"text":"# Status report\n\nall clear","filename":"status.md","description":"weekly status"}`)
	require.True(t, res.OK, "error: %s", res.Error)
	require.Equal(t, "/knowledge/agent/status.md", res.Path)
	require.Equal(t, "status.md", res.Filename)
	require.Equal(t, "text/plain", res.MIME, "MIME resolves from the filename extension")
	require.Equal(t, "weekly status", res.Description)
	require.Equal(t, int64(len("# Status report\n\nall clear")), res.Size)

	// The file is on disk in the agent's scope; the row carries the agent
	// source.
	data, err := os.ReadFile(filepath.Join(f.know.AgentDir("scout"), "status.md"))
	require.NoError(t, err)
	require.Equal(t, "# Status report\n\nall clear", string(data))
	docs, err := f.know.List(t.Context())
	require.NoError(t, err)
	require.Len(t, docs, 1)
	require.Equal(t, knowledge.ScopeAgent, docs[0].Scope)
	require.Equal(t, knowledge.SourceAgent, docs[0].Source)
	require.Equal(t, "agent-1", docs[0].AgentID)
	require.Equal(t, "scout", docs[0].AgentSlug)
}

// TestSaveDocumentTool_AttachmentCopy pins the attachment path: the bytes
// copy from the attachments store into the corpus under the upload's original
// filename when the model supplies none, and the declared MIME carries over.
func TestSaveDocumentTool_AttachmentCopy(t *testing.T) {
	f := newKnowledgeToolFixture(t)
	base, err := newSaveDocumentTool(f.know, f.atts, "agent-1", "scout")
	require.NoError(t, err)

	att, err := f.atts.Save(t.Context(), "meeting-notes.txt", "text/plain", []byte("decisions: ship friday"))
	require.NoError(t, err)
	res := runSaveDocumentTool(t, base, `{"attachment_key":`+quote(att.Key)+`,"description":"sync notes"}`)
	require.True(t, res.OK, "error: %s", res.Error)
	require.Equal(t, "/knowledge/agent/meeting-notes.txt", res.Path)
	require.Equal(t, "text/plain", res.MIME)

	saved, err := os.ReadFile(filepath.Join(f.know.AgentDir("scout"), "meeting-notes.txt"))
	require.NoError(t, err)
	require.Equal(t, "decisions: ship friday", string(saved), "the attachment's bytes copy verbatim")

	// An explicit filename overrides the upload's original name.
	att2, err := f.atts.Save(t.Context(), "raw.txt", "text/plain", []byte("renamed"))
	require.NoError(t, err)
	res = runSaveDocumentTool(t, base, `{"attachment_key":`+quote(att2.Key)+`,"filename":"decisions.md"}`)
	require.True(t, res.OK, "error: %s", res.Error)
	require.Equal(t, "/knowledge/agent/decisions.md", res.Path)
}

// TestSaveDocumentTool_Rejections covers the readable rejections: every gate
// verdict and malformed input comes back as {ok:false, error} — the turn
// continues — and nothing lands in the corpus.
func TestSaveDocumentTool_Rejections(t *testing.T) {
	f := newKnowledgeToolFixture(t)
	base, err := newSaveDocumentTool(f.know, f.atts, "agent-1", "scout")
	require.NoError(t, err)

	// Seed a duplicate target.
	_, err = f.know.Save(t.Context(), knowledge.SaveInput{
		Filename: "taken.md", MIME: "text/plain", Data: []byte("x"),
		Scope: knowledge.ScopeAgent, AgentID: "agent-1", AgentSlug: "scout", Source: knowledge.SourceUser,
	})
	require.NoError(t, err)
	// Seed an unsupported upload (the attachments store takes video; the
	// knowledge gate does not).
	video, err := f.atts.Save(t.Context(), "clip.mp4", "video/mp4", []byte("bytes"))
	require.NoError(t, err)

	cases := []struct {
		name    string
		args    string
		wantSub string
	}{
		{name: "neither source", args: `{"filename":"notes.md"}`, wantSub: "exactly one content source"},
		{name: "both sources", args: `{"text":"t","attachment_key":"` + video.Key + `","filename":"notes.md"}`, wantSub: "not both"},
		{name: "inline text without filename", args: `{"text":"content"}`, wantSub: "filename is required"},
		{name: "duplicate filename", args: `{"text":"content","filename":"taken.md"}`, wantSub: "already holds a document of that name"},
		{name: "unsupported attachment kind", args: `{"attachment_key":` + quote(video.Key) + `}`, wantSub: "only plain text"},
		{name: "unknown attachment key", args: `{"attachment_key":"0123456789abcdef0123456789abcdef"}`, wantSub: "no attachment with key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := runSaveDocumentTool(t, base, tc.args)
			require.False(t, res.OK)
			require.Contains(t, res.Error, tc.wantSub)
			require.Empty(t, res.Path)
		})
	}

	t.Run("slugless agent rejects at the gate", func(t *testing.T) {
		slugless, err := newSaveDocumentTool(f.know, f.atts, "agent-1", "")
		require.NoError(t, err)
		res := runSaveDocumentTool(t, slugless, `{"text":"content","filename":"notes.md"}`)
		require.False(t, res.OK)
		require.Contains(t, res.Error, "agent-scoped documents require the agent's id and slug")
	})

	// The rejections above left exactly the seeded duplicate in the corpus.
	docs, err := f.know.List(t.Context())
	require.NoError(t, err)
	require.Len(t, docs, 1)
	require.Equal(t, "taken.md", docs[0].Filename)
}

// TestGate_SaveDocumentWithhold pins the scheduler-style withhold: without a
// knowledge store the entry never reaches the turn; with one wired it is
// offered workspace-free (a slugless agent's saves reject at call time).
func TestGate_SaveDocumentWithhold(t *testing.T) {
	ctx := context.Background()

	t.Run("without a knowledge store the tool is withheld", func(t *testing.T) {
		fs := fake.New()
		m := newManager(fs, sessions.NewService(fs), &fakeRegistry{}, WithBrowserDir(t.TempDir()))
		surface, err := m.resolveTurnTools(ctx, store.Agent{Name: "a"})
		require.NoError(t, err)
		names, err := surface.names(ctx)
		require.NoError(t, err)
		require.NotContains(t, names, ToolSaveDocument)
	})

	t.Run("with a knowledge store the tool is offered", func(t *testing.T) {
		fs := fake.New()
		m := newManager(fs, sessions.NewService(fs), &fakeRegistry{},
			WithKnowledge(t.TempDir()), WithBrowserDir(t.TempDir()))
		surface, err := m.resolveTurnTools(ctx, store.Agent{Name: "a"})
		require.NoError(t, err)
		names, err := surface.names(ctx)
		require.NoError(t, err)
		require.Contains(t, names, ToolSaveDocument)
	})
}

// TestGate_KnowledgeMountExposure pins the /knowledge mount's exposure rule
// (add-knowledge-corpus design D7): the roots ride the surface only when the
// agent is workspace-rootable and at least one filesystem tool is offered to
// read the corpus with.
func TestGate_KnowledgeMountExposure(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	t.Run("rootable agent with fs tools exposes both scopes", func(t *testing.T) {
		fs := fake.New()
		m := newManager(fs, sessions.NewService(fs), &fakeRegistry{},
			WithWorkspaces(workspaces.NewManager(t.TempDir())), WithKnowledge(root), WithBrowserDir(t.TempDir()))
		surface, err := m.resolveTurnTools(ctx, store.Agent{Name: "scout", Slug: "scout"})
		require.NoError(t, err)
		require.NotNil(t, surface.knowledge)
		require.Equal(t, filepath.Join(root, "global"), surface.knowledge.GlobalDir)
		require.Equal(t, filepath.Join(root, "agents", "scout"), surface.knowledge.AgentDir)
	})

	t.Run("slugless agent exposes none", func(t *testing.T) {
		fs := fake.New()
		m := newManager(fs, sessions.NewService(fs), &fakeRegistry{},
			WithWorkspaces(workspaces.NewManager(t.TempDir())), WithKnowledge(root), WithBrowserDir(t.TempDir()))
		surface, err := m.resolveTurnTools(ctx, store.Agent{Name: "legacy"})
		require.NoError(t, err)
		require.Nil(t, surface.knowledge)
	})

	t.Run("no workspaces source exposes none", func(t *testing.T) {
		fs := fake.New()
		m := newManager(fs, sessions.NewService(fs), &fakeRegistry{}, WithKnowledge(root), WithBrowserDir(t.TempDir()))
		surface, err := m.resolveTurnTools(ctx, store.Agent{Name: "a", Slug: "a"})
		require.NoError(t, err)
		require.Nil(t, surface.knowledge)
	})

	t.Run("no knowledge store exposes none", func(t *testing.T) {
		fs := fake.New()
		m := newManager(fs, sessions.NewService(fs), &fakeRegistry{},
			WithWorkspaces(workspaces.NewManager(t.TempDir())), WithBrowserDir(t.TempDir()))
		surface, err := m.resolveTurnTools(ctx, store.Agent{Name: "a", Slug: "a"})
		require.NoError(t, err)
		require.Nil(t, surface.knowledge)
	})

	t.Run("agent without filesystem tools exposes none", func(t *testing.T) {
		fs := fake.New()
		m := newManager(fs, sessions.NewService(fs), &fakeRegistry{},
			WithWorkspaces(workspaces.NewManager(t.TempDir())), WithKnowledge(root), WithBrowserDir(t.TempDir()))
		surface, err := m.resolveTurnTools(ctx, store.Agent{Name: "a", Slug: "a", Tools: []string{ToolCurrentTime}})
		require.NoError(t, err)
		require.Nil(t, surface.knowledge)
	})
}

// TestComposeKnowledge_MountGuidanceRidesSystemPrompt pins the fs middleware
// guidance (design D3): with the mount composed the model's system context
// names /knowledge and states it is read-only; without it the guidance is
// absent and the jail composes unchanged.
func TestComposeKnowledge_MountGuidanceRidesSystemPrompt(t *testing.T) {
	ctx := t.Context()
	agentDir := t.TempDir()
	globalDir := filepath.Join(t.TempDir(), "global")
	agentKnowDir := filepath.Join(t.TempDir(), "agents", "scout")
	require.NoError(t, os.MkdirAll(globalDir, 0o700))
	require.NoError(t, os.MkdirAll(agentKnowDir, 0o700))

	run := func(roots *backend.KnowledgeRoots) string {
		var sysText string
		mdl := &fakeModel{reply: func(input []*schema.Message) fakeReply {
			sysText = textOfMessages(input, schema.System)
			return fakeReply{text: "ok"}
		}}
		cfg := &composeConfig{
			Name:        "scout",
			Instruction: "behave",
			ChatModel:   mdl,
			Filesystem: &filesystemCapability{
				AgentDir:  agentDir,
				Knowledge: roots,
			},
		}
		agent, _, err := composeTurnAgent(ctx, cfg)
		require.NoError(t, err)
		runner := adk.NewTypedRunner(adk.TypedRunnerConfig[*schema.AgenticMessage]{Agent: agent, EnableStreaming: true})
		iter := runner.Run(ctx, []*schema.AgenticMessage{schema.UserAgenticMessage("hi")})
		require.NoError(t, drainRunnerIterator(iter))
		return sysText
	}

	with := run(&backend.KnowledgeRoots{GlobalDir: globalDir, AgentDir: agentKnowDir})
	require.Contains(t, with, "/knowledge")
	require.Contains(t, with, "/knowledge/global")
	require.Contains(t, with, "/knowledge/agent")
	require.Contains(t, with, "never writable")

	without := run(nil)
	require.NotContains(t, without, "/knowledge")
}

// TestTurnInput_KnowledgeGuidanceOnlyWhenExposed pins the composer threading
// (add-knowledge-corpus design D7): the guidance rides the composed
// instruction exactly when the turn's fs capability exposes the mount.
func TestTurnInput_KnowledgeGuidanceOnlyWhenExposed(t *testing.T) {
	stubClock(t, time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC))

	exposed, _, err := assembleTurnInput(persona{}, sessions.Checkpoint{}, nil,
		turnRequest{message: "hi"}, turnComposer{providerType: "gemini", model: "some-model", knowledge: true})
	require.NoError(t, err)
	require.Contains(t, exposed, "## Knowledge corpus")
	require.Contains(t, exposed, "knowledge-search")

	plain, _, err := assembleTurnInput(persona{}, sessions.Checkpoint{}, nil,
		turnRequest{message: "hi"}, turnComposer{providerType: "gemini", model: "some-model"})
	require.NoError(t, err)
	require.NotContains(t, plain, "## Knowledge corpus")
}
