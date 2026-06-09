// Package grizzly is a dataframe library built from scratch as a learning
// project.
//
// Data is ingested thinking in rows (structs, CSV, JSON) but stored and
// processed in columns: each column keeps its values in a contiguous typed
// slice, which is what makes operations like Sum or Filter fast.
package grizzly

import (
	"errors"
	"fmt"
)

// ErrColumnNotFound is returned when the requested column does not exist in
// the dataframe.
var ErrColumnNotFound = errors.New("column does not exist in dataframe")

// ErrTypeMismatch is returned when an operation is applied to a column whose
// type does not support it (e.g. Sum over a string column).
var ErrTypeMismatch = errors.New("operation not supported for column type")

// Dataframe is an in-memory, column-oriented table.
//
// Columns are kept in a slice (not a map) to preserve their insertion order,
// which matters when printing or serializing. Lookup by name is a linear
// scan — fine for the realistic number of columns in a dataframe (tens, not
// millions).
type Dataframe struct {
	cols []Column
}

// NewDataframe builds a Dataframe from the given columns.
//
// All columns must have the same length and unique names; otherwise an error
// is returned. This is the low-level constructor — friendlier, row-oriented
// constructors (from structs, CSV, JSON) build on top of it.
func NewDataframe(cols ...Column) (Dataframe, error) {
	seen := make(map[string]bool, len(cols))
	for _, c := range cols {
		if seen[c.Name()] {
			return Dataframe{}, fmt.Errorf("duplicate column name %q", c.Name())
		}
		seen[c.Name()] = true
		if c.Len() != cols[0].Len() {
			return Dataframe{}, fmt.Errorf(
				"column %q has %d values, expected %d",
				c.Name(), c.Len(), cols[0].Len(),
			)
		}
	}
	logger.Debug("dataframe built", "cols", len(cols))
	return Dataframe{cols: cols}, nil
}

// NumRows returns the number of rows in the dataframe.
func (d Dataframe) NumRows() int {
	if len(d.cols) == 0 {
		return 0
	}
	return d.cols[0].Len()
}

// NumCols returns the number of columns in the dataframe.
func (d Dataframe) NumCols() int { return len(d.cols) }

// Column returns the column with the given name, or ErrColumnNotFound.
func (d Dataframe) Column(name string) (Column, error) {
	for _, c := range d.cols {
		if c.Name() == name {
			return c, nil
		}
	}
	return nil, fmt.Errorf("%w: %q", ErrColumnNotFound, name)
}

// Sum returns the sum of the numeric column with the given name.
//
// It returns ErrColumnNotFound if the column does not exist, and
// ErrTypeMismatch if the column is not numeric.
func (d Dataframe) Sum(name string) (float64, error) {
	col, err := d.Column(name)
	if err != nil {
		return 0, err
	}
	// Type-switch to reach the concrete typed slice: the hot loop below runs
	// over a contiguous []float64 with no interface indirection per value.
	switch c := col.(type) {
	case *Float64Column:
		var sum float64
		for _, v := range c.values {
			sum += v
		}
		return sum, nil
	default:
		return 0, fmt.Errorf("%w: cannot sum %s column %q", ErrTypeMismatch, col.DType(), name)
	}
}
