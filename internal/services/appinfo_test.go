package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAppInfo_Get_ReturnsNameAndVersion(t *testing.T) {
	svc := NewAppInfo()

	meta, err := svc.Get(context.Background())
	require.NoError(t, err)
	require.Equal(t, AppName, meta.Name)
	require.Equal(t, AppVersion, meta.Version)
}
