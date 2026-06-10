package grizzly

import (
	"fmt"
	"iter"
	"math/bits"
)

// DType identifies the data type of a column. Grizzly supports a closed set
// of column types; every Column implementation reports exactly one DType.
//
// Using a small named type instead of bare strings gives us compile-time
// safety wherever a function expects "a column type" rather than "any string".
type DType string

// The supported column data types.
const (
	Float64 DType = "float64"
	String  DType = "string"
)

// Column is the contract every typed column satisfies.
//
// A column stores its values in a contiguous typed slice (e.g. []float64),
// which is what makes columnar processing fast: sequential memory access,
// no per-value heap allocations, no type assertions in hot loops.
//
// The interface intentionally exposes only type-agnostic behavior. Anything
// that needs the concrete values (Sum, Filter...) type-switches on the
// concrete implementation (*Float64Column, *StringColumn...) to reach the
// underlying slice directly.
type Column interface {
	// Name returns the column's name, unique within a Dataframe.
	Name() string
	// Len returns the number of values in the column.
	Len() int
	// DType returns the column's data type.
	DType() DType
	// IsValid reports whether row i holds a value (true) or a null (false).
	// It panics if i is out of range.
	IsValid(i int) bool
	// NullCount returns the number of null rows in the column.
	NullCount() int
}

// Float64Column is a column of float64 values backed by a contiguous slice.
//
// Nulls are tracked by a validity bitmap (see bitmap.go): at null rows the
// values slice holds a placeholder (0.0) that operations never read. A nil
// bitmap means the column has no nulls.
type Float64Column struct {
	name     string
	values   []float64
	validity []uint64
}

// NewFloat64Column returns a Float64Column with the given name and values.
//
// The slice is stored as-is (not copied): the caller must not mutate it
// after construction. The column has no nulls; use NewFloat64ColumnWithNulls
// to mark some rows as null.
func NewFloat64Column(name string, values []float64) *Float64Column {
	return &Float64Column{name: name, values: values}
}

// NewFloat64ColumnWithNulls returns a Float64Column where valid[i] reports
// whether values[i] is a real value (true) or a null (false). values and
// valid must have the same length; otherwise an error is returned.
//
// The []bool mask is the ergonomic boundary representation: it is compacted
// here, once, into the internal validity bitmap (and dropped entirely when
// it contains no false entries). The value stored at a null row is kept as
// a placeholder that operations never read.
func NewFloat64ColumnWithNulls(name string, values []float64, valid []bool) (*Float64Column, error) {
	if len(values) != len(valid) {
		return nil, fmt.Errorf("column %q: %d values but %d validity entries",
			name, len(values), len(valid))
	}
	return &Float64Column{name: name, values: values, validity: validityFromBools(valid)}, nil
}

// Name returns the column's name.
func (c *Float64Column) Name() string { return c.name }

// Len returns the number of values in the column.
func (c *Float64Column) Len() int { return len(c.values) }

// DType returns Float64.
func (c *Float64Column) DType() DType { return Float64 }

// IsValid reports whether row i holds a value (true) or a null (false).
// It panics if i is out of range.
func (c *Float64Column) IsValid(i int) bool {
	_ = c.values[i] // bounds check even when the bitmap is nil
	return c.validity == nil || bitmapGet(c.validity, i)
}

// NullCount returns the number of null rows in the column.
func (c *Float64Column) NullCount() int {
	if c.validity == nil {
		return 0
	}
	return c.Len() - bitmapCountSet(c.validity)
}

// Value returns the value at row i and whether it is valid (comma-ok):
// ok is false when the row is null, and the returned value must then be
// ignored. It panics if i is out of range.
func (c *Float64Column) Value(i int) (float64, bool) {
	return c.values[i], c.validity == nil || bitmapGet(c.validity, i)
}

// validValues returns an iterator over the column's valid (non-null)
// values, in row order. It is the single implementation of the bitmap
// walk: every aggregation (Sum, Avg, Min, Max...) ranges over this
// instead of repeating the loop.
//
// Null-free columns take a branch-free pass over the contiguous slice.
// With a bitmap, the walk visits only set bits: TrailingZeros64 finds
// the lowest one (one CPU instruction), word &= word-1 clears it, so a
// word with k valid rows costs k iterations — placeholders at null rows
// are never read.
func (c *Float64Column) validValues() iter.Seq[float64] {
	return func(yield func(float64) bool) {
		if c.validity == nil {
			for _, v := range c.values {
				if !yield(v) {
					return
				}
			}
			return
		}
		for w, word := range c.validity {
			for word != 0 {
				i := w<<6 + bits.TrailingZeros64(word)
				if !yield(c.values[i]) {
					return
				}
				word &= word - 1
			}
		}
	}
}

// StringColumn is a column of string values backed by a contiguous slice.
//
// Nulls are tracked by a validity bitmap (see bitmap.go): at null rows the
// values slice holds a placeholder ("") that operations never read. A nil
// bitmap means the column has no nulls.
type StringColumn struct {
	name     string
	values   []string
	validity []uint64
}

// NewStringColumn returns a StringColumn with the given name and values.
//
// The slice is stored as-is (not copied): the caller must not mutate it
// after construction. The column has no nulls; use NewStringColumnWithNulls
// to mark some rows as null.
func NewStringColumn(name string, values []string) *StringColumn {
	return &StringColumn{name: name, values: values}
}

// NewStringColumnWithNulls returns a StringColumn where valid[i] reports
// whether values[i] is a real value (true) or a null (false). values and
// valid must have the same length; otherwise an error is returned.
//
// The []bool mask is the ergonomic boundary representation: it is compacted
// here, once, into the internal validity bitmap (and dropped entirely when
// it contains no false entries). The value stored at a null row is kept as
// a placeholder that operations never read.
func NewStringColumnWithNulls(name string, values []string, valid []bool) (*StringColumn, error) {
	if len(values) != len(valid) {
		return nil, fmt.Errorf("column %q: %d values but %d validity entries",
			name, len(values), len(valid))
	}
	return &StringColumn{name: name, values: values, validity: validityFromBools(valid)}, nil
}

// Name returns the column's name.
func (c *StringColumn) Name() string { return c.name }

// Len returns the number of values in the column.
func (c *StringColumn) Len() int { return len(c.values) }

// DType returns String.
func (c *StringColumn) DType() DType { return String }

// IsValid reports whether row i holds a value (true) or a null (false).
// It panics if i is out of range.
func (c *StringColumn) IsValid(i int) bool {
	_ = c.values[i] // bounds check even when the bitmap is nil
	return c.validity == nil || bitmapGet(c.validity, i)
}

// NullCount returns the number of null rows in the column.
func (c *StringColumn) NullCount() int {
	if c.validity == nil {
		return 0
	}
	return c.Len() - bitmapCountSet(c.validity)
}

// Value returns the value at row i and whether it is valid (comma-ok):
// ok is false when the row is null, and the returned value must then be
// ignored. It panics if i is out of range.
func (c *StringColumn) Value(i int) (string, bool) {
	return c.values[i], c.validity == nil || bitmapGet(c.validity, i)
}
