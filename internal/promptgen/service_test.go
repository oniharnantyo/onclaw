package promptgen

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
)

func TestService_Regenerate_EnhancesExistingDocuments(t *testing.T) {
	ctx := context.Background()

	var captured []*schema.Message
	factory := &fakeFactory{model: &fakeChatModel{
		generateFunc: func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
			captured = input
			return promptsToolCallMessage("enhanced identity", "enhanced soul", "enhanced bootstrap"), nil
		},
	}}
	docs := newFakeDocStore()
	docs.docs["scout"] = Documents{Identity: "old identity", Soul: "old soul", Bootstrap: "old bootstrap"}
	svc := NewService(factory, docs)

	got, err := svc.Regenerate(ctx, "scout", testParams())
	require.NoError(t, err)

	// The model call carried the current documents in enhance mode.
	require.Len(t, captured, 2)
	require.Contains(t, captured[0].Content, "ENHANCE")
	for _, want := range []string{
		"Current IDENTITY.md", "old identity",
		"Current SOUL.md", "old soul",
		"Current BOOTSTRAP.md", "old bootstrap",
		"Scout", "Scout the perimeter.",
	} {
		require.Contains(t, captured[1].Content, want)
	}

	// The enhanced documents are committed for the slug.
	require.Equal(t, 1, docs.commitCalls)
	stored := docs.docs["scout"]
	require.Equal(t, Documents{Identity: "enhanced identity", Soul: "enhanced soul", Bootstrap: "enhanced bootstrap"}, stored)
	require.Equal(t, stored, got)
}

func TestService_Regenerate_FreshWhenNoDocuments(t *testing.T) {
	ctx := context.Background()

	var captured []*schema.Message
	factory := &fakeFactory{model: &fakeChatModel{
		generateFunc: func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
			captured = input
			return promptsToolCallMessage("identity", "soul", "bootstrap"), nil
		},
	}}
	docs := newFakeDocStore()
	svc := NewService(factory, docs)

	_, err := svc.Regenerate(ctx, "scout", testParams())
	require.NoError(t, err)

	// No documents on disk: fresh framing, brief drives the generation.
	require.Len(t, captured, 2)
	require.NotContains(t, captured[0].Content, "ENHANCE")
	require.Contains(t, captured[0].Content, "IDENTITY.md")
	require.Contains(t, captured[1].Content, "Scout the perimeter.")
}

func TestService_Regenerate_FreshWhenDocumentsAllBlank(t *testing.T) {
	ctx := context.Background()

	var captured []*schema.Message
	factory := &fakeFactory{model: &fakeChatModel{
		generateFunc: func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
			captured = input
			return promptsToolCallMessage("identity", "soul", "bootstrap"), nil
		},
	}}
	docs := newFakeDocStore()
	docs.docs["scout"] = Documents{Identity: "   ", Soul: "", Bootstrap: "\n"}
	svc := NewService(factory, docs)

	_, err := svc.Regenerate(ctx, "scout", testParams())
	require.NoError(t, err)

	// A workspace holding only blanks has nothing worth enhancing.
	require.NotContains(t, captured[0].Content, "ENHANCE")
}

func TestService_Regenerate_InstructionRidesAlongWithCurrentDocs(t *testing.T) {
	ctx := context.Background()

	var captured []*schema.Message
	factory := &fakeFactory{model: &fakeChatModel{
		generateFunc: func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
			captured = input
			return promptsToolCallMessage("enhanced identity", "enhanced soul", "enhanced bootstrap"), nil
		},
	}}
	docs := newFakeDocStore()
	docs.docs["scout"] = Documents{Identity: "old identity", Soul: "old soul", Bootstrap: "old bootstrap"}
	svc := NewService(factory, docs)

	p := testParams()
	p.Instruction = "focus more on perimeter coverage"
	_, err := svc.Regenerate(ctx, "scout", p)
	require.NoError(t, err)

	// The old prompts are never dropped in favor of the instruction: current
	// documents AND the requested-changes section ride together.
	require.Contains(t, captured[0].Content, "ENHANCE")
	for _, want := range []string{
		"Requested Changes", "focus more on perimeter coverage",
		"old identity", "old soul", "old bootstrap",
	} {
		require.Contains(t, captured[1].Content, want)
	}
}

func TestService_Regenerate_InstructionInFreshMode(t *testing.T) {
	ctx := context.Background()

	var captured []*schema.Message
	factory := &fakeFactory{model: &fakeChatModel{
		generateFunc: func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
			captured = input
			return promptsToolCallMessage("identity", "soul", "bootstrap"), nil
		},
	}}
	docs := newFakeDocStore()
	svc := NewService(factory, docs)

	p := testParams()
	p.Instruction = "keep it terse"
	_, err := svc.Regenerate(ctx, "scout", p)
	require.NoError(t, err)

	require.NotContains(t, captured[0].Content, "ENHANCE")
	require.Contains(t, captured[1].Content, "Requested Changes")
	require.Contains(t, captured[1].Content, "keep it terse")
}

// TestService_Regenerate_FailuresLeaveDocumentsUntouched pins the DocStore
// contract's failure behavior: a commit failure — whether a plain write error
// or a backup failure — fails the generation and, per the fake, no document
// changed.
func TestService_Regenerate_FailuresLeaveDocumentsUntouched(t *testing.T) {
	tests := []struct {
		name      string
		commitErr error
	}{
		{name: "commit failure", commitErr: errors.New("write IDENTITY.md: disk full")},
		{name: "backup failure", commitErr: errors.New("backup SOUL.md: permission denied")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			factory := &fakeFactory{model: &fakeChatModel{
				generateFunc: func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
					return promptsToolCallMessage("new identity", "new soul", "new bootstrap"), nil
				},
			}}
			docs := newFakeDocStore()
			old := Documents{Identity: "old identity", Soul: "old soul", Bootstrap: "old bootstrap"}
			docs.docs["scout"] = old
			docs.commitErr = tt.commitErr
			svc := NewService(factory, docs)

			_, err := svc.Regenerate(ctx, "scout", testParams())
			require.Error(t, err)
			require.Contains(t, err.Error(), "commit prompt documents for \"scout\"")
			require.ErrorIs(t, err, tt.commitErr)

			// The previous documents are untouched and retryable.
			require.Equal(t, old, docs.docs["scout"])
		})
	}
}

func TestService_Regenerate_ReadFailureReturnsErrorWithoutBuilding(t *testing.T) {
	ctx := context.Background()
	factory := &fakeFactory{}
	docs := newFakeDocStore()
	docs.readErr = errors.New("workspace directory unreadable")
	svc := NewService(factory, docs)

	_, err := svc.Regenerate(ctx, "scout", testParams())
	require.Error(t, err)
	require.ErrorIs(t, err, docs.readErr)
	require.Contains(t, err.Error(), "read current prompt documents")
	require.Zero(t, factory.builds, "a failed read must never build a model")
}

func TestService_Regenerate_ModelErrorIsSanitized(t *testing.T) {
	ctx := context.Background()
	factory := &fakeFactory{model: &fakeChatModel{
		generateFunc: func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
			return nil, errors.New("429 Too Many Requests: Rate limit reached for key sk-secret-provider-key")
		},
	}}
	docs := newFakeDocStore()
	svc := NewService(factory, docs)

	_, err := svc.Regenerate(ctx, "scout", testParams())
	require.Error(t, err)
	require.Contains(t, err.Error(), "provider rate limit or quota exceeded — retry later")
	// The provider's key must not ride along in any form.
	require.NotContains(t, err.Error(), "sk-secret-provider-key")
	require.Zero(t, docs.commitCalls)
}

// poolsideRejection is the observed shape of a structured-output refusal: an
// OpenAI-compatible endpoint (here Poolside) that only accepts free-form or
// json_object response_format types answering a json_schema request with a
// 400 validation error.
const poolsideRejection = `error, status code: 400, status: 400 Bad Request, message: Invalid request: ["response_format.ResponseFormatText.type (json_schema): Input should be 'text'"]`

// TestService_Regenerate_RetriesFreeFormWhenStructuredOutputRejected pins the
// fallback for endpoints without native structured-output support: the first
// attempt builds with the constraint, and a response_format/json_schema
// refusal triggers exactly one free-form retry whose plain-JSON content the
// parser still recovers.
func TestService_Regenerate_RetriesFreeFormWhenStructuredOutputRejected(t *testing.T) {
	ctx := context.Background()

	calls := 0
	rejectingModel := &fakeChatModel{
		generateFunc: func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
			calls++
			return nil, errors.New(poolsideRejection)
		},
	}
	freeFormModel := &fakeChatModel{
		generateFunc: func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
			calls++
			return &schema.Message{Content: "Here are the documents:\n```json\n" +
				`{"identity":"free identity","soul":"free soul","bootstrap":"free bootstrap"}` +
				"\n```"}, nil
		},
	}
	factory := &fakeFactory{models: []*fakeChatModel{rejectingModel, freeFormModel}}
	docs := newFakeDocStore()
	svc := NewService(factory, docs)

	got, err := svc.Regenerate(ctx, "scout", testParams())
	require.NoError(t, err)
	require.Equal(t, 2, calls, "exactly one free-form retry after the refusal")

	// The first build carried the constraint, the retry did not.
	require.Equal(t, []bool{true, false}, factory.buildStructured)

	require.Equal(t, Documents{Identity: "free identity", Soul: "free soul", Bootstrap: "free bootstrap"}, got)
	require.Equal(t, 1, docs.commitCalls)
}

func TestService_Regenerate_NoRetryForUnrelatedErrors(t *testing.T) {
	ctx := context.Background()

	factory := &fakeFactory{model: &fakeChatModel{
		generateFunc: func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
			return nil, errors.New("error, status code: 401, status: 401 Unauthorized")
		},
	}}
	docs := newFakeDocStore()
	svc := NewService(factory, docs)

	_, err := svc.Regenerate(ctx, "scout", testParams())
	require.Error(t, err)
	require.Contains(t, err.Error(), "provider authentication failed")
	require.Equal(t, 1, factory.builds, "only a structured-output refusal earns a retry")
	require.Equal(t, []bool{true}, factory.buildStructured)
	require.Zero(t, docs.commitCalls)
}

func TestService_Regenerate_RetryRefusedAgainFailsSanitized(t *testing.T) {
	ctx := context.Background()

	factory := &fakeFactory{models: []*fakeChatModel{
		{generateFunc: func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
			return nil, errors.New(poolsideRejection)
		}},
		{generateFunc: func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
			return nil, errors.New(poolsideRejection)
		}},
	}}
	docs := newFakeDocStore()
	svc := NewService(factory, docs)

	_, err := svc.Regenerate(ctx, "scout", testParams())
	require.Error(t, err)
	require.Contains(t, err.Error(), "400 Bad Request")
	require.NotContains(t, err.Error(), "\n", "the reason must stay single-line")
	require.Equal(t, 2, factory.builds)
	require.Zero(t, docs.commitCalls)
}

func TestStructuredOutputRejected(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{
			name: "pydantic field-path refusal",
			err:  errors.New(poolsideRejection),
			want: true,
		},
		{
			name: "prose refusal",
			err:  errors.New("400 response_format is not supported by this model"),
			want: true,
		},
		{
			name: "json schema mention",
			err:  errors.New("json schema outputs are unavailable for this model"),
			want: true,
		},
		{name: "auth failure", err: errors.New("401 Unauthorized: invalid api key"), want: false},
		{name: "rate limit", err: errors.New("429 Too Many Requests"), want: false},
		{name: "timeout", err: context.DeadlineExceeded, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, structuredOutputRejected(tt.err))
		})
	}
}

func TestService_Regenerate_ParseFailureCommitsNothing(t *testing.T) {
	ctx := context.Background()
	factory := &fakeFactory{model: &fakeChatModel{
		generateFunc: func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
			return &schema.Message{Content: "Just plain text with no JSON object"}, nil
		},
	}}
	docs := newFakeDocStore()
	svc := NewService(factory, docs)

	_, err := svc.Regenerate(ctx, "scout", testParams())
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to parse generated identity, soul, and bootstrap prompts from model output")
	require.Zero(t, docs.commitCalls)
}

// TestService_Regenerate_TimesOut proves the service timeout is enforced: a
// model call that hangs past the configured budget fails the generation with
// the timeout reason instead of hanging the request indefinitely.
func TestService_Regenerate_TimesOut(t *testing.T) {
	ctx := context.Background()

	hangingModel := &fakeChatModel{
		generateFunc: func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(2 * time.Second):
				return promptsToolCallMessage("identity", "soul", "bootstrap"), nil
			}
		},
	}
	factory := &fakeFactory{model: hangingModel}
	docs := newFakeDocStore()
	svc := NewService(factory, docs, WithTimeout(50*time.Millisecond))

	_, err := svc.Regenerate(ctx, "scout", testParams())
	require.Error(t, err)
	require.Contains(t, err.Error(), "prompt generation timed out — retry")
	require.Zero(t, docs.commitCalls)
	require.Empty(t, docs.docs)
}

func TestService_DefaultTimeoutIsThreeMinutes(t *testing.T) {
	svc := NewService(&fakeFactory{}, newFakeDocStore())
	require.Equal(t, 3*time.Minute, svc.timeout)

	svc = NewService(&fakeFactory{}, newFakeDocStore(), WithTimeout(5*time.Second))
	require.Equal(t, 5*time.Second, svc.timeout)

	// Non-positive timeouts are ignored.
	svc = NewService(&fakeFactory{}, newFakeDocStore(), WithTimeout(0), WithTimeout(-time.Second))
	require.Equal(t, 3*time.Minute, svc.timeout)
}

func TestSanitizeError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "nil error", err: nil, want: ""},
		{
			name: "401 unauthorized",
			err:  errors.New("HTTP 401 Unauthorized: Invalid API key"),
			want: "provider authentication failed — check provider API key",
		},
		{
			name: "403 forbidden",
			err:  errors.New("403 Forbidden: permission_denied"),
			want: "provider permission denied — check API key permissions",
		},
		{
			name: "404 model not found",
			err:  errors.New("Error 404: model_not_found: The model does not exist"),
			want: "configured model not found or inaccessible with provider credentials",
		},
		{
			name: "429 rate limit",
			err:  errors.New("429 Too Many Requests: Rate limit reached"),
			want: "provider rate limit or quota exceeded — retry later",
		},
		{
			name: "timeout",
			err:  context.DeadlineExceeded,
			want: "prompt generation timed out — retry",
		},
		{
			name: "parse error",
			err:  errors.New("unable to parse output sections"),
			want: "failed to parse generated identity, soul, and bootstrap prompts from model output",
		},
		{
			name: "unknown error keeps first line only",
			err:  errors.New("something odd happened\nGET https://host/path\nstatus=500"),
			want: "something odd happened",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, SanitizeError(tt.err))
		})
	}
}

func TestSanitizeErrorString_CapsLongDefaultMessages(t *testing.T) {
	long := strings.Repeat("x", 300)
	got := SanitizeErrorString(long)
	require.Len(t, got, 150)
	require.True(t, strings.HasSuffix(got, "..."))
}

func TestRedactKey(t *testing.T) {
	require.Equal(t, "dial failed with <redacted>", redactKey("dial failed with sk-secret", "sk-secret"))
	// Empty key redacts nothing; absent key leaves the message alone.
	require.Equal(t, "dial failed", redactKey("dial failed", ""))
	require.Equal(t, "dial failed", redactKey("dial failed", "sk-absent"))
}
