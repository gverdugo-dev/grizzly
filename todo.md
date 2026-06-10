# todo

Working checklist of the project. The living design document is
[docs/README.md](docs/README.md); this is just the task view of it.

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
- [x] Learning notes: stack vs heap · make and maps · null handling

## Next up

- [x] **Nulls: design decided** — real `[]uint64` bitmap (`nil` = no nulls), comma-ok
      `Value(i) (T, bool)` API, always-nullable columns, SQL skip-null semantics.
      See [docs/nulls-in-go.md](docs/nulls-in-go.md)
- [x] **Nulls: implement** — bitmap + comma-ok `Value` on columns; `WithNulls`
      constructors ([]bool mask at the boundary); JSON `null` and empty CSV
      float cells → bit 0 (CSV `""` in string columns stays a value, unlike
      pandas); `Sum` skips nulls via set-bit iteration; `String` renders
      `null`; `Info` shows non-null counts and bitmap memory. Bonus: loaders
      split into `FromCSVReader`/`FromJSONReader` (path variants are sugar)

## Performance (seeded by the first benchmark — see
[docs/first-benchmark-lessons.md](docs/first-benchmark-lessons.md))

- [x] First benchmark vs pandas/polars (1M rows: beats pandas on CSV; JSON 6x slower
      than own CSV — culprit: `[]map[string]any` deserialization)
- [ ] Profile the JSON path with pprof (expect maps + boxing dominating)
- [ ] Rewrite `FromJSON` with token-level `json.Decoder` (no intermediate maps)
- [ ] Stream CSV: `csv.Reader` + `ReuseRecord = true` instead of `ReadAll`
- [ ] Parallelize parsing by chunks; re-measure the gap to polars

## Later

- [ ] More dtypes: `Int64Column`, `BoolColumn` (will stress the type-switch points:
      `cellString`, `colMemory`, loaders)
- [ ] Decide copy-vs-share semantics of constructor slices (`NewFloat64Column` stores
      the caller's slice without copying — documented but unresolved)
- [ ] Core operations: `Filter`, `Select`, then `GroupBy` and `Join`
- [x] More aggregations: `Avg` (SQL naming, not pandas' mean), `Min`, `Max`,
      `Count` — null-aware: skip nulls, `Avg` divides by valid count, and
      `Avg`/`Min`/`Max` of an empty or all-null column return
      `ErrNoValidValues`. All share the bitmap walk via the `validValues`
      iterator (range-over-func)
- [ ] Nulls in `FromStructs` — a struct's `float64` field always has a value;
      supporting nulls there means pointer fields (`*float64`) or `sql.Null[T]`
- [ ] Module path rename to `github.com/gverdugo-dev/grizzly` + `v0.1.0` tag
      (required to `go get` it from other repos)
- [ ] Tests (parked deliberately for now)
