# Getting started

## Install

```bash
go get github.com/gverdugo-dev/grizzly
```

Requires Go 1.26+. grizzly has zero dependencies — `go get` pulls nothing
else into your module.

## Your first dataframe

A complete program — paste it into a `main.go` and run it:

```go
package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/gverdugo-dev/grizzly"
)

func main() {
	// Untyped sources (CSV, JSON) load against an explicit schema:
	// you declare each column's type, grizzly never guesses.
	data := "city,temp\nmadrid,21.5\nbilbao,\nvalencia,28.25\nsevilla,35.5\n"
	schema := grizzly.Schema{
		{Name: "city", Type: grizzly.String},
		{Name: "temp", Type: grizzly.Float64},
	}

	df, err := grizzly.FromCSVReader(strings.NewReader(data), schema)
	if err != nil {
		log.Fatal(err)
	}

	// bilbao's empty temp cell loaded as a real null — not a fake 0.
	fmt.Println(df)
	// city      temp
	// madrid    21.5
	// bilbao    null
	// valencia  28.25
	// sevilla   35.5

	// Filter: comparators build masks, Where materializes the rows.
	warm, err := df.Gt("temp", 25.0)
	if err != nil {
		log.Fatal(err)
	}
	hot, err := df.Where(warm)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(hot)
	// city      temp
	// valencia  28.25
	// sevilla   35.5

	// Aggregate: nulls are skipped, SQL-style.
	avg, err := df.Avg("temp")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("avg temp:", avg) // mean of the 3 valid values
}
```

To load a file instead of a string, swap the reader call for the path
variant: `grizzly.FromCSV("cities.csv", schema)`.

## The shape of the API

Three things to know, and the rest of the API will feel predictable:

1. **Errors are values.** Every fallible operation returns `(result, error)`.
   Sentinel errors (`grizzly.ErrColumnNotFound`, `grizzly.ErrTypeMismatch`,
   `grizzly.ErrNoValidValues`) support `errors.Is` when you need to branch.
2. **Dataframes are immutable.** Operations like `Where`, `Select` and `Sort`
   return a *new* dataframe; the original is never modified.
3. **Loaders come in pairs.** Each format has a path version (`FromCSV`) and
   an `io.Reader` version (`FromCSVReader`); writers mirror them (`ToCSV` /
   `ToCSVWriter`).

## Inspecting a dataframe

- `fmt.Print(df)` — a truncated, aligned table (nulls print as `null`).
- `df.Info()` — pandas-style summary: row count, per-column dtype, non-null
  counts and memory usage.
- `df.NumRows()`, `df.NumCols()` — dimensions.

## Next steps

Continue with the [user guide](README.md#user-guide) — starting with
[Loading data](guide/loading-data.md) — or browse the
[runnable examples](https://pkg.go.dev/github.com/gverdugo-dev/grizzly#pkg-examples)
in the API reference.
