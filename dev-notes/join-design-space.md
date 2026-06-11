# Join: the design space

Learning note. Context: `Join` is the v0.3.0 milestone — the first operation that
takes **two** dataframes, which is why it gets its own phase ("relational"). This
note maps the design space before the design discussion settles it: six axes,
with a leading candidate per axis but **no decisions recorded yet** — the
discussion comes first, then this note's decision block gets written (the
Filter/GroupBy pattern). The conceptual frame: a join matches rows of a left and
a right dataframe by comparing key columns, and emits combined rows for the
matches.

```
left:                right:               left.InnerJoin(right, "store"):
store     price      store     city       store     price  city
downtown  1.5        downtown  Bilbao     downtown  1.5    Bilbao
downtown  0.75       airport   Loiu       downtown  0.75   Bilbao
uptown    3.2
```

(`uptown` has no match → dropped by inner; `airport` matched nothing → dropped
too. What to do with those rows is exactly axis 2.)

## Axis 1 — Algorithm: hash join vs sort-merge

**Hash join** (the industry default for unsorted in-memory data): two phases.
*Build* — walk the smaller side once, building a hash table from key → row
indices. *Probe* — walk the larger side once; each key looks up its matches in
the table. O(n + m) total.

**Sort-merge join**: sort both sides by key, then advance two cursors in
lockstep emitting matches. O(n·log n + m·log m), unless the data is already
sorted — databases pick it then, because the merge phase is a single
cache-friendly pass.

- ✅ Hash: linear, no reordering of either input, and grizzly already owns the
  machinery — `factorizeSlice` *is* a build phase (typed map from key to id),
  and `gatherRows` *is* the materialization step. A hash join is morally
  "factorize one side, probe with the other, gather both".
- ❌ Hash: per-distinct-key map overhead (the same cost GroupBy accepted);
  duplicate keys need the table to hold *lists* of row indices, not single ids
  — the one place it outgrows `factorizeSlice` as-is.
- ✅ Sort-merge: `Sort` exists now (it didn't when GroupBy chose); ordered
  output for free.
- ❌ Sort-merge: pays two full sorts even when one side is 10 rows; nulls-first
  ordering interacts messily with "nulls never match" (axis 4).

*Leading candidate: hash join, building on the smaller side* — consistent with
GroupBy's choice, reuses its mental model, and it is what pandas, polars and
every database's default equi-join do.

## Axis 2 — Join types, and which ones v0.3.0 needs

The full menu (polars supports all of these): **inner** (only matches),
**left** (all left rows; unmatched ones get nulls in the right's columns),
**right** (mirror), **full** (all rows from both sides), **semi** (left rows
*that have* a match — a filter, no right columns), **anti** (left rows
*without* a match), **cross** (cartesian product, no key).

The engineering observation: they are one algorithm with different
bookkeeping. Inner emits only probe hits; left also emits probe misses (with
a null row from the right); full additionally tracks which build-side rows
were never hit. Semi/anti don't combine columns at all — they reduce to a
mask over the left side, which grizzly's `Where` machinery already knows how
to materialize. Right is left with the arguments flipped.

- ✅ Inner first: the smallest complete vertical slice — algorithm, duplicates,
  nulls, collisions all get exercised.
- ✅ Left second: forces the "emit a null row" path, which is where the validity
  bitmap earns its keep (unmatched right columns are *null*, not zero — the
  exact mistake sentinel-value engines make).
- ❌ Full/semi/anti/cross in v0.3.0: scope creep; none of them changes the
  design, only the bookkeeping. They can land later without API breakage.

*Leading candidate: inner + left in v0.3.0*, rest unscheduled.

## Axis 3 — Duplicate keys: how many output rows?

If `downtown` appears twice on the left and three times on the right, SQL says
the join emits **2 × 3 = 6 rows** — every pairing of matches. That is the only
semantics that makes joins associative and is what SQL, pandas and polars all
do. The alternatives (first match only, or erroring on duplicates) silently
drop data or forbid legitimate inputs.

The real-world footgun is *accidental* duplicates: joining on a key you
believed unique and getting a row explosion. Polars addresses it without
changing semantics — a `validate` parameter (`"m:m"` default, `"1:1"`,
`"1:m"`, `"m:1"`) that checks the keys' actual cardinality and errors if the
data violates the declaration.

- ✅ Cartesian-per-key (SQL): predictable, composable, what every reference
  engine does; no data loss.
- ❌ Output size is data-dependent (n·m worst case) — but that is inherent to
  the operation, not a design flaw.

*Leading candidate: SQL semantics; a `validate`-style check is a candidate
follow-up, not v0.3.0 scope.*

## Axis 4 — Null keys: the GroupBy precedent does NOT transfer

GroupBy decided null == null (all null keys form one group). It is tempting to
carry that into Join — and it would be wrong, because the two operations ask
different questions:

- **Grouping is partitioning**: every row must land somewhere, so "the rows
  whose key is unknown" form a legitimate bucket. No comparison between two
  unknowns is ever asserted.
- **Joining is comparison**: emitting a match for two null keys asserts
  `unknown == unknown` → true — exactly what Kleene logic forbids, and what
  grizzly's own `Where` already implements (null comparisons are unknown,
  unknown rows don't pass).

SQL is explicit about this asymmetry: GROUP BY puts NULLs in one group, but
`NULL = NULL` in a join predicate is unknown, so null keys match nothing —
not even each other. (A left join still *keeps* the left row with a null key;
it just matches no right row.)

Polars provides the cautionary tale: before 0.20 it treated null keys as
regular values — nulls matched nulls. Version 0.20 **flipped the default** to
the SQL rule, keeping the old behavior behind an opt-in flag (`join_nulls=True`,
renamed `nulls_equal` in 1.24). A breaking semantic change shipped in a 0.x
minor — both a join lesson and a live example of the Cargo-style versioning
described in [Semver and Go modules](semver-and-go-modules.md).

- ✅ SQL rule (nulls never match): consistent with grizzly's Kleene masks,
  matches every engine's current default, no rows fabricated from unknowns.
- ❌ Surprises users coming from GroupBy's null-is-a-key behavior — worth a
  doc comment on the method explaining the asymmetry.

*Leading candidate: the SQL rule.* An `nulls_equal`-style opt-in only if a
real use case ever asks for it.

## Axis 5 — Column-name collisions and the key column

Both inputs may carry a column named `total`. And the key column itself exists
on both sides — should the output have one `store` or two?

For the **key column**, every engine agrees on the happy path: inner and left
joins emit a single key column (the values are equal on matches by
definition; on left-join misses the left value is the one that exists).
Polars calls this *coalescing* and does it automatically for inner/left/right;
only full joins keep both sides (there a row may have only the right key).

For **non-key collisions**, the options:

- **Suffix the right side**: `total` and `total_right`. Polars' choice
  (suffix `_right`); pandas does the same shape with `_x`/`_y`. Predictable,
  nothing dropped, output column names derivable without running the join.
- **Error**: force the user to `Select`/rename first. Strictest, zero
  ambiguity — and grizzly already has `Select`; a rename helper is trivial.
  But it makes the common case (wide tables sharing a few metadata columns)
  annoying.
- **Qualified names** (`right.total`): SQL's answer, but it needs a notion of
  frame aliases that grizzly's API doesn't have — and dots in column names
  poison every later lookup.

*Leading candidate: coalesced key + `_right` suffix for collisions* — the
polars behavior, boring and predictable. Erroring is the defensible
alternative; to be settled in discussion.

## Axis 6 — API shape in Go

The first two-dataframe method. Candidate shapes:

**A. One method per type**:

```go
out, err := left.InnerJoin(right, "store")
out, err := left.LeftJoin(right, "store")
```

**B. One method + a join-type value**:

```go
out, err := left.Join(right, "store", grizzly.Inner)
```

where `Inner`/`Left` are typed constants (`type JoinType int` + `const Inner
JoinType = iota...`) — the Go enum pattern; a plain string (`"inner"`) would
defer typos to runtime.

- ✅ A: smallest possible signatures, each method documents its own null/miss
  semantics, no enum vocabulary. Reads like grizzly's existing API (`Sort` /
  `SortDesc` made the same choice — two methods, not a direction flag).
- ❌ A: every future join type is a new exported method.
- ✅ B: one docstring, one place to grow (`validate`, suffix options could
  become functional options later).
- ❌ B: the join type reads as an argument you must remember; mixed
  abstraction (data + behavior selector).

Either shape needs the same decisions underneath: same-name key on both sides
only (`on "store"`), or also `leftOn`/`rightOn`? Single key column first
(GroupBy's scope-control move — multi-key joins compose key ids the same way
multi-key GroupBy will)?

*Leading candidate: A (per-type methods), single same-name key, multi-key and
asymmetric keys later.* The `Sort`/`SortDesc` precedent is the strongest
argument. Open for discussion.

## The simple version

Two guest lists: yours (names + what they're bringing) and the venue's (names
+ table number). A join writes the combined list. Hash vs sort-merge (axis 1):
copy the *shorter* list into a phone-book index once, then walk the longer
list looking each name up — versus alphabetizing both lists and reading them
side by side. Join types (axis 2): inner keeps only people on both lists; left
keeps all your guests, leaving table number blank for the ones the venue
doesn't know. Duplicates (axis 3): if two "García" entries are on your list
and three on theirs, you honestly don't know which is which — so you write
all six pairings, like SQL does. Nulls (axis 4): a guest with a blank name
matches nobody — not even the venue's blank entry, because "unknown equals
unknown" is not a match (though GroupBy would happily seat all the blanks at
one table — different question). Collisions (axis 5): if both lists have a
"notes" column, the venue's gets renamed "notes_right" so neither is lost.

## Further reading

- [Polars — Joins](https://docs.pola.rs/user-guide/transformations/joins/) —
  the full join-type menu (equi, semi/anti, non-equi, asof) with examples,
  plus the coalescing and `_right`-suffix behavior grizzly is weighing on
  axis 5. The reference engine's user-facing semantics.
- [Hash join (Wikipedia)](https://en.wikipedia.org/wiki/Hash_join) — the
  classic build/probe formulation behind axis 1, why you build on the smaller
  side, and the grace/hybrid variants databases use when the build side
  doesn't fit in memory (out of scope for grizzly, good perspective).
