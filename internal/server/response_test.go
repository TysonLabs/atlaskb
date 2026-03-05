package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPIErrorErrorMethod(t *testing.T) {
	e := &APIError{Status: http.StatusBadRequest, Message: "bad input"}
	if got := e.Error(); got != "bad input" {
		t.Fatalf("APIError.Error() = %q, want %q", got, "bad input")
	}
}

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

func TestWriteJSONWithETag_ReturnsNotModifiedForIfNoneMatchList(t *testing.T) {
	req1 := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	rec1 := httptest.NewRecorder()
	payload := map[string]any{"ok": true}
	writeJSONWithETag(rec1, req1, http.StatusOK, payload)
	etag := rec1.Header().Get("ETag")

	req2 := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	req2.Header.Set("If-None-Match", `"bogus", `+etag)
	rec2 := httptest.NewRecorder()
	writeJSONWithETag(rec2, req2, http.StatusOK, payload)

	if rec2.Code != http.StatusNotModified {
		t.Fatalf("response status = %d, want %d", rec2.Code, http.StatusNotModified)
	}
}

func TestWriteJSONWithETag_ReturnsNotModifiedForWildcard(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	req.Header.Set("If-None-Match", "*")
	rec := httptest.NewRecorder()
	writeJSONWithETag(rec, req, http.StatusOK, map[string]any{"ok": true})
	if rec.Code != http.StatusNotModified {
		t.Fatalf("response status = %d, want %d", rec.Code, http.StatusNotModified)
	}
}
