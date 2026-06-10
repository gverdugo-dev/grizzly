# Nulls in Go: the four design decisions

Learning note. Context: [Nulls in a typed columnar store](null-handling.md) settled the
*strategy* (a validity mask, like Arrow/Polars). This note covers the *Go-specific
design space*: implementing that strategy is not one decision but four independent
ones — physical representation, public API, type architecture, and semantics. All four
are now **decided** (recorded at the end of each section) and drive the null-handling
implementation across columns, loaders and `Info`.

## Axis 1 — Where validity lives: `[]bool` vs a real bitmap

Both are "strategy 3" (validity mask); they differ in physical representation.

### Option A: `[]bool` — one byte per row

```go
type Float64Column struct {
    name     string
    values   []float64
    validity []bool // validity[i] == true → values[i] is real
}
```

A Go `bool` occupies **1 byte**, not 1 bit — CPUs address bytes, not bits. For 1M rows
that is 1 MB on top of 8 MB of float64 data (~12%).

- ✅ Trivial code: `c.validity[i]`, zero bit arithmetic.
- ✅ Directly indexable, visible as-is in a debugger.
- ❌ 8x the memory of a real bitmap.
- ❌ No word-level tricks: counting nulls in 1M rows is a 1M-iteration loop.

### Option B: real bitmap — `[]uint64`, one bit per row

```go
type Float64Column struct {
    name     string
    values   []float64
    validity []uint64 // bit i → row i; 1 = valid
}
```

This brings in **bit manipulation**:

```go
// Is row i valid?
word := c.validity[i>>6]      // i/64  → which uint64 the row falls in
bit  := uint64(1) << (i & 63) // i%64  → which bit inside that word
ok   := word&bit != 0
```

(`i>>6` and `i&63` are the classic power-of-two shortcuts for `i/64` and `i%64`;
the compiler would do this rewrite anyway, but in bitmap code it is conventional to
write them explicitly.)

- ✅ 1 bit per row: 125 KB per 1M rows (0.4% overhead). What Arrow/Polars do.
- ✅ Word-level operations: `math/bits.OnesCount64(word)` (popcount) counts 64 rows
  in one instruction — `CountNulls` over 1M rows becomes ~15.6K iterations, not 1M.
- ✅ Combining masks of two columns (for a future `Filter`) is an AND of words.
- ❌ More code, and subtler: shifts, masks, and the partial last word when
  `len % 64 != 0`.

**A Go trick that applies to both**: the zero value of a slice is `nil`, and `nil`
can mean "**no nulls — everything valid**". Columns without nulls (the common case)
pay zero bytes, and the check is one predictable branch:

```go
func (c *Float64Column) Value(i int) (float64, bool) {
    if c.validity == nil {
        return c.values[i], true
    }
    // ... consult the bitmap
}
```

This is deeply idiomatic: *useful zero values* are a Go design principle (a `nil`
slice can be ranged over, a `nil` map can be read from).

> **Decision: real bitmap (`[]uint64`), with `nil` meaning "no nulls".** In
> production code `[]bool` would be a defensible pragmatic choice; in a learning
> project the bitmap is the point — bit twiddling, popcount, word-level operations,
> and the exact layout Arrow uses.

## Axis 2 — How the API exposes presence

Independent of axis 1. Three candidates:

### Option A: comma-ok — `Value(i) (float64, bool)`

```go
v, ok := col.Value(3)
if !ok { /* null */ }
```

**The** Go idiom for "there may be no value": map access (`v, ok := m[k]`), type
assertions (`f, ok := x.(float64)`), channel receive. Any Go reader understands it
without docs — and it forces the caller to *decide* what to do with a null; absence
cannot be ignored by accident.

### Option B: a `Null[T]` struct (the `database/sql` style)

```go
type Null[T any] struct {
    V     T
    Valid bool
}
```

This exists literally in the stdlib since Go 1.22 (`sql.Null[T]`). Useful when a null
must *travel* as a value (inside a struct, through a channel). But for reading a cell
it is more ceremony than comma-ok, and nothing stops the caller from reading `.V`
without checking `.Valid`.

### Option C: two methods — `IsValid(i) bool` + `At(i) float64`

Separating presence from value looks worse (you can call `At` on a null), but it has a
real use case: **hot loops**. `Sum` does not want a tuple per row; it wants to walk
the bitmap word by word and only touch `values` where bits are set.

> **Decision: comma-ok (`Value(i) (T, bool)`) as the public API.** Not exclusive with
> C: operations like `Sum` type-switch to the concrete column anyway, see the private
> fields, and read the slice + bitmap directly. Idiomatic surface, fast interior.

## Axis 3 — Separate nullable types vs always-nullable columns

- **Option A: every column is nullable**, with the `nil`-bitmap fast path. One type,
  one API, zero cost when there are no nulls.
- **Option B: separate types** (`Float64Column` and `NullableFloat64Column`). Type
  explosion: every dtype × 2, and every type-switch in the codebase (`Sum`,
  `cellString`, `colMemory`, the loaders) doubles its cases.

The industry is unanimous here: Arrow, Polars and modern pandas all do A — the bitmap
is part of the column, optional at runtime, not in the type system. (Old pandas
effectively lived a variant of B's pain: nullability changed your dtype.)

> **Decision: always-nullable (A).** The `nil` bitmap makes the no-nulls case free.

## Axis 4 — Semantics: what nulls *mean*

The representation stores nulls; it does not decide what operations do with them.
Minimal set of decisions, following the SQL/pandas/polars convention:

| Operation | Behavior |
|---|---|
| `Sum` | **skips nulls** (sums the valid values) |
| `Mean` | divides by the count of *valid* rows, not by `Len` |
| `Count` | counts valid rows only (≠ `Len`) |
| `Info` | shows a per-column `non-null count`, like pandas |

The alternative (one null poisons the aggregate → result is null) is what SQL does
for *scalar* expressions (`1 + NULL = NULL`) but not for aggregates — every dataframe
user expects `Sum` to skip.

Comparisons (`null == null` → SQL's three-valued logic) are **explicitly parked**
until `Filter` exists: don't design what nothing uses yet.

Loader behavior follows: an empty CSV cell or a JSON `null` stops being an error and
becomes bit 0 in the validity bitmap + a `0.0`/`""` placeholder in the value buffer
(a placeholder operations never read).

> **Decision: SQL aggregate convention (skip nulls); three-valued logic deferred.**

## The simple version

Deciding "we'll use a clipboard at the parking lot entrance" (the validity bitmap from
the previous note) still leaves four questions: what the clipboard is made of (a thick
notebook with one page per spot — `[]bool` — or a single card with one checkbox per
spot — the bitmap: harder to write on, but you can scan 64 spots at a glance);
how the attendant answers "is spot 7 taken?" (always answering both "yes/no" and
"which car" in one breath — comma-ok); whether you need different parking lots for
"may have empty spots" and "always full" (no — one lot, and if it's full you just
don't hang the clipboard at all — `nil`); and what the monthly report does with empty
spots (it reports average occupancy of *occupied* spots — skip nulls — instead of
refusing to produce a report because one spot was empty).

## Further reading

- [`math/bits` package documentation](https://pkg.go.dev/math/bits) — the stdlib
  toolbox for the bitmap: `OnesCount64` (popcount) for counting valid rows 64 at a
  time, plus `TrailingZeros64` and friends that will matter when iterating set bits
  in `Filter`. Compiler-intrinsified on most architectures (single CPU instructions).
- [`database/sql.Null[T]`](https://pkg.go.dev/database/sql#Null) — the stdlib's own
  generic nullable type (Go 1.22): the axis-2 road we did not take for cell access,
  worth knowing because it is the standard way a null travels *as a value* in Go.
