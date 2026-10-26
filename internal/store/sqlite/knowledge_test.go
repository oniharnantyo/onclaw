package sqlite

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/store"
)

// seedDocumentAgent creates one agent to hold agent-scoped documents (the
// documents FK targets agents.id).
func seedDocumentAgent(t *testing.T, ctx context.Context, s *Store, name, slug string) *store.Agent {
	t.Helper()
	agent := &store.Agent{Name: name, Slug: slug}
	require.NoError(t, s.Agents().Create(ctx, agent))
	return agent
}

// TestDocuments_RoundTrip covers the catalog contract: put assigns the id and
// stamps the creation time, get/list/by-name resolve the row, and the update
// paths rewrite scope and description.
func TestDocuments_RoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	docs := s.Documents()

	agent := seedDocumentAgent(t, ctx, s, "scout", "scout")

	put, err := docs.PutDocument(ctx, store.Document{
		Filename: "notes.txt", MIME: "text/plain", Size: 5,
		Scope: "agent", AgentID: agent.ID, AgentSlug: "scout",
		Description: "meeting notes", Source: "user",
	})
	require.NoError(t, err)
	require.Regexp(t, `^[0-9a-f]{32}$`, put.ID)
	require.False(t, put.CreatedAt.IsZero())
	require.Equal(t, agent.ID, put.AgentID)

	got, err := docs.GetDocument(ctx, put.ID)
	require.NoError(t, err)
	require.Equal(t, put, got)

	byName, found, err := docs.DocumentByName(ctx, "agent", "scout", "notes.txt")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, put, byName)

	_, found, err = docs.DocumentByName(ctx, "agent", "other", "notes.txt")
	require.NoError(t, err)
	require.False(t, found)

	require.NoError(t, docs.UpdateDocumentScope(ctx, put.ID, "global", "", ""))
	got, err = docs.GetDocument(ctx, put.ID)
	require.NoError(t, err)
	require.Equal(t, "global", got.Scope)
	require.Empty(t, got.AgentID, "a global row carries no agent linkage")
	require.Empty(t, got.AgentSlug)

	require.NoError(t, docs.UpdateDocumentDescription(ctx, put.ID, "promoted notes"))
	got, err = docs.GetDocument(ctx, put.ID)
	require.NoError(t, err)
	require.Equal(t, "promoted notes", got.Description)

	list, err := docs.ListDocuments(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "notes.txt", list[0].Filename)

	require.NoError(t, docs.DeleteDocument(ctx, put.ID))
	_, err = docs.GetDocument(ctx, put.ID)
	require.ErrorIs(t, err, store.ErrNotFound)

	_, err = docs.GetDocument(ctx, "ffffffffffffffffffffffffffffffff")
	require.ErrorIs(t, err, store.ErrNotFound)

	err = docs.DeleteDocument(ctx, put.ID)
	require.ErrorIs(t, err, store.ErrNotFound, "deleting an already-deleted row reports not found")
}

// TestDocuments_UniquePerScope covers migration 000014's composite UNIQUE:
// (scope, agent_slug, filename) rejects a duplicate within one scope, while
// the same filename may live in two agents and in global alongside an agent.
func TestDocuments_UniquePerScope(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	docs := s.Documents()

	scout := seedDocumentAgent(t, ctx, s, "scout", "scout")
	mapper := seedDocumentAgent(t, ctx, s, "mapper", "mapper")

	_, err := docs.PutDocument(ctx, store.Document{
		Filename: "runbook.md", MIME: "text/plain", Size: 1, Scope: "agent",
		AgentID: scout.ID, AgentSlug: "scout", Source: "user",
	})
	require.NoError(t, err)

	// Same agent scope: rejected with the duplicate sentinel.
	_, err = docs.PutDocument(ctx, store.Document{
		Filename: "runbook.md", MIME: "text/plain", Size: 2, Scope: "agent",
		AgentID: scout.ID, AgentSlug: "scout", Source: "user",
	})
	require.ErrorIs(t, err, store.ErrDuplicate)

	// Another agent may hold the same filename.
	_, err = docs.PutDocument(ctx, store.Document{
		Filename: "runbook.md", MIME: "text/plain", Size: 3, Scope: "agent",
		AgentID: mapper.ID, AgentSlug: "mapper", Source: "user",
	})
	require.NoError(t, err)

	// Global may hold it alongside an agent scope.
	_, err = docs.PutDocument(ctx, store.Document{
		Filename: "runbook.md", MIME: "text/plain", Size: 4, Scope: "global", Source: "user",
	})
	require.NoError(t, err)
}

// TestDocuments_CascadeOnAgentDelete covers migration 000014's foreign key:
// deleting the agents row removes the agent's scoped documents with it, while
// global rows and other agents' rows survive.
func TestDocuments_CascadeOnAgentDelete(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	docs := s.Documents()

	doomed := seedDocumentAgent(t, ctx, s, "doomed", "doomed")
	survivor := seedDocumentAgent(t, ctx, s, "survivor", "survivor")

	scoped, err := docs.PutDocument(ctx, store.Document{
		Filename: "gone.txt", MIME: "text/plain", Size: 1, Scope: "agent",
		AgentID: doomed.ID, AgentSlug: "doomed", Source: "user",
	})
	require.NoError(t, err)
	kept, err := docs.PutDocument(ctx, store.Document{
		Filename: "kept.txt", MIME: "text/plain", Size: 1, Scope: "agent",
		AgentID: survivor.ID, AgentSlug: "survivor", Source: "user",
	})
	require.NoError(t, err)
	glob, err := docs.PutDocument(ctx, store.Document{
		Filename: "shared.txt", MIME: "text/plain", Size: 1, Scope: "global", Source: "user",
	})
	require.NoError(t, err)

	require.NoError(t, s.Agents().Delete(ctx, doomed.ID))

	_, err = docs.GetDocument(ctx, scoped.ID)
	require.ErrorIs(t, err, store.ErrNotFound, "deleting the agent must cascade its scoped documents away")
	_, err = docs.GetDocument(ctx, kept.ID)
	require.NoError(t, err, "another agent's document must survive the cascade")
	_, err = docs.GetDocument(ctx, glob.ID)
	require.NoError(t, err, "global documents must survive the cascade")
}

// TestDocuments_RejectsConstraintViolations pins the schema-level gates the
// catalog leans on: an unknown agent id violates the FK and a non-negative
// size is enforced by the CHECK.
func TestDocuments_RejectsConstraintViolations(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	docs := s.Documents()

	_, err := docs.PutDocument(ctx, store.Document{
		Filename: "orphan.txt", MIME: "text/plain", Size: 1, Scope: "agent",
		AgentID: "ffffffffffffffffffffffffffffffff", AgentSlug: "ghost", Source: "user",
	})
	require.Error(t, err, "an unknown agent id must violate the foreign key")

	_, err = docs.PutDocument(ctx, store.Document{
		Filename: "neg.txt", MIME: "text/plain", Size: -1, Scope: "global", Source: "user",
	})
	require.Error(t, err, "a negative size must violate the CHECK")
}
