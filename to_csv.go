package grizzly

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
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
//
// Like ToJSONWriter, it writes bytes straight into a buffered writer
// instead of going through encoding/csv: csv.Writer only accepts
// []string records, which forced one string allocation per numeric cell
// (strconv.FormatFloat — the writer benchmark's whole allocation count).
// Here floats render via strconv.AppendFloat into one reused scratch
// buffer. Quoting follows encoding/csv's exact rules (see
// csvFieldNeedsQuotes), so the output is byte-for-byte what the previous
// implementation produced.
func (d Dataframe) ToCSVWriter(w io.Writer) error {
	bw := bufio.NewWriterSize(w, readerBufSize)

	for j, c := range d.cols {
		if j > 0 {
			bw.WriteByte(',')
		}
		writeCSVField(bw, c.Name())
	}
	bw.WriteByte('\n')

	// scratch holds each rendered number; reused across cells so the hot
	// loop does not allocate per value.
	var scratch []byte
	for i := 0; i < d.NumRows(); i++ {
		for j, c := range d.cols {
			if j > 0 {
				bw.WriteByte(',')
			}
			if !c.IsValid(i) {
				continue // null: empty cell
			}
			switch c := c.(type) {
			case *Float64Column:
				// 'g' with precision -1: shortest form that round-trips.
				// Never needs quoting: digits, sign, '.', 'e' only.
				scratch = strconv.AppendFloat(scratch[:0], c.values[i], 'g', -1, 64)
				bw.Write(scratch)
			case *StringColumn:
				writeCSVField(bw, c.values[i])
			case *BoolColumn:
				if bitmapGet(c.values, i) {
					bw.WriteString("true")
				} else {
					bw.WriteString("false")
				}
			default:
				return fmt.Errorf("%w: ToCSVWriter over unsupported column %q",
					ErrTypeMismatch, c.Name())
			}
		}
		bw.WriteByte('\n')
	}

	// The Write* calls above never fail directly — a bufio.Writer
	// remembers its first error and reports it here.
	return bw.Flush()
}

// csvFieldNeedsQuotes mirrors encoding/csv's quoting decision exactly
// (for Comma == ','): quotes are needed when the field contains a comma,
// a quote or a line break, when it starts with a space (any Unicode
// space), or when it is `\.` — a special case so the output stays usable
// as PostgreSQL COPY input, inherited from the stdlib for byte-for-byte
// compatibility.
func csvFieldNeedsQuotes(field string) bool {
	if field == "" {
		return false
	}
	if field == `\.` {
		return true
	}
	if strings.ContainsAny(field, ",\"\r\n") {
		return true
	}
	r, _ := utf8.DecodeRuneInString(field)
	return unicode.IsSpace(r)
}

// writeCSVField writes one string field, quoted only when the format
// demands it, with inner quotes doubled ("" — RFC 4180). Bytes other
// than the quote pass through untouched, exactly like encoding/csv with
// UseCRLF=false.
func writeCSVField(bw *bufio.Writer, field string) {
	if !csvFieldNeedsQuotes(field) {
		bw.WriteString(field)
		return
	}
	bw.WriteByte('"')
	for {
		i := strings.IndexByte(field, '"')
		if i < 0 {
			bw.WriteString(field)
			break
		}
		bw.WriteString(field[:i+1])
		bw.WriteByte('"') // double the quote
		field = field[i+1:]
	}
	bw.WriteByte('"')
}
