package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBrowseFS_DefaultPath(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodGet, "/api/fs/browse", "")
	w := httptest.NewRecorder()
	f.h.BrowseFS(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}
