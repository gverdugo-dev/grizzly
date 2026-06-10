package grizzly

import (
	"cmp"
	"fmt"
	"slices"
)

// Sort returns a new Dataframe with the rows ordered by the named column,
// ascending. Null rows sort first (also under SortDesc — polars' rule:
// the holes group at the top in both directions). The sort is stable:
// rows with equal keys keep their original relative order.
//
// Columnar engines do not reorder rows in place: Sort orders a permutation
// of row indices and then gathers every column through it once — the same
// indices-then-gather pattern GroupBy uses.
//
// It returns ErrColumnNotFound if the column does not exist.
func (d Dataframe) Sort(name string) (Dataframe, error) {
	return d.sortBy(name, false)
}

// SortDesc returns a new Dataframe with the rows ordered by the named
// column, descending. Null rows still sort first; see Sort.
func (d Dataframe) SortDesc(name string) (Dataframe, error) {
	return d.sortBy(name, true)
}

// sortBy implements Sort and SortDesc: build the comparator for the key
// column's concrete type, stable-sort a row-index permutation with it,
// and gather every column through the permutation.
func (d Dataframe) sortBy(name string, desc bool) (Dataframe, error) {
	col, err := d.Column(name)
	if err != nil {
		return Dataframe{}, err
	}

	var compare func(i, j int) int
	switch c := col.(type) {
	case *Float64Column:
		compare = cmpRows(c.values, c.validity, desc)
	case *StringColumn:
		compare = cmpRows(c.values, c.validity, desc)
	case *BoolColumn:
		compare = cmpBoolRows(c, desc)
	default:
		return Dataframe{}, fmt.Errorf("%w: Sort on unsupported column %q",
			ErrTypeMismatch, name)
	}

	perm := make([]int, d.NumRows())
	for i := range perm {
		perm[i] = i
	}
	// SortStableFunc passes slice elements (row indices) straight to the
	// comparator; stable keeps equal keys in their original order.
	slices.SortStableFunc(perm, compare)

	cols := make([]Column, d.NumCols())
	for j, c := range d.cols {
		gathered, err := gatherRows(c, perm)
		if err != nil {
			return Dataframe{}, fmt.Errorf("Sort: %w", err)
		}
		cols[j] = gathered
	}
	logger.Debug("sorted", "by", name, "desc", desc)
	return NewDataframe(cols...)
}

// cmpRows returns a row comparator over a typed value slice with nulls
// first: a null row sorts before a valid one regardless of direction, two
// nulls tie, and two valid rows compare by value (cmp.Compare returns
// -1/0/+1), flipped when descending.
//
// Generic over cmp.Ordered, like the comparison loops in mask.go: one
// implementation serves float64 and string keys.
func cmpRows[T cmp.Ordered](values []T, validity []uint64, desc bool) func(i, j int) int {
	return func(i, j int) int {
		vi := validity == nil || bitmapGet(validity, i)
		vj := validity == nil || bitmapGet(validity, j)
		if vi != vj {
			if vi {
				return 1 // null before valid: nulls first in both directions
			}
			return -1
		}
		if !vi {
			return 0 // both null: tie (stability keeps their original order)
		}
		c := cmp.Compare(values[i], values[j])
		if desc {
			return -c
		}
		return c
	}
}

// cmpBoolRows is cmpRows for the packed BoolColumn: false sorts before
// true ascending, with the same nulls-first rule.
func cmpBoolRows(c *BoolColumn, desc bool) func(i, j int) int {
	boolVal := func(i int) int {
		if bitmapGet(c.values, i) {
			return 1
		}
		return 0
	}
	return func(i, j int) int {
		vi := c.validity == nil || bitmapGet(c.validity, i)
		vj := c.validity == nil || bitmapGet(c.validity, j)
		if vi != vj {
			if vi {
				return 1
			}
			return -1
		}
		if !vi {
			return 0
		}
		r := cmp.Compare(boolVal(i), boolVal(j))
		if desc {
			return -r
		}
		return r
	}
}
