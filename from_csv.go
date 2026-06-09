package grizzly

import (
	"bufio"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
)

// FromCSV builds a Dataframe from a CSV file with a header row, using the
// given schema to decide each column's type (CSV itself is untyped: every
// value arrives as a string until someone decides otherwise).
//
// The file is read in streaming: one record at a time, never the whole file
// in memory, with the record slice reused across reads (ReuseRecord) so the
// hot loop does not allocate per row.
//
// The schema also selects and orders the columns: source columns not listed
// in it are ignored, and the Dataframe's column order is the schema's order,
// not the file's. A schema column missing from the file is an error.
func FromCSV(path string, schema Schema) (Dataframe, error) {
	f, err := os.Open(path)
	if err != nil {
		return Dataframe{}, fmt.Errorf("FromCSV: %w", err)
	}
	defer f.Close()

	// csv.Reader buffers internally, but feeding it from a bigger buffer
	// cuts syscalls on large files.
	r := csv.NewReader(bufio.NewReaderSize(f, 256<<10))
	// Reuse the record slice across reads: removes one allocation per row.
	// The price: a returned record is only valid until the next Read.
	r.ReuseRecord = true

	header, err := r.Read()
	if err != nil {
		return Dataframe{}, fmt.Errorf("FromCSV %s: reading header: %w", path, err)
	}
	header = slices.Clone(header) // ReuseRecord: the next Read overwrites it

	colIdx := make(map[string]int, len(header))
	for i, name := range header {
		colIdx[name] = i
	}

	// One fill closure per schema field: parses its cell and appends to its
	// typed slice. finish closures wrap the final slices into Columns once
	// the row count is known. All type decisions happen here, outside the
	// per-row loop.
	fills := make([]func(record []string, line int) error, 0, len(schema))
	finish := make([]func() Column, 0, len(schema))
	for _, field := range schema {
		src, ok := colIdx[field.Name]
		if !ok {
			return Dataframe{}, fmt.Errorf("FromCSV %s: %w: %q", path, ErrColumnNotFound, field.Name)
		}
		name := field.Name
		switch field.Type {
		case Float64:
			var values []float64
			fills = append(fills, func(record []string, line int) error {
				v, err := strconv.ParseFloat(record[src], 64)
				if err != nil {
					return fmt.Errorf("line %d, column %q: %w", line, name, err)
				}
				values = append(values, v)
				return nil
			})
			finish = append(finish, func() Column { return NewFloat64Column(name, values) })
		case String:
			// Keeping record[src] is safe (csv.Reader allocates fresh string
			// data per record even with ReuseRecord), but it pins the whole
			// row's backing string in memory. Fast; revisit if string-heavy
			// files show memory bloat.
			var values []string
			fills = append(fills, func(record []string, _ int) error {
				values = append(values, record[src])
				return nil
			})
			finish = append(finish, func() Column { return NewStringColumn(name, values) })
		default:
			return Dataframe{}, fmt.Errorf("FromCSV %s: unsupported dtype %q for column %q",
				path, field.Type, field.Name)
		}
	}

	// Stream the rows. Line numbers are 1-based and the header is line 1.
	rows := 0
	for line := 2; ; line++ {
		record, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Dataframe{}, fmt.Errorf("FromCSV %s: %w", path, err)
		}
		for _, fill := range fills {
			if err := fill(record, line); err != nil {
				return Dataframe{}, fmt.Errorf("FromCSV %s: %w", path, err)
			}
		}
		rows++
	}

	cols := make([]Column, len(finish))
	for i, fn := range finish {
		cols[i] = fn()
	}
	logger.Debug("dataframe loaded from csv", "path", path, "rows", rows, "cols", len(cols))
	return NewDataframe(cols...)
}
