# GroupBy: the design space

Learning note. Context: `GroupBy` is the biggest remaining v0.1.0 piece and the
heart of any dataframe engine — and its API and algorithm are **still an open
decision**. This note maps the design space: five axes, mostly independent,
each with a leading candidate. The conceptual frame for all of them is
**split-apply-combine**: split rows into groups by key, apply an aggregation
per group, combine the results into a new (one-row-per-group) dataframe.

```
GroupBy("store") over:              split:                 apply Sum(price), combine:
store     price                     downtown → rows 0,1
downtown  1.5                       uptown   → rows 2,3    store     sum_price
downtown  0.75                                             downtown  2.25
uptown    3.2                                              uptown    5.3
uptown    2.1
```

## Axis 1 — Grouping algorithm: hash vs sort

**Hash-based**: walk the key column once; a hash table maps each distinct key
to a group id. O(n), the industry default (pandas, polars, every database's
hash aggregate).

**Sort-based**: sort rows by key, then groups are consecutive runs. O(n log n),
but cache-friendly and the output comes ordered for free; databases pick it
when the data is already sorted.

- ✅ Hash: linear, no full-table reorder, natural fit for Go's `map`.
- ❌ Hash: group order needs explicit handling (see axis 5); a `map` per
  distinct key has allocation overhead for high-cardinality keys.
- ✅ Sort: ordered output, no hash table, predictable memory.
- ❌ Sort: pays O(n log n) even for 3 distinct keys; needs `Sort` to exist
  first (it doesn't yet).

*Leading candidate: hash-based* — it is also the better Go lesson (maps as
hash tables — the make-and-maps note was groundwork for exactly this).

## Axis 2 — Key representation in Go

The hash table needs a key type. For a **single key column**, the natural move
is a typed map per dtype, reached by type switch: `map[float64]int`,
`map[string]int`, `map[bool]int` (value = group id). Typed, no boxing, fast.

The interesting pattern is **factorize / group ids**, and it is what makes the
rest of the engine simple: the product of the grouping phase is not a map of
slices but a flat `groupIDs []int`, parallel to the rows — row i belongs to
group `groupIDs[i]` — plus the list of distinct keys in first-appearance
order. Group ids are assigned incrementally (first new key seen = group 0,
next = group 1...). Aggregation then never touches the hash table again: it
walks the data column once, accumulating into `sums[groupIDs[i]]`.

For **multi-column keys** (`GroupBy("store", "product")`) the options get
spicier:

- **String concatenation**: build `"downtown|apple"` per row and hash that.
  Simple, but allocates a string per row and needs separator escaping to
  avoid ambiguity (`"a|b" + "c"` vs `"a" + "b|c"`).
- **Iterative id combination**: factorize column 1 into ids; then group
  column 2 *within* each id by hashing the pair `(prevID, value)` — pairs of
  ints/values hash cleanly as small structs (`map[[2]any]...` no —
  `map[pairKey]int` with a small generic struct). No string building, no
  ambiguity; one pass per key column. This is morally what pandas' internal
  `group_index` composition does.

*Leading candidate: single-column first* (typed maps + group ids), with
iterative id combination as the documented path to multi-key later. Scope
control: multi-key GroupBy can land after v0.1.0 without API breakage
(`GroupBy` is variadic from day one).

## Axis 3 — API shape

**A. Eager, two-step**: `df.GroupBy("store")` returns a small intermediate
(`GroupedDataframe` holding the group ids), and `.Agg(...)` materializes:

```go
result, err := df.GroupBy("store").Agg(
    grizzly.Sum("price"),
    grizzly.Count("product"),
)
// store     sum_price  count_product
// downtown  2.25       2
// uptown    5.3        2
```

- ✅ Reads like SQL/pandas/polars; the intermediate is cheap (ids, not data).
- ✅ `Agg` specs are inspectable data (column + op), not opaque closures —
  the same reasoning that chose masks over closures for Filter.
- ❌ Needs a tiny spec vocabulary (`Sum`, `Avg`, `Min`, `Max`, `Count` as
  agg-spec constructors — name collision with the Dataframe methods to
  resolve: package-level functions vs methods).

**B. Map of sub-dataframes**: `GroupBy` returns `map[key]Dataframe`.

- ✅ Maximum flexibility (caller does anything per group).
- ❌ Materializes every group as a full dataframe — the memory/allocation
  explosion of splitting physically. Split-apply-combine engines never split
  physically; they split *logically* (group ids). Anti-columnar in spirit.

**C. Lazy expressions** (polars): discarded for the same reason as Filter's
option D — that is a query engine, a different project.

*Leading candidate: A.*

## Axis 4 — Null keys

What group does a null store belong to? Filtering dropped unknowns; grouping
must not silently drop rows.

- **SQL and polars**: all nulls form **one group together** — for grouping,
  null equals null (the opposite of WHERE's three-valued logic; SQL is
  explicit about this asymmetry: GROUP BY treats NULLs as one group,
  comparisons treat them as unknown).
- **pandas**: drops null keys by default (`dropna=True`) — and offers
  `dropna=False` precisely because silently losing rows surprises people.

*Leading candidate: the SQL/polars rule* — nulls form a group, shown as
`null` in the key column of the result. No data silently disappears, and it
needs no option flag.

## Axis 5 — Output order (and a Go gotcha)

A Go `map` iterates in **deliberately randomized order** — the runtime
varies it run to run so nobody depends on it. Iterating the hash table to
emit groups would make `GroupBy` non-deterministic: same input, shuffled
output, flaky Examples.

The factorize pattern solves this for free: group ids are assigned in
**first-appearance order**, so emitting groups `0, 1, 2...` yields rows in
the order the keys first showed up — deterministic and natural. (Sorting the
result by key is then the caller's choice once `Sort` exists, exactly like
SQL's "no ORDER BY, no promised order".)

## The simple version

Sorting mail into pigeonholes. Hash vs sort (axis 1): walk the pile once,
slotting each letter into the hole its name points to — versus alphabetizing
the entire pile first and cutting it where the name changes. Group ids
(axis 2): instead of physically moving letters into boxes, stamp each letter
with a box number and leave the pile alone — any later count ("letters per
box") is one pass over the stamps. API (axis 3): the clerk hands you a
stamped pile and waits for instructions ("count per box, weigh per box"),
rather than photocopying every box into its own room (option B). Null keys
(axis 4): letters with no name go in one shared "no name" hole — they don't
go in the trash. Output order (axis 5): boxes are numbered in the order
their first letter appeared, because asking the warehouse for "whatever
order you like" gives a different answer every day (Go maps shuffle on
purpose).

## Further reading

- [pandas — Group by: split-apply-combine](https://pandas.pydata.org/docs/user_guide/groupby.html)
  — the canonical statement of the split-apply-combine frame, plus the
  `dropna` behavior for null keys (and why pandas had to add the option) —
  the user-facing semantics grizzly is choosing between.
- [Polars — Aggregation](https://docs.pola.rs/user-guide/expressions/aggregation/)
  — group_by in the engine grizzly keeps benchmarking against: multiple
  aggregations per group, nested (multi-column) grouping, and why opaque
  per-group closures kill parallelism — the industrial argument for
  inspectable agg specs.
