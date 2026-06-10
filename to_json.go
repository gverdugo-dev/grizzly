package grizzly

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
)

// ToJSON writes the dataframe to a JSON file as an array of objects,
// creating or truncating the file. It delegates to ToJSONWriter; see that
// function for the format and null rules.
func (d Dataframe) ToJSON(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("ToJSON: %w", err)
	}
	if err := d.ToJSONWriter(f); err != nil {
		f.Close()
		return fmt.Errorf("ToJSON %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("ToJSON %s: %w", path, err)
	}
	logger.Debug("dataframe written to json", "path", path, "rows", d.NumRows())
	return nil
}

// ToJSONWriter writes the dataframe to a stream as a compact JSON array
// of objects, one object per row with the columns as keys, in dataframe
// order — the exact shape FromJSONReader loads.
//
// Null rule: a null row is written as a literal null, so the output
// round-trips exactly through FromJSONReader (a property CSV cannot offer
// for string columns; see ToCSVWriter).
//
// Like FromJSONReader, it streams at token level: rows are emitted
// straight to the buffered writer, never materialized as map[string]any
// (which would heap-allocate one map per row and box every value).
//
// JSON has no NaN or Infinity: a Float64 column holding one (possible via
// FromStructs) is an error rather than invalid output.
func (d Dataframe) ToJSONWriter(w io.Writer) error {
	bw := bufio.NewWriterSize(w, readerBufSize)

	// Column names are constant across rows: marshal them once, outside
	// the row loop (json.Marshal handles quoting and escaping).
	keys := make([][]byte, d.NumCols())
	for j, c := range d.cols {
		k, err := json.Marshal(c.Name())
		if err != nil {
			return fmt.Errorf("ToJSONWriter: column %q: %w", c.Name(), err)
		}
		keys[j] = k
	}

	bw.WriteByte('[')
	// scratch holds each rendered number; reused across cells so the hot
	// loop does not allocate per value.
	var scratch []byte
	for i := 0; i < d.NumRows(); i++ {
		if i > 0 {
			bw.WriteByte(',')
		}
		bw.WriteByte('{')
		for j, c := range d.cols {
			if j > 0 {
				bw.WriteByte(',')
			}
			bw.Write(keys[j])
			bw.WriteByte(':')

			if !c.IsValid(i) {
				bw.WriteString("null") // null is data; write it as such
				continue
			}
			switch c := c.(type) {
			case *Float64Column:
				v := c.values[i]
				if math.IsNaN(v) || math.IsInf(v, 0) {
					return fmt.Errorf("ToJSONWriter: column %q row %d: %v is not representable in JSON",
						c.name, i, v)
				}
				// 'g' with precision -1: shortest form that round-trips;
				// exponents like 1e+06 are valid JSON numbers.
				scratch = strconv.AppendFloat(scratch[:0], v, 'g', -1, 64)
				bw.Write(scratch)
			case *StringColumn:
				s, err := json.Marshal(c.values[i])
				if err != nil {
					return fmt.Errorf("ToJSONWriter: column %q row %d: %w", c.name, i, err)
				}
				bw.Write(s)
			case *BoolColumn:
				if bitmapGet(c.values, i) {
					bw.WriteString("true")
				} else {
					bw.WriteString("false")
				}
			default:
				return fmt.Errorf("%w: ToJSONWriter over unsupported column %q",
					ErrTypeMismatch, c.Name())
			}
		}
		bw.WriteByte('}')
	}
	bw.WriteByte(']')

	// The WriteByte/WriteString calls above never fail directly — a
	// bufio.Writer remembers its first error and reports it here.
	return bw.Flush()
}
