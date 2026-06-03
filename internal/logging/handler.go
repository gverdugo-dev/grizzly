package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
)

type colorHandler struct {
	out   io.Writer
	inner slog.Handler
}

func (h *colorHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h *colorHandler) Handle(ctx context.Context, r slog.Record) error {
	color := getColor(r.Level)
	level := "\033[" + color + "m" + r.Level.String() + "\033[0m"

	_, err := fmt.Fprintf(h.out, "%s %s %s\n", r.Time.Format("15:04:05"), level, r.Message)
	return err
}

func (h *colorHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &colorHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *colorHandler) WithGroup(name string) slog.Handler {
	return &colorHandler{inner: h.inner.WithGroup(name)}
}

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
