package grizzly_test

// Black-box tests for column construction and the row-level accessors
// (Value, IsValid, NullCount). The package is grizzly_test so the tests
// exercise the library exactly as a user would: through the exported API,
// with an explicit import.

import (
	"testing"

	"github.com/gverdugo-dev/grizzly"
)

// mustPanic runs fn and fails the test unless it panics. t.Helper() makes
// failure reports point at the caller's line, not this function's.
func mustPanic(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: expected panic, got none", name)
		}
	}()
	fn()
}

// TestWithNullsLengthMismatch verifies that every WithNulls constructor
// rejects a validity mask whose length differs from the values slice.
func TestWithNullsLengthMismatch(t *testing.T) {
	t.Run("float64", func(t *testing.T) {
		if _, err := grizzly.NewFloat64ColumnWithNulls("x", []float64{1, 2}, []bool{true}); err == nil {
			t.Error("expected error, got nil")
		}
	})
	t.Run("string", func(t *testing.T) {
		if _, err := grizzly.NewStringColumnWithNulls("x", []string{"a", "b"}, []bool{true}); err == nil {
			t.Error("expected error, got nil")
		}
	})
	t.Run("bool", func(t *testing.T) {
		if _, err := grizzly.NewBoolColumnWithNulls("x", []bool{true, false}, []bool{true}); err == nil {
			t.Error("expected error, got nil")
		}
	})
}

// TestAllTrueMaskMeansNoNulls pins the observable side of the nil-bitmap
// compaction: a WithNulls column built with an all-true mask behaves
// exactly like its null-free sibling.
func TestAllTrueMaskMeansNoNulls(t *testing.T) {
	col, err := grizzly.NewFloat64ColumnWithNulls("x", []float64{1, 2, 3}, []bool{true, true, true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := col.NullCount(); got != 0 {
		t.Errorf("NullCount = %d, want 0", got)
	}
	for i := 0; i < col.Len(); i++ {
		if !col.IsValid(i) {
			t.Errorf("IsValid(%d) = false, want true", i)
		}
	}
}

// TestValueCommaOk verifies the comma-ok contract on all three column
// types: ok is true with the real value at valid rows, false at nulls.
func TestValueCommaOk(t *testing.T) {
	valid := []bool{true, false, true}

	t.Run("float64", func(t *testing.T) {
		col, err := grizzly.NewFloat64ColumnWithNulls("x", []float64{1.5, 0, 2.5}, valid)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if v, ok := col.Value(0); !ok || v != 1.5 {
			t.Errorf("Value(0) = (%v, %v), want (1.5, true)", v, ok)
		}
		if _, ok := col.Value(1); ok {
			t.Error("Value(1) ok = true, want false (null row)")
		}
		if got := col.NullCount(); got != 1 {
			t.Errorf("NullCount = %d, want 1", got)
		}
	})

	t.Run("string", func(t *testing.T) {
		col, err := grizzly.NewStringColumnWithNulls("x", []string{"a", "", "c"}, valid)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if v, ok := col.Value(0); !ok || v != "a" {
			t.Errorf("Value(0) = (%q, %v), want (\"a\", true)", v, ok)
		}
		if _, ok := col.Value(1); ok {
			t.Error("Value(1) ok = true, want false (null row)")
		}
	})

	t.Run("bool", func(t *testing.T) {
		col, err := grizzly.NewBoolColumnWithNulls("x", []bool{true, false, false}, valid)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if v, ok := col.Value(0); !ok || v != true {
			t.Errorf("Value(0) = (%v, %v), want (true, true)", v, ok)
		}
		if _, ok := col.Value(1); ok {
			t.Error("Value(1) ok = true, want false (null row)")
		}
		if v, ok := col.Value(2); !ok || v != false {
			t.Errorf("Value(2) = (%v, %v), want (false, true)", v, ok)
		}
	})
}

// TestBoolColumnPacking round-trips 65 bool rows through the packed
// representation. 65 forces a second bitmap word, so the word-boundary
// index math is exercised through the public API too.
func TestBoolColumnPacking(t *testing.T) {
	values := make([]bool, 65)
	for i := range values {
		values[i] = i%3 == 0 // a pattern that is not all-true / all-false
	}
	col := grizzly.NewBoolColumn("flags", values)

	if got := col.Len(); got != 65 {
		t.Fatalf("Len = %d, want 65", got)
	}
	for i, want := range values {
		v, ok := col.Value(i)
		if !ok || v != want {
			t.Errorf("Value(%d) = (%v, %v), want (%v, true)", i, v, ok, want)
		}
	}
}

// TestOutOfRangePanics verifies the documented panic on out-of-range row
// indices. BoolColumn matters most: its packed storage has no per-row
// slice indexing, so its bounds check is hand-written.
func TestOutOfRangePanics(t *testing.T) {
	f := grizzly.NewFloat64Column("f", []float64{1})
	s := grizzly.NewStringColumn("s", []string{"a"})
	b := grizzly.NewBoolColumn("b", []bool{true})

	mustPanic(t, "Float64.Value(1)", func() { f.Value(1) })
	mustPanic(t, "Float64.IsValid(1)", func() { f.IsValid(1) })
	mustPanic(t, "String.Value(1)", func() { s.Value(1) })
	mustPanic(t, "String.IsValid(1)", func() { s.IsValid(1) })
	mustPanic(t, "Bool.Value(1)", func() { b.Value(1) })
	mustPanic(t, "Bool.Value(-1)", func() { b.Value(-1) })
	mustPanic(t, "Bool.IsValid(1)", func() { b.IsValid(1) })
}

// TestColumnMetadata covers the trivial accessors in one sweep: Name,
// DType and Len for each concrete type, through the Column interface.
func TestColumnMetadata(t *testing.T) {
	tests := []struct {
		col   grizzly.Column
		name  string
		dtype grizzly.DType
		n     int
	}{
		{grizzly.NewFloat64Column("price", []float64{1, 2, 3}), "price", grizzly.Float64, 3},
		{grizzly.NewStringColumn("city", []string{"a", "b"}), "city", grizzly.String, 2},
		{grizzly.NewBoolColumn("ok", []bool{true}), "ok", grizzly.Bool, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.col.Name(); got != tt.name {
				t.Errorf("Name = %q, want %q", got, tt.name)
			}
			if got := tt.col.DType(); got != tt.dtype {
				t.Errorf("DType = %q, want %q", got, tt.dtype)
			}
			if got := tt.col.Len(); got != tt.n {
				t.Errorf("Len = %d, want %d", got, tt.n)
			}
		})
	}
}
