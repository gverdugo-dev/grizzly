# Filter: the design space

Learning note. Context: `Filter` is the next v0.1.0 piece. This note maps the
candidate API designs and their trade-offs; the discussion settled on
**option C (columnar comparators + combinable masks)** — recorded at the end —
chosen for building on proven designs (polars/NumPy) rather than inventing one.
It also covers the semantics question that arrives with filtering whether we
like it or not: what does a comparison against null produce, and how do nulls
combine under AND/OR (the three-valued logic explicitly parked in
[Nulls in Go](nulls-in-go.md)).

Whatever the API, the engine underneath is the same: build a **row mask**
(which rows survive), then **materialize** a new Dataframe by gathering the
surviving rows of every column. The options differ in how the user *expresses*
the mask.

## Option A: row predicate — `df.Filter(func(row Row) bool)`

The instinct coming from Go's `slices.DeleteFunc` or any ORM: one callback
that sees a whole row.

- ✅ One method, infinitely expressive (any condition over any columns).
- ✅ Familiar shape for Go users.
- ❌ **Requires a `Row` abstraction that grizzly deliberately does not have.**
  Rows would be materialized views over columns: every accessed value either
  boxed into `any` or reached through per-call type switches — the exact
  per-row overhead the columnar design exists to avoid.
- ❌ Opaque to the engine: a closure cannot be inspected, split, or pushed
  down. Nothing can ever be optimized about it.
- ❌ Null handling lands on the user: every closure must remember to check
  validity itself.

## Option B: typed per-column predicate — `df.FilterFloat64("price", func(v float64) bool)`

One method per dtype, taking a typed Go closure for one column.

- ✅ Type-safe, no boxing, simple to implement (a tight loop over one typed
  slice; nulls skipped by the engine before the closure runs).
- ✅ Still very expressive for single-column conditions (regex on strings,
  ranges, anything Go can write).
- ❌ Method-per-type API growth (`FilterFloat64`, `FilterString`,
  `FilterBool`...) — the old pandas-dtype-explosion smell, in API form.
- ❌ **Combining conditions is chaining**, and each link materializes a full
  intermediate Dataframe: `df.FilterFloat64(...).FilterString(...)` copies
  the data twice for what is logically one pass.
- ❌ A closure call per row: correct, but the compiler cannot vectorize
  through it, and the engine still cannot see inside the predicate.

## Option C: columnar comparators + combinable masks (leading candidate)

The polars/NumPy model, adapted to Go. Comparators on the Dataframe produce
**masks**; masks combine with logical operators; one final call materializes:

```go
inStock, _ := df.Eq("in_stock", true)
cheap, _   := df.Lt("price", 2.0)
uptown, _  := df.Eq("store", "uptown")

result, _ := df.Where(cheap.And(uptown.Or(inStock))) // one materialization
```

A mask is conceptually a `BoolColumn` without a name: packed words for the
bits, validity for the nulls — machinery grizzly already has.

- ✅ **Pure columnar**: each comparator is a tight typed loop with no closure
  indirection; the comparison constant is known for the whole loop.
- ✅ **Mask combination is word-level**: `And`/`Or`/`Not` AND/OR 64 rows per
  instruction over the packed words — the bitmap's second superpower, and the
  reason this option makes grizzly's design shine.
- ✅ One materialization regardless of how many conditions combine.
- ✅ Masks are composable values: build once, reuse against several calls.
- ✅ Null semantics live in the engine, in exactly one place (see below).
- ❌ Bigger API surface: operators × types (`Eq`, `Ne`, `Lt`, `Le`, `Gt`,
  `Ge`... each accepting the right operand types), plus the `Mask` type and
  its methods.
- ❌ Two-step ergonomics: simple cases read less directly than one closure.
- ❌ Arbitrary predicates (regex, math on the value) need either an escape
  hatch — a B-style typed predicate that *returns a mask* — or don't exist.

The escape hatch deserves note: B composes *inside* C. A
`MaskFloat64("price", func(v float64) bool) Mask` gives closures back their
expressiveness while keeping combination and materialization columnar.

## Option D: expression strings — `df.Filter("price > 2 AND store = 'uptown'")`

SQL-ish strings, pandas' `query()`.

- ✅ Reads beautifully.
- ❌ Requires a lexer, parser, type checker and evaluator — a query language
  implementation. A fascinating project, and a different one. **Discarded**
  for grizzly (revisitable as a layer on top of C someday: parse to masks).

## The semantics that arrive with Filter: three-valued logic

Filtering forces the question nulls postponed. What is `price > 2.0` when
price is null? Not true, not false — **unknown** (null). SQL resolves this
with Kleene's three-valued logic, and the rule that matters for filtering is:

> **WHERE keeps a row only when the condition is TRUE.** Unknown is not
> true, so null rows are dropped.

Combination follows Kleene's tables — unknown wins unless the other operand
decides the outcome alone:

| a | b | a AND b | a OR b |
|---|---|---------|--------|
| true | unknown | unknown | **true** |
| false | unknown | **false** | unknown |
| unknown | unknown | unknown | unknown |

In mask terms this falls out naturally if masks carry validity: a comparison
against a null row yields an *invalid* mask bit; `And`/`Or` combine validity
with exactly the Kleene tables (e.g. `false AND unknown` is a *valid* false:
the false decided alone); `Where` keeps rows whose bit is **valid and true**.
NOT flips the value bit and leaves validity untouched (`NOT unknown` is
unknown).

This is why option C centralizes null handling: the Kleene tables are
implemented once, in the mask combinators, instead of being every closure's
responsibility.

> **Decision: option C — columnar comparators + combinable masks.** The
> deciding argument: it follows a proven model (polars/NumPy/Arrow) instead
> of inventing one, and it lets the bitmap machinery grizzly already built
> carry the feature. The B-style escape hatch (typed predicates returning
> masks) stays available if comparators ever fall short. D remains discarded;
> A is rejected outright (anti-columnar).

## The simple version

You're picking guests for a party from a long list. Option A: interview every
person one by one, asking everything (thorough, but you can't delegate or
speed it up — and you must remember yourself what to do when someone's age is
blank). Option B: shout one question at the whole room at a time ("everyone
under 30, stay!"), but after each question the remaining people must
physically move to a new room. Option C: hand out clipboards — each question
marks checkboxes on its own clipboard, you overlay the clipboards at the end
(two transparent sheets, one glance) and move people once; anyone whose
checkbox is a "?" (blank age) simply doesn't get in, per the bouncer's rule
that only a clear YES enters. Option D: hire someone who understands written
instructions in fancy legalese — great, but now you employ a lawyer.

## Further reading

- [Polars — Expressions and contexts](https://docs.pola.rs/user-guide/concepts/expressions-and-contexts/)
  — the industrial version of option C: lazy, composable expressions that the
  engine can inspect, optimize and parallelize; what grizzly's masks would
  grow into if they ever became lazy.
- [Three-valued logic](https://en.wikipedia.org/wiki/Three-valued_logic) —
  Kleene's K3 logic and the exact fragment SQL uses for NULL comparisons:
  the truth tables grizzly's mask combinators would implement.
