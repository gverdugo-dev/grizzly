# Grouping and aggregation

## Whole-column aggregations

The simplest aggregations run over one column of the dataframe:

```go
sum, err := df.Sum("price")
avg, err := df.Avg("price")   // SQL naming: Avg, not mean
min, err := df.Min("price")
max, err := df.Max("price")
n, err := df.Count("price")   // count of valid (non-null) values
```

All of them skip nulls, SQL-style: `Avg` divides by the count of *valid*
values, and an empty or all-null column returns `ErrNoValidValues` rather
than a misleading zero. (`Sum` and `Avg` use pairwise summation, so their
floating-point error grows logarithmically instead of linearly with column
size — on large columns the result matches what pandas and NumPy compute.)

## GroupBy

`GroupBy` partitions rows by a key column; `Agg` computes one or more
aggregations per group and returns a new dataframe with one row per group:

```go
out, err := df.GroupBy("store").Agg(
    grizzly.Sum("price"),
    grizzly.Avg("price").As("avg"),
)
// store  price  avg
// north  2      1
// south  4      2
```

The aggregation specs are package-level constructors — `grizzly.Sum`,
`grizzly.Avg`, `grizzly.Min`, `grizzly.Max`, `grizzly.Count` — each naming
the column to aggregate. The output column keeps the source column's name
unless renamed with `.As("name")`.

Note the chain: `GroupBy` returns a `GroupedDataframe` and defers any error
(unknown column, unsupported key type) to `Agg` — the same pattern as
`sql.Row.Scan`. Check the error where the chain ends.

### Semantics

- **Null keys form one group**, rendered as `null` in the key column. No
  rows are silently dropped (unlike pandas' default `dropna=True`). This is
  the SQL rule — and deliberately the opposite of filtering, where unknown
  rows don't pass; see [Nulls](nulls.md).
- **Groups come out in first-appearance order** — the order in which each
  key was first seen. Same input, same output, every run. If you want
  key-sorted results, sort afterwards: `out.Sort("store")`.
- **One key column** is supported today (`GroupBy` is variadic so that
  multi-column keys can arrive without breaking the API).

### How it works

grizzly uses hash-based grouping (the industry default): one pass over the
key column assigns each row a group id via a typed hash map, then each
aggregation is a map-free pass accumulating into a per-group slot. The
design space — hash vs sort grouping, why group ids beat materializing
sub-dataframes, the determinism gotcha with Go's randomized map iteration —
is mapped in the dev notes:
[GroupBy — the design space](../../dev-notes/groupby-design-space.md).
