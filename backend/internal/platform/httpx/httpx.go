// Package httpx contains the HTTP conventions shared by all modules:
// the Error format from openapi.yaml, strict JSON decoding and request IDs.
package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Error codes from the contract (components.schemas.Error.code).
const (
	CodeUnauthenticated = "UNAUTHENTICATED"
	CodeForbidden       = "FORBIDDEN"
	CodeNotFound        = "NOT_FOUND"
	CodeConflict        = "CONFLICT"
	CodeVersionConflict = "VERSION_CONFLICT"
	CodeValidation      = "VALIDATION_ERROR"
	CodeUnavailable     = "DEPENDENCY_UNAVAILABLE"
	CodeInternal        = "INTERNAL"
)

// Error is an API error. Message must be safe to show to the user.
type Error struct {
	Status  int
	Code    string
	Message string
	Details map[string]any
}

func (e *Error) Error() string { return fmt.Sprintf("%d %s: %s", e.Status, e.Code, e.Message) }

func Unauthenticated() *Error {
	return &Error{Status: http.StatusUnauthorized, Code: CodeUnauthenticated, Message: "Нужно войти."}
}

func Forbidden(msg string) *Error {
	return &Error{Status: http.StatusForbidden, Code: CodeForbidden, Message: msg}
}

// NotFound is also returned for objects that exist but are not visible to the subject.
func NotFound() *Error {
	return &Error{Status: http.StatusNotFound, Code: CodeNotFound, Message: "Объект не найден."}
}

// Conflict carries a machine-readable reason in details.reason.
func Conflict(reason, msg string, details map[string]any) *Error {
	d := map[string]any{"reason": reason}
	for k, v := range details {
		d[k] = v
	}
	return &Error{Status: http.StatusConflict, Code: CodeConflict, Message: msg, Details: d}
}

func VersionConflict(current int) *Error {
	return &Error{Status: http.StatusPreconditionFailed, Code: CodeVersionConflict,
		Message: "Черновик изменён в другом месте. Обновите редакцию.", Details: map[string]any{"currentVersion": current}}
}

func Validation(msg string, fields ...string) *Error {
	e := &Error{Status: http.StatusUnprocessableEntity, Code: CodeValidation, Message: msg}
	if len(fields) > 0 {
		e.Details = map[string]any{"fields": fields}
	}
	return e
}

type ctxKey int

const requestIDKey ctxKey = 0

// RequestID returns the request id stored by the middleware.
func RequestID(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "req-" + hex.EncodeToString(b)
}

// Middleware assigns a request id, logs technical fields only (no bodies) and recovers panics.
func Middleware(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" || len(id) > 64 {
			id = newID()
		}
		w.Header().Set("X-Request-ID", id)
		r = r.WithContext(context.WithValue(r.Context(), requestIDKey, id))
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		defer func() {
			if p := recover(); p != nil {
				log.Error("panic", "requestId", id, "panic", fmt.Sprint(p))
				WriteError(rec, r, fmt.Errorf("panic: %v", p))
			}
			log.Info("http", "requestId", id, "method", r.Method, "path", r.URL.Path,
				"status", rec.status, "ms", time.Since(start).Milliseconds())
		}()
		next.ServeHTTP(rec, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Handler is an http handler that may return an error.
type Handler func(w http.ResponseWriter, r *http.Request) error

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h(w, r); err != nil {
		WriteError(w, r, err)
	}
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteRawJSON writes an already encoded body (used for idempotent replays).
func WriteRawJSON(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

type errorBody struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	RequestID string         `json:"requestId"`
	Details   map[string]any `json:"details,omitempty"`
}

func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	var e *Error
	if !errors.As(err, &e) {
		slog.Error("internal error", "requestId", RequestID(r.Context()), "err", err.Error())
		e = &Error{Status: http.StatusInternalServerError, Code: CodeInternal, Message: "Внутренняя ошибка. Попробуйте ещё раз."}
	}
	WriteJSON(w, e.Status, errorBody{Code: e.Code, Message: e.Message, RequestID: RequestID(r.Context()), Details: e.Details})
}

const maxBody = 1 << 20

// DecodeJSON decodes a body strictly: unknown fields and trailing data are rejected,
// matching additionalProperties:false in the contract.
func DecodeJSON(r *http.Request, v any) error {
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		return Validation("Ожидается application/json.")
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return Validation("Некорректное тело запроса: " + err.Error())
	}
	if dec.More() {
		return Validation("Лишние данные после JSON.")
	}
	return nil
}
