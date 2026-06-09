package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
)

// colorHandler is a minimal slog.Handler that renders human-friendly,
// colorized lines: "15:04:05 INFO message key=value key=value".
//
// It implements the level check itself (a plain comparison) instead of
// delegating to a wrapped handler, and renders both the per-record attrs
// and the attrs accumulated via WithAttrs (logger.With(...)).
type colorHandler struct {
	out   io.Writer
	level slog.Level
	attrs []slog.Attr // attrs added with logger.With(...), printed on every record
}

// Enabled reports whether records at level l should be logged.
func (h *colorHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= h.level
}

// Handle renders one log record as a single line.
func (h *colorHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder

	color := getColor(r.Level)
	fmt.Fprintf(&b, "%s \033[%sm%s\033[0m %s",
		r.Time.Format("15:04:05"), color, r.Level.String(), r.Message)

	// Attrs accumulated via WithAttrs come first, then the record's own.
	for _, a := range h.attrs {
		writeAttr(&b, a)
	}
	// Record attrs are not a slice: slog exposes them through an iterator
	// callback. Returning true means "keep iterating".
	r.Attrs(func(a slog.Attr) bool {
		writeAttr(&b, a)
		return true
	})
	b.WriteByte('\n')

	// Single Write call so concurrent goroutines can't interleave a line.
	_, err := io.WriteString(h.out, b.String())
	return err
}

// writeAttr renders one attribute as " key=value", dimmed so the message
// stays visually dominant.
func writeAttr(b *strings.Builder, a slog.Attr) {
	fmt.Fprintf(b, " \033[2m%s=%v\033[0m", a.Key, a.Value)
}

// WithAttrs returns a copy of the handler that always logs the given attrs.
//
// Note that ALL fields are copied: forgetting one would silently leave it
// at its zero value in the new handler (this was a real bug here: a copy
// without out meant a nil writer).
func (h *colorHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &colorHandler{
		out:   h.out,
		level: h.level,
		// Clip forces the append to allocate a fresh array, so two child
		// handlers created from the same parent can't overwrite each other.
		attrs: append(slices.Clip(h.attrs), attrs...),
	}
}

// WithGroup is accepted but ignored: this handler renders attrs flat.
func (h *colorHandler) WithGroup(string) slog.Handler { return h }

// getColor maps a level to an ANSI color code.
func getColor(l slog.Level) string {
	switch l {
	case slog.LevelDebug:
		return "36"
	case slog.LevelInfo:
		return "32"
	case slog.LevelWarn:
		return "33"
	case slog.LevelError:
		return "31"
	default:
		return "0"
	}
}
