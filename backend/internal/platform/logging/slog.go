package logging

import (
	"log/slog"

	"github.com/go-logr/logr"
)

// NewSlogLogger wraps logger for libraries that accept only a *slog.Logger,
// such as acp-go-sdk, so their lines land in the structured log stream.
func NewSlogLogger(logger Logger) *slog.Logger {
	return slog.New(logr.ToSlogHandler(logr.New(NewLogrSink(logger))))
}
