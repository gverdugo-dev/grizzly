# Nulls

grizzly treats missing data as a first-class concept: every column is
nullable, a null is never silently a zero or an empty string, and each
operation has a documented, SQL-aligned rule for what it does with unknowns.

## The model

Each column carries an Arrow-style **validity bitmap**: one bit per row,
1 = value, 0 = null. Columns with no nulls don't allocate a bitmap at all —
null-free data pays nothing.

Reading a value uses Go's comma-ok idiom:

```go
col, err := df.Column("temp")
f64 := col.(*grizzly.Float64Column)

v, ok := f64.Value(2)   // ok == false → row 2 is null, ignore v
```

`Column.IsValid(i)` answers the validity question alone, and
`Column.NullCount()` counts nulls. Building columns with nulls from scratch
uses the `WithNulls` constructors and a parallel validity mask:

```go
temp, err := grizzly.NewFloat64ColumnWithNulls("temp",
    []float64{21.5, 0, 28.25},      // placeholder at the null position
    []bool{true, false, true})      // row 1 is null
```

## What each operation does with nulls

| Operation | Rule | In SQL terms |
|-----------|------|--------------|
| `Sum`, `Avg`, `Min`, `Max` | Skip nulls; `Avg` divides by the *valid* count. All-null or empty column → `ErrNoValidValues`. | Aggregates ignore NULL |
| `Count` | Counts **valid** values only | `COUNT(col)` |
| Comparators (`Eq`, `Lt`, ...) | Comparing a null yields *unknown*, not false | `NULL = x` is unknown |
| `And`/`Or`/`Not` on masks | Kleene three-valued logic (`unknown OR true = true`, `unknown AND true = unknown`...) | SQL boolean logic |
| `Where` | Keeps rows that are valid **and** true — unknowns don't pass | `WHERE` drops unknowns |
| `GroupBy` | All null keys form **one group** (no rows silently dropped) | `GROUP BY` groups NULLs together |
| `Sort` / `SortDesc` | Nulls first, in both directions | — (polars' rule) |
| Printing (`String`) | Renders `null` | |
| `Info` | Reports non-null counts per column | |

The asymmetry between `Where` (unknowns excluded) and `GroupBy` (nulls form
a group) is intentional and matches SQL: filtering asks a question about a
value (unknown → can't say yes), grouping partitions rows (every row must
land somewhere). The full rationale is in the
[design decisions](../design/README.md).

## Loading and writing

Where nulls come from is a loader concern — see
[Loading data](loading-data.md#where-nulls-come-from). How they serialize
(JSON `null` literals, empty CSV cells, and the one string caveat) is in
[Writing data](writing.md).
