package main

import (
	"fmt"
	"log/slog"
	"github.com/gverdugo-dev/grizzly"
	"github.com/gverdugo-dev/grizzly/internal/logging"
)

func main() {
	logger := logging.New(slog.LevelDebug)

	// Opt in to grizzly's internal logs — by default the library is silent.
	grizzly.SetLogger(logger)

	logger.Info("Testing grizzly dataframes")

	// Row-oriented loading: one struct = one row. Types come from the
	// struct fields; column names from the `grizzly` tags.
	type Sale struct {
		Product string  `grizzly:"product"`
		Price   float64 `grizzly:"price"`
	}
	df, err := grizzly.FromStructs([]Sale{
		{Product: "apple", Price: 1.50},
		{Product: "banana", Price: 0.75},
		{Product: "cherry", Price: 3.20},
	})
	if err != nil {
		logger.Error("building dataframe", "err", err)
		return
	}
	// fmt.Println uses Dataframe.String automatically (fmt.Stringer).
	fmt.Println(df)
	fmt.Println(df.Info())

	total, err := df.Sum("price")
	if err != nil {
		logger.Error("summing", "err", err)
		return
	}
	fmt.Printf("sum(price) = %.2f\n", total)

	// Summing a string column fails with a typed, inspectable error.
	if _, err := df.Sum("product"); err != nil {
		logger.Warn("expected failure", "err", err)
	}

	// Untyped sources need an explicit schema: the user declares the types
	// (and the column order — note it differs from the files').
	schema := grizzly.Schema{
		{Name: "store", Type: grizzly.String},
		{Name: "product", Type: grizzly.String},
		{Name: "price", Type: grizzly.Float64},
	}

	// The CSV has an empty price cell (kiwi → null) and a real 0.00 (lime):
	// the validity bitmap keeps them apart — String prints "null" for one
	// and 0 for the other, and aggregations skip only the former.
	fromCSV, err := grizzly.FromCSV("cmd/playground/testdata/sales.csv", schema)
	if err != nil {
		logger.Error("loading csv", "err", err)
		return
	}
	fmt.Println(fromCSV)
	fmt.Println(fromCSV.Info())

	count, _ := fromCSV.Count("price")
	sum, _ := fromCSV.Sum("price")
	avg, _ := fromCSV.Avg("price") // sum / valid count, NOT sum / rows
	mn, _ := fromCSV.Min("price")
	mx, _ := fromCSV.Max("price")
	fmt.Printf("price: count=%d sum=%.2f avg=%.4f min=%.2f max=%.2f (%d rows; nulls skipped)\n\n",
		count, sum, avg, mn, mx, fromCSV.NumRows())

	// The JSON has a null price (kiwi) and a null product: JSON's literal
	// null works for every column type.
	fromJSON, err := grizzly.FromJSON("cmd/playground/testdata/sales.json", schema)
	if err != nil {
		logger.Error("loading json", "err", err)
		return
	}
	fmt.Println(fromJSON)
	fmt.Println(fromJSON.Info())
}
