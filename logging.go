package grizzly

import "log/slog"

// logger is the package-wide logger used by grizzly internals.
//
// A library must be silent by default: users who import grizzly should see
// no output unless they opt in. DiscardHandler drops every record with
// near-zero cost (Enabled returns false, so log calls short-circuit before
// formatting anything).
var logger = slog.New(slog.DiscardHandler)

// SetLogger routes grizzly's internal logs to the given logger.
//
// By default grizzly is silent. Call this to see what the library is doing,
// e.g. for debugging:
//
//	grizzly.SetLogger(slog.Default())
func SetLogger(l *slog.Logger) {
	if l != nil {
		logger = l
	}
}
