package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
)

// APIError carries an HTTP status alongside a message safe to show the client.
type APIError struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
	err     error
}

func (e *APIError) Error() string {
	if e.err != nil {
		return e.err.Error()
	}
	return e.Message
}

func (e *APIError) Unwrap() error { return e.err }

func Err(status int, code, msg string) *APIError {
	return &APIError{Status: status, Code: code, Message: msg}
}

func Wrap(status int, code string, err error) *APIError {
	return &APIError{Status: status, Code: code, Message: err.Error(), err: err}
}

var (
	ErrUnauthorized = Err(http.StatusUnauthorized, "unauthorized", "authentication required")
	ErrForbidden    = Err(http.StatusForbidden, "forbidden", "your role does not permit this action")
	ErrNotFound     = Err(http.StatusNotFound, "not_found", "not found")
)

func BadRequest(format string, a ...any) *APIError {
	return Err(http.StatusBadRequest, "bad_request", fmt.Sprintf(format, a...))
}

func Internal(err error) *APIError {
	return &APIError{Status: http.StatusInternalServerError, Code: "internal", Message: "internal error", err: err}
}

func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("write json response", "err", err)
	}
}

func NoContent(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }

// Handler is a http.Handler whose errors are rendered centrally, so no route
// can accidentally leak an internal error string to the client.
type Handler func(w http.ResponseWriter, r *http.Request) error

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h(w, r); err != nil {
		WriteError(w, r, err)
	}
}

func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		apiErr = Internal(err)
	}
	if apiErr.Status >= 500 {
		slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", apiErr.Error())
	}
	if p, ok := PrincipalFrom(r.Context()); ok {
		p.FailureReason = apiErr.Message
	}
	JSON(w, apiErr.Status, map[string]any{"error": apiErr})
}

func DecodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 4<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return BadRequest("malformed request body: %v", err)
	}
	return nil
}
