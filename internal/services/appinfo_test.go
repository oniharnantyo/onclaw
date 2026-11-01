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

// TestAppInfo_Get_ReportsNativeDialogs pins the capability flag
// (add-artifact-save-as task 2.2): the default (desktop) build can open
// native dialogs, so the frontend may offer Save As.
func TestAppInfo_Get_ReportsNativeDialogs(t *testing.T) {
	svc := NewAppInfo()

	meta, err := svc.Get(context.Background())
	require.NoError(t, err)
	require.True(t, meta.NativeDialogs, "the desktop build can open native dialogs")
}
