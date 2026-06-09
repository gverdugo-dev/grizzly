# grizzly — living library document

The single living document of the library. Design decisions, open questions and
learning-note annexes live (or are linked) here. Updated as the project evolves.

## What grizzly is

A dataframe library written in Go from scratch, as a learning project: the goal is to
learn Go and to understand how dataframe engines work internally — not to compete with
existing libraries.

## Core design decisions

### 1. Row-oriented ingestion, column-oriented storage (decided)

Users think and load data in **rows**; the engine stores and processes **columns**.

- Constructors accept row-shaped input: Go structs, CSV paths, JSON paths.
- Internally, data lives in typed contiguous slices (`[]float64`, `[]string`...),
  because dataframe operations (`Sum`, `Filter`, `GroupBy`, `Join`) are inherently
  columnar and contiguous memory is what makes them fast.
- Construction transposes rows → columns once, at the boundary.

Why not `map[string]any` per row: per-entry map overhead, hash lookup on every access,
and every value boxed on the heap — see [Annex: Stack vs Heap](stack-vs-heap.md).

The guiding principle: **the more the compiler knows at compile time (types, sizes,
lifetimes), the faster the program** — typed columns are that principle applied.

### 2. Explicit schema on load (decided)

When loading untyped sources (CSV, JSON), the **user declares the column types** via an
explicit schema — like a database, unlike pandas. Rationale: zero ambiguity, no
guessing bugs (a `"08001"` zip code never becomes an int), and idiomatic Go
(explicit > magic). Type inference may be added later as an optional layer on top.
Struct-based construction needs no schema: types come from the struct fields themselves.

## Open questions

- **Column representation in Go** — leading candidate: a `Column` interface with typed
  implementations (`Float64Column`, `StringColumn`...) over a closed set of types
  (`float64`, `int64`, `string`, `bool`). Generics collapse into this anyway because a
  dataframe is heterogeneous at runtime.
- **Null handling** — not designed yet.

## Annexes (learning notes)

- [Stack vs Heap](stack-vs-heap.md) — the two memories of a program, Go's escape
  analysis, why `any` boxes values to the heap, and cache locality. The foundation for
  the columnar storage decision.
- [`make` and `map` in Go](make-and-maps.md) — maps as hash tables (Python dict, not
  JSON), zero values and the comma-ok idiom, why nil maps panic on write, and capacity
  hints. Underpins why `Dataframe` stores columns in an ordered slice and why
  constructors pre-size their slices.
