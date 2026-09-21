package agents_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/promptdocs"
)

// fakeMemories is a minimal store.MemoryStore for composer tests: maps keyed
// by the composite identity, Get returning (nil, nil) when nothing is stored.
type fakeMemories struct {
	user      map[string]*domain.Memory // key: workspaceID + "|" + userID
	workspace map[string]*domain.Memory // key: workspaceID
}

func newFakeMemories() *fakeMemories {
	return &fakeMemories{
		user:      make(map[string]*domain.Memory),
		workspace: make(map[string]*domain.Memory),
	}
}

func (f *fakeMemories) UserMemory(_ context.Context, workspaceID, userID string) (*domain.Memory, error) {
	return f.user[workspaceID+"|"+userID], nil
}

func (f *fakeMemories) UpsertUserMemory(_ context.Context, workspaceID, userID, content string) error {
	f.user[workspaceID+"|"+userID] = &domain.Memory{Content: content, UpdatedAt: time.Now().UTC()}
	return nil
}

func (f *fakeMemories) AppendUserMemory(_ context.Context, workspaceID, userID, content string) error {
	key := workspaceID + "|" + userID
	mem, ok := f.user[key]
	if !ok {
		f.user[key] = &domain.Memory{Content: content, UpdatedAt: time.Now().UTC()}
		return nil
	}
	mem.Content += content
	return nil
}

func (f *fakeMemories) WorkspaceMemory(_ context.Context, workspaceID string) (*domain.Memory, error) {
	return f.workspace[workspaceID], nil
}

func (f *fakeMemories) UpsertWorkspaceMemory(_ context.Context, workspaceID, content string) error {
	f.workspace[workspaceID] = &domain.Memory{Content: content, UpdatedAt: time.Now().UTC()}
	return nil
}

func (f *fakeMemories) AppendWorkspaceMemory(_ context.Context, workspaceID, content string) error {
	mem, ok := f.workspace[workspaceID]
	if !ok {
		f.workspace[workspaceID] = &domain.Memory{Content: content, UpdatedAt: time.Now().UTC()}
		return nil
	}
	mem.Content += content
	return nil
}

func TestInstructionComposer_AllDocumentsPresent(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	_ = os.WriteFile(filepath.Join(tempDir, "IDENTITY.md"), []byte("# Identity: Assistant"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, "SOUL.md"), []byte("# Soul: Helpful"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, "BOOTSTRAP.md"), []byte("# Bootstrap: Welcome"), 0644)

	ws := &domain.Workspace{
		Name:        "Acme Corp",
		Description: "Engineering Team",
	}
	user := &domain.User{
		Name:  "Alice",
		Email: "alice@example.com",
	}

	composer := agents.NewInstructionComposer()
	result, err := composer.Compose(ctx, agents.ComposeParams{
		AgentDir:  tempDir,
		Workspace: ws,
		User:      user,
		RoleName:  "Admin",
		Memories:  newFakeMemories(),
	})
	if err != nil {
		t.Fatalf("unexpected compose error: %v", err)
	}

	// Verify order: base prompt, IDENTITY, SOUL, WORKSPACE, USER, BOOTSTRAP.
	// The base prompt is injected per build (markdown-card-elements D8), never
	// read from the agent dir.
	baseIdx := strings.Index(result, "# OnClaw Agent Base System Prompt")
	identityIdx := strings.Index(result, "# Identity: Assistant")
	soulIdx := strings.Index(result, "# Soul: Helpful")
	wsIdx := strings.Index(result, "# Workspace")
	userIdx := strings.Index(result, "# Current User")
	bootstrapIdx := strings.Index(result, "# Bootstrap: Welcome")

	if baseIdx < 0 || identityIdx < 0 || soulIdx < 0 || wsIdx < 0 || userIdx < 0 || bootstrapIdx < 0 {
		t.Fatalf("one or more expected documents missing in output:\n%s", result)
	}

	if !(baseIdx < identityIdx && identityIdx < soulIdx && soulIdx < wsIdx && wsIdx < userIdx && userIdx < bootstrapIdx) {
		t.Fatalf("documents not in expected order (base < identity < soul < ws < user < bootstrap):\n%s", result)
	}

	// Verify workspace content
	if !strings.Contains(result, "Acme Corp") || !strings.Contains(result, "Engineering Team") {
		t.Errorf("workspace details missing from output:\n%s", result)
	}

	// Verify user content
	if !strings.Contains(result, "Alice") || !strings.Contains(result, "alice@example.com") || !strings.Contains(result, "Admin") {
		t.Errorf("user details missing from output:\n%s", result)
	}
}

func TestInstructionComposer_UserVariesByCaller(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	ws := &domain.Workspace{Name: "Acme Corp"}

	composer := agents.NewInstructionComposer()

	user1 := &domain.User{Name: "Alice", Email: "alice@example.com"}
	res1, err := composer.Compose(ctx, agents.ComposeParams{
		AgentDir:  tempDir,
		Workspace: ws,
		User:      user1,
		RoleName:  "Owner",
		Memories:  newFakeMemories(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	user2 := &domain.User{Name: "Bob", Email: "bob@example.com"}
	res2, err := composer.Compose(ctx, agents.ComposeParams{
		AgentDir:  tempDir,
		Workspace: ws,
		User:      user2,
		RoleName:  "Member",
		Memories:  newFakeMemories(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(res1, "Alice") || !strings.Contains(res1, "Owner") {
		t.Errorf("expected res1 to contain Alice and Owner, got:\n%s", res1)
	}
	if strings.Contains(res1, "Bob") {
		t.Errorf("res1 should not contain Bob")
	}

	if !strings.Contains(res2, "Bob") || !strings.Contains(res2, "Member") {
		t.Errorf("expected res2 to contain Bob and Member, got:\n%s", res2)
	}
	if strings.Contains(res2, "Alice") {
		t.Errorf("res2 should not contain Alice")
	}
}

func TestInstructionComposer_MissingDocumentsTolerated(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	// A stale seeded base prompt left on disk (e.g. by an old seed) must never
	// be read — the embedded base prompt supersedes it
	// (markdown-card-elements D8). The generated documents are absent (e.g.
	// failed prompt generation where IDENTITY, SOUL, BOOTSTRAP never landed).
	_ = os.WriteFile(filepath.Join(tempDir, "AGENTS.md"), []byte("# STALE-SEEDED-BASE-PROMPT"), 0644)

	ws := &domain.Workspace{Name: "My Workspace"}
	user := &domain.User{Name: "Charlie", Email: "charlie@example.com"}

	composer := agents.NewInstructionComposer()
	result, err := composer.Compose(ctx, agents.ComposeParams{
		AgentDir:  tempDir,
		Workspace: ws,
		User:      user,
		RoleName:  "Viewer",
		Memories:  newFakeMemories(),
	})
	if err != nil {
		t.Fatalf("unexpected error when prompt files are missing: %v", err)
	}

	if !strings.Contains(result, promptdocs.BasePrompt) {
		t.Errorf("expected the embedded base prompt to be injected")
	}
	if strings.Contains(result, "STALE-SEEDED-BASE-PROMPT") {
		t.Errorf("the on-disk AGENTS.md must never be read; the injected base prompt supersedes it:\n%s", result)
	}
	if !strings.Contains(result, "# Workspace") || !strings.Contains(result, "My Workspace") {
		t.Errorf("expected WORKSPACE.md virtual content to be present")
	}
	if !strings.Contains(result, "# Current User") || !strings.Contains(result, "Charlie") {
		t.Errorf("expected USER.md virtual content to be present")
	}
	if strings.Contains(result, "# Identity: Oracle") || strings.Contains(result, "# Bootstrap: Welcome") {
		t.Errorf("expected absent generated documents to not appear in result")
	}
}

func TestInstructionComposer_MemorySubsectionsCarryStoredContent(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	memories := newFakeMemories()
	if err := memories.UpsertWorkspaceMemory(ctx, "ws-1", "Ship on Thursdays."); err != nil {
		t.Fatalf("seed workspace memory: %v", err)
	}
	if err := memories.UpsertUserMemory(ctx, "ws-1", "user-1", "Prefers concise answers."); err != nil {
		t.Fatalf("seed user memory: %v", err)
	}

	composer := agents.NewInstructionComposer()
	result, err := composer.Compose(ctx, agents.ComposeParams{
		AgentDir:  tempDir,
		Workspace: &domain.Workspace{ID: "ws-1", Name: "Acme"},
		User:      &domain.User{ID: "user-1", Name: "Alice"},
		RoleName:  "Member",
		Memories:  memories,
	})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}

	wsIdx := strings.Index(result, "# Workspace")
	sharedIdx := strings.Index(result, "## Shared memory")
	sharedContentIdx := strings.Index(result, "Ship on Thursdays.")
	userIdx := strings.Index(result, "# Current User")
	// The embedded base prompt carries its own "## Memory" section
	// (markdown-card-elements D8), so the user doc's subsection is located
	// after the User doc's opening heading.
	userMemIdx := strings.Index(result[userIdx:], "## Memory") + userIdx
	userContentIdx := strings.Index(result[userIdx:], "Prefers concise answers.") + userIdx

	for name, idx := range map[string]int{
		"# Workspace": wsIdx, "## Shared memory": sharedIdx, "shared content": sharedContentIdx,
		"# Current User": userIdx, "## Memory": userMemIdx, "user memory content": userContentIdx,
	} {
		if idx < 0 {
			t.Fatalf("expected %s in output:\n%s", name, result)
		}
	}

	// The shared memory subsection lives inside the Workspace doc; the user
	// memory subsection inside the User doc.
	if !(wsIdx < sharedIdx && sharedIdx < sharedContentIdx && sharedContentIdx < userIdx) {
		t.Errorf("shared memory must render inside the Workspace doc:\n%s", result)
	}
	if !(userIdx < userMemIdx && userMemIdx < userContentIdx) {
		t.Errorf("user memory must render inside the User doc:\n%s", result)
	}
}

func TestInstructionComposer_EmptyMemoryOmitsSubsections(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	composer := agents.NewInstructionComposer()
	result, err := composer.Compose(ctx, agents.ComposeParams{
		AgentDir:  tempDir,
		Workspace: &domain.Workspace{ID: "ws-1", Name: "Acme"},
		User:      &domain.User{ID: "user-1", Name: "Alice"},
		RoleName:  "Member",
		Memories:  newFakeMemories(),
	})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}

	// The shared-memory subsection heading must be absent entirely; the user
	// doc must not grow a memory subsection (the embedded base prompt has its
	// own "## Memory" section, markdown-card-elements D8, so the check is
	// scoped to the User doc).
	if strings.Contains(result, "## Shared memory") {
		t.Errorf("empty workspace memory must omit its subsection entirely:\n%s", result)
	}
	if userIdx := strings.Index(result, "# Current User"); userIdx >= 0 && strings.Contains(result[userIdx:], "## Memory") {
		t.Errorf("empty user memory must omit its subsection entirely:\n%s", result)
	}
}

func TestInstructionComposer_MemoryFreshPerExecution(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	memories := newFakeMemories()
	params := agents.ComposeParams{
		AgentDir:  tempDir,
		Workspace: &domain.Workspace{ID: "ws-1", Name: "Acme"},
		User:      &domain.User{ID: "user-1", Name: "Alice"},
		RoleName:  "Member",
		Memories:  memories,
	}

	composer := agents.NewInstructionComposer()
	first, err := composer.Compose(ctx, params)
	if err != nil {
		t.Fatalf("first compose: %v", err)
	}
	if strings.Contains(first, "First-turn note.") {
		t.Fatalf("nothing appended yet, first turn must not carry it:\n%s", first)
	}

	// The agent appends to WORKSPACE.md and USER.md during the first turn;
	// the second execution's instruction must already carry the appended text.
	if err := memories.AppendWorkspaceMemory(ctx, "ws-1", "First-turn note."); err != nil {
		t.Fatalf("append workspace memory: %v", err)
	}
	if err := memories.AppendUserMemory(ctx, "ws-1", "user-1", "Second-turn note."); err != nil {
		t.Fatalf("append user memory: %v", err)
	}

	second, err := composer.Compose(ctx, params)
	if err != nil {
		t.Fatalf("second compose: %v", err)
	}
	if !strings.Contains(second, "First-turn note.") {
		t.Errorf("second turn must see the first turn's workspace append:\n%s", second)
	}
	if !strings.Contains(second, "Second-turn note.") {
		t.Errorf("second turn must see the first turn's user append:\n%s", second)
	}
}

// TestInstructionComposer_InjectsEmbeddedBasePrompt pins the per-build L1
// injection (markdown-card-elements D8/D7): the embedded base prompt opens
// every attended composition verbatim even when the agent dir carries no
// AGENTS.md at all, and it carries the rich-cards fence conventions.
func TestInstructionComposer_InjectsEmbeddedBasePrompt(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir() // no AGENTS.md on disk

	composer := agents.NewInstructionComposer()
	result, err := composer.Compose(ctx, agents.ComposeParams{
		AgentDir:  tempDir,
		Workspace: &domain.Workspace{ID: "ws-1", Name: "Acme"},
		User:      &domain.User{ID: "user-1", Name: "Alice"},
		RoleName:  "Member",
		Memories:  newFakeMemories(),
	})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}

	if !strings.HasPrefix(result, promptdocs.BasePrompt) {
		t.Errorf("the embedded base prompt must open the composed instruction verbatim, got:\n%s", result)
	}
	if !strings.Contains(result, "## Rich cards") {
		t.Errorf("the injected base prompt must carry the rich-cards section:\n%s", result)
	}
}

// TestInstructionComposer_SchedulerAndHeartbeatProfilesInjectBasePrompt pins
// the same injection for both unattended profiles: the trimmed stacks carry
// the embedded base prompt (and its rich-cards section) even with an empty
// agent dir.
func TestInstructionComposer_SchedulerAndHeartbeatProfilesInjectBasePrompt(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir() // no AGENTS.md on disk

	composer := agents.NewInstructionComposer()

	scheduler, err := composer.Compose(ctx, agents.ComposeParams{
		AgentDir:         tempDir,
		Workspace:        &domain.Workspace{ID: "ws-1", Name: "Acme"},
		Memories:         newFakeMemories(),
		SchedulerProfile: true,
	})
	if err != nil {
		t.Fatalf("compose (scheduler): %v", err)
	}
	if !strings.Contains(scheduler, promptdocs.BasePrompt) || !strings.Contains(scheduler, "## Rich cards") {
		t.Errorf("scheduler profile must carry the embedded base prompt and the rich-cards section:\n%s", scheduler)
	}

	heartbeat, err := composer.Compose(ctx, agents.ComposeParams{
		AgentDir:         tempDir,
		Workspace:        &domain.Workspace{ID: "ws-1", Name: "Acme"},
		Memories:         newFakeMemories(),
		HeartbeatProfile: true,
	})
	if err != nil {
		t.Fatalf("compose (heartbeat): %v", err)
	}
	if !strings.Contains(heartbeat, promptdocs.BasePrompt) || !strings.Contains(heartbeat, "## Rich cards") {
		t.Errorf("heartbeat profile must carry the embedded base prompt and the rich-cards section:\n%s", heartbeat)
	}
}
