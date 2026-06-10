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

// FromCSV builds a Dataframe from a CSV file with a header row. It opens
// the file and delegates to FromCSVReader; see that function for the
// format, schema and null rules.
func FromCSV(path string, schema Schema) (Dataframe, error) {
	f, err := os.Open(path)
	if err != nil {
		return Dataframe{}, fmt.Errorf("FromCSV: %w", err)
	}
	defer f.Close()

	// csv.Reader buffers internally, but feeding it from a bigger buffer
	// cuts syscalls on large files.
	df, err := FromCSVReader(bufio.NewReaderSize(f, 256<<10), schema)
	if err != nil {
		return Dataframe{}, fmt.Errorf("FromCSV %s: %w", path, err)
	}
	logger.Debug("dataframe loaded from csv", "path", path, "rows", df.NumRows())
	return df, nil
}

// FromCSVReader builds a Dataframe from a stream of CSV data with a header
// row, using the given schema to decide each column's type (CSV itself is
// untyped: every value arrives as a string until someone decides otherwise).
//
// The stream is read one record at a time, never whole in memory, with the
// record slice reused across reads (ReuseRecord) so the hot loop does not
// allocate per row.
//
// The schema also selects and orders the columns: source columns not listed
// in it are ignored, and the Dataframe's column order is the schema's order,
// not the stream's. A schema column missing from the header is an error.
//
// Null rule: an empty cell in a Float64 column is a null (validity bit 0).
// In a String column it stays a real empty string — CSV cannot distinguish
// an empty cell from a legitimate "", and silently turning every "" into a
// null (as pandas does) destroys real data. Explicit over magic; a
// configurable null marker can be added later if needed.
func FromCSVReader(r io.Reader, schema Schema) (Dataframe, error) {
	cr := csv.NewReader(r)
	// Reuse the record slice across reads: removes one allocation per row.
	// The price: a returned record is only valid until the next Read.
	cr.ReuseRecord = true

	header, err := cr.Read()
	if err != nil {
		return Dataframe{}, fmt.Errorf("reading header: %w", err)
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
	finish := make([]func() (Column, error), 0, len(schema))
	for _, field := range schema {
		src, ok := colIdx[field.Name]
		if !ok {
			return Dataframe{}, fmt.Errorf("%w: %q", ErrColumnNotFound, field.Name)
		}
		name := field.Name
		switch field.Type {
		case Float64:
			var values []float64
			var valid []bool
			fills = append(fills, func(record []string, line int) error {
				if record[src] == "" { // empty cell: placeholder value, validity bit 0
					values = append(values, 0)
					valid = append(valid, false)
					return nil
				}
				v, err := strconv.ParseFloat(record[src], 64)
				if err != nil {
					return fmt.Errorf("line %d, column %q: %w", line, name, err)
				}
				values = append(values, v)
				valid = append(valid, true)
				return nil
			})
			finish = append(finish, func() (Column, error) {
				return NewFloat64ColumnWithNulls(name, values, valid)
			})
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
			finish = append(finish, func() (Column, error) {
				return NewStringColumn(name, values), nil
			})
		case Bool:
			var values []bool
			var valid []bool
			fills = append(fills, func(record []string, line int) error {
				if record[src] == "" { // empty cell: placeholder value, validity bit 0
					values = append(values, false)
					valid = append(valid, false)
					return nil
				}
				// ParseBool accepts 1/0, t/f, true/false in any common casing.
				v, err := strconv.ParseBool(record[src])
				if err != nil {
					return fmt.Errorf("line %d, column %q: %w", line, name, err)
				}
				values = append(values, v)
				valid = append(valid, true)
				return nil
			})
			finish = append(finish, func() (Column, error) {
				return NewBoolColumnWithNulls(name, values, valid)
			})
		default:
			return Dataframe{}, fmt.Errorf("unsupported dtype %q for column %q",
				field.Type, field.Name)
		}
	}

	// Stream the rows. Line numbers are 1-based and the header is line 1.
	for line := 2; ; line++ {
		record, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Dataframe{}, err
		}
		for _, fill := range fills {
			if err := fill(record, line); err != nil {
				return Dataframe{}, err
			}
		}
	}

	cols := make([]Column, len(finish))
	for i, fn := range finish {
		col, err := fn()
		if err != nil {
			return Dataframe{}, err
		}
		cols[i] = col
	}
	return NewDataframe(cols...)
}
