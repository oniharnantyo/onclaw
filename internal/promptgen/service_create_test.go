package promptgen

// service_create_test.go mirrors onclaw-v2's service_create_test.go cases,
// adapted to the scripted fake ModelFactory + in-memory fake DocStore: create
// commits the three generated documents; a failed generation returns a
// sanitized error and commits nothing.

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
)

func TestService_GenerateForCreate_CommitsDocuments(t *testing.T) {
	ctx := context.Background()
	factory := &fakeFactory{model: &fakeChatModel{
		generateFunc: func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
			return promptsToolCallMessage(
				"# Identity\nNew agent identity.",
				"# Soul\nNew agent soul.",
				"# Bootstrap\nNew agent bootstrap.",
			), nil
		},
	}}
	docs := newFakeDocStore()
	svc := NewService(factory, docs)

	got, err := svc.GenerateForCreate(ctx, "scout", testParams())
	require.NoError(t, err)

	// The generation ran on the agent's own provider and model.
	require.Equal(t, 1, factory.builds)
	require.Equal(t, "prov-1", factory.gotProvider.ID)
	require.Equal(t, "openai", factory.gotProvider.Type)
	require.Equal(t, "gpt-4o", factory.gotModelName)

	// The documents were committed for the slug.
	require.Equal(t, 1, docs.commitCalls)
	stored, ok := docs.docs["scout"]
	require.True(t, ok, "documents must be committed for the slug")
	require.Equal(t, "# Identity\nNew agent identity.", stored.Identity)
	require.Equal(t, "# Soul\nNew agent soul.", stored.Soul)
	require.Equal(t, "# Bootstrap\nNew agent bootstrap.", stored.Bootstrap)
	require.Equal(t, stored, got)
}

func TestService_GenerateForCreate_FailureReturnsErrorAndCommitsNothing(t *testing.T) {
	ctx := context.Background()
	factory := &fakeFactory{model: &fakeChatModel{
		generateFunc: func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
			return nil, errors.New("401 unauthorized from provider")
		},
	}}
	docs := newFakeDocStore()
	svc := NewService(factory, docs)

	_, err := svc.GenerateForCreate(ctx, "drifter", testParams())
	require.Error(t, err)
	require.Contains(t, err.Error(), "provider authentication failed")

	// Nothing was committed.
	require.Zero(t, docs.commitCalls)
	require.Empty(t, docs.docs)
}

func TestService_GenerateForCreate_RequiresSlugNameAndBrief(t *testing.T) {
	ctx := context.Background()
	factory := &fakeFactory{}
	docs := newFakeDocStore()
	svc := NewService(factory, docs)

	tests := []struct {
		name   string
		slug   string
		mutate func(p Params) Params
	}{
		{name: "empty slug", slug: "  ", mutate: func(p Params) Params { return p }},
		{name: "empty name", slug: "scout", mutate: func(p Params) Params { p.AgentName = " "; return p }},
		{name: "empty brief", slug: "scout", mutate: func(p Params) Params { p.Brief = ""; return p }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.GenerateForCreate(ctx, tt.slug, tt.mutate(testParams()))
			require.ErrorIs(t, err, ErrInvalid)
			require.Zero(t, factory.builds, "an invalid request must never build a model")
			require.Zero(t, docs.commitCalls)
		})
	}
}

func TestService_GenerateForCreate_AdapterBuildFailureIsSanitized(t *testing.T) {
	ctx := context.Background()
	factory := &fakeFactory{buildErr: errors.New("post https://api.openai.com/v1: dial tcp: connection refused with key sk-secret-provider-key")}
	docs := newFakeDocStore()
	svc := NewService(factory, docs)

	_, err := svc.GenerateForCreate(ctx, "scout", testParams())
	require.Error(t, err)

	// The raw adapter error must not leak the key into the surfaced reason.
	require.NotContains(t, err.Error(), "sk-secret-provider-key")
	require.Contains(t, err.Error(), "failed to initialize model adapter")
	require.Zero(t, docs.commitCalls)
}
