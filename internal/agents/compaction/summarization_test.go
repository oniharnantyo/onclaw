package compaction

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSummarization_ContextLimitDerivation(t *testing.T) {
	ctx := t.Context()
	fakeMdl := &fakeChatModel{reply: "OK"}

	// 1. With ContextLimit = 200,000 -> 0.9 * 200,000 = 180,000
	cap1 := &SummarizationCapability{
		Model:        fakeMdl,
		ContextLimit: 200000,
	}
	mw1, err := NewSummarizationMiddleware(ctx, cap1)
	require.NoError(t, err)
	require.NotNil(t, mw1)

	// 2. With ContextLimit = 100,000 -> 0.9 * 100,000 = 90,000
	cap2 := &SummarizationCapability{
		Model:        fakeMdl,
		ContextLimit: 100000,
	}
	mw2, err := NewSummarizationMiddleware(ctx, cap2)
	require.NoError(t, err)
	require.NotNil(t, mw2)

	// 3. Fallback when ContextLimit <= 0
	cap3 := &SummarizationCapability{
		Model:        fakeMdl,
		ContextLimit: 0,
	}
	mw3, err := NewSummarizationMiddleware(ctx, cap3)
	require.NoError(t, err)
	require.NotNil(t, mw3)

	// 4. Missing Model returns error
	cap4 := &SummarizationCapability{}
	_, err = NewSummarizationMiddleware(ctx, cap4)
	require.Error(t, err)
}

func TestDefaultSummarizationTokenCounter(t *testing.T) {
	ctx := t.Context()
	count, err := DefaultSummarizationTokenCounter(ctx, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}
