# Writing data

The writers mirror the loaders: each format has a path version and an
`io.Writer` version.

```go
err := df.ToCSV("out.csv")
err = df.ToCSVWriter(os.Stdout)

err = df.ToJSON("out.json")
err = df.ToJSONWriter(&buf)
```

## CSV output

Header row first, then one record per row. Quoting follows `encoding/csv`'s
exact rules (the output is byte-identical to what the standard library would
produce — pinned by oracle tests), but the hot path is a hand-written
byte-level writer: writing a 100k-row dataframe costs 2 allocations, not two
per cell.

**Nulls become empty cells** (polars' default):

```
city,temp
madrid,21.5
bilbao,
```

## JSON output

A compact array of objects — exactly the shape `FromJSONReader` loads —
with literal `null` for missing values:

```json
[{"city":"madrid","temp":21.5},{"city":"bilbao","temp":null}]
```

JSON output rejects `NaN` and `±Inf` values with an error (JSON has no
representation for them; silently writing `null` would corrupt data).

## Round-trip guarantees

Write-then-read returns the same dataframe — with one documented caveat:

| Format | Round-trip |
|--------|-----------|
| JSON | **Exact**, all dtypes: values, nulls and floats reload byte-identically. |
| CSV, `Float64`/`Bool` columns | Exact: empty cell ↔ null. |
| CSV, `String` columns | **Asymmetric for nulls**: a null string writes as an empty cell, but an empty CSV cell *loads* as the empty string `""`, not as null (see [Loading data](loading-data.md#where-nulls-come-from)). A string column with nulls does not round-trip through CSV. |

If your string columns carry nulls and you need them back, use JSON. (A
configurable CSV null marker that would close this asymmetry is on the
roadmap.)

## Performance

Both writers are byte-level: floats are formatted with `strconv.AppendFloat`
into reused buffers, and JSON strings take a fast path when they contain no
characters needing escapes. Output equivalence with `encoding/csv` and
`json.Marshal` is enforced by oracle tests over a battery of tricky strings.
