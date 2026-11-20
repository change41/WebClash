// Package util ports server/src/util.ts: errors, filesystem helpers, JSON
// encoding, randomness, password/session crypto and object merging.
package util

import (
	"os"
	"strings"

	"webclash/internal/buildinfo"
)

// AppName is the human-facing product name.
const AppName = "WebClash"

// AppVersion honors WEBCLASH_VERSION, otherwise the link-time build version.
func AppVersion() string {
	if v := strings.TrimSpace(os.Getenv("WEBCLASH_VERSION")); v != "" {
		return v
	}
	return buildinfo.Version
}

// HttpError carries an explicit HTTP status code (port of util.ts HttpError).
type HttpError struct {
	StatusCode int
	Message    string
}

func (e *HttpError) Error() string { return e.Message }

// NewHttpError builds an HttpError.
func NewHttpError(statusCode int, message string) *HttpError {
	return &HttpError{StatusCode: statusCode, Message: message}
}

// ErrorMessage renders an error's message, or "" for nil.
func ErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// TrimSpaceEnv reads an environment variable and trims surrounding whitespace.
func TrimSpaceEnv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}
