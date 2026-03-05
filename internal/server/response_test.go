package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteJSONWithETag_ReturnsNotModifiedWhenIfNoneMatchMatches(t *testing.T) {
	req1 := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	rec1 := httptest.NewRecorder()
	payload := map[string]any{"ok": true, "value": 1}
	writeJSONWithETag(rec1, req1, http.StatusOK, payload)

	if rec1.Code != http.StatusOK {
		t.Fatalf("first response status = %d, want %d", rec1.Code, http.StatusOK)
	}
	etag := rec1.Header().Get("ETag")
	if etag == "" {
		t.Fatalf("expected ETag header to be set")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	req2.Header.Set("If-None-Match", etag)
	rec2 := httptest.NewRecorder()
	writeJSONWithETag(rec2, req2, http.StatusOK, payload)

	if rec2.Code != http.StatusNotModified {
		t.Fatalf("second response status = %d, want %d", rec2.Code, http.StatusNotModified)
	}
	if rec2.Body.Len() != 0 {
		t.Fatalf("expected empty body for 304 response")
	}
}
