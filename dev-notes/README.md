# grizzly — living library document

The single living document of the library. Design decisions, open questions and
learning-note annexes live (or are linked) here. Updated as the project evolves.

> These are the **dev notes**: the engineering journey, written for learning.
> The official user-facing documentation lives in [docs/](../docs/README.md),
> which distills the design decisions recorded here.

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

- **Join design** — the six axes (algorithm, join types in scope, duplicate
  keys, null keys, name collisions, API shape) are mapped with leading
  candidates in [Annex: Join — the design space](join-design-space.md);
  decisions pending the design discussion.

## Roadmap (versioned phases)

Versions follow semver with git tags: patch = fixes only, minor = new API
(one milestone per minor), 1.0 deferred until the core API has settled. `v0.x`
means the API can still change freely between minors. The full rationale and
the strategies considered are in
[Annex: Semver and Go modules](semver-and-go-modules.md).
[todo.md](../todo.md) is the task-level view of this.

### v0.1.0 — load, look, ask (done — tagged 2026-06-10)

One table end-to-end: load real data, inspect it, query and transform it.

- Done: loaders (structs, CSV, JSON, with `io.Reader` variants), explicit
  schema, `String`/`Info`, nulls end-to-end, aggregations
  (`Sum`/`Avg`/`Min`/`Max`/`Count`), `BoolColumn`, `Filter` (masks + `Where`),
  `Select`, `GroupBy`/`Agg`, `Sort`/`SortDesc`, `ToCSV`/`ToJSON` writers
  (mirror of the loaders; JSON round-trips exactly, CSV's string-null
  asymmetry is documented), unit tests and runnable `Example` tests over
  the public API, clean-code audit (healthy), `columnBuilder` refactor
  (per-dtype loading knowledge in one place), custom log handler removed
  (stdlib `log/slog` only).

### v0.2.0 — fast (done — tagged 2026-06-11)

Performance, guided by profiling, not guessing. Principles in
[Annex: v0.2.0 performance principles](v0.2.0-principles.md); the journey in
[Annex: Parallel CSV parsing](parallel-csv-chunks.md) and
[Annex: Parsing JSON by hand](json-byte-parser.md).

- Done: benchmark harness + benchstat baseline, parallel CSV by chunks
  (file load 0.54s → 0.15s on the external 1M-row benchmark), byte-level
  JSON parser + parallel chunks (3.46s → 0.37s — ahead of polars' 0.75s),
  byte-level writers (~395k allocs → 13, same bytes out), pairwise
  summation in `Sum`/`Avg` (error O(ε·log n), checksum now matches
  pandas/polars, and 21% faster), fuzz tests with stdlib oracles.
- The one external gap left on purpose: polars' CSV (0.032s) needs SIMD
  parsing and Arrow-native memory — out of scope for grizzly.

### v0.3.0 — relational

`Join` across dataframes, and whatever GroupBy's design forces us to revisit.
Design space mapped in [Annex: Join — the design space](join-design-space.md);
decisions open (see Open questions).

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
- [v0.2.0 performance principles](v0.2.0-principles.md) — the rules of the "fast"
  phase: measure before touching, fast sequential before parallel, parallelism at
  the boundaries (CPU-bound parsing) not in the kernels (memory-bound), semantics
  intact, stdlib only, API stable. Plus the planned improvements per area and the
  new Go vocabulary (goroutines, WaitGroup, channels, pprof, benchstat). Underpins
  the v0.2.0 roadmap entry.
- [Parallel CSV parsing](parallel-csv-chunks.md) — grizzly's first goroutines:
  the split → fan-out → merge shape, `sync.WaitGroup.Go` and lock-free
  index-addressed results, the quote-parity chunk splitter (IndexByte hopping,
  RFC 4180), the `-race` detector, and two war stories — the correct-but-lopsided
  splitter only the profiler caught, and Amdahl's fee paid with pre-sized
  builders (2x → 3.4x). Underpins the parallel `FromCSV` implementation.
- [Parsing JSON by hand](json-byte-parser.md) — why encoding/json had to leave
  the hot path (discarded internal errors, boxing, no public escape hatch), the
  byte-level parser (allocation-free keys, strict number grammar, escape
  analysis keeping `string(raw)` on the stack), fuzzing with the stdlib as
  oracle (the invalid-UTF-8 catch, and the missing-`]` bug it found in the OLD
  code), chunk boundaries by bracket depth, pipelined worker dispatch, and the
  module-proxy stale-`@main` trap. Underpins the byte-level `FromJSON`
  implementation.
- [Join: the design space](join-design-space.md) — the six axes of the Join
  design: hash vs sort-merge (and how `factorizeSlice`/`gatherRows` already
  cover most of a hash join), join-type scope (inner + left first), SQL
  duplicate-key semantics, why GroupBy's null==null rule must NOT transfer to
  joins (partitioning vs comparison — with polars' 0.20 default flip as the
  cautionary tale), key coalescing and `_right` suffixes, and the Go API shape
  (per-type methods vs a JoinType enum). Underpins the (open) Join design
  decision.
- [Semver and Go modules](semver-and-go-modules.md) — what each version number
  measures (API surface, not effort), how Go's toolchain acts on the numbers
  (`go get -u=patch`, MVS, pseudo-versions, the `/v2` import-path rule), and the
  four pre-1.0 strategies with pros and cons (strict semver vs Cargo-style
  shifted-down vs early 1.0 vs perpetual 0.x, with polars' history as the worked
  example). Underpins the roadmap's versioning policy and the Join = v0.3.0
  decision.
- [Pairwise summation](pairwise-summation.md) — float64 addition is not
  associative: why the sequential loop's error grows O(ε·n) and the halving
  tree's O(ε·log n), NumPy's 128-element leaves, the 200-bit math/big oracle
  test, the checksum now matching pandas/polars, and the bonus 21% speedup
  (direct leaf loops vs the range-over-func iterator). Underpins `Sum`/`Avg`'s
  pairwise implementation — the documented exception to principle 4.
