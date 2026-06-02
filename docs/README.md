# grizzly — Design Docs

> **grizzly** is a DataFrame library written from scratch in Go.
> A panda is a bear. A grizzly is a faster, meaner one. 🐻

This folder is the **design notebook** for the project. Its purpose is *not* to be a
final spec — it's a set of discussion documents, one per design decision, so we can
argue each one on its own before writing code.

The project has two goals, in this order:

1. **Learn Go by building a real, non-trivial system by hand.** We deliberately
   implement the hard parts ourselves (columnar memory, typed columns, the group-by
   engine, parallelism) instead of leaning on existing libraries.
2. **Maybe fill a real gap.** There is no maintained, idiomatic, fast, native Go
   DataFrame. If grizzly turns out well, the niche is open.

## How to read these docs

Each numbered doc covers one design aspect we can discuss independently. They follow
the same shape: the question, why it matters, the options with trade-offs, the current
leaning, open questions, and references to articles that explain the topic.

**Status legend:**
- 🟡 **Open** — not decided yet, up for debate
- 🟢 **Decided** — we've agreed a direction (recorded for posterity)
- ⚪ **Later** — out of scope for now, parked on purpose

## Index

| Doc | Topic | Status |
|-----|-------|--------|
| [00 — Vision & Scope](00-vision-scope.md) | What grizzly is, goals, non-goals | 🟡 |
| [01 — Prior Art](01-prior-art.md) | Existing Go DataFrames, pandas/Polars internals | 🟡 |
| [02 — Memory Model](02-memory-model.md) | Columnar layout, Series, build-by-hand vs Arrow | 🟡 |
| [03 — Type System](03-type-system.md) | Generics vs interfaces for typed columns | 🟡 |
| [04 — Null Handling](04-null-handling.md) | `[]bool` mask vs validity bitmap | 🟡 |
| [05 — Execution Model](05-execution-model.md) | Eager vs lazy evaluation | 🟡 |
| [06 — Engine: GroupBy & Join](06-engine-groupby-join.md) | The core algorithms | 🟡 |
| [07 — Concurrency](07-concurrency.md) | Goroutines, parallel aggregation | 🟡 |
| [08 — I/O](08-io.md) | CSV / Parquet reading | 🟡 |
| [09 — Performance & Benchmarking](09-performance-benchmarking.md) | testing, pprof, SIMD | 🟡 |
| [10 — API Design](10-api-design.md) | Idiomatic Go public API | 🟡 |
| [Roadmap](roadmap.md) | Phased plan, each phase = a Go concept | 🟡 |

## Ground rule: scope discipline

The fastest way to kill a learning project is unbounded scope. Each doc has a
**non-goals** section. When in doubt, cut. We can always add a feature later; we can't
finish a project that never stops growing.
