# 07 — Concurrency & Parallelism

> Status: 🟡 Open for discussion
> Affects: performance, the "why Go" story

## The question

How do we use Go's concurrency (goroutines, channels, `sync`) to parallelize the engine —
and where is parallelism actually worth it?

## Why it matters

This is grizzly's headline advantage over pandas (single-threaded) and the most enjoyable
part of Go to learn. But naive parallelism is *slower* (scheduling + synchronization
overhead), so the lesson is knowing **when** to parallelize, not just how.

## Where parallelism pays off

- **Aggregations over big columns** — partition the rows, sum each partition in its own
  goroutine, combine the partials. Classic map-reduce.
- **GroupBy** — partition by hash of the key, each worker owns a disjoint set of groups,
  then merge. (Avoids lock contention on a shared map.)
- **Independent columns** — apply a per-column transform across columns concurrently.
- **CSV parsing** — parse chunks in parallel (tricky with quoted newlines — see [08](08-io.md)).

Where it does **not** pay off: tiny data (overhead > work), inherently sequential ops.

## Patterns to learn (in order)

1. **`sync.WaitGroup`** — fan out N goroutines over N partitions, wait for all.
2. **Worker pool** — fixed workers pulling chunks off a channel (bounded parallelism,
   usually `GOMAXPROCS`).
3. **Fan-out / fan-in pipeline** — channels connecting stages.
4. **`errgroup`** — worker pool with first-error propagation and cancellation.

```go
// parallel sum sketch
func parallelSum(data []float64, workers int) float64 {
    chunk := (len(data) + workers - 1) / workers
    partials := make([]float64, workers)
    var wg sync.WaitGroup
    for w := 0; w < workers; w++ {
        lo, hi := w*chunk, min((w+1)*chunk, len(data))
        wg.Add(1)
        go func(w, lo, hi int) {
            defer wg.Done()
            var s float64
            for _, v := range data[lo:hi] { s += v }
            partials[w] = s
        }(w, lo, hi)
    }
    wg.Wait()
    var total float64
    for _, p := range partials { total += p }
    return total
}
```

## Open questions

- [ ] A global parallelism threshold (only parallelize above N rows) — tune by benchmark.
- [ ] Worker count: `GOMAXPROCS` default, user-overridable? A package-level config?
- [ ] False sharing on the `partials` slice (adjacent cache lines) — pad accumulators?
- [ ] Memory model gotchas: any shared mutable state needs `sync` or disjoint ownership. Lean hard on *disjoint partitions* to avoid locks entirely.

## References

- [Go Concurrency Patterns: Pipelines and cancellation (Go blog)](https://go.dev/blog/pipelines) — fan-out/fan-in, the core pattern for the engine.
- [`sync` package](https://pkg.go.dev/sync) — `WaitGroup`, `Mutex`, `Once`, the primitives.
- [`golang.org/x/sync/errgroup`](https://pkg.go.dev/golang.org/x/sync/errgroup) — bounded parallelism with error handling.
- [The Go Memory Model](https://go.dev/ref/mem) — what's guaranteed when goroutines share data (read before getting clever).
