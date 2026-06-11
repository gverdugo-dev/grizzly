# Design decisions

The architectural choices that define grizzly, distilled. Each decision
links to the dev note that maps its full design space — the alternatives
considered, the trade-offs, and why the winner won.

## 1. Row-oriented ingestion, column-oriented storage

Users load data in rows (structs, CSV records, JSON objects); grizzly stores
it in typed, contiguous columns. The transpose happens once, at the loading
boundary. Dataframe operations are inherently columnar — `Sum` reads one
column start to finish — and contiguous typed slices give sequential memory
access with no per-value boxing. The guiding principle: the more the
compiler knows at compile time, the faster the program.
→ [Stack vs heap](../../dev-notes/stack-vs-heap.md)

## 2. Three dtypes: float64, string, bool

Every number is a `float64` — the JSON model. One numeric type keeps
schemas, loaders and type switches small. Documented trade-off: integers are
exact only up to 2^53, so a 17-digit numeric ID would lose precision (load
it as a string).

## 3. Explicit schemas, no type inference

CSV and JSON load against a schema the user declares, like a database table
— never by sniffing values. Zero guessing bugs (`"08001"` stays a string),
and explicit-over-magic is the Go way. Struct loading needs no schema: the
field types are the schema.

## 4. Nulls: validity bitmaps with comma-ok access

Every column is nullable, backed by an Arrow-style `[]uint64` bitmap; a
`nil` bitmap means "no nulls", so null-free columns pay nothing. Values read
via comma-ok (`Value(i) (T, bool)`). Rejected alternatives: sentinel values
(NaN scars, old-pandas style) and pointer slices (per-value heap
allocations).
→ [Null handling](../../dev-notes/null-handling.md) ·
[Nulls in Go: the four decisions](../../dev-notes/nulls-in-go.md) ·
[Bitmaps and machine words](../../dev-notes/bitmaps-and-words.md)

## 5. Null semantics follow SQL

Aggregations skip nulls; comparisons yield *unknown* and flow through Kleene
three-valued logic; `WHERE` drops unknowns; `GROUP BY` gathers null keys
into one group; sorts put nulls first. One coherent, database-shaped answer
instead of per-operation improvisation — including the deliberate
WHERE/GROUP BY asymmetry (filtering asks a question; grouping partitions).

## 6. Column interface + typed implementations

A small `Column` interface (`Name`, `Len`, `DType`, `IsValid`, `NullCount`)
with one concrete type per dtype. Anything needing values type-switches to
the concrete column and reads the typed slice directly — interfaces at the
edges, concrete types in the hot loops.

## 7. Filter: comparators → masks → Where

Comparators produce packed boolean masks; masks combine word-level with
`And`/`Or`/`Not`; `Where` materializes once. Chosen over row predicates
(boxing, opaque to the engine) and expression strings (a parser project).
→ [Filter — the design space](../../dev-notes/filter-design-space.md)

## 8. GroupBy: hash factorize + eager Agg specs

One hash pass assigns group ids; aggregations are map-free slice-indexed
passes. Specs (`grizzly.Sum("price")`) are inspectable data, not closures.
Output in first-appearance order — deterministic despite Go's randomized
map iteration.
→ [GroupBy — the design space](../../dev-notes/groupby-design-space.md)

## 9. Zero dependencies, permanently

The standard library is the only import. Where stdlib performance wasn't
enough (JSON parsing, CSV writing), grizzly reimplements the hot path
byte-level — and pins behavioral equivalence with fuzz and oracle tests
against the stdlib instead of trusting itself.
→ [Parsing JSON by hand](../../dev-notes/json-byte-parser.md) ·
[v0.2.0 performance principles](../../dev-notes/v0.2.0-principles.md)

## Open designs

Decisions still being made live in the dev notes' living document —
currently [Join](../../dev-notes/join-design-space.md).
