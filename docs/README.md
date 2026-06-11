# grizzly documentation

Official documentation for [grizzly](../README.md), a zero-dependency
dataframe library for Go.

## Where to start

- **[Getting started](getting-started.md)** — install grizzly and run your
  first dataframe program in five minutes.
- **[API reference (godoc)](https://pkg.go.dev/github.com/gverdugo-dev/grizzly)**
  — every exported symbol, with runnable examples.

## User guide

One page per area, in the order you'll likely need them:

1. **[Loading data](guide/loading-data.md)** — from Go structs, CSV and JSON;
   schemas; where nulls come from.
2. **[Nulls](guide/nulls.md)** — the null model and how every operation
   treats missing values.
3. **[Filtering](guide/filtering.md)** — comparators, combinable masks and
   `Where`.
4. **[Grouping and aggregation](guide/grouping-and-aggregation.md)** —
   `GroupBy`/`Agg` and the whole-column aggregations.
5. **[Sorting and selecting](guide/sorting-and-selecting.md)** — `Sort`,
   `SortDesc` and `Select`.
6. **[Writing data](guide/writing.md)** — `ToCSV`/`ToJSON` and their
   round-trip guarantees.

## Design

- **[Design decisions](design/README.md)** — the architectural choices that
  define grizzly (columnar storage, validity bitmaps, explicit schemas, the
  three-dtype model...), each with its rationale, distilled.

## Dev notes

The full engineering journey — design-space explorations, performance war
stories, and learning notes on the concepts under the hood — lives in
[dev-notes/](../dev-notes/README.md). This guide tells you *what* grizzly
does; the dev notes tell you *why* and *how it got there*.
