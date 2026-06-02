# Roadmap

> Status: 🟡 Open for discussion
> Each phase is shippable on its own and maps to a Go concept you'll learn.

The ordering is deliberate: start with the basics of the language, end with concurrency and
profiling. Every phase produces something that runs and is tested.

## Phase 0 — Skeleton
**Goal:** project bootstrapped, one typed `Series` with `Sum()` / `Mean()`, tests passing.
**Go you learn:** project layout, `go.mod`, `testing`, basic slices.
**Done when:** `go test ./...` is green and you can sum a `Series`.

## Phase 1 — Typed columns + DataFrame
**Goal:** multiple columns of different dtypes; `Select`, `Filter`, `Head`, pretty-print.
**Go you learn:** generics vs interfaces ([03](03-type-system.md)), the hybrid model.
**Done when:** you can load a hand-built DataFrame and slice/filter it.

## Phase 2 — The engine: GroupBy + Agg
**Goal:** `df.GroupBy("k").Agg(Sum("x"), Mean("y"))` with hash aggregation.
**Go you learn:** maps, hashing, algorithm design, API design ([06](06-engine-groupby-join.md)).
**Done when:** group-by results match pandas on a test dataset.

## Phase 3 — Parallelism
**Goal:** parallelize aggregation / group-by with goroutines; measure the speedup.
**Go you learn:** goroutines, channels, `sync`, worker pools ([07](07-concurrency.md)).
**Done when:** a benchmark shows a real multi-core speedup over Phase 2.

## Phase 4 — I/O
**Goal:** `ReadCSV` with type inference and streaming; `WriteCSV`.
**Go you learn:** `io` interfaces, `encoding/csv`, idiomatic errors ([08](08-io.md)).
**Done when:** you can load a real-world CSV end-to-end and group it.

## Phase 5 — Honest benchmark
**Goal:** reproducible harness vs pandas and Polars; publish wins and losses.
**Go you learn:** `testing.B`, `pprof`, benchstat, optimization ([09](09-performance-benchmarking.md)).
**Done when:** you have numbers and a README section with a methodology.

## Phase 6+ — Stretch / "final boss" ⚪
- **Joins** (hash join, then outer variants) — [06](06-engine-groupby-join.md).
- **Validity bitmap** migration — [04](04-null-handling.md).
- **Lazy engine** + projection/predicate pushdown — [05](05-execution-model.md).
- **SIMD kernels** via `avo` — [09](09-performance-benchmarking.md).
- **Arrow export** path — [02](02-memory-model.md).

## Guiding principle

> Finish each phase before starting the next. A working Phase 2 beats a half-built Phase 6.
> Scope discipline is the whole game ([00](00-vision-scope.md)).
