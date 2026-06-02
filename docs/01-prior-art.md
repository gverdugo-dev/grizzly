# 01 — Prior Art

> Status: 🟡 Open for discussion
> Affects: positioning, what to learn from / avoid

## The question

What already exists in the Go DataFrame space, why did those projects stall, and what
can we steal (ideas) or reject (mistakes)?

## Why it matters

We're not the first to try this. The graveyard tells us where the hard parts are and why
"native Go DataFrame" is still an open niche. It also tells us what *not* to do.

## The landscape

| Project | What it is | Verdict |
|---------|-----------|---------|
| **gota** (`go-gota/gota`) | The best-known Go DataFrame. | Row-ish design, weak performance, effectively unmaintained. |
| **qframe** (`tobgu/qframe`) | Immutable, columnar, cleaner design. | Good ideas, largely stalled. |
| **dataframe-go** (`rocketlaunchr`) | Time-series leaning. | Heavy API, half-alive. |
| **Apache Arrow Go** | Columnar memory primitives. | Alive, but a *building block*, not a user-facing DataFrame. |
| **polars (Go bindings)** | FFI wrapper over Rust Polars. | Not native Go — defeats our purpose. |

**Takeaway:** there is genuinely no maintained, idiomatic, fast, *native* Go DataFrame.
The niche is open — but it's open *because it's a lot of work*, which is exactly why the
others stalled. Scope discipline (see [00](00-vision-scope.md)) is our edge.

## What to learn from the giants

- **pandas** — the `BlockManager` consolidates same-dtype columns into contiguous NumPy
  blocks. Powerful but legacy; even its creator criticizes it. We learn *the idea*
  (columns grouped by type) without copying the complexity.
- **Polars** — columnar (Apache Arrow model), multi-threaded, lazy query optimizer.
  This is our north star for architecture, even if we don't match its speed.

## Open questions

- [ ] Do we read qframe's source for inspiration, or stay clean-room to learn more?
- [ ] Worth checking each library's last-commit date to confirm "stalled" before we cite it publicly?

## References

- [gota](https://github.com/go-gota/gota) — the canonical Go DataFrame attempt.
- [qframe](https://github.com/tobgu/qframe) — immutable columnar design worth studying.
- [dataframe-go](https://github.com/rocketlaunchr/dataframe-go) — time-series oriented.
- [Apache Arrow Go](https://github.com/apache/arrow-go) — columnar primitives we deliberately re-implement to learn.
- [The one pandas internal I teach all my new colleagues: the BlockManager](https://uwekorn.com/2020/05/24/the-one-pandas-internal.html) — pandas' core data structure explained.
- [Polars vs pandas (Real Python)](https://realpython.com/polars-vs-pandas/) — architectural contrast.
