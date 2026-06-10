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

### 2. Nulls: validity bitmap, comma-ok API, SQL semantics (decided)

Null handling rests on four independent decisions, all settled (the full design
space and rationale live in [Annex: Nulls in Go](nulls-in-go.md)):

- **Representation**: a real validity bitmap (`[]uint64`, 1 bit per row), Arrow-style;
  `nil` bitmap means "no nulls" so null-free columns pay nothing.
- **API**: comma-ok — `Value(i) (T, bool)` — as the public surface; operations
  type-switch to the concrete column and read slice + bitmap directly in hot loops.
- **Type architecture**: every column is nullable (no separate `Nullable*` types).
- **Semantics**: SQL aggregate convention — `Sum`/`Mean`/`Count` skip nulls; loaders
  turn empty CSV cells / JSON `null`s into bit 0 + placeholder. Three-valued logic
  for comparisons is parked until `Filter` exists.

### 3. Three base dtypes: float64, string, bool (decided)

No integer column: every number is a float64 — the JSON/JavaScript model. One
numeric type keeps the schema, the loaders and every type-switch point small.
The documented trade-off: float64 represents integers exactly only up to 2^53
(~9·10^15), so a 17-digit ID would silently lose precision. Acceptable for the
data grizzly targets; revisit only if it ever bites.

### 4. Explicit schema on load (decided)

When loading untyped sources (CSV, JSON), the **user declares the column types** via an
explicit schema — like a database, unlike pandas. Rationale: zero ambiguity, no
guessing bugs (a `"08001"` zip code never becomes an int), and idiomatic Go
(explicit > magic). Type inference may be added later as an optional layer on top.
Struct-based construction needs no schema: types come from the struct fields themselves.

### 5. Column representation: interface + typed implementations (decided)

A `Column` interface exposing only type-agnostic behavior (`Name`, `Len`, `DType`,
`IsValid`, `NullCount`), with one concrete implementation per dtype
(`Float64Column`, `StringColumn`, `BoolColumn`) over contiguous (or packed)
buffers. Anything needing concrete values type-switches to the implementation.
This was the original leading candidate and survived contact with reality:
generics collapse into it anyway because a dataframe is heterogeneous at runtime.

### 6. Filter: columnar comparators + combinable masks (decided)

Comparators on the Dataframe (`Eq`, `Lt`, `Gt`...) produce **masks**; masks
combine with `And`/`Or`/`Not` at word level over packed bits; one final
`Where` materializes the surviving rows — the polars/NumPy model, chosen
because it builds on proven designs and makes the bitmap machinery carry the
feature. Null comparisons follow Kleene three-valued logic, implemented once
in the mask combinators (`Where` keeps only valid-and-true rows, like SQL's
WHERE). The full design space and rejected options (row predicates, typed
per-column predicates, expression strings) are mapped in
[Annex: Filter — the design space](filter-design-space.md).

### 7. GroupBy: hash-based factorize + eager Agg specs (decided)

Hash-based grouping with typed maps per dtype, producing flat
`groupIDs []int` (the factorize pattern) so aggregations run as map-free
slice-indexed passes. Eager API in execution order —
`df.GroupBy("store").Agg(grizzly.Sum("price"), ...)` — with inspectable
package-level agg specs; the SQL-written-order shape
(`Select(Sum(...)).GroupBy(...)`) is impossible without a lazy planner.
Null keys form one group (SQL/polars rule: for grouping, null equals null —
the opposite of WHERE); output rows come in first-appearance order
(deterministic despite Go's randomized map iteration). Full design space in
[Annex: GroupBy — the design space](groupby-design-space.md).

## Open questions

- None right now.

## Roadmap (versioned phases)

Versions follow semver with git tags. `v0.x` means the API can still change
freely between minors. [todo.md](../todo.md) is the task-level view of this.

### v0.1.0 — load, look, ask (in progress)

One table end-to-end: load real data, inspect it, query and transform it.

- Done: loaders (structs, CSV, JSON, with `io.Reader` variants), explicit
  schema, `String`/`Info`, nulls end-to-end, aggregations
  (`Sum`/`Avg`/`Min`/`Max`/`Count`).
- Pending: `BoolColumn` (closes the base dtypes), `Filter`, `Select`,
  `GroupBy`, `Sort`, `Example`-based tests, and the tag.

### v0.2.0 — fast

Performance, guided by profiling, not guessing (see
[Annex: Lessons from the first benchmark](first-benchmark-lessons.md)):
pprof the loaders, parallelize parsing by chunks, re-benchmark against
pandas/polars.

### v0.3.0 — relational

`Join` across dataframes, and whatever GroupBy's design forces us to revisit.

## Annexes (learning notes)

- [Stack vs Heap](stack-vs-heap.md) — the two memories of a program, Go's escape
  analysis, why `any` boxes values to the heap, and cache locality. The foundation for
  the columnar storage decision.
- [`make` and `map` in Go](make-and-maps.md) — maps as hash tables (Python dict, not
  JSON), zero values and the comma-ok idiom, why nil maps panic on write, and capacity
  hints. Underpins why `Dataframe` stores columns in an ordered slice and why
  constructors pre-size their slices.
- [Nulls in a typed columnar store](null-handling.md) — why `[]float64` has no "empty"
  state, and the three classic answers: sentinel values (old pandas' NaN scars),
  pointer slices (the heap disaster revisited), validity bitmaps (Arrow/Polars).
  Groundwork for the null-handling decision.
- [Nulls in Go: the four design decisions](nulls-in-go.md) — the Go-specific design
  space behind the null-handling decision: `[]bool` vs real `[]uint64` bitmap (bit
  twiddling, popcount, the `nil` = no-nulls trick), comma-ok vs `Null[T]` vs two-method
  APIs, always-nullable columns, and SQL skip-null semantics. Records all four
  decisions.
- [GroupBy: the design space](groupby-design-space.md) — split-apply-combine and
  the five GroupBy design axes: hash vs sort grouping, the factorize/group-ids
  pattern and multi-column keys, eager Agg-specs API vs map-of-dataframes, null
  keys as their own group, and deterministic output order despite Go's randomized
  map iteration. Underpins the (open) GroupBy design decision.
- [Filter: the design space](filter-design-space.md) — the four candidate Filter
  APIs (row predicate, typed per-column predicate, columnar masks, expression
  strings) with pros and cons, and the three-valued (Kleene) null logic that
  filtering forces. Underpins the Filter design decision (mask-based, option C).
- [Bitmaps and machine words](bitmaps-and-words.md) — the low-level vocabulary under
  the validity bitmap and `BoolColumn`'s storage choice: what a 64-bit word is, the
  `i>>6` / `i&63` arithmetic, popcount and set-bit iteration, the trailing-bits-zero
  invariant, and why a packed column needs an explicit `length` (logical length ≠
  buffer size, as in Arrow).
- [Lessons from the first benchmark](first-benchmark-lessons.md) — grizzly vs
  pandas/polars on 1M rows: beating pandas' C parser on CSV single-threaded, why
  polars' 0.032s needs parallel+SIMD, `FromJSON`'s map/boxing cost as the real weak
  point, and float summation order explaining the checksum mismatch. Seeds the
  performance roadmap in todo.md.
