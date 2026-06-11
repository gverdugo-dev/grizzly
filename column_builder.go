package grizzly

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// This file centralizes the per-dtype loading knowledge shared by the
// CSV and JSON loaders: how each column type parses a raw cell, how it
// accumulates values and validity, and how it becomes a Column.
//
// The knowledge is organized dtype-major (one builder per type that knows
// every source) rather than source-major (one switch per loader that
// knows every type, the previous layout) because dtypes are the likelier
// growth axis: adding a type means one new builder and one factory case,
// instead of editing every loader in parallel and risking silent
// divergence in their null rules.
//
// FromStructs deliberately does not use builders: it parses nothing (the
// struct fields carry the types), has no null rows, and pre-sizes its
// slices because the row count is known up front.

// readerBufSize is the read buffer the path-based loaders put in front of
// the file. 256 KB cuts syscalls on large files; past this size the
// returns diminish (the parser, not the read, dominates).
const readerBufSize = 256 << 10

// columnBuilder accumulates one column's values while a loader streams
// rows, and builds the final Column when the stream ends. One
// implementation per dtype; each knows how to parse itself from every
// supported source.
type columnBuilder interface {
	// appendCSV parses one CSV cell and appends it. The 1-based line
	// number is for error context only.
	appendCSV(cell string, line int) error
	// appendJSON decodes the value at the decoder's cursor and appends
	// it. The 0-based row number is for error context only.
	appendJSON(dec *json.Decoder, row int) error
	// finish wraps the accumulated values into a Column.
	finish() (Column, error)
}

// newColumnBuilder returns the builder for a schema field — the single
// place that maps a DType to its loading behavior.
//
// capHint pre-sizes the value and validity slices when the caller can
// estimate the row count (the parallel CSV path counts each chunk's
// newlines); pass 0 when streaming with no idea (append grows them as
// usual, amortized but with realloc+copy along the way).
func newColumnBuilder(f Field, capHint int) (columnBuilder, error) {
	switch f.Type {
	case Float64:
		return &float64Builder{
			name:   f.Name,
			values: make([]float64, 0, capHint),
			valid:  make([]bool, 0, capHint),
		}, nil
	case String:
		return &stringBuilder{
			name:   f.Name,
			values: make([]string, 0, capHint),
			valid:  make([]bool, 0, capHint),
		}, nil
	case Bool:
		return &boolBuilder{
			name:   f.Name,
			values: make([]bool, 0, capHint),
			valid:  make([]bool, 0, capHint),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported dtype %q for column %q", f.Type, f.Name)
	}
}

// finishColumns runs every builder's finish and collects the resulting
// columns — the shared tail of every schema-driven loader.
func finishColumns(builders []columnBuilder) ([]Column, error) {
	cols := make([]Column, len(builders))
	for i, b := range builders {
		col, err := b.finish()
		if err != nil {
			return nil, err
		}
		cols[i] = col
	}
	return cols, nil
}

// float64Builder accumulates a Float64 column.
//
// Null rules: an empty CSV cell and a JSON null are nulls (placeholder
// value, validity bit 0).
type float64Builder struct {
	name   string
	values []float64
	valid  []bool
	buf    float64 // JSON decode buffer, reused across rows
}

func (b *float64Builder) appendCSV(cell string, line int) error {
	if cell == "" {
		b.values = append(b.values, 0)
		b.valid = append(b.valid, false)
		return nil
	}
	v, err := strconv.ParseFloat(cell, 64)
	if err != nil {
		return fmt.Errorf("line %d, column %q: %w", line, b.name, err)
	}
	b.values = append(b.values, v)
	b.valid = append(b.valid, true)
	return nil
}

func (b *float64Builder) appendJSON(dec *json.Decoder, row int) error {
	// Null detection relies on encoding/json's pointer rule: decoding
	// into a **T sets the *T to nil on a JSON null, and otherwise writes
	// through it into the value it already points at. The pointer is
	// re-armed every call because a previous null left a nil behind.
	ptr := &b.buf
	if err := dec.Decode(&ptr); err != nil {
		return fmt.Errorf("row %d, key %q: %w", row, b.name, err)
	}
	if ptr == nil {
		b.values = append(b.values, 0)
		b.valid = append(b.valid, false)
		return nil
	}
	b.values = append(b.values, b.buf)
	b.valid = append(b.valid, true)
	return nil
}

func (b *float64Builder) finish() (Column, error) {
	return NewFloat64ColumnWithNulls(b.name, b.values, b.valid)
}

// stringBuilder accumulates a String column.
//
// Null rules: a JSON null is a null, but an empty CSV cell is a REAL
// empty string — CSV cannot distinguish an empty cell from a legitimate
// "", and silently turning every "" into a null (as pandas does)
// destroys real data. Explicit over magic; a configurable null marker
// can be added later if needed.
type stringBuilder struct {
	name   string
	values []string
	valid  []bool
	buf    string // JSON decode buffer, reused across rows
}

func (b *stringBuilder) appendCSV(cell string, _ int) error {
	// Keeping the cell is safe (csv.Reader allocates fresh string data
	// per record even with ReuseRecord), but it pins the whole row's
	// backing string in memory. Fast; revisit if string-heavy files show
	// memory bloat.
	b.values = append(b.values, cell)
	b.valid = append(b.valid, true)
	return nil
}

func (b *stringBuilder) appendJSON(dec *json.Decoder, row int) error {
	ptr := &b.buf
	if err := dec.Decode(&ptr); err != nil {
		return fmt.Errorf("row %d, key %q: %w", row, b.name, err)
	}
	if ptr == nil {
		b.values = append(b.values, "")
		b.valid = append(b.valid, false)
		return nil
	}
	b.values = append(b.values, b.buf)
	b.valid = append(b.valid, true)
	return nil
}

func (b *stringBuilder) finish() (Column, error) {
	return NewStringColumnWithNulls(b.name, b.values, b.valid)
}

// boolBuilder accumulates a Bool column. The values stay as []bool until
// finish, which packs them to one bit per row.
//
// Null rules: an empty CSV cell and a JSON null are nulls.
type boolBuilder struct {
	name   string
	values []bool
	valid  []bool
	buf    bool // JSON decode buffer, reused across rows
}

func (b *boolBuilder) appendCSV(cell string, line int) error {
	if cell == "" {
		b.values = append(b.values, false)
		b.valid = append(b.valid, false)
		return nil
	}
	// ParseBool accepts 1/0, t/f, true/false in any common casing.
	v, err := strconv.ParseBool(cell)
	if err != nil {
		return fmt.Errorf("line %d, column %q: %w", line, b.name, err)
	}
	b.values = append(b.values, v)
	b.valid = append(b.valid, true)
	return nil
}

func (b *boolBuilder) appendJSON(dec *json.Decoder, row int) error {
	ptr := &b.buf
	if err := dec.Decode(&ptr); err != nil {
		return fmt.Errorf("row %d, key %q: %w", row, b.name, err)
	}
	if ptr == nil {
		b.values = append(b.values, false)
		b.valid = append(b.valid, false)
		return nil
	}
	b.values = append(b.values, b.buf)
	b.valid = append(b.valid, true)
	return nil
}

func (b *boolBuilder) finish() (Column, error) {
	return NewBoolColumnWithNulls(b.name, b.values, b.valid)
}
