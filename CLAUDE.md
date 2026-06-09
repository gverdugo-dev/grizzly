# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this project is

**grizzly** is a dataframe library written in Go from scratch. It is first and foremost a
**learning project**: Gonzalo is using it to learn Go (he is not experienced with the
language) and to understand how dataframe engines work internally.

This shapes how Claude should work here:

- **Explain while building.** When writing or changing code, explain the Go concepts
  involved (generics, interfaces, slices vs maps, zero values, error handling idioms...)
  and *why* a design was chosen over the alternatives. Short inline lessons > silent code.
- **Document all code in English** with proper Go doc comments (comment directly above
  the declaration, starting with the symbol's name). Code, identifiers, comments and
  commit messages are in English; conversation with Gonzalo is in Spanish.
- **Don't write design documents.** Gonzalo writes the design guides himself as part of
  the learning process. Claude discusses design in conversation and writes code + doc
  comments, but does not generate `.md` design/architecture docs unless explicitly asked.
- **Discuss before implementing** anything architecturally significant. The point is the
  journey, not the destination.

## Core architecture decision

**Row-oriented ingestion, column-oriented storage and processing.**

- Users *think and load data* in rows: constructors accept row-shaped input
  (Go structs, CSV files, JSON files...).
- Internally the dataframe stores data as **typed columns** (contiguous slices), because
  dataframe operations (`Sum`, `Filter`, `GroupBy`, `Join`...) are inherently columnar:
  cache locality, no per-row map/boxing overhead.
- Construction transposes rows → columns once, at the boundary. After that, everything
  is columnar.

Planned construction paths (the current focus):

- From Go structs (a slice of structs, via reflection or generics)
- From a CSV file path
- From a JSON file path

## Layout

```
grizzly/
├── dataframe.go        # Core Dataframe type (package grizzly, library root)
├── cmd/playground/     # Scratch binary to try the library out manually
└── internal/logging/   # Custom slog-based logger
```

The library is the root package (`module grizzly`). Executables live under `cmd/`.

## Commands

```bash
go build ./...        # Compile everything
go test ./...         # Run tests
go vet ./...          # Static analysis
go run ./cmd/playground   # Run the scratch playground
go doc grizzly.Dataframe  # View generated docs for a symbol
```

## Conventions

- Standard Go style: `gofmt`, exported symbols documented, errors as values
  (sentinel errors like `ErrColumnNotFound` where callers need to branch on them).
- Runnable `Example...` functions in `_test.go` files are preferred over prose docs —
  they show up in godoc *and* run as tests.
- Keep the public API small; prefer `internal/` for anything users shouldn't import.
