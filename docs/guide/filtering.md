# Filtering

Filtering in grizzly is a three-step pipeline — build masks, combine masks,
materialize once:

```go
warm, err := df.Gt("temp", 20.0)   // 1. comparators produce masks
mild, err := df.Lt("temp", 30.0)
out, err := df.Where(warm.And(mild)) // 2. combine  3. materialize
```

This is the NumPy/polars model: each comparator scans one column and yields
a packed boolean **`Mask`**; masks combine with word-level boolean algebra;
and only `Where` touches the full table, gathering the surviving rows in a
single pass.

## Comparators

All six comparison operators are methods on the dataframe, taking a column
name and a value:

| Method | Meaning |
|--------|---------|
| `Eq`, `Ne` | equal / not equal — all dtypes |
| `Lt`, `Le`, `Gt`, `Ge` | ordering — `Float64` and `String` columns |

The value's type must match the column's dtype (`float64` for `Float64`
columns, `string` for `String`, `bool` for `Bool`); a mismatch returns
`ErrTypeMismatch`, and a missing column `ErrColumnNotFound`. Bool columns
support `Eq`/`Ne` only — ordering booleans has no meaning.

## Combining masks

`Mask` values combine without touching the data again:

```go
m := a.And(b)      // both
m = a.Or(b)        // either
m = a.Not()        // negation
m = a.And(b.Not()) // compose freely
```

Combinators operate on whole 64-bit words, so combining masks costs ~n/64
operations, not n.

## Nulls: three-valued logic

Comparing a null doesn't yield `false` — it yields **unknown**, and unknowns
propagate through `And`/`Or`/`Not` by Kleene logic, exactly like SQL:

- `unknown AND true → unknown`, `unknown AND false → false`
- `unknown OR true → true`, `unknown OR false → unknown`
- `NOT unknown → unknown`

`Where` then keeps only rows whose final mask value is **valid and true** —
unknown rows are excluded, matching SQL's `WHERE`. The practical
consequence: `df.Where(eq)` plus `df.Where(eq.Not())` does *not* add up to
the whole table if the column has nulls — the null rows match neither, just
like in a database.

```go
warm, _ := df.Gt("temp", 20.0)  // bilbao's temp is null → unknown
out, _ := df.Where(warm)        // bilbao is not in the output
```

## Why masks instead of predicates?

A `Filter(func(row) bool)` API would be more familiar — and would box every
row, defeat the columnar layout, and hide the logic from the engine. Masks
keep the scan typed and sequential, and make composition (`And`/`Or`/`Not`)
data instead of opaque code. The full comparison of the four candidate
designs is in the dev notes:
[Filter — the design space](../../dev-notes/filter-design-space.md).
