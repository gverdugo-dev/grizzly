# todo

Working checklist of the project. The living design document is
[docs/README.md](docs/README.md) (the roadmap section defines the phases);
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
      See [docs/nulls-in-go.md](docs/nulls-in-go.md)
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
      [docs/bitmaps-and-words.md](docs/bitmaps-and-words.md)
- [x] `Filter`: design decided — columnar comparators (`Eq`/`Lt`/`Gt`...) →
      combinable masks (`And`/`Or`/`Not`, word-level, Kleene null logic) →
      `Where` materializes. See
      [docs/filter-design-space.md](docs/filter-design-space.md)
- [x] `Filter`: implement — `Mask` (packed bits + validity), comparators
      `Eq`/`Ne`/`Lt`/`Le`/`Gt`/`Ge` (generic `cmp.Ordered` core; bool gets
      Eq/Ne from its packed words), word-level Kleene `And`/`Or`/`Not`,
      `Where` gathers valid-AND-true rows in one materialization
- [x] `Select` — column projection and reorder, O(cols): shares the Column
      pointers (safe: columns are immutable after construction), duplicate
      and missing names error via NewDataframe revalidation
- [x] `GroupBy`: design decided — hash factorize (typed maps → `groupIDs []int`),
      eager `GroupBy(...).Agg(specs...)`, null keys = one group, first-appearance
      order. See [docs/groupby-design-space.md](docs/groupby-design-space.md)
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

- [ ] Profile the JSON path with pprof (expect remaining cost in Decode calls)
- [ ] Parallelize parsing by chunks; re-measure the gap to polars

## v0.3.0 — relational

- [ ] `Join`

## Unscheduled

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
