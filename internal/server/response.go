package server

import (
	"encoding/json"
	"net/http"
)

type APIError struct {
	Status  int    `json:"-"`
	Message string `json:"error"`
}

func (e *APIError) Error() string { return e.Message }

func NewBadRequest(msg string) *APIError   { return &APIError{Status: http.StatusBadRequest, Message: msg} }
func NewNotFound(msg string) *APIError     { return &APIError{Status: http.StatusNotFound, Message: msg} }
func NewInternal(msg string) *APIError     { return &APIError{Status: http.StatusInternalServerError, Message: msg} }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	if apiErr, ok := err.(*APIError); ok {
		writeJSON(w, apiErr.Status, apiErr)
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
}
