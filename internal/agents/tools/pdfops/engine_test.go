package pdfops

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewEngine(t *testing.T) {
	require.NotNil(t, NewEngine())
}

func TestApply_NilOperation(t *testing.T) {
	_, err := NewEngine().Apply(context.Background(), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil operation")
}

// fakeOp is an operation outside the sealed set, for dispatch tests.
type fakeOp struct{ name string }

func (f fakeOp) opName() string { return f.name }

func TestApply_UnknownOperationType(t *testing.T) {
	_, err := NewEngine().Apply(context.Background(), fakeOp{name: "explode"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported operation")
}
