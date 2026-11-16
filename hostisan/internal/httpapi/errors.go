package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
)

// WriteError writes {"error":{"code","message"}}. message is an English msgid the UI
// translates, or "".
func WriteError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, APIError{Error: ErrorBody{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	encode(w, v)
}

// writeJSONHeadersSet writes a 200 JSON body when the caller has set its own headers.
func writeJSONHeadersSet(w http.ResponseWriter, v any) {
	w.WriteHeader(http.StatusOK)
	encode(w, v)
}

func encode(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("write response", "err", err)
	}
}

func serverError(w http.ResponseWriter, err error) {
	slog.Error("request failed", "err", err)
	WriteError(w, http.StatusInternalServerError, "server_error", "")
}

// invalidFormatMsg is the bin's "invalid <name> format :: <value>" (check_result
// E_INVALID) as a msgid; args are the name and the refused value.
const invalidFormatMsg = "invalid %s format :: %s"

// invalidFormat answers 400 invalid naming the refused field and value, as the bin
// would. Args[0] is the field, which forms use to mark it.
func invalidFormat(w http.ResponseWriter, name, value string) {
	writeJSON(w, http.StatusBadRequest, APIError{Error: ErrorBody{Code: "invalid", Message: invalidFormatMsg, Args: []string{name, value}}})
}

// Whole-sentence msgids for a 400 that has no single field to name.
const (
	bulkCountMsg     = "Select between 1 and %s items."
	requestTooBigMsg = "The request is too large."
	badRequestMsg    = "The request could not be read."
)

// bulkCount answers 400 when a bulk run names no items or more than limit.
func bulkCount(w http.ResponseWriter, limit int) {
	writeJSON(w, http.StatusBadRequest, APIError{Error: ErrorBody{Code: "invalid", Message: bulkCountMsg, Args: []string{strconv.Itoa(limit)}}})
}

// invalidValueMsg is the bin's "invalid value :: %s", for a value no field name fits.
const invalidValueMsg = "invalid value :: %s"

// bulkShapeOK answers 400 unless the action is known and 1 to limit names were sent.
func bulkShapeOK(w http.ResponseWriter, known bool, action string, n, limit int) bool {
	switch {
	case !known:
		invalidFormat(w, "action", action)
	case n == 0 || n > limit:
		bulkCount(w, limit)
	default:
		return true
	}
	return false
}
