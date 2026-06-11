# Loading data

grizzly is row-oriented at the boundary and columnar inside: you load data in
the row shape you have it (structs, CSV, JSON), and the loader transposes it
once into typed columns. After that, every operation is columnar.

## The three dtypes

Every column is one of three types — the JSON data model:

| DType | Go storage | Notes |
|-------|-----------|-------|
| `grizzly.Float64` | `[]float64` | Every number. Integers are exact up to 2^53. |
| `grizzly.String` | `[]string` | |
| `grizzly.Bool` | bit-packed | 1 bit per value, Arrow-style. |

There is deliberately no integer column — one numeric type keeps schemas and
type switches small. See [design decisions](../design/README.md).

## From Go structs

`FromStructs` takes a slice of structs and produces one column per exported
field. No schema needed: the types come from the fields. A `grizzly` tag
renames the column:

```go
type sale struct {
    Product string  `grizzly:"product"`
    Price   float64 `grizzly:"price"`
    Sold    bool    `grizzly:"sold"`
}
df, err := grizzly.FromStructs([]sale{
    {Product: "apple", Price: 1.5, Sold: true},
    {Product: "pear", Price: 2, Sold: false},
})
```

Fields must be `float64`, `string` or `bool`. Struct fields always have a
value in Go, so struct-loaded columns contain no nulls.

## From CSV and JSON: explicit schemas

Untyped sources load against a `Schema` — an ordered list of column names
and types that **you** declare. grizzly performs no type inference: a
`"08001"` zip code stays a string because you said so, never becoming the
number 8001 by guesswork.

```go
schema := grizzly.Schema{
    {Name: "city", Type: grizzly.String},
    {Name: "temp", Type: grizzly.Float64},
}

df, err := grizzly.FromCSV("cities.csv", schema)
df, err = grizzly.FromJSON("cities.json", schema)
```

Each loader has an `io.Reader` twin — `FromCSVReader`, `FromJSONReader` —
for data that doesn't live in a file (network responses, embedded strings,
decompression streams).

**CSV** expects a header row; parse errors report line and column context.
**JSON** expects an array of objects (`[{...}, {...}]`); each object's keys
are matched against the schema.

## Where nulls come from

| Source | Becomes null |
|--------|--------------|
| JSON `null` (any column type) | yes |
| Empty CSV cell, `Float64` column | yes |
| Empty CSV cell, `Bool` column | yes |
| Empty CSV cell, `String` column | **no** — `""` is a real (empty) string |
| Go struct field | never — struct fields always hold a value |

The string case is deliberate: unlike pandas, grizzly does not assume an
empty string means "missing". See [Nulls](nulls.md) for how operations treat
null values, and [Writing data](writing.md) for the round-trip implications.

## Performance notes

- `FromCSV` and `FromJSON` (the path variants) parse **in parallel chunks**
  for inputs from ~1 MiB up, using byte-level parsers written for the hot
  path. On a 1M-row file this is several times faster than the sequential
  path.
- `FromCSVReader` and `FromJSONReader` stay **streaming and sequential** —
  they never load the whole input into memory, which is what you want for
  unbounded readers.
- Parser behavior is pinned against the standard library by fuzz and oracle
  tests: same accepted inputs, same rejected inputs, same values out.
