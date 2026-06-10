package grizzly_test

// Black-box tests for the comparators, the Kleene mask combinators and
// Where. Unknown (a comparison that touched a null) is not directly
// observable from outside — Where drops both false and unknown — so the
// Kleene edge cases are asserted through composition: Not() flips a
// KNOWN false into a kept row, but leaves an unknown dropped.

import (
	"errors"
	"slices"
	"testing"

	"github.com/gverdugo-dev/grizzly"
)

// maskFixture builds the dataframe used across the mask tests:
//
//	id    a     b
//	r0    1     null
//	r1    null  2
//	r2    3     0
//	r3    4     4
func maskFixture(t *testing.T) grizzly.Dataframe {
	t.Helper()
	a, err := grizzly.NewFloat64ColumnWithNulls("a",
		[]float64{1, 0, 3, 4}, []bool{true, false, true, true})
	if err != nil {
		t.Fatalf("building a: %v", err)
	}
	b, err := grizzly.NewFloat64ColumnWithNulls("b",
		[]float64{0, 2, 0, 4}, []bool{false, true, true, true})
	if err != nil {
		t.Fatalf("building b: %v", err)
	}
	id := grizzly.NewStringColumn("id", []string{"r0", "r1", "r2", "r3"})
	df, err := grizzly.NewDataframe(id, a, b)
	if err != nil {
		t.Fatalf("building dataframe: %v", err)
	}
	return df
}

// survivors applies the mask with Where and returns the id column values
// of the surviving rows.
func survivors(t *testing.T, df grizzly.Dataframe, m grizzly.Mask) []string {
	t.Helper()
	out, err := df.Where(m)
	if err != nil {
		t.Fatalf("Where: %v", err)
	}
	c, err := out.Column("id")
	if err != nil {
		t.Fatalf("Column(id): %v", err)
	}
	sc := c.(*grizzly.StringColumn)
	ids := make([]string, sc.Len())
	for i := range ids {
		ids[i], _ = sc.Value(i)
	}
	return ids
}

// TestComparators runs each comparison operator through Where and checks
// the surviving rows. Row r1 (a = null) must never survive: a comparison
// against null is unknown, and unknown is not true.
func TestComparators(t *testing.T) {
	df := maskFixture(t)

	tests := []struct {
		name string
		mask func() (grizzly.Mask, error)
		want []string
	}{
		{"Eq", func() (grizzly.Mask, error) { return df.Eq("a", 3.0) }, []string{"r2"}},
		{"Ne", func() (grizzly.Mask, error) { return df.Ne("a", 3.0) }, []string{"r0", "r3"}},
		{"Lt", func() (grizzly.Mask, error) { return df.Lt("a", 3.0) }, []string{"r0"}},
		{"Le", func() (grizzly.Mask, error) { return df.Le("a", 3.0) }, []string{"r0", "r2"}},
		{"Gt", func() (grizzly.Mask, error) { return df.Gt("a", 3.0) }, []string{"r3"}},
		{"Ge", func() (grizzly.Mask, error) { return df.Ge("a", 3.0) }, []string{"r2", "r3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := tt.mask()
			if err != nil {
				t.Fatalf("building mask: %v", err)
			}
			if got := survivors(t, df, m); !slices.Equal(got, tt.want) {
				t.Errorf("survivors = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestKleeneNotKeepsUnknown pins NOT's Kleene rule on the Ne comparator
// (implemented as Eq().Not()): row r1 has a null in column a, so both
// Eq and Ne are unknown there — the row survives neither. If Not()
// wrongly flipped unknown into known, Ne would resurrect it.
func TestKleeneNotKeepsUnknown(t *testing.T) {
	df := maskFixture(t)

	eq, err := df.Eq("a", 99.0)
	if err != nil {
		t.Fatalf("Eq: %v", err)
	}
	ne, err := df.Ne("a", 99.0)
	if err != nil {
		t.Fatalf("Ne: %v", err)
	}
	if got := survivors(t, df, eq); slices.Contains(got, "r1") {
		t.Errorf("Eq survivors %v contain r1 (null row)", got)
	}
	// Every non-null row differs from 99, so Ne keeps all of them — except
	// r1, whose unknown must stay unknown through the Not().
	if got, want := survivors(t, df, ne), []string{"r0", "r2", "r3"}; !slices.Equal(got, want) {
		t.Errorf("Ne survivors = %v, want %v", got, want)
	}
}

// TestKleeneTrueOrUnknown pins OR's short-circuit rule: true OR unknown
// is true, so a row that satisfies one condition is kept even when the
// other condition touched a null.
func TestKleeneTrueOrUnknown(t *testing.T) {
	df := maskFixture(t)

	aPos, err := df.Gt("a", 0.0) // r0: true,    r1: unknown
	if err != nil {
		t.Fatalf("Gt(a): %v", err)
	}
	bPos, err := df.Gt("b", 0.0) // r0: unknown, r1: true
	if err != nil {
		t.Fatalf("Gt(b): %v", err)
	}

	// r0: true OR unknown = true. r1: unknown OR true = true (symmetric).
	// r2: true OR false = true. r3: true OR true = true.
	got := survivors(t, df, aPos.Or(bPos))
	if want := []string{"r0", "r1", "r2", "r3"}; !slices.Equal(got, want) {
		t.Errorf("Or survivors = %v, want %v", got, want)
	}

	// The AND dual: r0 true AND unknown = unknown (dropped), r2 true AND
	// false = false (dropped) — only r3 is known-true on both sides.
	got = survivors(t, df, aPos.And(bPos))
	if want := []string{"r3"}; !slices.Equal(got, want) {
		t.Errorf("And survivors = %v, want %v", got, want)
	}
}

// TestKleeneFalseAndUnknownIsKnownFalse distinguishes known-false from
// unknown — invisible to Where directly — through composition: at row r0,
// (a < 0) is known false and (b > 0) is unknown. If AND wrongly produced
// unknown, Not() would keep it unknown and drop the row; the Kleene rule
// (false decides an AND) makes Not(false) = true and keeps r0.
func TestKleeneFalseAndUnknownIsKnownFalse(t *testing.T) {
	df := maskFixture(t)

	aNeg, err := df.Lt("a", 0.0) // r0: known false
	if err != nil {
		t.Fatalf("Lt: %v", err)
	}
	bPos, err := df.Gt("b", 0.0) // r0: unknown
	if err != nil {
		t.Fatalf("Gt: %v", err)
	}

	got := survivors(t, df, aNeg.And(bPos).Not())
	if !slices.Contains(got, "r0") {
		t.Errorf("Not(false AND unknown) dropped r0: AND produced unknown instead of known false (survivors %v)", got)
	}
}

// TestBoolEq covers the bool comparator: Eq(true), Eq(false) (the no-loop
// word-level negation path) and the rejection of ordering operators.
func TestBoolEq(t *testing.T) {
	flags, err := grizzly.NewBoolColumnWithNulls("ok",
		[]bool{true, false, false}, []bool{true, true, false})
	if err != nil {
		t.Fatalf("building flags: %v", err)
	}
	id := grizzly.NewStringColumn("id", []string{"r0", "r1", "r2"})
	df, err := grizzly.NewDataframe(id, flags)
	if err != nil {
		t.Fatalf("building dataframe: %v", err)
	}

	mTrue, err := df.Eq("ok", true)
	if err != nil {
		t.Fatalf("Eq(true): %v", err)
	}
	if got, want := survivors(t, df, mTrue), []string{"r0"}; !slices.Equal(got, want) {
		t.Errorf("Eq(true) survivors = %v, want %v", got, want)
	}

	// Eq(false) negates the packed words; the null row r2 must still be
	// dropped (its placeholder bit flipped to 1, but it is unknown).
	mFalse, err := df.Eq("ok", false)
	if err != nil {
		t.Fatalf("Eq(false): %v", err)
	}
	if got, want := survivors(t, df, mFalse), []string{"r1"}; !slices.Equal(got, want) {
		t.Errorf("Eq(false) survivors = %v, want %v", got, want)
	}

	if _, err := df.Lt("ok", true); !errors.Is(err, grizzly.ErrTypeMismatch) {
		t.Errorf("Lt on bool err = %v, want ErrTypeMismatch", err)
	}
}

// TestCompareTypeMismatch verifies the value-type check, including the
// classic any-trap: an untyped 1 stored in an any is an int, not a
// float64, so Eq("a", 1) must be rejected — Eq("a", 1.0) is the right call.
func TestCompareTypeMismatch(t *testing.T) {
	df := maskFixture(t)

	if _, err := df.Eq("a", "high"); !errors.Is(err, grizzly.ErrTypeMismatch) {
		t.Errorf("Eq(float col, string) err = %v, want ErrTypeMismatch", err)
	}
	if _, err := df.Eq("a", 1); !errors.Is(err, grizzly.ErrTypeMismatch) {
		t.Errorf("Eq(float col, int) err = %v, want ErrTypeMismatch", err)
	}
	if _, err := df.Eq("nope", 1.0); !errors.Is(err, grizzly.ErrColumnNotFound) {
		t.Errorf("Eq(missing col) err = %v, want ErrColumnNotFound", err)
	}
}

// TestWhereLengthMismatch verifies Where rejects a mask built over a
// different dataframe (different row count) with an error, not a panic:
// crossing dataframes is plausible user error, not programmer error.
func TestWhereLengthMismatch(t *testing.T) {
	df := maskFixture(t)
	other, err := grizzly.FromStructs([]struct{ X float64 }{{1}, {2}})
	if err != nil {
		t.Fatalf("FromStructs: %v", err)
	}
	m, err := other.Gt("X", 0.0)
	if err != nil {
		t.Fatalf("Gt: %v", err)
	}
	if _, err := df.Where(m); err == nil {
		t.Error("Where with foreign mask: expected error, got nil")
	}
}
