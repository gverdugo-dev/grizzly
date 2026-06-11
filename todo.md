# todo

Working checklist of the project. The living design document is
[dev-notes/README.md](dev-notes/README.md) (the roadmap section defines the phases);
this is just the task view of it.

## Done

- [x] Core architecture decision: row-oriented ingestion, column-oriented storage
- [x] `Column` interface + typed implementations (`Float64Column`, `StringColumn`)
- [x] `Dataframe` over ordered columns: validated constructor, lookup, `Sum`
- [x] `String()` (fmt.Stringer, truncated table) and `Info()` (pandas-like, with memory)
- [x] Library-internal logger, silent by default, opt-in via `SetLogger`
- [x] Fix colorHandler (render attrs, WithAttrs field copy)
- [x] `FromStructs` (generics + reflection + struct tags)
- [x] Explicit `Schema` (ordered fields, no type inference) — design decision
- [x] `FromCSV` (header + schema, parse errors with line/column context)
- [x] `FromJSON` (array of objects + schema)
- [x] Docs system: living doc + learning notes + `learning-note` skill
- [x] Learning notes: stack vs heap · make and maps · null handling · nulls in Go
- [x] First benchmark vs pandas/polars (1M rows: beats pandas on CSV; JSON 6x slower
      than own CSV — culprit: `[]map[string]any` deserialization)
- [x] Rewrite `FromJSON` with token-level `json.Decoder` (no intermediate maps)
- [x] Stream CSV: `csv.Reader` + `ReuseRecord = true` instead of `ReadAll`
- [x] Module path rename to `github.com/gverdugo-dev/grizzly`
- [x] **Nulls: design decided** — real `[]uint64` bitmap (`nil` = no nulls), comma-ok
      `Value(i) (T, bool)` API, always-nullable columns, SQL skip-null semantics.
      See [dev-notes/nulls-in-go.md](dev-notes/nulls-in-go.md)
- [x] **Nulls: implement** — bitmap + comma-ok `Value` on columns; `WithNulls`
      constructors ([]bool mask at the boundary); JSON `null` and empty CSV
      float cells → bit 0 (CSV `""` in string columns stays a value, unlike
      pandas); `Sum` skips nulls via set-bit iteration; `String` renders
      `null`; `Info` shows non-null counts and bitmap memory. Bonus: loaders
      split into `FromCSVReader`/`FromJSONReader` (path variants are sugar)
- [x] Aggregations: `Avg` (SQL naming, not pandas' mean), `Min`, `Max`, `Count` —
      null-aware (skip nulls, `Avg` divides by valid count, `ErrNoValidValues`
      over empty/all-null columns), sharing the bitmap walk via the
      `validValues` iterator (range-over-func)
- [x] Dtype decision: float64 · string · bool only, no integer column (numbers
      are float64, exact up to 2^53 — the JSON model)

## v0.1.0 — load, look, ask

- [x] `BoolColumn` — packed values (1 bit per row, Arrow-style), explicit
      logical `length` field (buffer words ≠ rows), hand-written bounds
      checks; loads from CSV (`ParseBool`, empty = null), JSON (literal
      null) and structs (`reflect.Bool`). `FromStructs` moved to the
      finish-closure pattern along the way. See
      [dev-notes/bitmaps-and-words.md](dev-notes/bitmaps-and-words.md)
- [x] `Filter`: design decided — columnar comparators (`Eq`/`Lt`/`Gt`...) →
      combinable masks (`And`/`Or`/`Not`, word-level, Kleene null logic) →
      `Where` materializes. See
      [dev-notes/filter-design-space.md](dev-notes/filter-design-space.md)
- [x] `Filter`: implement — `Mask` (packed bits + validity), comparators
      `Eq`/`Ne`/`Lt`/`Le`/`Gt`/`Ge` (generic `cmp.Ordered` core; bool gets
      Eq/Ne from its packed words), word-level Kleene `And`/`Or`/`Not`,
      `Where` gathers valid-AND-true rows in one materialization
- [x] `Select` — column projection and reorder, O(cols): shares the Column
      pointers (safe: columns are immutable after construction), duplicate
      and missing names error via NewDataframe revalidation
- [x] `GroupBy`: design decided — hash factorize (typed maps → `groupIDs []int`),
      eager `GroupBy(...).Agg(specs...)`, null keys = one group, first-appearance
      order. See [dev-notes/groupby-design-space.md](dev-notes/groupby-design-space.md)
- [x] `GroupBy`: implement — generic `factorizeSlice[T comparable]` (+ map-free
      bool variant), `GroupedDataframe` with deferred error (sql.Row.Scan
      pattern, keeps the `GroupBy().Agg()` chain compiling), package-level agg
      specs, map-free per-group passes, `gatherRows` primitive (reusable by
      `Sort`). Single key column; multi-key later via id combination
- [x] `Sort` / `SortDesc` — stable permutation sort (slices.SortStableFunc +
      cmp.Compare over row indices, generic cmpRows for float64/string),
      nulls first in both directions (polars' rule), columns reordered via
      gatherRows; original dataframe untouched
- [x] Tests — unit tests per source file (white-box `bitmap_test.go` in package
      `grizzly`; everything else black-box in `grizzly_test`): bitmap word math
      and partial last word, column constructors and comma-ok, the three loaders
      (CSV/JSON null rules, ParseBool, errors), aggregations (skip-null,
      sentinels via `errors.Is`), Kleene mask edges observed through `Where`
      composition, GroupBy (first-appearance determinism ×20, null keys = one
      group, deferred errors), Sort (nulls first both ways, stability)
- [x] `Example`-based tests covering the public API — runnable godoc examples
      with `// Output:` blocks (`example_test.go`): the full tour, float data
      with exact binary representations so output stays stable
- [x] Remove the custom color handler (`internal/logging/`): stdlib `log/slog`
      only — library side unchanged (silent by default, `SetLogger` opt-in),
      playground uses `slog.NewTextHandler`
- [x] Clean-code audit (2026-06-10): healthy, 0 CRIT / 0 HIGH. Report in
      `reports/clean-code-audit-2026-06-10-2208.md`; MEDIUMs tracked below
- [x] Refactor: `columnBuilder` (audit MEDIUM) — per-dtype builders own parse,
      null rules and finish for CSV and JSON; loaders are streaming drivers;
      adding a dtype = one builder + one factory case. Named `readerBufSize`
- [x] `ToCSV`/`ToJSON` writers — `io.Writer` core + path sugar, mirroring the
      loaders. CSV nulls = empty cells (polars' default): exact round-trip for
      float64/bool, documented asymmetry for strings; JSON literal nulls,
      token-level streaming, exact round-trip, rejects NaN/Inf. Round-trip
      tests + godoc examples
- [x] Tag `v0.1.0` — tagged and pushed 2026-06-10 🎉

## v0.2.0 — fast

Principles and rationale in [dev-notes/v0.2.0-principles.md](dev-notes/v0.2.0-principles.md):
measure first, fast sequential before parallel, parallelism at the boundaries.

- [x] In-repo benchmarks (`go test -bench` + benchstat baseline): loaders,
      writers, and kernels (`Sum`, `Where`, `GroupBy`, `Sort`) over a shared
      deterministic dataset (`bench_test.go`, 100k rows, seeded PCG); baseline
      in `reports/bench-baseline-v0.1.0.txt`. Findings: JSON load 203ms /
      4.9M allocs (the monster), CSV write ~2 allocs/row (`FormatFloat`),
      JSON write ~2 allocs/row (`json.Marshal` per string cell), `Sort` 31ms
      with 17 allocs (algorithmic, not allocation), kernels healthy
      (`Sum` = 0 allocs)
- [x] Profile the JSON path with pprof — found: ~36% of CPU and ~43% of
      allocations are encoding/json building *discarded* internal error
      values (`scanner.error` → `quoteChar`/`concatstrings`) every time a
      per-cell `Decode` scans for its value's end
- [x] ~~Token-per-value experiment~~ — falsified by benchstat: `dec.Token()`
      shares the same `readValue` machinery AND boxes every value into an
      interface → +11% time, +10% allocs. Reverted. Lesson recorded: the
      profile says where it hurts, only the after-benchmark says the cure
      works. Sequential JSON is at the stdlib's ceiling until json/v2
      graduates (still GOEXPERIMENT in Go 1.26)
- [x] Optimize sequential JSON load — byte-level parser
      (`from_json_bytes.go`, "B1"): keys compared as raw bytes, strict
      JSON-number grammar + `ParseFloat` (the `string(raw)` conversion
      stays on the stack — escape analysis), string fast path
      (no-backslash + valid UTF-8 → one copy) with full escape/surrogate/
      invalid-UTF-8 handling mirroring encoding/json. `FromJSON` uses it;
      `FromJSONReader` keeps the stdlib decoder (and got a missing-`]`
      strictness fix the new tests caught). Verified by a fuzz test with
      the stdlib as oracle (7.1M execs clean; first catch: invalid UTF-8
      must coerce to U+FFFD — saved in `testdata/fuzz/`).
      **206ms → 24.7ms (8.4x), 4.9M → 100k allocs (49x)** on the 100k-row
      benchmark; 26 → 221 MB/s single-threaded
- [x] Parallelize JSON load by chunks ("B2", `from_json_parallel.go`):
      boundaries at top-level `{` (bracket depth outside strings; CSV's
      quote-parity trick doesn't transfer — JSON escapes with `\"`, so the
      splitter reuses `scanString`), **pipelined dispatch** (each worker
      launches the moment its chunk is known, hiding the sequential scan —
      profiling showed an up-front split gating all workers), canonical
      errors via sequential re-parse on the error path, `mergeColumn`
      reuse. Splitter lesson: bulk `bytes.Count` skipping lost to a tight
      byte loop — JSON's between-string segments are tiny and per-call
      overhead dominated. **24.7ms → 12.6ms (2x over B1; 16.3x total over
      v0.1.0's 206ms)** on the 100k-row benchmark
- [x] Writers: `ToCSVWriter` rewritten byte-level like `ToJSONWriter`
      (encoding/csv only accepts []string records — one FormatFloat
      allocation per numeric cell): `AppendFloat` into reused scratch,
      hand quoting mirroring encoding/csv's exact rules (incl. leading
      Unicode space and the `\.` PostgreSQL case). `ToJSONWriter` got a
      string fast path (plain printable ASCII straight out; quotes,
      escapes, non-ASCII and HTML `<>&` fall back to `json.Marshal`).
      Byte-for-byte output compatibility pinned by oracle tests against
      encoding/csv and json.Marshal over a tricky-strings battery.
      **CSV 15.5 → 12.3ms, 194,909 → 2 allocs; JSON 20.5 → 13.3ms,
      200,011 → 11 allocs.** Lesson: allocs dropped ~100%, time only
      ~25% — float formatting (CPU), not allocation, dominates writing
- [x] Pairwise summation in `Sum`/`Avg` — recursive halving with 128-element
      sequential leaves (NumPy's design): error O(ε·log n) vs O(ε·n), and
      21% *faster* (leaf loops beat the validValues iterator; still 0
      allocs). Accuracy pinned against a 200-bit math/big reference (exact
      on 500k values where sequential drifts 3e-6); external checksum now
      matches pandas/polars (`499828455.03099`) — closing the mismatch
      documented in the first benchmark's lesson 4
- [x] Tag `v0.2.0` — tagged and pushed 2026-06-11 🎉
- [x] Parallelize CSV parsing by chunks (`from_csv_parallel.go`): quote-parity
      boundary scan (IndexByte hopping, embedded newlines in quoted fields
      handled correctly), one goroutine + own builders per chunk
      (`sync.WaitGroup.Go`, index-addressed results, no locks), builder-level
      merge with `slices.Grow`, pre-sized builders via per-chunk newline
      counts. `FromCSV` reads the file into memory and parallelizes when
      ≥1MiB; `FromCSVReader` stays streaming-sequential. Equivalence,
      balance, error-parity and `-race` tested. **18.97ms → 5.48ms (3.4x)**
      on the 100k-row benchmark. War story recorded: the first splitter
      passed every correctness test while producing a 10%/90% split — only
      a chunk-*balance* test caught it
- [x] Re-run the external benchmark (1M rows, same machine, same checksum
      `499828455.03102165`): CSV 0.54s → **0.15s** (vs polars 0.032s,
      pandas 0.60s), JSON 3.46s → **0.37s** — **faster than polars'
      0.75s and pandas' 1.67s: best JSON load on the table**. Caveat:
      pandas/polars numbers are from the 2026-06-09 run; re-run all three
      together before publishing any comparison. Bonus lesson: `go get
      @main` resolves through proxy.golang.org's cache — the first re-run
      silently measured yesterday's commit (`GOPROXY=direct` + explicit
      commit hash fixes it; always check the pseudo-version in go.mod)
- [x] Cross-engine benchmark, all three engines fresh (2026-06-11): full
      pipeline (read→sum→avg→sort→groupby→write), CSV+JSON, 1 → 10M rows,
      verification values match across engines. Closes the "re-run before
      publishing" caveat — comparison now published in the README. Raw
      tables + reading in `reports/cross-engine-bench-2026-06-11.txt`.
      Headline: grizzly is the best JSON loader on the table at scale
      (10M: 4.15s vs polars 46.7s). New finding: `Sort` is the clearest
      ceiling (loses to both engines from ~100k rows; tracked below)

## v0.3.0 — relational

- [ ] `Join`: design discussion — the design space (six axes, leading candidates,
      decisions open) is mapped in [dev-notes/join-design-space.md](dev-notes/join-design-space.md)
- [ ] `Join`: implement

## Unscheduled

- [ ] `Sort` at scale — the one kernel both pandas and polars beat from ~100k
      rows (10M: grizzly 7.0s vs pandas 2.24s, polars 0.70s; cross-engine
      bench 2026-06-11). Single-threaded permutation sort with comparison
      indirection; candidates to explore: parallel merge of sorted chunks,
      radix sort for float64 keys, sorting keys instead of row indices
- [ ] Charts/plot — long-term vision (2026-06-11): grizzly grows a plotting layer
      ("polars + matplotlib in one library"). Constraints already decided: separate
      subpackage (`grizzly/plot`, gonum/plot-style) so the root API stays small, and
      dependency-free rendering (emit SVG/HTML directly — the byte-level writer
      muscle from v0.2.0). Does not constrain the Join design; charts consume
      columns, which already exist
- [ ] Decide copy-vs-share semantics of constructor slices (`NewFloat64Column` stores
      the caller's slice without copying — documented but unresolved)
- [ ] Nulls in `FromStructs` — a struct's `float64` field always has a value;
      supporting nulls there means pointer fields (`*float64`) or `sql.Null[T]`
- [ ] Configurable null marker for CSV (read **and** write) — would close the
      string-null round-trip asymmetry documented in `ToCSVWriter`
- [ ] Audit MEDIUMs remaining (see `reports/clean-code-audit-2026-06-10-2208.md`):
      `Validity()` accessor on the `Column` interface (removes `columnValidity`),
      `Where` reusing `gatherRows`, drop the `method string` param in `compare`
- [ ] Audit LOWs: share the null-first preamble between `cmpRows`/`cmpBoolRows`,
      rename `floatValue` → `floatAt` in tests

Parquet support was considered and rejected (2026-06-10): no stdlib support in
Go, and grizzly stays dependency-free.
