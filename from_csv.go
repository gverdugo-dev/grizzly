package grizzly

import (
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
)

// FromCSV builds a Dataframe from a CSV file with a header row, using the
// given schema to decide each column's type (CSV itself is untyped: every
// value arrives as a string until someone decides otherwise).
//
// The schema also selects and orders the columns: source columns not listed
// in it are ignored, and the Dataframe's column order is the schema's order,
// not the file's. A schema column missing from the file is an error.
func FromCSV(path string, schema Schema) (Dataframe, error) {
	f, err := os.Open(path)
	if err != nil {
		return Dataframe{}, fmt.Errorf("FromCSV: %w", err)
	}
	// defer runs when the function returns, on EVERY path (success or any
	// of the error returns below) — Go's idiom for cleanup, instead of
	// try/finally or context managers.
	defer f.Close()

	// ReadAll loads the whole file in memory. Fine for now: it gives us the
	// row count, so columns can be pre-sized. A streaming reader would scale
	// to files larger than RAM — future optimization, noted and skipped.
	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return Dataframe{}, fmt.Errorf("FromCSV %s: %w", path, err)
	}
	if len(records) == 0 {
		return Dataframe{}, fmt.Errorf("FromCSV %s: file has no header row", path)
	}
	header, data := records[0], records[1:]

	// Map header names to their position once, so the per-column loop below
	// doesn't re-scan the header.
	colIdx := make(map[string]int, len(header))
	for i, name := range header {
		colIdx[name] = i
	}

	cols := make([]Column, 0, len(schema))
	for _, field := range schema {
		src, ok := colIdx[field.Name]
		if !ok {
			return Dataframe{}, fmt.Errorf("FromCSV %s: %w: %q", path, ErrColumnNotFound, field.Name)
		}

		switch field.Type {
		case Float64:
			values := make([]float64, len(data))
			for i, record := range data {
				v, err := strconv.ParseFloat(record[src], 64)
				if err != nil {
					// +2: 1-based line numbers, plus the header line. An
					// error without the line number would be useless on a
					// million-row file.
					return Dataframe{}, fmt.Errorf(
						"FromCSV %s: line %d, column %q: %w", path, i+2, field.Name, err)
				}
				values[i] = v
			}
			cols = append(cols, NewFloat64Column(field.Name, values))
		case String:
			values := make([]string, len(data))
			for i, record := range data {
				values[i] = record[src]
			}
			cols = append(cols, NewStringColumn(field.Name, values))
		default:
			return Dataframe{}, fmt.Errorf("FromCSV %s: unsupported dtype %q for column %q",
				path, field.Type, field.Name)
		}
	}

	logger.Debug("dataframe loaded from csv", "path", path, "rows", len(data), "cols", len(cols))
	return NewDataframe(cols...)
}
