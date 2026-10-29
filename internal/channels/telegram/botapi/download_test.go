package botapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDownloadFileFetchesBytes pins the file-download endpoint shape: a GET to
// <base>/file/bot<token>/<file_path> answering with the raw bytes.
func TestDownloadFileFetchesBytes(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF-1.4 bytes"))
	}))
	t.Cleanup(srv.Close)

	c := New(testToken, WithBaseURL(srv.URL))
	data, err := c.DownloadFile(context.Background(), "documents/report.pdf")
	require.NoError(t, err)
	require.Equal(t, http.MethodGet, gotMethod)
	require.Equal(t, "/file/bot"+testToken+"/documents/report.pdf", gotPath)
	require.Equal(t, "%PDF-1.4 bytes", string(data))
}

// TestDownloadFileLeadingSlash pins the path normalization: a leading slash on
// the server-relative path must not double in the URL.
func TestDownloadFileLeadingSlash(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte("x"))
	}))
	t.Cleanup(srv.Close)

	c := New(testToken, WithBaseURL(srv.URL))
	_, err := c.DownloadFile(context.Background(), "/photos/img.jpg")
	require.NoError(t, err)
	require.Equal(t, "/file/bot"+testToken+"/photos/img.jpg", gotPath)
}

// TestDownloadFileHTTPErrorIsTransient pins the non-200 classification: the
// file endpoint has no Bot API envelope, so any non-200 is a transient error.
func TestDownloadFileHTTPErrorIsTransient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	c := New(testToken, WithBaseURL(srv.URL))
	_, err := c.DownloadFile(context.Background(), "gone.txt")
	require.Error(t, err)
	var apiErr *Error
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, KindTransient, apiErr.Kind)
}
