package grizzly

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
)

// FromCSV builds a Dataframe from a CSV file with a header row. See
// FromCSVReader for the format, schema and null rules — both paths load
// identical dataframes.
//
// Unlike FromCSVReader, it reads the whole file into memory and, when the
// file is big enough, parses it in parallel by chunks (see
// from_csv_parallel.go): a file path offers random access, which is what
// makes splitting into byte ranges possible. Use FromCSVReader when
// streaming matters more than speed.
func FromCSV(path string, schema Schema) (Dataframe, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Dataframe{}, fmt.Errorf("FromCSV: %w", err)
	}

	df, err := fromCSVBytes(data, schema)
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

	// One builder per schema field (see column_builder.go), plus the index
	// of its source column in the stream. All type decisions happen here,
	// outside the per-row loop.
	builders := make([]columnBuilder, len(schema))
	src := make([]int, len(schema))
	for j, field := range schema {
		i, ok := colIdx[field.Name]
		if !ok {
			return Dataframe{}, fmt.Errorf("%w: %q", ErrColumnNotFound, field.Name)
		}
		b, err := newColumnBuilder(field, 0) // streaming: row count unknown
		if err != nil {
			return Dataframe{}, err
		}
		builders[j], src[j] = b, i
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
		for j, b := range builders {
			if err := b.appendCSV(record[src[j]], line); err != nil {
				return Dataframe{}, err
			}
		}
	}

	cols, err := finishColumns(builders)
	if err != nil {
		return Dataframe{}, err
	}
	return NewDataframe(cols...)
}
