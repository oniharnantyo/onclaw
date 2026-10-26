package backend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	einofs "github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/components/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedKnowledgeTree creates a host layout that mirrors production: a parent
// directory (which holds other agents' scopes and must never be exposed)
// containing the global and agent knowledge dirs, plus a workspace dir.
// Returns the workspace, knowledge parent, and both scope directories.
func seedKnowledgeTree(t *testing.T) (primary, parent, globalDir, agentDir string) {
	t.Helper()
	primary = t.TempDir()
	parent = t.TempDir()
	globalDir = filepath.Join(parent, "global")
	agentDir = filepath.Join(parent, "agent")
	require.NoError(t, os.MkdirAll(globalDir, 0o755))
	require.NoError(t, os.MkdirAll(agentDir, 0o755))

	// A sibling scope in the parent: the mount must never reach it.
	require.NoError(t, os.WriteFile(filepath.Join(parent, "other-agent-secret.txt"), []byte("host secret"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(globalDir, "overview.md"), []byte("global corpus overview"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(globalDir, "guide.pdf"), []byte("%PDF-1.4 dummy corpus pdf"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(agentDir, "notes.md"), []byte("agent scoped notes"), 0o644))
	return primary, parent, globalDir, agentDir
}

func mustKnowledgeJail(t *testing.T, primary string, knowledge KnowledgeRoots) einofs.Backend {
	t.Helper()
	jail, err := NewFilesystemJailedWithKnowledge(primary, knowledge)
	require.NoError(t, err)
	return jail
}

func TestKnowledgeMount_ReadsBothScopes(t *testing.T) {
	ctx := context.Background()
	primary, _, globalDir, agentDir := seedKnowledgeTree(t)
	jail := mustKnowledgeJail(t, primary, KnowledgeRoots{GlobalDir: globalDir, AgentDir: agentDir})

	t.Run("read global scope file", func(t *testing.T) {
		content, err := jail.Read(ctx, &einofs.ReadRequest{FilePath: KnowledgeMountPoint + "/global/overview.md"})
		require.NoError(t, err)
		assert.Equal(t, "global corpus overview", content.Content)
	})

	t.Run("read agent scope file", func(t *testing.T) {
		content, err := jail.Read(ctx, &einofs.ReadRequest{FilePath: KnowledgeMountPoint + "/agent/notes.md"})
		require.NoError(t, err)
		assert.Equal(t, "agent scoped notes", content.Content)
	})

	t.Run("scopes stay separate", func(t *testing.T) {
		_, err := jail.Read(ctx, &einofs.ReadRequest{FilePath: KnowledgeMountPoint + "/global/notes.md"})
		require.Error(t, err, "agent file must not be visible under /knowledge/global")
	})

	t.Run("list a scope directory", func(t *testing.T) {
		infos, err := jail.LsInfo(ctx, &einofs.LsInfoRequest{Path: KnowledgeMountPoint + "/global"})
		require.NoError(t, err)
		names := make(map[string]bool, len(infos))
		for _, info := range infos {
			names[info.Path] = true
		}
		assert.True(t, names["overview.md"], "expected overview.md in listing, got %v", names)
		assert.True(t, names["guide.pdf"], "expected guide.pdf in listing, got %v", names)
		assert.False(t, names["notes.md"], "agent file must not be listed under /knowledge/global")
	})
}

func TestKnowledgeMount_VirtualRootListing(t *testing.T) {
	ctx := context.Background()

	t.Run("both roots present", func(t *testing.T) {
		primary, _, globalDir, agentDir := seedKnowledgeTree(t)
		jail := mustKnowledgeJail(t, primary, KnowledgeRoots{GlobalDir: globalDir, AgentDir: agentDir})

		infos, err := jail.LsInfo(ctx, &einofs.LsInfoRequest{Path: KnowledgeMountPoint})
		require.NoError(t, err)
		require.Len(t, infos, 2, "listing must show exactly the existing scopes")
		assert.Equal(t, "global", infos[0].Path)
		assert.True(t, infos[0].IsDir)
		assert.Equal(t, "agent", infos[1].Path)
		assert.True(t, infos[1].IsDir)
		for _, info := range infos {
			assert.NotContains(t, info.Path, string(filepath.Separator),
				"virtual root entries must be scope names, not host paths")
		}
	})

	t.Run("missing global leaves only agent", func(t *testing.T) {
		primary, parent, _, agentDir := seedKnowledgeTree(t)
		jail := mustKnowledgeJail(t, primary, KnowledgeRoots{
			GlobalDir: filepath.Join(parent, "does-not-exist"),
			AgentDir:  agentDir,
		})

		infos, err := jail.LsInfo(ctx, &einofs.LsInfoRequest{Path: KnowledgeMountPoint})
		require.NoError(t, err)
		require.Len(t, infos, 1)
		assert.Equal(t, "agent", infos[0].Path)
	})

	t.Run("both roots missing lists nothing", func(t *testing.T) {
		primary, parent, _, _ := seedKnowledgeTree(t)
		jail := mustKnowledgeJail(t, primary, KnowledgeRoots{
			GlobalDir: filepath.Join(parent, "missing-global"),
			AgentDir:  filepath.Join(parent, "missing-agent"),
		})

		infos, err := jail.LsInfo(ctx, &einofs.LsInfoRequest{Path: KnowledgeMountPoint})
		require.NoError(t, err)
		assert.Empty(t, infos)
	})

	t.Run("host parent entries never leak into the listing", func(t *testing.T) {
		primary, _, globalDir, agentDir := seedKnowledgeTree(t)
		jail := mustKnowledgeJail(t, primary, KnowledgeRoots{GlobalDir: globalDir, AgentDir: agentDir})

		infos, err := jail.LsInfo(ctx, &einofs.LsInfoRequest{Path: KnowledgeMountPoint})
		require.NoError(t, err)
		for _, info := range infos {
			assert.NotEqual(t, "other-agent-secret.txt", info.Path,
				"the host parent of the knowledge dirs must never be exposed")
		}
	})
}

func TestKnowledgeMount_GrepAndGlobExplicitPath(t *testing.T) {
	ctx := context.Background()
	primary, _, globalDir, agentDir := seedKnowledgeTree(t)
	jail := mustKnowledgeJail(t, primary, KnowledgeRoots{GlobalDir: globalDir, AgentDir: agentDir})

	t.Run("grep under global scope", func(t *testing.T) {
		matches, err := jail.GrepRaw(ctx, &einofs.GrepRequest{
			Pattern: "corpus overview",
			Path:    KnowledgeMountPoint + "/global",
		})
		require.NoError(t, err)
		require.Len(t, matches, 1)
		assert.Equal(t, KnowledgeMountPoint+"/global/overview.md", matches[0].Path)
	})

	t.Run("grep under agent scope", func(t *testing.T) {
		matches, err := jail.GrepRaw(ctx, &einofs.GrepRequest{
			Pattern: "scoped notes",
			Path:    KnowledgeMountPoint + "/agent",
		})
		require.NoError(t, err)
		require.Len(t, matches, 1)
		assert.Equal(t, KnowledgeMountPoint+"/agent/notes.md", matches[0].Path)
	})

	t.Run("grep does not cross scopes", func(t *testing.T) {
		matches, err := jail.GrepRaw(ctx, &einofs.GrepRequest{
			Pattern: "scoped notes",
			Path:    KnowledgeMountPoint + "/global",
		})
		require.NoError(t, err)
		assert.Empty(t, matches)
	})

	t.Run("glob under global scope", func(t *testing.T) {
		infos, err := jail.GlobInfo(ctx, &einofs.GlobInfoRequest{
			Pattern: "*.pdf",
			Path:    KnowledgeMountPoint + "/global",
		})
		require.NoError(t, err)
		require.Len(t, infos, 1)
		assert.Equal(t, KnowledgeMountPoint+"/global/guide.pdf", infos[0].Path)
	})

	t.Run("glob under agent scope", func(t *testing.T) {
		infos, err := jail.GlobInfo(ctx, &einofs.GlobInfoRequest{
			Pattern: "*.md",
			Path:    KnowledgeMountPoint + "/agent",
		})
		require.NoError(t, err)
		require.Len(t, infos, 1)
		assert.Equal(t, KnowledgeMountPoint+"/agent/notes.md", infos[0].Path)
	})
}

func TestKnowledgeMount_WritesRejected(t *testing.T) {
	ctx := context.Background()
	primary, _, globalDir, agentDir := seedKnowledgeTree(t)
	jail := mustKnowledgeJail(t, primary, KnowledgeRoots{GlobalDir: globalDir, AgentDir: agentDir})

	t.Run("write under global scope rejected", func(t *testing.T) {
		err := jail.Write(ctx, &einofs.WriteRequest{
			FilePath: KnowledgeMountPoint + "/global/evil.md",
			Content:  "planted",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "absolute paths are not allowed")
		_, statErr := os.Stat(filepath.Join(globalDir, "evil.md"))
		assert.True(t, os.IsNotExist(statErr), "write must not create files in the corpus")
	})

	t.Run("write under agent scope rejected", func(t *testing.T) {
		err := jail.Write(ctx, &einofs.WriteRequest{
			FilePath: KnowledgeMountPoint + "/agent/evil.md",
			Content:  "planted",
		})
		require.Error(t, err)
		_, statErr := os.Stat(filepath.Join(agentDir, "evil.md"))
		assert.True(t, os.IsNotExist(statErr), "write must not create files in the corpus")
	})

	t.Run("write to the mount root rejected", func(t *testing.T) {
		err := jail.Write(ctx, &einofs.WriteRequest{
			FilePath: KnowledgeMountPoint,
			Content:  "planted",
		})
		require.Error(t, err)
	})

	t.Run("edit corpus file rejected and unchanged on disk", func(t *testing.T) {
		err := jail.Edit(ctx, &einofs.EditRequest{
			FilePath:  KnowledgeMountPoint + "/global/overview.md",
			OldString: "overview",
			NewString: "tampered",
		})
		require.Error(t, err)
		data, readErr := os.ReadFile(filepath.Join(globalDir, "overview.md"))
		require.NoError(t, readErr)
		assert.Equal(t, "global corpus overview", string(data), "corpus file must be unmodified")
	})

	t.Run("delete_file under the mount rejected", func(t *testing.T) {
		base, err := NewDeleteFile(primary)
		require.NoError(t, err)
		tool, ok := base.(tool.InvokableTool)
		require.True(t, ok, "delete_file must be invokable")
		_, err = tool.InvokableRun(ctx, `{"path":"`+KnowledgeMountPoint+`/global/overview.md"}`)
		require.Error(t, err, "delete_file must reject knowledge-mount paths")
		data, readErr := os.ReadFile(filepath.Join(globalDir, "overview.md"))
		require.NoError(t, readErr)
		assert.Equal(t, "global corpus overview", string(data), "corpus file must survive")
	})

	t.Run("workspace stays writable", func(t *testing.T) {
		err := jail.Write(ctx, &einofs.WriteRequest{FilePath: DefaultMountPoint + "/ok.txt", Content: "x"})
		require.NoError(t, err)
	})
}

func TestKnowledgeMount_TraversalRejected(t *testing.T) {
	ctx := context.Background()
	primary, _, globalDir, agentDir := seedKnowledgeTree(t)
	jail := mustKnowledgeJail(t, primary, KnowledgeRoots{GlobalDir: globalDir, AgentDir: agentDir})

	escapeAttempts := []struct {
		path         string
		containsDots bool
	}{
		{KnowledgeMountPoint + "/global/../../etc/passwd", true},
		{KnowledgeMountPoint + "/agent/../../../etc/passwd", true},
		{KnowledgeMountPoint + "/global/sub/../../../etc/passwd", true},
		{KnowledgeMountPoint + "/../workspace/notes.md", true},
		{KnowledgeMountPoint + "/../..", true},
		{KnowledgeMountPoint + "/global/..%2fetc%2fpasswd", true},
		{KnowledgeMountPoint + "/global/%2e%2e/%2e%2e/etc/passwd", false},
		{KnowledgeMountPoint + "/../../tmp/buddies-escape", true},
	}

	for _, attempt := range escapeAttempts {
		_, err := jail.Read(ctx, &einofs.ReadRequest{FilePath: attempt.path})
		require.Error(t, err, "read %q must be rejected", attempt.path)
		if attempt.containsDots {
			assert.Contains(t, err.Error(), "..", "expected .. rejection for %q, got: %v", attempt.path, err)
		}
	}

	t.Run("write-side traversal rejected", func(t *testing.T) {
		err := jail.Write(ctx, &einofs.WriteRequest{
			FilePath: KnowledgeMountPoint + "/../../tmp/buddies-evil.md",
			Content:  "x",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "..")
	})

	t.Run("host parent secret unreachable", func(t *testing.T) {
		// The parent of the knowledge dirs holds other agents' scopes; no
		// spelling may reach its contents through the mount.
		_, err := jail.Read(ctx, &einofs.ReadRequest{FilePath: KnowledgeMountPoint + "/global/../other-agent-secret.txt"})
		require.Error(t, err)
		_, err = jail.Read(ctx, &einofs.ReadRequest{FilePath: KnowledgeMountPoint + "/other-agent-secret.txt"})
		require.Error(t, err)
	})
}

func TestKnowledgeMount_MissingAgentDirTolerated(t *testing.T) {
	ctx := context.Background()
	primary, parent, globalDir, _ := seedKnowledgeTree(t)
	jail := mustKnowledgeJail(t, primary, KnowledgeRoots{
		GlobalDir: globalDir,
		AgentDir:  filepath.Join(parent, "missing-agent"),
	})

	t.Run("reads under the absent scope are not-found", func(t *testing.T) {
		_, err := jail.Read(ctx, &einofs.ReadRequest{FilePath: KnowledgeMountPoint + "/agent/notes.md"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("listing omits the absent scope", func(t *testing.T) {
		infos, err := jail.LsInfo(ctx, &einofs.LsInfoRequest{Path: KnowledgeMountPoint})
		require.NoError(t, err)
		require.Len(t, infos, 1)
		assert.Equal(t, "global", infos[0].Path)
	})

	t.Run("scope directory itself is not found", func(t *testing.T) {
		_, err := jail.LsInfo(ctx, &einofs.LsInfoRequest{Path: KnowledgeMountPoint + "/agent"})
		require.Error(t, err)
	})

	t.Run("global scope unaffected", func(t *testing.T) {
		content, err := jail.Read(ctx, &einofs.ReadRequest{FilePath: KnowledgeMountPoint + "/global/overview.md"})
		require.NoError(t, err)
		assert.Equal(t, "global corpus overview", content.Content)
	})

	t.Run("primary root still writable", func(t *testing.T) {
		err := jail.Write(ctx, &einofs.WriteRequest{FilePath: "ok.txt", Content: "x"})
		require.NoError(t, err)
	})
}

func TestKnowledgeMount_MultiModalPDF(t *testing.T) {
	ctx := context.Background()
	primary, _, globalDir, agentDir := seedKnowledgeTree(t)
	jail := mustKnowledgeJail(t, primary, KnowledgeRoots{GlobalDir: globalDir, AgentDir: agentDir})
	mmReader, ok := jail.(einofs.MultiModalReader)
	require.True(t, ok, "jail backend must implement einofs.MultiModalReader")

	t.Run("corpus PDF renders as parts", func(t *testing.T) {
		res, err := mmReader.MultiModalRead(ctx, &einofs.MultiModalReadRequest{
			ReadRequest: einofs.ReadRequest{FilePath: KnowledgeMountPoint + "/global/guide.pdf"},
		})
		require.NoError(t, err)
		require.NotEmpty(t, res.Parts, "corpus PDF must render as a structured part")
		assert.Equal(t, einofs.FileContentPartTypePDF, res.Parts[0].Type)
		assert.Equal(t, "application/pdf", res.Parts[0].MIMEType)
		assert.NotEmpty(t, res.Parts[0].Data)
		assert.True(t, strings.HasPrefix(string(res.Parts[0].Data), "%PDF-1.4"))
	})

	t.Run("corpus text reads as text", func(t *testing.T) {
		res, err := mmReader.MultiModalRead(ctx, &einofs.MultiModalReadRequest{
			ReadRequest: einofs.ReadRequest{FilePath: KnowledgeMountPoint + "/agent/notes.md"},
		})
		require.NoError(t, err)
		require.NotNil(t, res.FileContent)
		assert.Equal(t, "agent scoped notes", res.FileContent.Content)
	})
}

func TestKnowledgeMount_SymlinkEscapeRejected(t *testing.T) {
	ctx := context.Background()
	primary, _, globalDir, agentDir := seedKnowledgeTree(t)
	jail := mustKnowledgeJail(t, primary, KnowledgeRoots{GlobalDir: globalDir, AgentDir: agentDir})

	outsideFile := filepath.Join(t.TempDir(), "secret.txt")
	require.NoError(t, os.WriteFile(outsideFile, []byte("secret"), 0o644))
	escapeLink := filepath.Join(globalDir, "escape_link.md")
	require.NoError(t, os.Symlink(outsideFile, escapeLink))

	t.Run("read through escape symlink rejected", func(t *testing.T) {
		_, err := jail.Read(ctx, &einofs.ReadRequest{FilePath: KnowledgeMountPoint + "/global/escape_link.md"})
		require.Error(t, err)
		data, readErr := os.ReadFile(outsideFile)
		require.NoError(t, readErr)
		assert.Equal(t, "secret", string(data))
	})

	t.Run("glob filters the escape symlink", func(t *testing.T) {
		infos, err := jail.GlobInfo(ctx, &einofs.GlobInfoRequest{
			Pattern: "*.md",
			Path:    KnowledgeMountPoint + "/global",
		})
		require.NoError(t, err)
		for _, info := range infos {
			assert.NotContains(t, info.Path, "escape_link", "glob must not return symlinks escaping the corpus")
		}
	})

	t.Run("agent scope symlink escape rejected", func(t *testing.T) {
		link := filepath.Join(agentDir, "escape.md")
		require.NoError(t, os.Symlink(outsideFile, link))
		_, err := jail.Read(ctx, &einofs.ReadRequest{FilePath: KnowledgeMountPoint + "/agent/escape.md"})
		require.Error(t, err)
	})
}

func TestKnowledgeMount_BareRootOperationsFail(t *testing.T) {
	ctx := context.Background()
	primary, _, globalDir, agentDir := seedKnowledgeTree(t)
	jail := mustKnowledgeJail(t, primary, KnowledgeRoots{GlobalDir: globalDir, AgentDir: agentDir})
	mmReader := jail.(einofs.MultiModalReader)

	t.Run("read of the virtual root fails", func(t *testing.T) {
		_, err := jail.Read(ctx, &einofs.ReadRequest{FilePath: KnowledgeMountPoint})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "virtual root")
	})

	t.Run("grep of the virtual root fails", func(t *testing.T) {
		_, err := jail.GrepRaw(ctx, &einofs.GrepRequest{Pattern: "x", Path: KnowledgeMountPoint})
		require.Error(t, err)
	})

	t.Run("glob of the virtual root fails", func(t *testing.T) {
		_, err := jail.GlobInfo(ctx, &einofs.GlobInfoRequest{Pattern: "*", Path: KnowledgeMountPoint})
		require.Error(t, err)
	})

	t.Run("multimodal read of the virtual root fails", func(t *testing.T) {
		_, err := mmReader.MultiModalRead(ctx, &einofs.MultiModalReadRequest{
			ReadRequest: einofs.ReadRequest{FilePath: KnowledgeMountPoint},
		})
		require.Error(t, err)
	})

	t.Run("unknown scope fails with guidance", func(t *testing.T) {
		_, err := jail.Read(ctx, &einofs.ReadRequest{FilePath: KnowledgeMountPoint + "/sneaky/file.txt"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown knowledge scope")
	})
}

func TestKnowledgeMount_EmptyPathSearchStaysInWorkspace(t *testing.T) {
	ctx := context.Background()
	primary, _, globalDir, agentDir := seedKnowledgeTree(t)
	require.NoError(t, os.WriteFile(filepath.Join(primary, "local.txt"), []byte("needle here"), 0o644))
	jail := mustKnowledgeJail(t, primary, KnowledgeRoots{GlobalDir: globalDir, AgentDir: agentDir})

	t.Run("grep without Path searches the workspace only", func(t *testing.T) {
		matches, err := jail.GrepRaw(ctx, &einofs.GrepRequest{Pattern: "needle"})
		require.NoError(t, err)
		require.Len(t, matches, 1, "knowledge roots must not be searched implicitly")
		assert.Equal(t, "local.txt", matches[0].Path)
	})

	t.Run("glob without Path searches the workspace only", func(t *testing.T) {
		infos, err := jail.GlobInfo(ctx, &einofs.GlobInfoRequest{Pattern: "*.md"})
		require.NoError(t, err)
		for _, info := range infos {
			assert.NotContains(t, info.Path, "overview", "knowledge roots must not be globbed implicitly")
		}
	})
}

func TestNewFilesystemJailedWithKnowledge_Construction(t *testing.T) {
	t.Run("file as knowledge dir is skipped", func(t *testing.T) {
		primary, _, _, agentDir := seedKnowledgeTree(t)
		notADir := filepath.Join(t.TempDir(), "plain.txt")
		require.NoError(t, os.WriteFile(notADir, []byte("x"), 0o644))

		jail := mustKnowledgeJail(t, primary, KnowledgeRoots{GlobalDir: notADir, AgentDir: agentDir})

		infos, err := jail.LsInfo(context.Background(), &einofs.LsInfoRequest{Path: KnowledgeMountPoint})
		require.NoError(t, err)
		require.Len(t, infos, 1)
		assert.Equal(t, "agent", infos[0].Path)
	})

	t.Run("empty knowledge roots construct fine", func(t *testing.T) {
		primary := t.TempDir()
		jail := mustKnowledgeJail(t, primary, KnowledgeRoots{})

		infos, err := jail.LsInfo(context.Background(), &einofs.LsInfoRequest{Path: KnowledgeMountPoint})
		require.NoError(t, err)
		assert.Empty(t, infos)

		_, err = jail.Read(context.Background(), &einofs.ReadRequest{FilePath: KnowledgeMountPoint + "/global/x.md"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown knowledge scope")
	})

	t.Run("missing primary root still fails", func(t *testing.T) {
		_, err := NewFilesystemJailedWithKnowledge("/nonexistent/primary/root", KnowledgeRoots{})
		require.Error(t, err)
	})

	t.Run("symlinked knowledge dir is normalized", func(t *testing.T) {
		primary, _, globalDir, agentDir := seedKnowledgeTree(t)
		link := filepath.Join(t.TempDir(), "global-link")
		require.NoError(t, os.Symlink(globalDir, link))

		jail := mustKnowledgeJail(t, primary, KnowledgeRoots{GlobalDir: link, AgentDir: agentDir})
		content, err := jail.Read(context.Background(), &einofs.ReadRequest{FilePath: KnowledgeMountPoint + "/global/overview.md"})
		require.NoError(t, err)
		assert.Equal(t, "global corpus overview", content.Content)
	})
}
