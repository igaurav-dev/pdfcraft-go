package pdfcraft

import (
	"encoding/json"
	"fmt"
)

// Error is every failure this client returns from an API call. Switch on Code,
// never on the message — the code is the stable part of the contract and the
// message is written for a human to read.
//
// Mirrors the TypeScript and Python SDKs deliberately, down to which statuses
// count as retryable, so a bug report against one can be read by someone
// holding another.
type Error struct {
	Code    string
	Message string
	Status  int
	DocsURL string
	// Retryable is true for a network failure, a 429, or any 5xx. A 4xx other
	// than 429 will fail identically however many times it is asked.
	Retryable bool
}

func (e *Error) Error() string {
	if e.Status == 0 {
		return fmt.Sprintf("pdfcraft: %s: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("pdfcraft: %s (HTTP %d): %s", e.Code, e.Status, e.Message)
}

// IsRetryable reports whether err is a PDFCraft error worth retrying. Useful
// in a caller's own retry loop when they have disabled ours.
func IsRetryable(err error) bool {
	var e *Error
	if As(err, &e) {
		return e.Retryable
	}
	return false
}

func newError(code, message string, status int, docsURL string) *Error {
	return &Error{
		Code:      code,
		Message:   message,
		Status:    status,
		DocsURL:   docsURL,
		Retryable: code == "network_error" || status == 429 || status >= 500,
	}
}

type wireError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		DocsURL string `json:"docs_url"`
	} `json:"error"`
}

// toError turns an error response body into a typed error.
//
// A non-JSON body is tolerated on purpose: a proxy in front of the API returns
// an HTML error page, and a client that fails to parse it would hide the
// status code that actually explains the problem.
func toError(status int, body []byte) *Error {
	var parsed wireError
	_ = json.Unmarshal(body, &parsed)

	code := parsed.Error.Code
	if code == "" {
		code = "internal_error"
	}
	message := parsed.Error.Message
	if message == "" {
		message = fmt.Sprintf("PDFCraft responded %d", status)
	}
	return newError(code, message, status, parsed.Error.DocsURL)
}
