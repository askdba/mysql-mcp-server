// internal/api/response.go
package api

import (
	"encoding/json"
	"net/http"
)

// Response is the standard JSON response structure for all API endpoints.
type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// WriteJSON writes a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// WriteSuccess writes a successful JSON response with status 200.
func WriteSuccess(w http.ResponseWriter, data interface{}) {
	WriteJSON(w, http.StatusOK, Response{Success: true, Data: data})
}

// WriteError writes an error JSON response with the given status code.
func WriteError(w http.ResponseWriter, status int, message string) {
	WriteJSON(w, status, Response{Success: false, Error: message})
}

// WriteBadRequest writes a 400 Bad Request error response.
func WriteBadRequest(w http.ResponseWriter, message string) {
	WriteError(w, http.StatusBadRequest, message)
}

// WriteInternalError writes a 500 Internal Server Error response.
func WriteInternalError(w http.ResponseWriter, message string) {
	WriteError(w, http.StatusInternalServerError, message)
}

// WriteNotFound writes a 404 Not Found error response.
func WriteNotFound(w http.ResponseWriter, message string) {
	WriteError(w, http.StatusNotFound, message)
}

// WriteMethodNotAllowed writes a 405 Method Not Allowed error response.
func WriteMethodNotAllowed(w http.ResponseWriter, message string) {
	WriteError(w, http.StatusMethodNotAllowed, message)
}
