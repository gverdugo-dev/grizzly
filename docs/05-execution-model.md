# 05 — Execution Model: Eager vs Lazy

> Status: 🟡 Open for discussion
> Affects: API shape, performance ceiling, complexity budget

## The question

Does each operation run immediately (eager, like pandas), or do we build a plan and only
execute on `.collect()` (lazy, like Polars)?

## Why it matters

This decides both the API and how fast we can ever be. Lazy is where Polars' biggest wins
come from — but a query optimizer is a whole project on its own. Getting this ordering
wrong (lazy too early) is the classic way to drown a learning project.

## Options

### Option A — Eager ✅ (leaning, to start)
Every call computes a new result now.
```go
df.Filter(...).GroupBy("k").Agg(Sum("x"))   // each step runs immediately
```
- **Pro:** Simple, intuitive, easy to debug, easy to test. Matches pandas mental model.
- **Con:** No cross-operation optimization; materializes intermediates.

### Option B — Lazy
Operations build a logical plan; execution + optimization happen on `.collect()`.
- **Pro:** Predicate/projection pushdown, operation reordering, less memory — the real wins.
- **Con:** Needs a plan representation, an optimizer, and an executor. Big complexity jump.

## Current leaning

**Eager first, no debate.** Lazy is a **🟢→⚪ "final boss"** parked for a late phase
(see [roadmap](roadmap.md)), attempted only once the eager engine and the language are
solid. When we do tackle it, the natural path is:

1. Build a logical plan (a tree of operations).
2. Add the two highest-value optimizations: **projection pushdown** (drop unused columns
   early) and **predicate pushdown** (filter before heavy ops).
3. Keep eager as the default; lazy as an opt-in `LazyFrame`.

Designing the eager API so a lazy layer can wrap it later (operations as describable
steps) is a cheap insurance we *can* take now.

## Open questions

- [ ] Volcano/iterator model vs vectorized (batch) execution when we get to lazy? Vectorized fits columnar better.
- [ ] Do we even need lazy for the learning goals, or is it scope creep dressed as ambition?
- [ ] Streaming (larger-than-RAM) — explicitly out of scope, or a distant dream?

## References

- [Polars — Lazy API concepts](https://docs.pola.rs/user-guide/concepts/lazy-api/) — what lazy buys you and how `.collect()` works.
- [Query optimization (overview)](https://en.wikipedia.org/wiki/Query_optimization) — background on the optimizations a lazy engine performs.
