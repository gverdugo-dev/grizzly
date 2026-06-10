package grizzly_test

// Runnable examples for the public API: they render in godoc under their
// symbol (ExampleDataframe_Where shows under Dataframe.Where) AND run as
// tests — go test compares each function's stdout against its // Output:
// block, so the documentation can never drift from the behavior.
//
// Float data sticks to values with exact binary representations (.5, .25)
// so the printed numbers are stable across platforms.

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/gverdugo-dev/grizzly"
)

// ExampleFromStructs shows the row-oriented front door: a slice of structs
// becomes a dataframe, one column per exported field, renamed by tags.
func ExampleFromStructs() {
	type sale struct {
		Product string  `grizzly:"product"`
		Price   float64 `grizzly:"price"`
		Sold    bool    `grizzly:"sold"`
	}
	df, err := grizzly.FromStructs([]sale{
		{Product: "apple", Price: 1.5, Sold: true},
		{Product: "pear", Price: 2, Sold: false},
		{Product: "orange", Price: 0.5, Sold: true},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(df)
	// Output:
	// product  price  sold
	// apple    1.5    true
	// pear     2      false
	// orange   0.5    true
}

// ExampleFromCSVReader loads typed columns from untyped CSV text: the
// schema decides the types, and an empty cell in a float64 column loads
// as a null (printed as "null", never as a fake 0).
func ExampleFromCSVReader() {
	data := "city,temp\nmadrid,21.5\nbilbao,\nvalencia,28.25\n"
	schema := grizzly.Schema{
		{Name: "city", Type: grizzly.String},
		{Name: "temp", Type: grizzly.Float64},
	}
	df, err := grizzly.FromCSVReader(strings.NewReader(data), schema)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(df)
	// Output:
	// city      temp
	// madrid    21.5
	// bilbao    null
	// valencia  28.25
}

// ExampleDataframe_Where filters rows by combining comparison masks:
// build masks with the comparators, combine with And/Or/Not, materialize
// once with Where. Errors are elided to keep the example focused.
func ExampleDataframe_Where() {
	type city struct {
		Name string  `grizzly:"city"`
		Temp float64 `grizzly:"temp"`
	}
	df, _ := grizzly.FromStructs([]city{
		{"madrid", 21.5}, {"bilbao", 12.5}, {"valencia", 28.25}, {"sevilla", 35.5},
	})

	warm, _ := df.Gt("temp", 20.0)
	mild, _ := df.Lt("temp", 30.0)
	out, _ := df.Where(warm.And(mild))
	fmt.Print(out)
	// Output:
	// city      temp
	// madrid    21.5
	// valencia  28.25
}

// ExampleDataframe_Select projects columns by name, in the requested
// order, without copying any data.
func ExampleDataframe_Select() {
	type city struct {
		Name string  `grizzly:"city"`
		Temp float64 `grizzly:"temp"`
	}
	df, _ := grizzly.FromStructs([]city{
		{"madrid", 21.5}, {"bilbao", 12.5},
	})

	out, _ := df.Select("temp", "city")
	fmt.Print(out)
	// Output:
	// temp  city
	// 21.5  madrid
	// 12.5  bilbao
}

// ExampleDataframe_Sum shows the whole-column aggregations. Nulls are
// skipped, SQL-style: Avg divides by the count of valid values.
func ExampleDataframe_Sum() {
	temp, _ := grizzly.NewFloat64ColumnWithNulls("temp",
		[]float64{21.5, 0, 28.25}, []bool{true, false, true})
	df, _ := grizzly.NewDataframe(temp)

	sum, _ := df.Sum("temp")
	avg, _ := df.Avg("temp")
	n, _ := df.Count("temp")
	fmt.Println(sum, avg, n)
	// Output:
	// 49.75 24.875 2
}

// ExampleDataframe_GroupBy groups rows by a key column and aggregates per
// group. Output columns keep the source column's name unless renamed with
// As; groups appear in first-appearance order.
func ExampleDataframe_GroupBy() {
	type sale struct {
		Store string  `grizzly:"store"`
		Price float64 `grizzly:"price"`
	}
	df, _ := grizzly.FromStructs([]sale{
		{"north", 1.5}, {"south", 2.5}, {"north", 0.5}, {"south", 1.5},
	})

	out, err := df.GroupBy("store").Agg(
		grizzly.Sum("price"),
		grizzly.Avg("price").As("avg"),
	)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(out)
	// Output:
	// store  price  avg
	// north  2      1
	// south  4      2
}

// ExampleDataframe_ToCSVWriter writes the dataframe as CSV. Nulls become
// empty cells (note bilbao's missing temp), which round-trip for float64
// and bool columns — see ToCSVWriter for the string caveat.
func ExampleDataframe_ToCSVWriter() {
	src := `[{"city": "madrid", "temp": 21.5}, {"city": "bilbao", "temp": null}]`
	schema := grizzly.Schema{
		{Name: "city", Type: grizzly.String},
		{Name: "temp", Type: grizzly.Float64},
	}
	df, _ := grizzly.FromJSONReader(strings.NewReader(src), schema)

	if err := df.ToCSVWriter(os.Stdout); err != nil {
		log.Fatal(err)
	}
	// Output:
	// city,temp
	// madrid,21.5
	// bilbao,
}

// ExampleDataframe_ToJSONWriter writes the dataframe as a compact JSON
// array of objects — the exact shape FromJSONReader loads, with literal
// nulls, so the output round-trips exactly.
func ExampleDataframe_ToJSONWriter() {
	src := `[{"city": "madrid", "temp": 21.5}, {"city": "bilbao", "temp": null}]`
	schema := grizzly.Schema{
		{Name: "city", Type: grizzly.String},
		{Name: "temp", Type: grizzly.Float64},
	}
	df, _ := grizzly.FromJSONReader(strings.NewReader(src), schema)

	if err := df.ToJSONWriter(os.Stdout); err != nil {
		log.Fatal(err)
	}
	// Output:
	// [{"city":"madrid","temp":21.5},{"city":"bilbao","temp":null}]
}

// ExampleDataframe_Sort orders rows by a column, returning a new
// dataframe. Nulls sort first, in both directions.
func ExampleDataframe_Sort() {
	data := `[
		{"city": "sevilla", "temp": 35.5},
		{"city": "bilbao",  "temp": null},
		{"city": "madrid",  "temp": 21.5}
	]`
	schema := grizzly.Schema{
		{Name: "city", Type: grizzly.String},
		{Name: "temp", Type: grizzly.Float64},
	}
	df, _ := grizzly.FromJSONReader(strings.NewReader(data), schema)

	out, _ := df.Sort("temp")
	fmt.Print(out)
	// Output:
	// city     temp
	// bilbao   null
	// madrid   21.5
	// sevilla  35.5
}
