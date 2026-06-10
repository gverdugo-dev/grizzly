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
- [ ] `Sort` ← next (gatherRows already does the reorder; the work is
      producing the permutation: sort.Slice over row indices, null placement
      to decide — nulls first vs last)
- [ ] `Select` (column projection)
- [ ] `GroupBy` (+ aggregations over groups)
- [ ] `Sort`
- [ ] `Example`-based tests covering the public API
- [ ] Tag `v0.1.0` (required to `go get` it from other repos)

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
