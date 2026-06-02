# 10 — API Design

> Status: 🟡 Open for discussion
> Affects: how the library *feels*, adoption, maintainability

## The question

What does the public API look like? Method chaining like pandas/Polars, or something more
idiomatically Go? How do we handle errors, options, and immutability in the surface?

## Why it matters

The internals can be brilliant and the library still unusable if the API fights the
language. This is where we learn idiomatic Go package and API design — and where we decide
whether grizzly feels like "pandas in Go" or like "a Go library that does dataframes."

## Tensions specific to Go

- **Method chaining vs errors.** pandas/Polars chain fluently (`df.filter().group_by().agg()`),
  but Go's idiom is `result, err := ...`. Chaining and explicit errors conflict. Options:
  - Store a deferred error on the frame, checked at the end (fluent, less idiomatic).
  - Return `(`*`DataFrame, error)` at each step (idiomatic, verbose).
  - Panic on programmer errors (bad column name), return errors only for runtime/data errors.
- **Functional options** for configuration (`ReadCSV(path, WithHeader(true), WithDelim(';'))`)
  — the idiomatic Go pattern for optional params (Go has no keyword args).
- **Immutability.** Copy-on-write frames are safer with goroutines and match qframe's design,
  at the cost of more allocation. Mutable is cheaper but dangerous to share.

## A possible shape

```go
df, err := grizzly.ReadCSV("sales.csv", grizzly.WithHeader(true))

out, err := df.
    Filter(grizzly.Col("country").Eq("ES")).
    GroupBy("city").
    Agg(grizzly.Sum("sales"), grizzly.Mean("margin"))

fmt.Println(out)   // pretty-printed table via String()
```

This needs an **expression API** (`grizzly.Col("x").Gt(10)`) — a small DSL for columnar
predicates, which is also the bridge to a future lazy engine ([05](05-execution-model.md)).

## Open questions

- [ ] Deferred-error chaining vs `(df, err)` everywhere — pick one and commit. (Leaning: deferred error for fluency, documented clearly.)
- [ ] How Go-flavored vs how pandas-familiar? Naming: `GroupBy` vs `Group`, `Filter` vs `Where`.
- [ ] Expression DSL design: how much do we build before it's worth it?
- [ ] `String()` / pretty-printing format for the REPL/terminal experience.
- [ ] One package or sub-packages (`grizzly`, `grizzly/expr`, `grizzly/io`)?

## References

- [Effective Go](https://go.dev/doc/effective_go) — the baseline for idiomatic Go API and naming.
- [Package names (Go blog)](https://go.dev/blog/package-names) — naming the package and its symbols well.
- [Functional options for friendly APIs (Dave Cheney)](https://dave.cheney.net/2014/10/17/functional-options-for-friendly-apis) — the options pattern for `ReadCSV` & friends.
