# 03 — Type System: Generics vs Interfaces

> Status: 🟡 Open for discussion
> Affects: the entire core architecture — **the central decision**

## The question

A column of `int64` and a column of `float64` should share operations, but a DataFrame
holds columns of *different* types together. How do we model typed columns in Go's type
system?

## Why it matters

This is *the* Go-specific design fork in the whole project, and it has no clean answer.
Understanding *why* you can't just "use generics everywhere" teaches you the real shape
and limits of Go's type system. This is the pedagogical heart of grizzly.

## The tension

- **Generics** (`Column[T]`) give clean, fast, boxing-free code per type. **But** Go has
  no generic methods, and you **cannot** hold `[]Column[T]` with mixed `T` — so a
  heterogeneous DataFrame can't be a slice of a generic type.
- **Interfaces** (`type Column interface{...}`) let a DataFrame hold `[]Column` with mixed
  concrete types. **But** you pay dynamic dispatch and sometimes boxing in hot loops.

## Options

### Option A — Pure generics
Clean per-type code, but you hit the wall the moment the DataFrame needs mixed columns.
Realistically impossible as the *only* mechanism. ❌

### Option B — Pure interfaces (`any`-backed)
A `Column` interface with methods like `Len()`, `Get(i) any`, `Sum() any`. Works, simple,
but `any` boxing in tight loops kills performance and loses type safety. 😐

### Option C — Hybrid ✅ (leaning)
- A non-generic `Column` **interface** so the DataFrame can store `[]Column`.
- Generic `typedColumn[T]` **implementations** underneath (`int64`, `float64`, …).
- In hot kernels, **type-switch** down to the concrete slice (`[]float64`) and run a tight,
  monomorphic loop — no boxing where it counts.

```go
type Column interface {
    Len() int
    DType() DType
    // ... type-erased ops for the DataFrame layer
}

type typedColumn[T Numeric] struct {
    data  []T
    valid []bool
}

// hot path: recover the concrete type once, loop tight
func sumFloat(c Column) float64 {
    tc := c.(*typedColumn[float64])
    var s float64
    for _, v := range tc.data { s += v }
    return s
}
```

**Current leaning:** **Option C (hybrid).** It's the idiomatic Go answer and the most
instructive — you feel exactly where generics help and where interfaces are unavoidable.

## Open questions

- [ ] How many type-switch branches can we tolerate before it gets ugly? Code generation (`go generate`) as an alternative to hand-writing per-type kernels?
- [ ] Do we expose generics in the *public* API, or keep generics internal and present a clean type-erased surface to users?
- [ ] `constraints.Ordered` / a custom `Numeric` constraint — which operations need which constraints?

## References

- [An Introduction To Generics (Go blog)](https://go.dev/blog/intro-generics) — the basics and the limitations (no generic methods).
- [When To Use Generics (Go blog)](https://go.dev/blog/when-generics) — official guidance on when generics help and when they don't.
- [Go Data Structures: Interfaces (Russ Cox)](https://research.swtch.com/interfaces) — how interface dispatch and boxing actually work under the hood.
- [Generics tutorial](https://go.dev/doc/tutorial/generics) — hands-on starting point.
