package grizzly_test

// Black-box tests for the dataframe-level aggregations: Sum, Avg, Min,
// Max, Count. The interesting behavior is null handling (SQL aggregate
// semantics: skip nulls everywhere) and the sentinel errors, asserted
// with errors.Is so message wording and wrapping stay free to change.

import (
	"errors"
	"testing"

	"github.com/gverdugo-dev/grizzly"
)

// aggFixture builds a dataframe with one float column [1.5, null, 2.5, null],
// one all-null float column, and one string column.
func aggFixture(t *testing.T) grizzly.Dataframe {
	t.Helper()
	temp, err := grizzly.NewFloat64ColumnWithNulls("temp",
		[]float64{1.5, 0, 2.5, 0}, []bool{true, false, true, false})
	if err != nil {
		t.Fatalf("building temp: %v", err)
	}
	void, err := grizzly.NewFloat64ColumnWithNulls("void",
		[]float64{0, 0, 0, 0}, []bool{false, false, false, false})
	if err != nil {
		t.Fatalf("building void: %v", err)
	}
	city := grizzly.NewStringColumn("city", []string{"a", "b", "c", "d"})
	df, err := grizzly.NewDataframe(temp, void, city)
	if err != nil {
		t.Fatalf("building dataframe: %v", err)
	}
	return df
}

// TestAggSkipNulls pins the SQL semantics on a column with nulls:
// Sum adds only valid values, Count counts only valid rows, and Avg
// divides by the valid count (not the row count).
func TestAggSkipNulls(t *testing.T) {
	df := aggFixture(t)

	if got, err := df.Sum("temp"); err != nil || got != 4.0 {
		t.Errorf("Sum = (%v, %v), want (4, nil)", got, err)
	}
	if got, err := df.Count("temp"); err != nil || got != 2 {
		t.Errorf("Count = (%v, %v), want (2, nil)", got, err)
	}
	// 4.0 / 2 valid rows = 2.0; dividing by 4 total rows would give 1.0.
	if got, err := df.Avg("temp"); err != nil || got != 2.0 {
		t.Errorf("Avg = (%v, %v), want (2, nil)", got, err)
	}
	if got, err := df.Min("temp"); err != nil || got != 1.5 {
		t.Errorf("Min = (%v, %v), want (1.5, nil)", got, err)
	}
	if got, err := df.Max("temp"); err != nil || got != 2.5 {
		t.Errorf("Max = (%v, %v), want (2.5, nil)", got, err)
	}
}

// TestAggAllNull pins the all-null column behavior: Sum is 0 and Count
// is 0 (the sum/count of nothing exist), while Avg, Min and Max return
// ErrNoValidValues (they have no honest answer).
func TestAggAllNull(t *testing.T) {
	df := aggFixture(t)

	if got, err := df.Sum("void"); err != nil || got != 0 {
		t.Errorf("Sum(void) = (%v, %v), want (0, nil)", got, err)
	}
	if got, err := df.Count("void"); err != nil || got != 0 {
		t.Errorf("Count(void) = (%v, %v), want (0, nil)", got, err)
	}
	for _, agg := range []struct {
		name string
		fn   func(string) (float64, error)
	}{
		{"Avg", df.Avg}, {"Min", df.Min}, {"Max", df.Max},
	} {
		if _, err := agg.fn("void"); !errors.Is(err, grizzly.ErrNoValidValues) {
			t.Errorf("%s(void) err = %v, want ErrNoValidValues", agg.name, err)
		}
	}
}

// TestAggTypeMismatch verifies that numeric aggregations reject a string
// column with ErrTypeMismatch — while Count, which is type-agnostic,
// accepts it.
func TestAggTypeMismatch(t *testing.T) {
	df := aggFixture(t)

	for _, agg := range []struct {
		name string
		fn   func(string) (float64, error)
	}{
		{"Sum", df.Sum}, {"Avg", df.Avg}, {"Min", df.Min}, {"Max", df.Max},
	} {
		if _, err := agg.fn("city"); !errors.Is(err, grizzly.ErrTypeMismatch) {
			t.Errorf("%s(city) err = %v, want ErrTypeMismatch", agg.name, err)
		}
	}

	if got, err := df.Count("city"); err != nil || got != 4 {
		t.Errorf("Count(city) = (%v, %v), want (4, nil): Count is type-agnostic", got, err)
	}
}

// TestAggColumnNotFound verifies every aggregation reports a missing
// column with the ErrColumnNotFound sentinel.
func TestAggColumnNotFound(t *testing.T) {
	df := aggFixture(t)

	if _, err := df.Sum("nope"); !errors.Is(err, grizzly.ErrColumnNotFound) {
		t.Errorf("Sum(nope) err = %v, want ErrColumnNotFound", err)
	}
	if _, err := df.Count("nope"); !errors.Is(err, grizzly.ErrColumnNotFound) {
		t.Errorf("Count(nope) err = %v, want ErrColumnNotFound", err)
	}
}
