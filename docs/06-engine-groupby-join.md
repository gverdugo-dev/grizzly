# 06 — Engine: GroupBy, Join & Aggregations

> Status: 🟡 Open for discussion
> Affects: the core value of the library

## The question

How do we implement the operations that make a DataFrame useful — `group by`, `join`,
and the aggregations on top of them?

## Why it matters

Anyone can write `Sum([]float64)`. The engine — turning `df.GroupBy("city").Agg(Mean("temp"))`
into something correct and fast — is where a DataFrame earns its name. It's also where
the meaty algorithms (hashing, partitioning) live.

## GroupBy + Aggregate

The standard approach is **hash aggregation**:

1. Build a hash map from group key → an accumulator (or row indices).
2. Scan the key column(s) once, routing each row to its group.
3. Fold each group's values into the aggregator (sum, mean, count, min, max…).

```go
df.GroupBy("city").Agg(
    Sum("sales"),
    Mean("temp"),
    Count(),
)
```

Design decisions:
- **Key encoding:** single key is easy; composite keys need a combined hash (concatenate /
  hash-combine). Go's built-in `map` is the obvious v1; a custom open-addressing table is a
  later optimization.
- **Accumulator design:** one aggregator object per (group, measure), updated streaming —
  avoids materializing per-group slices.

## Join

Start with **hash join** (inner first):
1. Build a hash table on the smaller side's key.
2. Probe with the larger side, emit matched rows.

Then extend to left / right / outer. Sort-merge join is a possible later alternative for
pre-sorted data.

## Aggregations to support (v1)

`sum`, `mean`, `count`, `min`, `max` — over numeric columns, null-aware (see [04](04-null-handling.md)).

## Open questions

- [ ] Go's built-in `map` for grouping (simple, decent) vs a hand-rolled open-addressing hash table (faster, far more educational)? Probably: `map` first, hand-rolled as an optimization exercise.
- [ ] How do composite keys get hashed without allocating a string per row?
- [ ] Index-based grouping (store row indices per group) vs streaming accumulators (no indices)? Trade-off: flexibility vs memory.
- [ ] Sorting: which algorithm, and stable vs unstable? `sort.Slice` first, radix sort for ints later?

## References

- [Hash join (Wikipedia)](https://en.wikipedia.org/wiki/Hash_join) — the build/probe algorithm we start from.
- [Go maps in action (Go blog)](https://go.dev/blog/maps) — how Go's built-in map works, our v1 grouping structure.
- [Apache Arrow — Columnar Format](https://arrow.apache.org/docs/format/Columnar.html) — why columnar layout makes these scans cache-efficient.
