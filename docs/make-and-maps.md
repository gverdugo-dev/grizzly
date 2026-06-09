# `make` and `map` in Go

Learning note. Context: `NewDataframe` uses `make(map[string]bool, len(cols))` to
detect duplicate column names, and the constructors will use `make([]T, 0, n)`
constantly when transposing rows → columns. Both are applications of grizzly's guiding
principle: *give the program information up front and it runs faster*.

## `map`: yes, it's Python's dict (but not a JSON)

A Go `map[K]V` is a **hash table** — the same data structure as Python's `dict`:
unique keys, amortized O(1) lookup/insert/delete.

JSON is a different thing entirely: JSON is **text**, a serialization format. A map is
a *live in-memory structure*. The relationship: a JSON object (`{"a": 1}`) is usually
*deserialized into* a `map[string]any` — just like Python's `json.loads` gives you a
dict. The map is the destination; JSON is the transport format.

Key differences from Python's dict:

| | Python `dict` | Go `map[K]V` |
|---|---|---|
| Types | anything, mixed | K and V fixed at declaration: `map[string]float64` accepts only that |
| Order | insertion order (since 3.7) | **deliberately randomized** — Go shuffles iteration order so nobody depends on it |
| Missing key | raises `KeyError` | returns the value type's *zero value* (`0`, `""`, `false`); use the "comma ok" idiom to distinguish: `v, ok := m["x"]` |

Two grizzly decisions explained by this table:

- `Dataframe` stores columns in a **slice, not a map**: maps don't preserve order, and
  column order matters (printing, serialization).
- The `seen` set in `NewDataframe` is a `map[string]bool`: asking for an absent key
  returns `false` — exactly the semantics of "have I seen this name before?".

## `make`: the initializer for types with internal machinery

Most Go types are born usable at their zero value: `var x float64` → `0`,
`var s []int` → a nil slice you can `append` to. But three types have internal
machinery (hash buckets, backing arrays, buffers) that *someone* must build:
**map, slice and channel**. That someone is `make`:

```go
m := make(map[string]bool)      // empty hash table, ready for writes
m := make(map[string]bool, 10)  // capacity hint: pre-size buckets for ~10 keys
s := make([]float64, 0, 1000)   // len=0 but capacity 1000 already reserved
```

Why it matters: **a nil map cannot be written to.**

```go
var m map[string]bool  // nil map (zero value)
_ = m["x"]             // reading is fine: returns false
m["x"] = true          // 💥 panic: assignment to entry in nil map
```

### Capacity hints are pure performance

The second argument to `make` is the up-front information principle in action:

- `make(map[string]bool, n)` — "I'll insert ~n keys": buckets are allocated once
  instead of growing and re-hashing as entries arrive.
- `make([]T, 0, n)` — `append` fills the reserved capacity without reallocating.
  Without the hint, appending 1M values reallocates + copies the backing array ~20
  times (capacity grows geometrically). With it: zero reallocations.

This is why the row→column transposition in grizzly's constructors will always
pre-size its column slices when the row count is known.

## The simple version

A **map** is a filing cabinet with labeled drawers: you ask for the label, you get the
contents instantly, no matter how many drawers there are. Python calls it a dict; JSON
is just a *photo* of the cabinet printed on paper — useful for sending by mail, but
you can't put anything into the photo.

**`make`** is the carpenter who builds the cabinet. Declaring `var m map[string]bool`
just *names* a cabinet that doesn't exist yet — look inside (read) and you see
nothing, fine; try to file something (write) and there's no furniture: crash. `make`
builds it. And the capacity hint is telling the carpenter "I'll need room for 1,000
folders" so he builds it the right size once, instead of rebuilding a bigger cabinet
every time you run out of drawers.

## Further reading

- [Go maps in action](https://go.dev/blog/maps) — the official Go blog walkthrough of
  maps: declaration, the comma-ok idiom, exploiting zero values (the `map[string]bool`
  set trick we use in `NewDataframe`), and why concurrent writes need a mutex. The
  canonical next step.
- [Go Slices: usage and internals](https://go.dev/blog/slices-intro) — how slices work
  under the hood (pointer + len + cap over a backing array) and what `append`/`copy`
  really do; explains *why* the capacity hint avoids reallocations.
