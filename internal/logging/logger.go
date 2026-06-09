package logging

import (
	"log/slog"
	"os"
)

// New returns a colorized, human-friendly logger writing to stderr,
// showing records at or above the given level.
func New(level slog.Level) *slog.Logger {
	return slog.New(&colorHandler{out: os.Stderr, level: level})
}
