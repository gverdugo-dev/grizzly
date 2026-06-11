# Sorting and selecting

## Sort

`Sort` and `SortDesc` order rows by one column, returning a **new**
dataframe — the original is untouched:

```go
asc, err := df.Sort("temp")
desc, err := df.SortDesc("temp")
```

Three rules worth knowing:

- **Stable**: rows that compare equal keep their original relative order —
  sorting by one column then another behaves predictably.
- **Nulls first, in both directions** (polars' rule): a null is neither
  smallest nor largest — it is unknown — so rather than pretend it has a
  position, grizzly always surfaces nulls at the top.
- **Whole-row reorder**: every column is reordered by the same permutation;
  rows stay intact.

```go
out, _ := df.Sort("temp")
// city     temp
// bilbao   null     ← null first
// madrid   21.5
// sevilla  35.5
```

Sortable dtypes are `Float64` and `String` (`Bool` columns can't be sort
keys — ordering booleans has no meaning).

## Select

`Select` projects columns by name, in the order you ask for them:

```go
out, err := df.Select("temp", "city")   // reorder + drop the rest
```

It is O(columns), not O(data): the returned dataframe **shares** the
underlying column storage with the original, which is safe because columns
are immutable after construction. Use it to drop columns, reorder them for
presentation, or narrow a wide table before writing.

Unknown names return `ErrColumnNotFound`; duplicate names in the request are
an error too (a dataframe's column names are unique).

## Immutability, in general

`Sort`, `SortDesc`, `Select` and `Where` all return new dataframes and never
modify the receiver. Chains read naturally:

```go
out, err := df.Select("city", "temp")
if err != nil { ... }
out, err = out.Sort("temp")
```

The cost model: `Select` is cheap (shares columns), `Sort` and `Where`
materialize (they must produce reordered/filtered column data).
