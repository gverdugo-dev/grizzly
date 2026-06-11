<p align="center">
  <img src="docs/assets/logo.jpeg" alt="grizzly logo" width="220">
</p>

# grizzly

[![Go Reference](https://pkg.go.dev/badge/github.com/gverdugo-dev/grizzly.svg)](https://pkg.go.dev/github.com/gverdugo-dev/grizzly)
![Go Version](https://img.shields.io/badge/go-1.26-00ADD8?logo=go)
![Dependencies](https://img.shields.io/badge/dependencies-zero-success)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

A dataframe library for Go, written from scratch on the standard library alone.
Load rows, work in columns: typed columnar storage, first-class nulls, and a
small, explicit API.

```go
df, err := grizzly.FromCSV("sales.csv", grizzly.Schema{
    {Name: "store", Type: grizzly.String},
    {Name: "price", Type: grizzly.Float64},
})

warm, _ := df.Gt("price", 1.0)
out, _ := df.Where(warm)

summary, _ := out.GroupBy("store").Agg(
    grizzly.Sum("price"),
    grizzly.Avg("price").As("avg"),
)
fmt.Print(summary)
```

## Features

- **Row-oriented ingestion, column-oriented storage.** Load data the way you
  have it — Go structs, CSV, JSON — and grizzly transposes it once into typed,
  contiguous columns (`float64`, `string`, `bool`) that operations scan at
  cache speed.
- **First-class nulls.** Arrow-style validity bitmaps (null-free columns pay
  nothing), comma-ok access (`Value(i) (T, bool)`), SQL semantics in
  aggregations (nulls are skipped, never counted as zero) and Kleene
  three-valued logic in filters.
- **Explicit schemas, no guessing.** Untyped sources (CSV, JSON) load against
  a schema you declare — a `"08001"` zip code never silently becomes a number.
  Struct loading needs no schema: the types come from the fields.
- **The core operations.** Comparators + combinable masks (`Eq`/`Lt`/`Gt`...,
  `And`/`Or`/`Not`) materialized by `Where`; `Select`; `GroupBy(...).Agg(...)`
  with inspectable aggregation specs; stable `Sort`/`SortDesc` (nulls first);
  whole-column `Sum`/`Avg`/`Min`/`Max`/`Count`.
- **Round-tripping writers.** `ToCSV`/`ToJSON` mirror the loaders; JSON output
  reloads byte-exact.
- **Fast by measurement, not folklore.** Parallel chunked CSV and JSON
  loading, hand-written byte-level parsers and writers, pairwise summation
  (NumPy's algorithm: error O(ε·log n) — and faster than the naive loop).
  Every optimization landed with a benchstat comparison and an oracle test
  against the standard library.
- **Zero dependencies.** The standard library is the only import, and that is
  a permanent design constraint.

## Install

```bash
go get github.com/gverdugo-dev/grizzly
```

Requires Go 1.26+.

## Quick tour

Every snippet below is a runnable [example test](example_test.go) — the
documentation cannot drift from the behavior.

### Load

From structs (one column per exported field, renamed by tags):

```go
type sale struct {
    Product string  `grizzly:"product"`
    Price   float64 `grizzly:"price"`
    Sold    bool    `grizzly:"sold"`
}
df, err := grizzly.FromStructs([]sale{
    {Product: "apple", Price: 1.5, Sold: true},
    {Product: "pear", Price: 2, Sold: false},
})
```

From CSV or JSON — file paths or any `io.Reader`, against an explicit schema.
Empty CSV cells and JSON `null`s load as real nulls, never as fake zeros:

```go
schema := grizzly.Schema{
    {Name: "city", Type: grizzly.String},
    {Name: "temp", Type: grizzly.Float64},
}
df, err := grizzly.FromCSV("cities.csv", schema)       // or FromCSVReader
df, err = grizzly.FromJSON("cities.json", schema)      // or FromJSONReader

fmt.Print(df)
// city      temp
// madrid    21.5
// bilbao    null
// valencia  28.25
```

### Filter

Comparators build masks; masks combine; `Where` materializes once. Null
comparisons follow three-valued logic — unknown rows don't pass:

```go
warm, _ := df.Gt("temp", 20.0)
mild, _ := df.Lt("temp", 30.0)
out, _ := df.Where(warm.And(mild))
```

### Group and aggregate

Groups come out in first-appearance order — deterministic, every run:

```go
summary, err := df.GroupBy("store").Agg(
    grizzly.Sum("price"),
    grizzly.Avg("price").As("avg"),
)
// store  price  avg
// north  2      1
// south  4      2
```

### Sort, select, write

```go
out, _ := df.Sort("temp")        // stable; nulls first (SortDesc to reverse)
out, _ = out.Select("temp", "city")
err = out.ToCSV("out.csv")       // nulls become empty cells
err = out.ToJSON("out.json")     // literal nulls; reloads byte-exact
```

### Inspect

`fmt.Print(df)` renders a truncated table; `df.Info()` gives the
pandas-style summary: per-column dtype, non-null counts and memory usage.

## Data model

Three column types: **float64**, **string** and **bool** — the JSON model.
Every number is a float64 (exact for integers up to 2^53); bool columns are
bit-packed. Every column is nullable, backed by a validity bitmap that costs
nothing while a column has no nulls. The rationale behind each of these
decisions is documented in [docs/design](docs/design/README.md).

## Performance

The loaders parse in parallel chunks and the hot paths are byte-level
(no `encoding/json` or `encoding/csv` in the fast lanes — but their behavior
is pinned by fuzz and oracle tests against them). On a 1M-row, 3-column
dataset (commodity laptop):

| Operation | v0.1.0 | v0.2.0 |
|-----------|-------:|-------:|
| CSV file load | 0.54s | **0.15s** |
| JSON file load | 3.46s | **0.37s** |
| CSV/JSON write (100k rows) | ~395k allocs | **13 allocs** |

Benchmarks live in [`bench_test.go`](bench_test.go); methodology and
comparisons against other engines are tracked in the
[dev notes](dev-notes/README.md) and re-validated before being cited.

## Status and versioning

grizzly is **pre-1.0**: the API can change between minor versions. Releases
follow [semver](https://semver.org/) via git tags — patches are fixes only,
each minor is a milestone (`v0.1.0` core API → `v0.2.0` performance →
`v0.3.0` relational, in progress). The roadmap lives in the
[living document](dev-notes/README.md).

grizzly is also a from-scratch project by design: every data structure —
validity bitmaps, hash-based grouping, the JSON parser — is built and
documented here rather than imported, with notes explaining each design
decision and the alternatives it beat. The official documentation is in
[docs/](docs/README.md); if you want to understand how a dataframe engine
works from the inside, the [dev notes](dev-notes/README.md) are the guided
tour.

## License

[MIT](LICENSE).
