package grizzly

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
)

// ToCSV writes the dataframe to a CSV file with a header row, creating or
// truncating the file. It delegates to ToCSVWriter; see that function for
// the format and null rules.
func (d Dataframe) ToCSV(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("ToCSV: %w", err)
	}
	// Close can fail on write-behind filesystems: capture its error too.
	if err := d.ToCSVWriter(f); err != nil {
		f.Close()
		return fmt.Errorf("ToCSV %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("ToCSV %s: %w", path, err)
	}
	logger.Debug("dataframe written to csv", "path", path, "rows", d.NumRows())
	return nil
}

// ToCSVWriter writes the dataframe to a stream as CSV: one header row
// with the column names, then one record per row, columns in dataframe
// order.
//
// Null rule (mirror of FromCSVReader): a null is written as an empty
// cell. For Float64 and Bool columns this round-trips exactly — an empty
// cell loads back as a null. For String columns it does NOT: grizzly
// deliberately reads an empty string cell as a real "" (see
// FromCSVReader), so a null string written by ToCSVWriter comes back as
// a valid empty string. Use ToJSONWriter when exact null round-trips
// matter; a configurable null marker may be added later.
func (d Dataframe) ToCSVWriter(w io.Writer) error {
	cw := csv.NewWriter(w)

	header := make([]string, d.NumCols())
	for j, c := range d.cols {
		header[j] = c.Name()
	}
	if err := cw.Write(header); err != nil {
		return err
	}

	// One record slice reused across rows — the writing twin of the
	// reader's ReuseRecord.
	record := make([]string, d.NumCols())
	for i := 0; i < d.NumRows(); i++ {
		for j, c := range d.cols {
			if !c.IsValid(i) {
				record[j] = "" // null: empty cell
				continue
			}
			// cellString is the package's single point of value rendering
			// (format.go); its float format ('g', -1) round-trips exactly.
			record[j] = cellString(c, i)
		}
		if err := cw.Write(record); err != nil {
			return err
		}
	}

	// csv.Writer buffers internally: Flush pushes everything to w, and
	// Error reports any write error swallowed along the way.
	cw.Flush()
	return cw.Error()
}
