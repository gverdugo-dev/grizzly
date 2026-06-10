package grizzly

import (
	"cmp"
	"fmt"
)

// Mask is the result of a comparison over a Dataframe column: one bit per
// row telling whether that row satisfies the condition. Masks are produced
// by the Dataframe comparators (Eq, Ne, Lt, Le, Gt, Ge), combined with
// And, Or and Not, and finally consumed by Dataframe.Where, which
// materializes the surviving rows — one materialization no matter how many
// conditions were combined.
//
// A mask carries Kleene three-valued logic: comparing a null row yields
// neither true nor false but unknown, tracked in a validity bitmap exactly
// like a column's (nil = no unknowns). Combinators implement the Kleene
// truth tables at word level — 64 rows per instruction — and Where keeps
// only rows whose bit is valid AND true, mirroring SQL's WHERE.
type Mask struct {
	length   int
	bits     []uint64 // bit i = result at row i (placeholder at unknown rows)
	validity []uint64 // nil = all known; bit 0 = unknown (null comparison)
}

// checkLen panics if the two masks have different lengths: masks are only
// meaningful over the same dataframe, so a mismatch is a programmer error,
// like indexing out of range — not a data condition worth an error value.
func (m Mask) checkLen(o Mask) {
	if m.length != o.length {
		panic(fmt.Sprintf("grizzly: combining masks of lengths %d and %d", m.length, o.length))
	}
}

// knownWord returns the validity word w of a mask, where a nil validity
// bitmap means "everything known" (all ones).
func knownWord(validity []uint64, w int) uint64 {
	if validity == nil {
		return ^uint64(0)
	}
	return validity[w]
}

// And returns the logical AND of the two masks under Kleene three-valued
// logic: the result is known when both operands are known, or when either
// known operand is false (false decides an AND on its own — false AND
// unknown is false, not unknown). It panics if the lengths differ.
func (m Mask) And(o Mask) Mask {
	m.checkLen(o)
	out := Mask{length: m.length, bits: make([]uint64, len(m.bits))}
	needValidity := m.validity != nil || o.validity != nil
	var known []uint64
	if needValidity {
		known = make([]uint64, len(m.bits))
	}
	for w := range m.bits {
		a, b := m.bits[w], o.bits[w]
		out.bits[w] = a & b
		if needValidity {
			aK, bK := knownWord(m.validity, w), knownWord(o.validity, w)
			// &^ is Go's AND NOT: aK &^ a = "known on the left AND false
			// there". The three Kleene ways an AND result is known.
			known[w] = (aK & bK) | (aK &^ a) | (bK &^ b)
		}
	}
	if needValidity {
		clearTrailingBits(known, m.length) // ^a set the spare bits; re-zero them
		out.validity = known
	}
	return out
}

// Or returns the logical OR of the two masks under Kleene three-valued
// logic: the result is known when both operands are known, or when either
// known operand is true (true decides an OR on its own — true OR unknown
// is true, not unknown). It panics if the lengths differ.
func (m Mask) Or(o Mask) Mask {
	m.checkLen(o)
	out := Mask{length: m.length, bits: make([]uint64, len(m.bits))}
	needValidity := m.validity != nil || o.validity != nil
	var known []uint64
	if needValidity {
		known = make([]uint64, len(m.bits))
	}
	for w := range m.bits {
		a, b := m.bits[w], o.bits[w]
		out.bits[w] = a | b
		if needValidity {
			aK, bK := knownWord(m.validity, w), knownWord(o.validity, w)
			known[w] = (aK & bK) | (aK & a) | (bK & b)
		}
	}
	if needValidity {
		clearTrailingBits(known, m.length)
		out.validity = known
	}
	return out
}

// Not returns the logical negation of the mask. Unknown stays unknown
// (NOT null is null, per Kleene), so the validity bitmap is shared
// untouched — only the value bits flip.
func (m Mask) Not() Mask {
	bits := make([]uint64, len(m.bits))
	for w, word := range m.bits {
		bits[w] = ^word
	}
	clearTrailingBits(bits, m.length) // ^word set the spare bits; re-zero them
	return Mask{length: m.length, bits: bits, validity: m.validity}
}

// compareOp identifies a comparison operator inside the generic
// comparison loop. Ne has no entry: it is implemented as Eq().Not().
type compareOp int

const (
	opEq compareOp = iota
	opLt
	opLe
	opGt
	opGe
)

// orderedBits builds comparison bits for a typed slice against a target
// value. Generic over cmp.Ordered (any type supporting < and ==), so the
// same code serves float64 and string columns.
//
// One loop per operator, chosen once: the switch is hoisted out of the
// hot loop instead of re-deciding the operator per row.
func orderedBits[T cmp.Ordered](values []T, target T, op compareOp) []uint64 {
	bits := newBitmap(len(values))
	switch op {
	case opEq:
		for i, v := range values {
			if v == target {
				bitmapSet(bits, i)
			}
		}
	case opLt:
		for i, v := range values {
			if v < target {
				bitmapSet(bits, i)
			}
		}
	case opLe:
		for i, v := range values {
			if v <= target {
				bitmapSet(bits, i)
			}
		}
	case opGt:
		for i, v := range values {
			if v > target {
				bitmapSet(bits, i)
			}
		}
	case opGe:
		for i, v := range values {
			if v >= target {
				bitmapSet(bits, i)
			}
		}
	}
	return bits
}

// compare is the shared engine behind the public comparators: column
// lookup, value type check against the column's dtype, and the typed
// comparison loop. The mask shares the column's validity bitmap (columns
// are immutable after construction, so sharing is safe): a null row is an
// unknown comparison.
func (d Dataframe) compare(method string, op compareOp, name string, value any) (Mask, error) {
	col, err := d.Column(name)
	if err != nil {
		return Mask{}, err
	}
	switch c := col.(type) {
	case *Float64Column:
		v, ok := value.(float64)
		if !ok {
			return Mask{}, fmt.Errorf("%w: %s on float64 column %q with %T value",
				ErrTypeMismatch, method, name, value)
		}
		return Mask{length: c.Len(), bits: orderedBits(c.values, v, op), validity: c.validity}, nil
	case *StringColumn:
		v, ok := value.(string)
		if !ok {
			return Mask{}, fmt.Errorf("%w: %s on string column %q with %T value",
				ErrTypeMismatch, method, name, value)
		}
		return Mask{length: c.Len(), bits: orderedBits(c.values, v, op), validity: c.validity}, nil
	case *BoolColumn:
		if op != opEq {
			return Mask{}, fmt.Errorf("%w: %s not supported on bool column %q (only Eq/Ne)",
				ErrTypeMismatch, method, name)
		}
		v, ok := value.(bool)
		if !ok {
			return Mask{}, fmt.Errorf("%w: %s on bool column %q with %T value",
				ErrTypeMismatch, method, name, value)
		}
		// The column's values are already packed bits: Eq(true) is a copy,
		// Eq(false) is its negation. No per-row loop at all.
		bits := make([]uint64, len(c.values))
		copy(bits, c.values)
		if !v {
			for w := range bits {
				bits[w] = ^bits[w]
			}
			clearTrailingBits(bits, c.length)
		}
		return Mask{length: c.length, bits: bits, validity: c.validity}, nil
	default:
		return Mask{}, fmt.Errorf("%w: %s on unsupported column %q", ErrTypeMismatch, method, name)
	}
}

// Eq returns a mask of the rows where the named column equals value.
// The value's type must match the column's dtype (float64, string or
// bool); null rows compare to unknown and are dropped by Where.
//
// It returns ErrColumnNotFound if the column does not exist and
// ErrTypeMismatch if the value's type does not match the column's.
func (d Dataframe) Eq(name string, value any) (Mask, error) {
	return d.compare("Eq", opEq, name, value)
}

// Ne returns a mask of the rows where the named column differs from
// value. Implemented as Eq().Not(), which preserves Kleene semantics:
// a null row is unknown for Ne too (null != x is not true in SQL either).
//
// It returns ErrColumnNotFound if the column does not exist and
// ErrTypeMismatch if the value's type does not match the column's.
func (d Dataframe) Ne(name string, value any) (Mask, error) {
	m, err := d.compare("Ne", opEq, name, value)
	if err != nil {
		return Mask{}, err
	}
	return m.Not(), nil
}

// Lt returns a mask of the rows where the named column is less than
// value. Supported on float64 and string columns (strings compare
// lexicographically, like Go's < operator).
//
// It returns ErrColumnNotFound if the column does not exist and
// ErrTypeMismatch if the value's type does not match the column's or the
// column does not support ordering (bool).
func (d Dataframe) Lt(name string, value any) (Mask, error) {
	return d.compare("Lt", opLt, name, value)
}

// Le returns a mask of the rows where the named column is less than or
// equal to value. See Lt for the supported column types and errors.
func (d Dataframe) Le(name string, value any) (Mask, error) {
	return d.compare("Le", opLe, name, value)
}

// Gt returns a mask of the rows where the named column is greater than
// value. See Lt for the supported column types and errors.
func (d Dataframe) Gt(name string, value any) (Mask, error) {
	return d.compare("Gt", opGt, name, value)
}

// Ge returns a mask of the rows where the named column is greater than
// or equal to value. See Lt for the supported column types and errors.
func (d Dataframe) Ge(name string, value any) (Mask, error) {
	return d.compare("Ge", opGe, name, value)
}

// Where returns a new Dataframe containing the rows whose mask bit is
// valid AND true — SQL WHERE semantics: an unknown (a comparison that
// involved a null) is not true, so the row is dropped.
//
// This is the single materialization point of a filter: every column's
// surviving values (and their validity) are gathered into fresh buffers.
// It returns an error if the mask's length does not match the dataframe's
// row count.
func (d Dataframe) Where(m Mask) (Dataframe, error) {
	if m.length != d.NumRows() {
		return Dataframe{}, fmt.Errorf("Where: mask has %d rows, dataframe has %d",
			m.length, d.NumRows())
	}

	// keep = bits AND validity: unknown is not true.
	keep := make([]uint64, len(m.bits))
	copy(keep, m.bits)
	if m.validity != nil {
		for w := range keep {
			keep[w] &= m.validity[w]
		}
	}
	n := bitmapCountSet(keep)

	cols := make([]Column, d.NumCols())
	for j, col := range d.cols {
		switch c := col.(type) {
		case *Float64Column:
			values := make([]float64, 0, n) // pre-sized: popcount told us n
			var valid []bool
			if c.validity != nil {
				valid = make([]bool, 0, n)
			}
			for i := range setBits(keep) {
				values = append(values, c.values[i])
				if valid != nil {
					valid = append(valid, bitmapGet(c.validity, i))
				}
			}
			if valid == nil {
				cols[j] = NewFloat64Column(c.name, values)
			} else {
				nc, err := NewFloat64ColumnWithNulls(c.name, values, valid)
				if err != nil {
					return Dataframe{}, fmt.Errorf("Where: %w", err)
				}
				cols[j] = nc
			}
		case *StringColumn:
			values := make([]string, 0, n)
			var valid []bool
			if c.validity != nil {
				valid = make([]bool, 0, n)
			}
			for i := range setBits(keep) {
				values = append(values, c.values[i])
				if valid != nil {
					valid = append(valid, bitmapGet(c.validity, i))
				}
			}
			if valid == nil {
				cols[j] = NewStringColumn(c.name, values)
			} else {
				nc, err := NewStringColumnWithNulls(c.name, values, valid)
				if err != nil {
					return Dataframe{}, fmt.Errorf("Where: %w", err)
				}
				cols[j] = nc
			}
		case *BoolColumn:
			values := make([]bool, 0, n)
			var valid []bool
			if c.validity != nil {
				valid = make([]bool, 0, n)
			}
			for i := range setBits(keep) {
				values = append(values, bitmapGet(c.values, i))
				if valid != nil {
					valid = append(valid, bitmapGet(c.validity, i))
				}
			}
			if valid == nil {
				cols[j] = NewBoolColumn(c.name, values)
			} else {
				nc, err := NewBoolColumnWithNulls(c.name, values, valid)
				if err != nil {
					return Dataframe{}, fmt.Errorf("Where: %w", err)
				}
				cols[j] = nc
			}
		default:
			return Dataframe{}, fmt.Errorf("%w: Where over unsupported column %q",
				ErrTypeMismatch, col.Name())
		}
	}
	logger.Debug("where", "kept", n, "of", d.NumRows())
	return NewDataframe(cols...)
}
