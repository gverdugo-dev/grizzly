# 02 — Memory Model

> Status: 🟡 Open for discussion
> Affects: everything downstream — this is the foundation

## The question

How do we lay out data in memory? Row-oriented or columnar? And do we build the columnar
storage **by hand** or sit on top of Apache Arrow Go?

## Why it matters

The memory model decides cache behaviour, how easy vectorization is, and how the whole
API feels. For a *learning* project, it's also the single richest Go lesson: slices,
memory layout, and how the runtime manages allocations.

## Columnar, not row-oriented

Analytical workloads (sum a column, group by a key) touch whole columns, not whole rows.
Storing each column as a contiguous typed slice (`[]float64`) means:

- **Cache-friendly** sequential scans.
- **SIMD-friendly** later (homogeneous data).
- Per-column compression and null tracking.

This is what Arrow, Polars, and modern pandas all do. We follow suit.

## Build by hand vs Apache Arrow Go

This is the real fork, given the learning goal:

### Option A — Build the columnar core by hand ✅ (leaning)
- A `Series` is roughly `{ data []T, valid []bool, name string }`.
- A `DataFrame` is an ordered set of equal-length `Series`.
- **Pro:** *This is the lesson.* You learn slices, memory, and the design space.
- **Con:** You reinvent things Arrow gives free; no zero-copy interop with the Arrow world.

### Option B — Sit on Apache Arrow Go
- **Pro:** Free, battle-tested columnar buffers; interop with Parquet/Polars/DuckDB.
- **Con:** Hides the most educational part. Wrong choice for *learning by hand*.

**Current leaning:** **Option A** for the core. Keep an Arrow *export* path as a far-future
"later" so we don't paint ourselves into a corner, but write the storage ourselves.

## Sketch

```go
type Series struct {
    name  string
    data  any      // []int64 | []float64 | []string | []bool  (see doc 03)
    valid []bool   // nil = no nulls; else per-element validity (see doc 04)
}

type DataFrame struct {
    cols  []*Series
    byName map[string]int
}
```

## Open questions

- [ ] Do we mirror pandas' `BlockManager` (group same-dtype columns) or keep one slice per column (simpler)? Leaning: one-slice-per-column for clarity.
- [ ] Immutable (qframe-style, copy-on-write) or mutable DataFrames? Immutability is safer with goroutines.
- [ ] Do we need an explicit row index / labels like pandas, or is positional enough? (pandas' index is a major complexity source.)

## References

- [Apache Arrow — Columnar Format spec](https://arrow.apache.org/docs/format/Columnar.html) — the reference design for columnar memory (we learn it, then build our own).
- [Go Slices: usage and internals](https://go.dev/blog/slices-intro) — how Go slices and their backing arrays actually work.
- [The pandas BlockManager](https://uwekorn.com/2020/05/24/the-one-pandas-internal.html) — the "group columns by dtype" approach and its costs.
