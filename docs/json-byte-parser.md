# Parsing JSON by hand: the byte-level loader, fuzzing as an oracle

Learning note. Context: the v0.2.0 JSON sessions ("B1" sequential rewrite +
"B2" parallelization) that took `FromJSON` from 206ms to 12.6ms on the
100k-row benchmark (16.3x) and from 3.46s to 0.37s on the external 1M-row
dataset — past polars' 0.75s. It records why grizzly stopped using
`encoding/json` in the hot path, and the verification machinery (a fuzz test
with the stdlib as oracle) that made hand-rolling a parser a sane thing to do.

## Why the stdlib had to go (from the hot path)

Profiling the old loader told an unambiguous story:

- Each per-cell `Decode` made encoding/json scan for the value's end through
  an internal path that **formats and discards an error per call** — ~36% of
  CPU and ~43% of allocations were error messages nobody ever saw.
- The obvious fix (per-value `dec.Token()`) was **falsified by benchstat**:
  Token shares the same scanning machinery AND boxes every value into an
  interface (+11% time, +10% allocs).
- `encoding/json/v2`, whose `jsontext` tokenizer fixes exactly this, is still
  behind `GOEXPERIMENT` in Go 1.26 — unusable by a library.

With every door inside the stdlib closed, the remaining move is the polars
one: own the bytes. `FromJSON` now reads the file and scans it directly;
`FromJSONReader` keeps the stdlib decoder as the streaming-correct reference —
the same two-tier shape as `FromCSV`/`FromCSVReader`.

## What "owning the bytes" bought

- **Keys without allocation**: raw key bytes compare against precomputed
  schema key bytes (`bytes.Equal`); the old path allocated a string per key.
- **Numbers**: `strconv.ParseFloat` over the exact token slice — after a
  ~30-line validator enforcing JSON's number grammar, because ParseFloat
  alone happily accepts `.5`, `1.`, `01`, `+1` and `Inf`, all of which JSON
  forbids. Looseness there would silently load files the reference path
  rejects (the "semantics do not change" principle).
- **Strings**: a fast path when escape-free — one pass that simultaneously
  rejects raw control characters and detects pure ASCII (ASCII skips even
  `utf8.Valid`) — and a slow path handling the eight escapes, `\uXXXX`,
  UTF-16 surrogate pairs, and invalid-UTF-8 coercion to U+FFFD, all
  mirroring `encoding/json`.
- **An escape-analysis gift**: `ParseFloat(string(raw))` looks like one
  allocation per number, but the compiler proves the converted string never
  escapes and keeps it on the stack. Zero allocs — measured, not assumed.
  Total: 4.9M → 100k allocs/op, which is just the unavoidable one-string-
  per-string-cell.

## Fuzzing with an oracle

Hand-rolling a parser is only sane with brutal verification. Besides
equivalence tests, `FuzzFromJSONBytes` (`go test -fuzz`) mutates millions of
inputs and holds one directional contract:

> Whatever `FromJSONReader` (stdlib) loads, `fromJSONBytes` must load
> **identically**. The reverse is weaker by design: the byte path may accept
> some malformed documents whose damage hides inside skipped non-schema
> values.

The oracle earned its keep immediately, in both directions:

- **Catch #1 (90 seconds in)**: a string containing the invalid UTF-8 byte
  `0x80`. The stdlib *coerces* invalid UTF-8 to U+FFFD; the new parser passed
  it through raw. No hand-written test would have thought of it. The input
  lives in `testdata/fuzz/` as a permanent regression case.
- **A bug in the OLD code**: the equivalence tests revealed `FromJSONReader`
  accepted documents missing the final `]` — its `for dec.More()` loop just
  stops at EOF. The reference implementation got the fix, not the new one.

After the U+FFFD fix: 7.1M executions, zero divergences.

## Parallelizing: what transferred from CSV and what didn't

The B2 chunk model is the CSV one — boundary scan, one goroutine per chunk
with private builders, ordered merge — with three JSON-specific lessons:

1. **Quote parity doesn't transfer.** CSV escapes quotes by doubling (`""`),
   which keeps parity counting correct; JSON escapes with a backslash
   (`\"`), so the splitter jumps strings with the real string scanner
   (backslash-run parity) and tracks `{}`/`[]` depth in between. A safe cut
   is a `{` at depth 1, outside any string.
2. **Bulk skipping lost to a tight loop.** The first splitter crossed
   far-from-target segments with four SIMD `bytes.Count` calls, like CSV's
   IndexByte hopping. But JSON's between-string segments (key, value, key…)
   are tiny: per-call overhead on 10-byte slices cost more than one plain
   byte loop. CSV's segments are line-sized; JSON's are token-sized. Same
   trick, different data shape, opposite verdict — measured, not guessed.
3. **Pipeline the dispatch.** Profiling showed the sequential split (17% of
   CPU) gating all twelve workers behind it. Now each worker launches the
   moment the splitter finds its chunk's end — the scan overlaps the parsing
   it feeds. Errors stay canonical by falling back to one sequential
   re-parse (absolute row numbers in messages), paid only on the error path.

## The module proxy postscript

The external re-benchmark initially measured **yesterday's commit**: `go get
module@main` resolves through `proxy.golang.org`, which caches — new
versions "may not show up right away". The fix: `GOPROXY=direct go get
module@<commit>`, and always check the pseudo-version
(`v0.1.1-0.20260611071708-10aeb13f3284` encodes date + commit) in `go.mod`
against the commit you think you are measuring. A benchmark of the wrong
code is worse than no benchmark — it looks exactly like a real one.

## The simple version

The post office (encoding/json) was opening every envelope with a ceremony:
gloves on, form filled in triplicate, form thrown away. We asked for the
ceremony-free counter and there isn't one — so we learned to open envelopes
ourselves. To be sure we open them exactly like the post office does, we
hired a tireless intern (the fuzzer) to send millions of weird letters to
both counters and compare: the intern caught us mishandling one stamp
(invalid UTF-8) in seconds — and also caught the post office accepting
letters with no closing flap (the missing `]`). Then we put ten people on
the job (B2), discovered the mail-sorting queue was making them all wait
(the up-front split), and let each one start the moment their stack was
ready (pipelined dispatch).

## Further reading

- [Tutorial: Getting started with fuzzing](https://go.dev/doc/tutorial/fuzz) —
  the official walkthrough of `go test -fuzz`: seed corpus with `f.Add`, the
  fuzz target, debugging failures; its example even revolves around UTF-8
  edge cases, the exact family of bugs our oracle caught.
- [proxy.golang.org](https://proxy.golang.org/) — the module mirror's own
  docs: what it caches and why "new versions may not show up right away",
  the small print behind the stale-`@main` benchmark trap.
