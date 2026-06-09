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

- [ ] **Nulls** ← next topic. Decide representation (validity mask: real bitmap vs
      `[]bool`) and semantics (what do `Sum`/`mean`/comparisons do with nulls), then
      implement across columns, loaders and `Info`. Groundwork:
      [docs/null-handling.md](docs/null-handling.md)

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
- [ ] More aggregations: `Mean`, `Min`, `Max`, `Count`
- [ ] Module path rename to `github.com/gverdugo-dev/grizzly` + `v0.1.0` tag
      (required to `go get` it from other repos)
- [ ] Tests (parked deliberately for now)
