package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

func TestProviderModels_CRUDAndNotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	p := &store.Provider{Name: "anthropic", Type: "anthropic"}
	require.NoError(t, s.Providers().Create(ctx, p))

	model := &store.ProviderModel{
		ProviderID:    p.ID,
		ModelID:       "claude-3-7-sonnet",
		ContextWindow: 200000,
		ImageInput:    true,
		PDFInput:      true,
		Enabled:       true,
	}
	require.NoError(t, s.ProviderModels().Create(ctx, model))
	require.Positive(t, model.ID, "store must assign autoincrement ID")
	require.False(t, model.CreatedAt.IsZero(), "store must stamp CreatedAt")
	require.False(t, model.UpdatedAt.IsZero(), "store must stamp UpdatedAt")

	// ByID
	got, err := s.ProviderModels().ByID(ctx, model.ID)
	require.NoError(t, err)
	require.Equal(t, model.ID, got.ID)
	require.Equal(t, p.ID, got.ProviderID)
	require.Equal(t, "claude-3-7-sonnet", got.ModelID)
	require.Equal(t, 200000, got.ContextWindow)
	require.True(t, got.ImageInput)
	require.True(t, got.PDFInput)
	require.True(t, got.Enabled)
	require.True(t, got.CreatedAt.Equal(model.CreatedAt))
	require.True(t, got.UpdatedAt.Equal(model.UpdatedAt))

	// ByProvider
	list, err := s.ProviderModels().ByProvider(ctx, p.ID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, model.ID, list[0].ID)

	// Update
	time.Sleep(10 * time.Millisecond) // ensure clock tick for UpdatedAt
	model.ContextWindow = 250000
	model.ImageInput = false
	model.Enabled = false
	require.NoError(t, s.ProviderModels().Update(ctx, model))

	gotAfterUpdate, err := s.ProviderModels().ByID(ctx, model.ID)
	require.NoError(t, err)
	require.Equal(t, 250000, gotAfterUpdate.ContextWindow)
	require.False(t, gotAfterUpdate.ImageInput)
	require.True(t, gotAfterUpdate.PDFInput)
	require.False(t, gotAfterUpdate.Enabled)
	require.True(t, gotAfterUpdate.UpdatedAt.After(model.CreatedAt) || gotAfterUpdate.UpdatedAt.Equal(model.CreatedAt))

	// Absent update & ByID
	missing := &store.ProviderModel{
		ID:         999999,
		ProviderID: p.ID,
		ModelID:    "nonexistent",
	}
	require.ErrorIs(t, s.ProviderModels().Update(ctx, missing), store.ErrNotFound)
	_, err = s.ProviderModels().ByID(ctx, 999999)
	require.ErrorIs(t, err, store.ErrNotFound)

	// Delete
	require.NoError(t, s.ProviderModels().Delete(ctx, model.ID))
	require.ErrorIs(t, s.ProviderModels().Delete(ctx, model.ID), store.ErrNotFound)
	require.ErrorIs(t, s.ProviderModels().Delete(ctx, 999999), store.ErrNotFound)
	_, err = s.ProviderModels().ByID(ctx, model.ID)
	require.ErrorIs(t, err, store.ErrNotFound)

	// DeleteByProvider
	m1 := &store.ProviderModel{ProviderID: p.ID, ModelID: "m1"}
	m2 := &store.ProviderModel{ProviderID: p.ID, ModelID: "m2"}
	require.NoError(t, s.ProviderModels().Create(ctx, m1))
	require.NoError(t, s.ProviderModels().Create(ctx, m2))
	list, err = s.ProviderModels().ByProvider(ctx, p.ID)
	require.NoError(t, err)
	require.Len(t, list, 2)

	require.NoError(t, s.ProviderModels().DeleteByProvider(ctx, p.ID))
	list, err = s.ProviderModels().ByProvider(ctx, p.ID)
	require.NoError(t, err)
	require.Empty(t, list)
	require.NotNil(t, list)

	// Idempotent delete on empty provider
	require.NoError(t, s.ProviderModels().DeleteByProvider(ctx, p.ID))
	require.NoError(t, s.ProviderModels().DeleteByProvider(ctx, "nonexistent-provider"))
}

func TestProviderModels_DuplicateRejection(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	p := &store.Provider{Name: "openai", Type: "openai"}
	require.NoError(t, s.Providers().Create(ctx, p))

	m1 := &store.ProviderModel{
		ProviderID:    p.ID,
		ModelID:       "gpt-4o",
		ContextWindow: 128000,
		Enabled:       true,
	}
	require.NoError(t, s.ProviderModels().Create(ctx, m1))

	// Re-inserting with duplicate (provider_id, model_id) must return ErrDuplicate
	m1Dup := &store.ProviderModel{
		ProviderID:    p.ID,
		ModelID:       "gpt-4o",
		ContextWindow: 200000,
		Enabled:       false,
	}
	err := s.ProviderModels().Create(ctx, m1Dup)
	require.ErrorIs(t, err, store.ErrDuplicate, "Create must reject duplicate (provider_id, model_id) with ErrDuplicate")

	// Different provider may use the same model_id
	p2 := &store.Provider{Name: "openrouter", Type: "openrouter"}
	require.NoError(t, s.Providers().Create(ctx, p2))

	m1OtherProvider := &store.ProviderModel{
		ProviderID: p2.ID,
		ModelID:    "gpt-4o",
	}
	require.NoError(t, s.ProviderModels().Create(ctx, m1OtherProvider))

	// Updating to conflict with an existing model on the same provider must return ErrDuplicate
	m2 := &store.ProviderModel{
		ProviderID: p.ID,
		ModelID:    "gpt-4o-mini",
	}
	require.NoError(t, s.ProviderModels().Create(ctx, m2))

	m2.ModelID = "gpt-4o"
	err = s.ProviderModels().Update(ctx, m2)
	require.ErrorIs(t, err, store.ErrDuplicate, "Update must reject duplicate (provider_id, model_id) with ErrDuplicate")
}

func TestProviderModels_CascadeOnProviderDelete(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	p1 := &store.Provider{Name: "provider-1", Type: "anthropic"}
	require.NoError(t, s.Providers().Create(ctx, p1))
	p2 := &store.Provider{Name: "provider-2", Type: "openai"}
	require.NoError(t, s.Providers().Create(ctx, p2))

	m1 := &store.ProviderModel{ProviderID: p1.ID, ModelID: "p1-model-1"}
	m2 := &store.ProviderModel{ProviderID: p1.ID, ModelID: "p1-model-2"}
	require.NoError(t, s.ProviderModels().Create(ctx, m1))
	require.NoError(t, s.ProviderModels().Create(ctx, m2))

	m3 := &store.ProviderModel{ProviderID: p2.ID, ModelID: "p2-model-1"}
	require.NoError(t, s.ProviderModels().Create(ctx, m3))

	list1, err := s.ProviderModels().ByProvider(ctx, p1.ID)
	require.NoError(t, err)
	require.Len(t, list1, 2)

	// Deleting p1 must cascade delete m1 and m2 via foreign key
	require.NoError(t, s.Providers().Delete(ctx, p1.ID))

	list1After, err := s.ProviderModels().ByProvider(ctx, p1.ID)
	require.NoError(t, err)
	require.Empty(t, list1After)

	_, err = s.ProviderModels().ByID(ctx, m1.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
	_, err = s.ProviderModels().ByID(ctx, m2.ID)
	require.ErrorIs(t, err, store.ErrNotFound)

	// p2's models must survive intact
	list2After, err := s.ProviderModels().ByProvider(ctx, p2.ID)
	require.NoError(t, err)
	require.Len(t, list2After, 1)
	require.Equal(t, m3.ID, list2After[0].ID)
}

func TestProviderModels_UpsertMissing(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	p := &store.Provider{Name: "gemini", Type: "gemini"}
	require.NoError(t, s.Providers().Create(ctx, p))

	// Pre-create m1 with custom disabled state and custom context window
	m1 := &store.ProviderModel{
		ProviderID:    p.ID,
		ModelID:       "gemini-1.5-pro",
		ContextWindow: 500000,
		Enabled:       false,
	}
	require.NoError(t, s.ProviderModels().Create(ctx, m1))

	// Batch upsert containing m1 (which exists) and m2, m3 (which are missing)
	models := []*store.ProviderModel{
		{
			ProviderID:    p.ID,
			ModelID:       "gemini-1.5-pro",
			ContextWindow: 1000000,
			Enabled:       true,
		},
		{
			ProviderID:    p.ID,
			ModelID:       "gemini-1.5-flash",
			ContextWindow: 1000000,
			ImageInput:    true,
			Enabled:       true,
		},
		{
			ProviderID:    p.ID,
			ModelID:       "gemini-2.0-flash",
			ContextWindow: 1000000,
			PDFInput:      true,
			Enabled:       true,
		},
	}

	inserted, err := s.ProviderModels().UpsertMissing(ctx, models)
	require.NoError(t, err)
	require.Equal(t, 2, inserted, "should insert exactly 2 missing models")

	// Verify m1 was preserved, not overwritten
	gotM1, err := s.ProviderModels().ByID(ctx, m1.ID)
	require.NoError(t, err)
	require.Equal(t, 500000, gotM1.ContextWindow, "existing ContextWindow must not be overwritten")
	require.False(t, gotM1.Enabled, "existing Enabled flag must not be overwritten")

	// Verify m2 and m3 were inserted
	list, err := s.ProviderModels().ByProvider(ctx, p.ID)
	require.NoError(t, err)
	require.Len(t, list, 3)

	// Re-run upsert with same list -> 0 inserted
	insertedAgain, err := s.ProviderModels().UpsertMissing(ctx, models)
	require.NoError(t, err)
	require.Equal(t, 0, insertedAgain)

	// Empty list returns 0
	insertedEmpty, err := s.ProviderModels().UpsertMissing(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, 0, insertedEmpty)
}

func TestProviderModels_EffortsAndVideoInputRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	p := &store.Provider{Name: "anthropic", Type: "anthropic"}
	require.NoError(t, s.Providers().Create(ctx, p))

	// Declared efforts and the video-input flag survive create/read in
	// declared order.
	model := &store.ProviderModel{
		ProviderID:    p.ID,
		ModelID:       "claude-3-7-sonnet",
		ContextWindow: 200000,
		ImageInput:    true,
		VideoInput:    true,
		Efforts:       []string{"none", "low", "high"},
		Enabled:       true,
	}
	require.NoError(t, s.ProviderModels().Create(ctx, model))

	got, err := s.ProviderModels().ByID(ctx, model.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"none", "low", "high"}, got.Efforts)
	require.True(t, got.VideoInput)

	list, err := s.ProviderModels().ByProvider(ctx, p.ID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, []string{"none", "low", "high"}, list[0].Efforts)
	require.True(t, list[0].VideoInput)

	// Declared efforts store as the JSON string array in declared order.
	var rawEfforts string
	require.NoError(t, s.db.QueryRow(`SELECT efforts FROM provider_models WHERE id = ?`, model.ID).Scan(&rawEfforts))
	require.Equal(t, `["none","low","high"]`, rawEfforts)

	// Nil efforts read back empty and store the undeclared sentinel ''.
	undeclared := &store.ProviderModel{ProviderID: p.ID, ModelID: "claude-3-5-haiku"}
	require.NoError(t, s.ProviderModels().Create(ctx, undeclared))
	gotUndeclared, err := s.ProviderModels().ByID(ctx, undeclared.ID)
	require.NoError(t, err)
	require.Empty(t, gotUndeclared.Efforts)
	require.False(t, gotUndeclared.VideoInput)
	require.NoError(t, s.db.QueryRow(`SELECT efforts FROM provider_models WHERE id = ?`, undeclared.ID).Scan(&rawEfforts))
	require.Equal(t, "", rawEfforts, "undeclared efforts must store the '' sentinel")

	// Update rewrites both the declaration and the flag.
	time.Sleep(10 * time.Millisecond) // ensure clock tick for UpdatedAt
	model.Efforts = []string{"high"}
	model.VideoInput = false
	require.NoError(t, s.ProviderModels().Update(ctx, model))

	gotAfterUpdate, err := s.ProviderModels().ByID(ctx, model.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"high"}, gotAfterUpdate.Efforts)
	require.False(t, gotAfterUpdate.VideoInput)

	// UpsertMissing carries efforts and video input onto inserted rows.
	fresh := &store.ProviderModel{
		ProviderID: p.ID,
		ModelID:    "claude-opus-4",
		VideoInput: true,
		Efforts:    []string{"low", "high"},
	}
	inserted, err := s.ProviderModels().UpsertMissing(ctx, []*store.ProviderModel{fresh})
	require.NoError(t, err)
	require.Equal(t, 1, inserted)

	gotFresh, err := s.ProviderModels().ByID(ctx, fresh.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"low", "high"}, gotFresh.Efforts)
	require.True(t, gotFresh.VideoInput)
}
