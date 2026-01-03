package errors

import "net/http"

type HTTPError struct {
	StatusCode int
	Message    string
}

func (e *HTTPError) Error() string { return e.Message }

func BadRequest(msg string) *HTTPError { return &HTTPError{StatusCode: http.StatusBadRequest, Message: msg} }

func Unauthorized(msg string) *HTTPError { return &HTTPError{StatusCode: http.StatusUnauthorized, Message: msg} }

func Forbidden(msg string) *HTTPError { return &HTTPError{StatusCode: http.StatusForbidden, Message: msg} }

func NotFound(msg string) *HTTPError { return &HTTPError{StatusCode: http.StatusNotFound, Message: msg} }

func Internal(msg string) *HTTPError { return &HTTPError{StatusCode: http.StatusInternalServerError, Message: msg} }

