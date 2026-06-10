package grizzly_test

// Black-box tests for Sort and SortDesc: nulls-first in both directions,
// stability over equal keys, and the immutability of the source dataframe
// (sortBy gathers into fresh columns; the original must not move).

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/gverdugo-dev/grizzly"
)

// sortFixture builds a dataframe with nulls and duplicate keys:
//
//	id    temp
//	r0    2.5
//	r1    null
//	r2    0.5
//	r3    null
//	r4    2.5
func sortFixture(t *testing.T) grizzly.Dataframe {
	t.Helper()
	temp, err := grizzly.NewFloat64ColumnWithNulls("temp",
		[]float64{2.5, 0, 0.5, 0, 2.5}, []bool{true, false, true, false, true})
	if err != nil {
		t.Fatalf("building temp: %v", err)
	}
	id := grizzly.NewStringColumn("id", []string{"r0", "r1", "r2", "r3", "r4"})
	df, err := grizzly.NewDataframe(id, temp)
	if err != nil {
		t.Fatalf("building dataframe: %v", err)
	}
	return df
}

// TestSortNullsFirst pins the chosen null rule: nulls group at the top in
// BOTH directions (polars' rule), and stability keeps equal keys — the
// two nulls and the two 2.5s — in their original relative order.
func TestSortNullsFirst(t *testing.T) {
	df := sortFixture(t)

	t.Run("ascending", func(t *testing.T) {
		out, err := df.Sort("temp")
		if err != nil {
			t.Fatalf("Sort: %v", err)
		}
		ids, _ := stringValues(t, out, "id")
		// nulls first (r1 before r3: stability), then 0.5, then the 2.5s
		// in original order (r0 before r4: stability again).
		if want := []string{"r1", "r3", "r2", "r0", "r4"}; !slices.Equal(ids, want) {
			t.Errorf("ids = %v, want %v", ids, want)
		}
	})

	t.Run("descending", func(t *testing.T) {
		out, err := df.SortDesc("temp")
		if err != nil {
			t.Fatalf("SortDesc: %v", err)
		}
		ids, _ := stringValues(t, out, "id")
		// nulls STILL first, then 2.5s, then 0.5.
		if want := []string{"r1", "r3", "r0", "r4", "r2"}; !slices.Equal(ids, want) {
			t.Errorf("ids = %v, want %v", ids, want)
		}
	})
}

// TestSortLeavesOriginalUntouched verifies Sort returns a new dataframe:
// the source's row order must survive the sort (columnar engines gather
// into fresh buffers; nothing moves in place).
func TestSortLeavesOriginalUntouched(t *testing.T) {
	df := sortFixture(t)
	if _, err := df.Sort("temp"); err != nil {
		t.Fatalf("Sort: %v", err)
	}

	ids, _ := stringValues(t, df, "id")
	if want := []string{"r0", "r1", "r2", "r3", "r4"}; !slices.Equal(ids, want) {
		t.Errorf("original ids = %v, want %v (Sort mutated its source)", ids, want)
	}
	temps, valid := floatValues(t, df, "temp")
	if temps[0] != 2.5 || valid[1] {
		t.Errorf("original temp column changed: values %v valid %v", temps, valid)
	}
}

// TestSortByStringAndBool covers the other key types: lexicographic
// strings and false-before-true bools, each with a null sorting first.
func TestSortByStringAndBool(t *testing.T) {
	t.Run("string key", func(t *testing.T) {
		src := `[
			{"city": "sevilla"},
			{"city": null},
			{"city": "bilbao"},
			{"city": "madrid"}
		]`
		df, err := grizzly.FromJSONReader(strings.NewReader(src),
			grizzly.Schema{{Name: "city", Type: grizzly.String}})
		if err != nil {
			t.Fatalf("FromJSONReader: %v", err)
		}
		out, err := df.Sort("city")
		if err != nil {
			t.Fatalf("Sort: %v", err)
		}
		cities, valid := stringValues(t, out, "city")
		if valid[0] {
			t.Errorf("row 0 = (%q, valid), want the null first", cities[0])
		}
		if want := []string{"bilbao", "madrid", "sevilla"}; !slices.Equal(cities[1:], want) {
			t.Errorf("cities[1:] = %v, want %v", cities[1:], want)
		}
	})

	t.Run("bool key", func(t *testing.T) {
		df, err := grizzly.FromStructs([]struct {
			Sold bool
			ID   float64
		}{
			{true, 1}, {false, 2}, {true, 3},
		})
		if err != nil {
			t.Fatalf("FromStructs: %v", err)
		}
		out, err := df.Sort("Sold")
		if err != nil {
			t.Fatalf("Sort: %v", err)
		}
		ids, _ := floatValues(t, out, "ID")
		// false first ascending; the two trues keep their order (stability).
		if want := []float64{2, 1, 3}; !slices.Equal(ids, want) {
			t.Errorf("ids = %v, want %v", ids, want)
		}
	})
}

// TestSortErrors covers the rejection path: an unknown column reports
// ErrColumnNotFound.
func TestSortErrors(t *testing.T) {
	df := sortFixture(t)
	if _, err := df.Sort("nope"); !errors.Is(err, grizzly.ErrColumnNotFound) {
		t.Errorf("Sort(nope) err = %v, want ErrColumnNotFound", err)
	}
}
