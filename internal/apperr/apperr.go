// Package apperr defines typed domain errors that carry a Kind, which maps
// to an HTTP status code at the API boundary.
package apperr

import (
	"errors"
	"net/http"
)

// Code classifies a domain error.
type Code int

const (
	// KindInternal is the zero value and the fallback for unclassified errors.
	KindInternal Code = iota
	KindInvalid
	KindUnauthorized
	KindForbidden
	KindNotFound
	KindConflict
)

// Error is a domain error carrying a Code and an optional wrapped cause.
type Error struct {
	code Code
	msg  string
	err  error
}

// New creates an Error with the given code and message.
func New(code Code, msg string) *Error {
	return &Error{code: code, msg: msg}
}

// Wrap creates an Error with the given code and message that wraps cause.
func Wrap(code Code, msg string, cause error) *Error {
	return &Error{code: code, msg: msg, err: cause}
}

func (e *Error) Error() string {
	if e.err != nil {
		return e.msg + ": " + e.err.Error()
	}
	return e.msg
}

// Unwrap exposes the wrapped cause for errors.Is/As.
func (e *Error) Unwrap() error { return e.err }

// Kind returns the Code of err, searching the wrapped chain. Errors that are
// not (and do not wrap) an *Error are treated as KindInternal.
func Kind(err error) Code {
	var e *Error
	if errors.As(err, &e) {
		return e.code
	}
	return KindInternal
}

// HTTPStatus maps an error to an HTTP status code via its Kind.
func HTTPStatus(err error) int {
	switch Kind(err) {
	case KindInvalid:
		return http.StatusBadRequest
	case KindUnauthorized:
		return http.StatusUnauthorized
	case KindForbidden:
		return http.StatusForbidden
	case KindNotFound:
		return http.StatusNotFound
	case KindConflict:
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}
