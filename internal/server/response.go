package server

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type APIError struct {
	Status  int    `json:"-"`
	Message string `json:"error"`
}

func (e *APIError) Error() string { return e.Message }

func NewBadRequest(msg string) *APIError {
	return &APIError{Status: http.StatusBadRequest, Message: msg}
}
func NewNotFound(msg string) *APIError { return &APIError{Status: http.StatusNotFound, Message: msg} }
func NewInternal(msg string) *APIError {
	return &APIError{Status: http.StatusInternalServerError, Message: msg}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeJSONWithETag(w http.ResponseWriter, r *http.Request, status int, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "encoding response"})
		return
	}
	sum := sha256.Sum256(data)
	etag := fmt.Sprintf("W/%q", fmt.Sprintf("%x", sum[:]))

	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	if inm := strings.TrimSpace(r.Header.Get("If-None-Match")); inm != "" && etagMatches(inm, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

func etagMatches(ifNoneMatchHeader, currentETag string) bool {
	if strings.TrimSpace(ifNoneMatchHeader) == "*" {
		return true
	}
	cur := normalizeETag(currentETag)
	for _, part := range strings.Split(ifNoneMatchHeader, ",") {
		if normalizeETag(part) == cur {
			return true
		}
	}
	return false
}

func normalizeETag(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "W/")
	v = strings.TrimSpace(v)
	v = strings.Trim(v, "\"")
	return v
}

func writeError(w http.ResponseWriter, err error) {
	if apiErr, ok := err.(*APIError); ok {
		writeJSON(w, apiErr.Status, apiErr)
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
}
