# Lessons from the first benchmark (grizzly vs pandas vs polars)

Learning note. Context: first external benchmark of grizzly's loaders (separate
benchmark repo; 1M-row dataset, CSV and JSON, files warm in OS page cache for all
engines). The results reshape the optimization roadmap — they identified `FromJSON`
as the real weak point and seeded the "Performance" section of [todo.md](../todo.md).

## The numbers (medians)

| Engine  | CSV    | JSON  |
|---------|--------|-------|
| polars  | 0.032s | 0.75s |
| grizzly | 0.54s  | 3.46s |
| pandas  | 0.60s  | 1.67s |

## Lesson 1: you are not competing against Python

Python only orchestrates. `pd.read_csv` runs a C parser with ~15 years of
optimization; polars runs a multithreaded Rust engine with SIMD and the Arrow format.
The Python script makes one call and waits. So the real lineup is:

- **grizzly** — Go, single-threaded, straightforward code, a few weeks old
- **pandas** — optimized C parser behind a Python API
- **polars** — 2020s-design Rust engine: parallel, SIMD, Arrow-native

Read that way, the table tells a different story: **grizzly beats pandas' C parser on
CSV** (0.54s vs 0.60s), single-threaded and trick-free. A very dignified result for a
learning project.

## Lesson 2: how polars does 0.032s

~100MB parsed in 0.032s is ~3GB/s. That only happens with: the file already in the OS
page cache (true for everyone here — fair comparison), the file split into chunks
parsed **in parallel on all cores**, SIMD float parsing, and writing straight into
columnar Arrow memory with no intermediate steps. Go *the language* could get close;
`encoding/csv` *the standard library* cannot — it isn't designed for that.

## Lesson 3: grizzly's JSON path is the real weak point (3.46s)

There is a concrete culprit in our code: `from_json.go` deserializes into
`[]map[string]any`. For 1M rows that means:

- 1M maps allocated on the heap
- 10M values boxed in `any` (every float64 escapes to the heap)
- string-key hashing on every access
- everything through `encoding/json`'s reflection

It is exactly the layout the file's own comment rejects for storage — and we pay its
full cost during loading: ~6x slower than our own CSV parser on the same data.
"Boxing at the boundary" was the right *design* call, but the boundary is allowed to
be fast too.

## Lesson 4: the checksums differ in the last decimal — and it's not a bug

grizzly: `499828455.03102` · pandas/polars: `499828455.03099`. The parsed data is
identical; the difference is **float64 summation order**. grizzly sums sequentially
(error grows O(εn)); pandas/polars use pairwise summation — divide and conquer — whose
error grows O(ε·log n). Floating-point addition is not associative: change the order,
change the last bits. Their answer is actually the *more* accurate one.

## The simple version

We entered a street race with a homemade car and discovered the rivals are: a Python
taxi whose actual engine is a Formula 1 V8 under the hood (pandas), and a factory
Rust team running four cars in parallel that share the work (polars). Our homemade
car *still beat the taxi* on the CSV track — respectable. But on the JSON track we
lost badly, and the telemetry says why: we drive every passenger (value) to a
separate hotel across town (heap-allocated maps and boxed values) before the race
even starts. Next stop: read the telemetry properly (pprof), then stop booking
hotels (token-level decoding).

## Next steps recorded in todo.md

1. Profile the JSON path with pprof (maps + boxing should dominate the flame graph)
2. Rewrite `FromJSON` with a token-level `json.Decoder` (no intermediate maps)
3. Stream CSV with `csv.Reader` + `ReuseRecord = true` instead of `ReadAll`
4. Parallelize parsing by chunks and measure the gap to polars again

## Further reading

- [Profiling Go Programs](https://go.dev/blog/pprof) — the canonical Go blog post on
  `go tool pprof`: finds bottlenecks in a real program and lands an 11x speedup,
  largely by removing maps and reusing memory — almost literally our JSON to-do list.
- [Pairwise summation](https://en.wikipedia.org/wiki/Pairwise_summation) — why
  summation order changes float results and why pairwise error grows O(ε·log n) vs
  O(εn) sequential; explains the checksum mismatch (and why pandas/polars are more
  accurate, not just different).
