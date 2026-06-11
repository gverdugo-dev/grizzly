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

// ErrNoValidValues is returned by aggregations that have no honest answer
// over zero values (Avg, Min, Max of an empty or all-null column) — the
// Go-flavored equivalent of SQL returning NULL in that case.
var ErrNoValidValues = errors.New("no valid values in column")

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

// Select returns a new Dataframe containing the named columns, in the
// given order — column projection, SQL's SELECT clause (Where is the
// WHERE). Duplicate names are an error.
//
// Columns are shared with the original dataframe, not copied: columns are
// immutable after construction, so sharing is safe and Select is
// O(columns) regardless of row count. polars does the same with its
// immutable buffers.
//
// It returns ErrColumnNotFound if any requested column does not exist.
func (d Dataframe) Select(names ...string) (Dataframe, error) {
	cols := make([]Column, len(names))
	for i, name := range names {
		col, err := d.Column(name)
		if err != nil {
			return Dataframe{}, err
		}
		cols[i] = col
	}
	// NewDataframe re-validates: duplicate names in the selection fail here.
	return NewDataframe(cols...)
}

// Sum returns the sum of the numeric column with the given name.
//
// Null rows are skipped (SQL aggregate semantics): the sum of the valid
// values is returned, and an all-null column sums to 0.
//
// The summation is pairwise (recursive halving with sequential leaves),
// so the floating-point rounding error grows O(ε·log n) instead of the
// naive loop's O(ε·n) — the same algorithm NumPy uses, and the reason
// pandas/polars agreed with each other (and not with grizzly's old
// sequential loop) on the benchmark checksum's last decimals.
//
// It returns ErrColumnNotFound if the column does not exist, and
// ErrTypeMismatch if the column is not numeric.
func (d Dataframe) Sum(name string) (float64, error) {
	col, err := d.Column(name)
	if err != nil {
		return 0, err
	}
	// Type-switch to reach the concrete column: pairwiseSum reads the
	// contiguous typed slice (skipping null rows via the bitmap) with no
	// interface indirection per value.
	switch c := col.(type) {
	case *Float64Column:
		return pairwiseSum(c.values, c.validity, 0, len(c.values)), nil
	default:
		return 0, fmt.Errorf("%w: cannot sum %s column %q", ErrTypeMismatch, col.DType(), name)
	}
}

// pairwiseSumLeaf is the range size below which pairwiseSum stops
// splitting and sums sequentially. Within a leaf the sequential error is
// negligible, and a tight loop is what the hardware wants; 128 is the
// standard choice (NumPy's, among others).
const pairwiseSumLeaf = 128

// pairwiseSum sums values[lo:hi], skipping rows whose validity bit is 0
// (a nil bitmap means all rows are valid), by recursive halving.
//
// Why the order matters: float64 addition is not associative — each
// addition rounds to 53 bits, and a long sequential chain accumulates
// O(ε·n) of that rounding. Splitting the range in halves and summing the
// halves independently makes the error tree-shaped, O(ε·log n): for 1M
// values, the difference shows up in the last decimals (it explained the
// grizzly-vs-pandas/polars checksum mismatch in the first benchmark).
// The tree shape is also what would make this parallelizable, if Sum
// ever became a bottleneck — it is memory-bound today.
func pairwiseSum(values []float64, validity []uint64, lo, hi int) float64 {
	if hi-lo <= pairwiseSumLeaf {
		var sum float64
		if validity == nil {
			for _, v := range values[lo:hi] {
				sum += v
			}
		} else {
			for i := lo; i < hi; i++ {
				if bitmapGet(validity, i) {
					sum += values[i]
				}
			}
		}
		return sum
	}
	mid := lo + (hi-lo)/2
	return pairwiseSum(values, validity, lo, mid) + pairwiseSum(values, validity, mid, hi)
}

// Count returns the number of valid (non-null) rows in the column with the
// given name — SQL's COUNT(col), not COUNT(*). It works for any column
// type, and an empty or all-null column counts 0 (not an error: the count
// of nothing is zero).
//
// It returns ErrColumnNotFound if the column does not exist.
func (d Dataframe) Count(name string) (int, error) {
	col, err := d.Column(name)
	if err != nil {
		return 0, err
	}
	// Len and NullCount are on the Column interface, so no type switch is
	// needed — and NullCount is popcount-based, not a per-row loop.
	return col.Len() - col.NullCount(), nil
}

// Avg returns the average of the valid values in the numeric column with
// the given name: the sum of the valid values divided by their count, per
// the SQL aggregate convention (nulls are skipped, in both numerator and
// denominator).
//
// It returns ErrColumnNotFound if the column does not exist,
// ErrTypeMismatch if the column is not numeric, and ErrNoValidValues if
// the column is empty or all-null (an average over zero values has no
// honest answer).
func (d Dataframe) Avg(name string) (float64, error) {
	// Pure composition: Sum and Count are already null-aware.
	sum, err := d.Sum(name)
	if err != nil {
		return 0, err
	}
	n, err := d.Count(name)
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, fmt.Errorf("%w: cannot average column %q", ErrNoValidValues, name)
	}
	return sum / float64(n), nil
}

// Min returns the smallest valid value in the numeric column with the
// given name. Null rows are skipped.
//
// It returns ErrColumnNotFound if the column does not exist,
// ErrTypeMismatch if the column is not numeric, and ErrNoValidValues if
// the column is empty or all-null (the minimum of nothing does not exist).
func (d Dataframe) Min(name string) (float64, error) {
	col, err := d.Column(name)
	if err != nil {
		return 0, err
	}
	switch c := col.(type) {
	case *Float64Column:
		best, found := 0.0, false
		for v := range c.validValues() {
			if !found || v < best {
				best, found = v, true
			}
		}
		if !found {
			return 0, fmt.Errorf("%w: cannot take min of column %q", ErrNoValidValues, name)
		}
		return best, nil
	default:
		return 0, fmt.Errorf("%w: cannot take min of %s column %q", ErrTypeMismatch, col.DType(), name)
	}
}

// Max returns the largest valid value in the numeric column with the
// given name. Null rows are skipped.
//
// It returns ErrColumnNotFound if the column does not exist,
// ErrTypeMismatch if the column is not numeric, and ErrNoValidValues if
// the column is empty or all-null (the maximum of nothing does not exist).
func (d Dataframe) Max(name string) (float64, error) {
	col, err := d.Column(name)
	if err != nil {
		return 0, err
	}
	switch c := col.(type) {
	case *Float64Column:
		best, found := 0.0, false
		for v := range c.validValues() {
			if !found || v > best {
				best, found = v, true
			}
		}
		if !found {
			return 0, fmt.Errorf("%w: cannot take max of column %q", ErrNoValidValues, name)
		}
		return best, nil
	default:
		return 0, fmt.Errorf("%w: cannot take max of %s column %q", ErrTypeMismatch, col.DType(), name)
	}
}
