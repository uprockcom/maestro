package api

import "fmt"

// Error is a structured API error with an HTTP status code.
type Error struct {
	Status  int    `json:"-"`
	Message string `json:"error"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("api error %d: %s", e.Status, e.Message)
}

// ErrorResponse is the wire format for error responses.
type ErrorResponse struct {
	Error string `json:"error"`
}

// Sentinel errors for common cases.
var (
	ErrStateHashMismatch = &Error{Status: 409, Message: "state has changed since list was fetched"}
	ErrNotFound          = &Error{Status: 404, Message: "not found"}
)
