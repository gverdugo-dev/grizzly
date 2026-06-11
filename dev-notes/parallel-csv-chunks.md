# Parallel CSV parsing: goroutines, chunk splitting and Amdahl's law

Learning note. Context: the v0.2.0 "parallelize CSV by chunks" milestone
(`from_csv_parallel.go`) — grizzly's first use of goroutines, and the first time
[the v0.2.0 principles](v0.2.0-principles.md) (measure first, parallelism at the
boundaries) got exercised end to end, war stories included. The result: `FromCSV`
went from 18.97ms to 5.48ms (3.4x) on the 100k-row benchmark.

## The shape: split → fan out → merge

```
file (bytes in memory)
  │
  ├─ 1. splitCSVChunks: ONE cheap scan finds safe boundaries
  │       [chunk 0][chunk 1][chunk 2] ... [chunk 9]
  │
  ├─ 2. fan out: one goroutine per chunk, each with PRIVATE builders
  │       g0 ──parse──▶ builders₀     (no locks, nothing shared)
  │       g1 ──parse──▶ builders₁
  │
  └─ 3. merge: concatenate builders in chunk order, finish once
        → columns bit-for-bit identical to the sequential path's
```

Parsing is CPU-bound (every byte triggers tokenizing, `ParseFloat`, null rules),
so chunks scale near-linearly across cores — exactly the "parallelism at the
boundaries" principle. The merge happens at the *builder* level, while values
and validity are still plain slices: concatenating finished `BoolColumn`s or
validity bitmaps would mean shifting packed bits across word boundaries;
concatenating `[]bool` is an append.

## The Go vocabulary this milestone introduced

### `sync.WaitGroup` — "wait until these N finish"

```go
var wg sync.WaitGroup
for i, c := range chunks {
    wg.Go(func() {               // spawns the goroutine AND registers it
        results[i], errs[i] = parseCSVChunk(...)
    })
}
wg.Wait()                        // blocks until every one has finished
```

`wg.Go` (Go 1.25+) fuses the classic trio — `wg.Add(1)`, `go func(){...}`,
`defer wg.Done()` — into one call, making the forgotten-`Done` deadlock
impossible. Closures capture `i` and `c` safely: since Go 1.22 each loop
iteration has its own variables (before that, this exact pattern was *the*
canonical concurrency bug — every goroutine saw the last value).

### Fan-out without channels: index-addressed results

Each goroutine writes only to **its own index** of `results[i]` and `errs[i]`.
Two goroutines never touch the same element, so there is no race and nothing to
lock. `wg.Wait()` doubles as the memory barrier: after it returns, the main
goroutine sees everything the workers wrote. Channels shine for *streams*; for
"N jobs, N results" a pre-sized slice is simpler and faster. The best
concurrency is the kind with nothing to synchronize.

### `go test -race` — the data race detector

Instruments every memory access and reports any pair of unsynchronized accesses
from different goroutines, with stack traces. It only sees races that actually
*happen* at runtime, so it is only as good as the tests that drive it — and it
costs 2-20x in speed, which is why it is a test-time flag, not a default. From
this milestone on, the suite runs with `-race` before every commit.

## The quote-parity splitter

A chunk may only start where a record starts — and you cannot cut a CSV at an
arbitrary `\n`, because a newline **inside a quoted field is data**:

```
1,"line one
line two",42        ← one record, two physical lines
```

The scan tracks **quote parity**: an even count of `"` seen so far means
*outside* quotes, odd means *inside*; only newlines outside quotes qualify as
cut points. RFC 4180 escapes a quote inside a quoted field by doubling it
(`""`), which flips the parity twice and lands back where it was — so plain
parity counting stays correct without actually parsing the CSV.

To avoid a byte-by-byte walk, the scan *hops* from quote to quote with
`bytes.IndexByte` — SIMD-accelerated in the runtime, multiple GB/s — and only
searches for `\n` inside even-parity segments. A cached next-quote position
ensures the file is scanned once in total, not once per boundary.

## War story 1: correct, balanced — pick both

The first splitter passed **every correctness test** (parallel output identical
to sequential, tricky quoted fields included) while silently producing **one**
boundary instead of nine — a termination branch pushed the scan cursor to EOF,
killing the remaining searches. Result: a 10%/90% split, effective parallelism
1.4x.

The tests could not see it; the **profile** could: ~760ms of sampled CPU over
~550ms of wall clock = 1.4 average utilization, when ~10 workers should show
~10. A performance bug is invisible to correctness tests by construction. The
fix earned its own regression test (`TestSplitCSVChunksBalance`) that asserts
the *shape of the split* — n chunks, similar sizes — not the result.

## War story 2: Amdahl collects his fee

Balanced chunks: only 2x faster. The profile now showed `growslice` (builder
slices repeatedly reallocating as they grew) and GC assist work. Amdahl's law
in one sentence: **the speedup is capped by the part you did not parallelize** —
and that part is usually allocation and copying, not compute. Two surgical
fixes:

- **Pre-sized builders**: the splitter already counts each chunk's newlines, so
  each worker knows its row count up front — `make([]float64, 0, rows)` once
  instead of grow-and-copy cycles (the capacity-hint lesson from
  [`make` and `map` in Go](make-and-maps.md), now load-bearing).
- **Pre-grown merge**: `slices.Grow` reserves the final size before
  concatenating, so the destination reallocates once instead of once per chunk.

2x → 3.4x. The remaining gap to ideal is the still-sequential tail: file read,
boundary scan, merge copy, and `csv.Reader`'s inherent per-record allocation.

## The trade-off, recorded

`FromCSV` (path) now reads the whole file into memory — random access is what
makes byte-range splitting possible — and parallelizes above 1MiB.
`FromCSVReader` (stream) stays sequential: an `io.Reader` has no random access.
Same loaded dataframe either way; pick by whether streaming matters more than
speed.

## The simple version

Photocopying a 1000-page book with ten copiers: don't hand copier #1 pages
1-100 *mid-sentence* — split at chapter ends (newlines), and mind that some
"page breaks" are fake (newlines inside quotes are part of the text). Our first
attempt at splitting gave one person 100 pages and another 900 and the
supervisor's clipboard (the profiler), not the quality inspector (the tests),
caught everyone idle. And once the copying was truly parallel, the bottleneck
became the single stapler at the end of the room (the sequential merge) —
Amdahl's law: the queue you didn't parallelize sets your ceiling.

## Further reading

- [Data Race Detector](https://go.dev/doc/articles/race_detector) — the
  official guide to `-race`: how to read its reports, the typical races it
  catches (loop counters, accidentally shared variables), and its runtime cost.
  The tool that certifies our "nothing shared" design actually shares nothing.
- [Amdahl's law](https://en.wikipedia.org/wiki/Amdahl%27s_law) — why the
  sequential fraction caps parallel speedup, with the formulas and the
  diminishing-returns curve; explains precisely why balanced chunks alone gave
  2x and killing the sequential allocations was worth another 1.4x.
