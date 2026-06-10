package grizzly

import "fmt"

// GroupedDataframe is the intermediate result of Dataframe.GroupBy: the
// source dataframe plus the grouping decision, waiting for Agg to say
// which aggregations to compute.
//
// It is cheap: grouping does not split the data into per-group copies, it
// only stamps every row with a group id (the factorize pattern). groupIDs
// is a flat []int parallel to the rows, and firstRows remembers where each
// group's key first appeared — which both makes the output deterministic
// (Go maps iterate in random order on purpose) and lets Agg rebuild the
// key column by gathering those rows.
type GroupedDataframe struct {
	src       Dataframe
	key       Column
	groupIDs  []int // row i belongs to group groupIDs[i]
	firstRows []int // first row of each group, in first-appearance order
	err       error // deferred GroupBy error, reported by Agg
}

// GroupBy groups the dataframe's rows by the values of the named key
// column, returning a GroupedDataframe to aggregate with Agg:
//
//	result, err := df.GroupBy("store").Agg(grizzly.Sum("price"))
//
// Null keys follow the SQL rule: all nulls form one group together (for
// grouping, null equals null — the opposite of comparisons, where null is
// unknown). No row is silently dropped.
//
// The signature is variadic for forward compatibility, but only a single
// key column is supported for now; multiple names are an error.
//
// GroupBy returns no error itself so the call chains —
// df.GroupBy(...).Agg(...) would not compile against a multi-valued
// return. Errors (unknown column, unsupported key type, multiple names)
// are deferred and reported by Agg, the same pattern sql.Row.Scan uses.
func (d Dataframe) GroupBy(names ...string) GroupedDataframe {
	if len(names) != 1 {
		return GroupedDataframe{err: fmt.Errorf(
			"GroupBy: exactly one key column supported for now, got %d", len(names))}
	}
	col, err := d.Column(names[0])
	if err != nil {
		return GroupedDataframe{err: err}
	}

	groupIDs := make([]int, col.Len())
	var firstRows []int
	switch c := col.(type) {
	case *Float64Column:
		firstRows = factorizeSlice(c.values, c.validity, groupIDs)
	case *StringColumn:
		firstRows = factorizeSlice(c.values, c.validity, groupIDs)
	case *BoolColumn:
		firstRows = factorizeBool(c, groupIDs)
	default:
		return GroupedDataframe{err: fmt.Errorf("%w: GroupBy on unsupported column %q",
			ErrTypeMismatch, names[0])}
	}
	logger.Debug("grouped", "key", names[0], "groups", len(firstRows))
	return GroupedDataframe{src: d, key: col, groupIDs: groupIDs, firstRows: firstRows}
}

// factorizeSlice assigns a group id to every row of a typed key slice:
// groupIDs[i] = the id of row i's group. Ids are handed out incrementally
// on first appearance, so they double as the output order. The returned
// slice holds the first row index of each group.
//
// Generic over comparable — the minimal constraint for being a map key —
// so float64 and string keys share one implementation. This is the only
// place GroupBy touches a hash table: aggregation loops afterwards index
// slices with the precomputed ids (~1ns) instead of hashing keys
// (~20-50ns per lookup).
func factorizeSlice[T comparable](values []T, validity []uint64, groupIDs []int) []int {
	ids := make(map[T]int)
	var firstRows []int
	nullID := -1
	for i, v := range values {
		if validity != nil && !bitmapGet(validity, i) {
			if nullID < 0 { // first null seen founds the null group
				nullID = len(firstRows)
				firstRows = append(firstRows, i)
			}
			groupIDs[i] = nullID
			continue
		}
		id, ok := ids[v]
		if !ok {
			id = len(firstRows)
			ids[v] = id
			firstRows = append(firstRows, i)
		}
		groupIDs[i] = id
	}
	return firstRows
}

// factorizeBool is factorizeSlice for the packed BoolColumn: the key
// domain is only {true, false, null}, so three plain ints replace the
// hash table entirely.
func factorizeBool(c *BoolColumn, groupIDs []int) []int {
	trueID, falseID, nullID := -1, -1, -1
	var firstRows []int
	for i := 0; i < c.length; i++ {
		if c.validity != nil && !bitmapGet(c.validity, i) {
			if nullID < 0 {
				nullID = len(firstRows)
				firstRows = append(firstRows, i)
			}
			groupIDs[i] = nullID
			continue
		}
		if bitmapGet(c.values, i) {
			if trueID < 0 {
				trueID = len(firstRows)
				firstRows = append(firstRows, i)
			}
			groupIDs[i] = trueID
		} else {
			if falseID < 0 {
				falseID = len(firstRows)
				firstRows = append(firstRows, i)
			}
			groupIDs[i] = falseID
		}
	}
	return firstRows
}

// aggOp identifies the operation of an AggSpec.
type aggOp int

const (
	aggSum aggOp = iota
	aggAvg
	aggMin
	aggMax
	aggCount
)

// aggNames maps each operation to the prefix of its output column name
// (sum_price, count_product...).
var aggNames = map[aggOp]string{
	aggSum:   "sum",
	aggAvg:   "avg",
	aggMin:   "min",
	aggMax:   "max",
	aggCount: "count",
}

// AggSpec describes one aggregation for GroupedDataframe.Agg: which
// operation over which column. Build specs with the package-level
// constructors Sum, Avg, Min, Max and Count — package functions, distinct
// from the Dataframe methods of the same names (Go resolves them by
// receiver, so grizzly.Sum("price") and df.Sum("price") coexist).
type AggSpec struct {
	op  aggOp
	col string
}

// Sum returns an AggSpec that sums the named column per group.
func Sum(col string) AggSpec { return AggSpec{op: aggSum, col: col} }

// Avg returns an AggSpec that averages the named column per group.
func Avg(col string) AggSpec { return AggSpec{op: aggAvg, col: col} }

// Min returns an AggSpec that takes the per-group minimum of the named column.
func Min(col string) AggSpec { return AggSpec{op: aggMin, col: col} }

// Max returns an AggSpec that takes the per-group maximum of the named column.
func Max(col string) AggSpec { return AggSpec{op: aggMax, col: col} }

// Count returns an AggSpec that counts the valid (non-null) rows of the
// named column per group — SQL's COUNT(col).
func Count(col string) AggSpec { return AggSpec{op: aggCount, col: col} }

// Agg computes the given aggregations over the groups and returns the
// result as a new Dataframe: the key column first (one row per group, in
// first-appearance order, named like the original), then one float64
// column per spec, named <op>_<column> (sum_price, count_product...).
//
// Null semantics mirror the whole-column aggregations, translated to
// per-group form: nulls are skipped; a group whose values are all null
// yields 0 for Sum and Count (the sum/count of nothing) and null for Avg,
// Min and Max (the per-group equivalent of ErrNoValidValues — a group
// cannot return an error, so it returns null).
//
// Sum, Avg, Min and Max require a float64 column; Count accepts any.
// It returns ErrColumnNotFound for unknown columns and ErrTypeMismatch
// for unsupported ones.
func (g GroupedDataframe) Agg(specs ...AggSpec) (Dataframe, error) {
	if g.err != nil { // deferred GroupBy error (see GroupBy's doc comment)
		return Dataframe{}, g.err
	}
	if len(specs) == 0 {
		return Dataframe{}, fmt.Errorf("Agg: at least one aggregation required")
	}
	n := len(g.firstRows)
	cols := make([]Column, 0, 1+len(specs))

	keyCol, err := gatherRows(g.key, g.firstRows)
	if err != nil {
		return Dataframe{}, fmt.Errorf("Agg: %w", err)
	}
	cols = append(cols, keyCol)

	for _, spec := range specs {
		col, err := g.src.Column(spec.col)
		if err != nil {
			return Dataframe{}, err
		}
		outName := aggNames[spec.op] + "_" + spec.col

		// Count works for every column type: it only needs validity.
		if spec.op == aggCount {
			counts := make([]float64, n) // float64: grizzly's only number
			validity := columnValidity(col)
			for i := 0; i < col.Len(); i++ {
				if validity == nil || bitmapGet(validity, i) {
					counts[g.groupIDs[i]]++
				}
			}
			cols = append(cols, NewFloat64Column(outName, counts))
			continue
		}

		c, ok := col.(*Float64Column)
		if !ok {
			return Dataframe{}, fmt.Errorf("%w: %s over %s column %q",
				ErrTypeMismatch, aggNames[spec.op], col.DType(), spec.col)
		}

		// The factorize payoff: these hot loops never touch a map — one
		// pass over the contiguous values, accumulating into slots indexed
		// by the precomputed group ids.
		switch spec.op {
		case aggSum:
			sums := make([]float64, n)
			for i, v := range c.values {
				if c.validity == nil || bitmapGet(c.validity, i) {
					sums[g.groupIDs[i]] += v
				}
			}
			cols = append(cols, NewFloat64Column(outName, sums))
		case aggAvg:
			sums := make([]float64, n)
			counts := make([]float64, n)
			for i, v := range c.values {
				if c.validity == nil || bitmapGet(c.validity, i) {
					gid := g.groupIDs[i]
					sums[gid] += v
					counts[gid]++
				}
			}
			avgs := make([]float64, n)
			valid := make([]bool, n)
			for gid := range avgs {
				if counts[gid] > 0 {
					avgs[gid] = sums[gid] / counts[gid]
					valid[gid] = true
				}
			}
			ac, err := NewFloat64ColumnWithNulls(outName, avgs, valid)
			if err != nil {
				return Dataframe{}, fmt.Errorf("Agg: %w", err)
			}
			cols = append(cols, ac)
		case aggMin, aggMax:
			best := make([]float64, n)
			found := make([]bool, n)
			for i, v := range c.values {
				if c.validity == nil || bitmapGet(c.validity, i) {
					gid := g.groupIDs[i]
					if !found[gid] || (spec.op == aggMin && v < best[gid]) ||
						(spec.op == aggMax && v > best[gid]) {
						best[gid] = v
						found[gid] = true
					}
				}
			}
			bc, err := NewFloat64ColumnWithNulls(outName, best, found)
			if err != nil {
				return Dataframe{}, fmt.Errorf("Agg: %w", err)
			}
			cols = append(cols, bc)
		}
	}
	logger.Debug("aggregated", "groups", n, "aggs", len(specs))
	return NewDataframe(cols...)
}

// columnValidity returns a column's validity bitmap (nil = no nulls)
// through a type switch — the type-agnostic escape hatch for code that
// only needs validity, like Count.
func columnValidity(col Column) []uint64 {
	switch c := col.(type) {
	case *Float64Column:
		return c.validity
	case *StringColumn:
		return c.validity
	case *BoolColumn:
		return c.validity
	}
	return nil
}
