# Nulls in a typed columnar store

Learning note. Context: grizzly's open question on null handling — real-world data is
full of holes (empty CSV cells, JSON `null`s, missing keys), but our columns are
contiguous typed slices, and a `[]float64` has no way to say "nothing here". This note
explains the problem and the three classic strategies; the decision itself is still
open in the living document.

## The problem: a typed slice has no empty slot

Every position of a `[]float64` holds *some* float64 — there is no "absent" state.
When `FromCSV` meets an empty cell today, the only honest options are to error out
(current behavior) or to invent a value. Both are wrong for real data: errors make the
loader useless on real datasets, and invented values silently corrupt statistics
(a missing price is not a `0.0` price — `Sum` would be right by accident, but `mean`,
`min` and `count` would all lie).

So a null-aware column needs to answer two independent questions per row:

1. **Is there a value here?** (presence)
2. **If so, what is it?** (the value)

The whole design space is about where to store answer #1.

## Strategy 1: sentinel values (old pandas)

Reserve one value of the type to mean "null": `NaN` for floats, `""` for strings,
`math.MinInt64` for ints.

- ✅ Zero extra memory; presence is encoded inside the value itself.
- ❌ **The sentinel is a legitimate value.** `""` is a real string (and a common one);
  a `0/0` computation produces a *real* NaN you may want to distinguish from "missing".
- ❌ **Some types have no spare value at all.** A `bool` is `true` or `false` — there
  is no third bit pattern. This is exactly why old pandas silently *upcast* int columns
  to float64 the moment a null appeared (ints have no NaN; floats do): the dtype of
  your column changed because of one missing cell.
- ❌ Every operation must remember the magic value and check for it.

## Strategy 2: pointer slices (`[]*float64`)

`nil` pointer = null. The natural first instinct coming from languages with `None`.

- ✅ Presence is unambiguous; works uniformly for every type.
- ❌ **Destroys everything we built.** Each value moves to its own heap allocation;
  the slice becomes pointers to scattered memory — the exact `[]any` disaster from the
  stack-vs-heap note (cache misses, GC pressure, 8 bytes of pointer + heap object per
  value), just with a typed label on it.

## Strategy 3: validity bitmap (Arrow, Polars, modern pandas)

Keep the typed slice as-is, and add a **separate bitmap**: 1 bit per row, `1` = valid,
`0` = null. At null positions the value buffer holds an arbitrary placeholder (usually
zero) that operations never read.

```
values:   [ 1.50 | 0.00 | 3.20 | 2.10 ]   ← still contiguous []float64
validity: [  1   |  0   |  1   |  1   ]   ← 1 bit each: row 1 is null
```

- ✅ The value buffer stays contiguous and typed — all the cache-locality work survives.
- ✅ Cost is 1 bit per row: 0.4% overhead on a float64 column (vs 100%+ for pointers).
- ✅ Works identically for every type, `bool` included; no dtype changes, no magic values.
- ✅ Presence checks are cheap and vectorizable (whole-word operations on the bitmap:
  64 rows per instruction; `count nulls` = popcount).
- ❌ More code: every operation now consults two buffers, and the column type needs a
  null-aware API (`Value(i) (float64, ok bool)` style).
- ❌ Null **semantics** become explicit design work: does `Sum` skip nulls or return
  null? What does `mean` divide by? Comparisons enter three-valued logic territory
  (`null == null` is null, not true, in SQL). The bitmap stores the nulls; it doesn't
  decide what they *mean*.

A pragmatic Go variant of the same idea is `[]bool` instead of a real bitmap: one byte
per row instead of one bit — 8x the memory of a bitmap (still tiny vs the data) but
much simpler code. Same design, relaxed compression.

## The simple version

Imagine a parking lot (the column) where every spot must contain *something* — the
concrete doesn't have a "no car" state.

- **Sentinel**: park a beat-up yellow Beetle in every empty spot and tell everyone
  "the yellow Beetle doesn't count". Works until a customer actually drives a yellow
  Beetle (a real `""` or NaN) — now you can't tell them apart. And some lots
  (booleans) only ever see two car models, so there's no spare model to sacrifice.
- **Pointers**: replace the parking lot with a valet stand holding tickets; every car
  is parked somewhere random across town. No ambiguity about empty tickets, but
  fetching each car is a trip across town — you've traded your parking lot for traffic.
- **Validity bitmap**: keep the lot exactly as it is and hang a clipboard at the
  entrance with one checkbox per spot: occupied or free. The lot stays fast to walk,
  the clipboard is tiny, and you never confuse a real car with a placeholder.

## Further reading

- [Arrow Columnar Format — specification](https://arrow.apache.org/docs/format/Columnar.html)
  — the industry-standard definition of validity bitmaps: every Arrow array carries a
  dedicated null bitmap buffer. The exact design Polars and modern pandas build on,
  and the reference to study before designing grizzly's version.
- [pandas — Working with missing data](https://pandas.pydata.org/docs/user_guide/missing_data.html)
  — shows the sentinel strategy's real-world scars: NaN coercing int columns to
  float64, and the newer nullable dtypes (`pd.NA`) pandas added to escape it.
