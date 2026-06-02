# 09 — Performance & Benchmarking

> Status: 🟡 Open for discussion
> Affects: the "is it fast?" claim, the optimization phase

## The question

How do we measure performance honestly, find the real bottlenecks, and how far do we push
optimization (down to SIMD)?

## Why it matters

"It feels fast" is worthless. Go has first-class benchmarking and profiling built in —
learning to *measure before optimizing* is one of the most valuable habits the language
teaches. And our whole pitch ("beats pandas") has to be backed by numbers.

## Measure first

- **`testing.B` benchmarks** — `go test -bench=. -benchmem` for ns/op and allocs/op.
- **`pprof`** — CPU and memory profiles to find the actual hot spot (it's never where you
  guess). Flame graphs via `go tool pprof`.
- **Benchstat** — compare before/after runs with statistical significance, not eyeballing.

Rule: **no optimization without a benchmark that proves it helped.**

## The optimization ladder (cheap → expensive)

1. **Algorithm** — better big-O beats any micro-tuning (e.g. hash join vs nested loop).
2. **Allocations** — reuse buffers, `sync.Pool`, preallocate slices with known capacity.
   GC pressure is Go's #1 perf tax for data work.
3. **Parallelism** — see [07](07-concurrency.md). Multi-core is our edge over pandas.
4. **Memory layout** — columnar + validity bitmap ([04](04-null-handling.md)) for cache use.
5. **SIMD** — last resort, big payoff for numeric kernels. Go has no portable SIMD; options:
   - Go assembly (`.s` files) — full control, painful.
   - **`avo`** — generate Go assembly from Go code, far more maintainable.
   - Accept the LLVM/SIMD gap vs Polars (see [00](00-vision-scope.md)) and not chase it.

## The honest benchmark

A reproducible harness comparing grizzly vs pandas vs Polars on the **same** dataset and
ops (groupby+agg, filter, join, CSV load), reporting time *and* memory. Document the
hardware. Publish wins *and* losses — credibility comes from honesty.

## Open questions

- [ ] What's the canonical benchmark dataset/size? (Borrow Polars' PDS-H-style queries?)
- [ ] How far down the ladder do we go for the learning goal? (Leaning: through #4; SIMD only as a curiosity.)
- [ ] Do we track benchmarks over time (CI regression detection) or run them ad hoc?

## References

- [`testing` — Benchmarks](https://pkg.go.dev/testing#hdr-Benchmarks) — how to write `func BenchmarkXxx(b *testing.B)`.
- [Profiling Go Programs (Go blog)](https://go.dev/blog/pprof) — using pprof to find real bottlenecks.
- [avo](https://github.com/mmcloughlin/avo) — generate Go assembly (the sane path to SIMD).
- [Go assembly reference](https://go.dev/doc/asm) — if we ever hand-write kernels.
