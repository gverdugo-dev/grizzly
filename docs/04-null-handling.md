# 04 — Null Handling

> Status: 🟡 Open for discussion
> Affects: memory layout, every kernel, the API

## The question

How do we represent missing values? A parallel `[]bool` mask, or a packed validity bitmap
(1 bit per value, Arrow-style)?

## Why it matters

Nulls leak into *everything*: every aggregation, filter, and join has to decide what to do
with them. Pick the representation early because it's painful to change later. It's also a
neat Go lesson in the trade-off between clarity and bit-level efficiency.

## Options

### Option A — Parallel `[]bool` mask ✅ (leaning, to start)
`valid []bool` alongside the data; `valid[i] == false` means null. `nil` mask = no nulls
(fast path).
- **Pro:** Trivial to read and write, easy to debug, obvious code.
- **Con:** 1 byte per value (8× a bitmap), not SIMD-friendly.

### Option B — Validity bitmap (Arrow-style)
1 bit per value packed into `[]uint64`/`[]byte`.
- **Pro:** 8× smaller, cache-friendly, interops with Arrow, fast popcount-based null counts.
- **Con:** Bit twiddling everywhere; easy to get off-by-one wrong.

## Current leaning

Start with **Option A (`[]bool`)** for clarity while we're learning, with a `nil`-mask
fast path for null-free columns. Then, *as a deliberate optimization exercise*, migrate to
**Option B (bitmap)** during the performance phase — so we *experience* why the bitmap
exists instead of cargo-culting it.

Key design rule from day one: **isolate null logic behind the column abstraction** so the
representation can change without touching every kernel.

## Open questions

- [ ] Null semantics in aggregations: skip (pandas `skipna=True`) or propagate (SQL `NULL`)? Leaning: skip-by-default, like pandas.
- [ ] Do `int64` columns support nulls, or only `float64` (via NaN) like classic NumPy-era pandas? Leaning: real null mask for *all* types (the modern, correct choice).
- [ ] How do nulls behave in joins and group keys? (A null key — one group or dropped?)

## References

- [Apache Arrow — Validity bitmaps](https://arrow.apache.org/docs/format/Columnar.html#validity-bitmaps) — the canonical bitmap design we'll migrate toward.
- [pandas — Working with missing data](https://pandas.pydata.org/docs/user_guide/missing_data.html) — semantics decisions (skipna, NA propagation) worth copying or rejecting deliberately.
