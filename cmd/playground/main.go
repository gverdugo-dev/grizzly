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
}
