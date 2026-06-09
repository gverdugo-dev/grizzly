package main

import (
	"fmt"
	"log/slog"
	"grizzly"
	"grizzly/internal/logging"
)

func main() {
	logger := logging.New(slog.LevelDebug)

	// Opt in to grizzly's internal logs — by default the library is silent.
	grizzly.SetLogger(logger)

	logger.Info("Testing grizzly dataframes")

	// Build typed columns: each one holds a contiguous slice of its real type.
	products := grizzly.NewStringColumn("product", []string{"apple", "banana", "cherry"})
	prices := grizzly.NewFloat64Column("price", []float64{1.50, 0.75, 3.20})

	df, err := grizzly.NewDataframe(products, prices)
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
}
