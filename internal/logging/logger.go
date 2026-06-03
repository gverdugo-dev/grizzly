package logging

import (
	"log/slog"
	"os"
)

func New(level slog.Level) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}
	inner := slog.NewTextHandler(os.Stderr, opts)
	return slog.New(&colorHandler{inner: inner, out: os.Stderr})
}
