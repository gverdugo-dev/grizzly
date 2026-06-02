# 00 — Vision & Scope

> Status: 🟡 Open for discussion
> Affects: everything

## The question

What *is* grizzly, what does it deliberately **not** try to be, and how do we keep the
scope small enough to actually finish?

## Why it matters

Every existing Go DataFrame (gota, qframe, dataframe-go) either stalled or stayed a toy.
The usual cause is unbounded scope: trying to be pandas + Spark + a SQL engine at once.
Defining what we *won't* build is more important than defining what we will.

## The vision

> **The native Go DataFrame.** Zero Python, zero Rust FFI, one static binary, goroutine
> parallelism, built for data pipelines embedded inside Go services.

We are **not** trying to be "the fastest DataFrame" — Polars (Rust + SIMD + no GC) and
DuckDB own that ceiling. Competing on raw speed is a losing game. We compete on *fit*:
idiomatic Go, trivial deployment, easy concurrency.

Against pandas, though, the bar is low (single-threaded Python). Beating pandas is a
realistic milestone; getting within a few × of Polars is an ambitious stretch goal.

## Goals

- Learn Go deeply by implementing the hard parts by hand.
- A columnar, in-memory DataFrame with a clean, idiomatic API.
- `select` / `filter` / `group by` / `agg` / `join` / `sort`.
- CSV in/out; Parquet later.
- Honest benchmarks vs pandas and Polars.

## Non-goals (for now)

- ⚪ Distributed / out-of-core execution (no Spark replacement).
- ⚪ A SQL front-end.
- ⚪ Lazy evaluation & a query optimizer (parked until late — see [05](05-execution-model.md)).
- ⚪ Exotic dtypes (categoricals, nested, decimals). Start with 4: `int64`, `float64`, `string`, `bool`.
- ⚪ Beating Polars on raw speed.

## Open questions

- [ ] Is the audience "me, learning" only, or do we want it usable by others from day one?
- [ ] Do we publish it (public GitHub) or keep it private while it's rough?
- [ ] Minimum Go version? (Generics need 1.18+; we'll likely target a recent stable.)

## References

- [pandas — About](https://pandas.pydata.org/about/) — what pandas is built from (Python, Cython, C).
- [Polars](https://pola.rs/) — the performance bar we're measuring against, not chasing.
