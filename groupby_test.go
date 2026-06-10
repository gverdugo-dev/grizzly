package grizzly_test

// Black-box tests for GroupBy/Agg: the factorize semantics (null keys,
// first-appearance order), the per-group null rules, the As renaming, and
// the deferred-error pattern (GroupBy reports nothing; Agg reports all).

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/gverdugo-dev/grizzly"
)

// stringValues returns the values of a string column as (value, valid)
// pairs flattened into two parallel slices.
func stringValues(t *testing.T, df grizzly.Dataframe, name string) ([]string, []bool) {
	t.Helper()
	c, err := df.Column(name)
	if err != nil {
		t.Fatalf("Column(%q): %v", name, err)
	}
	sc, ok := c.(*grizzly.StringColumn)
	if !ok {
		t.Fatalf("column %q is %T, want *StringColumn", name, c)
	}
	values := make([]string, sc.Len())
	valid := make([]bool, sc.Len())
	for i := range values {
		values[i], valid[i] = sc.Value(i)
	}
	return values, valid
}

// floatValues is stringValues for a float64 column.
func floatValues(t *testing.T, df grizzly.Dataframe, name string) ([]float64, []bool) {
	t.Helper()
	c, err := df.Column(name)
	if err != nil {
		t.Fatalf("Column(%q): %v", name, err)
	}
	fc, ok := c.(*grizzly.Float64Column)
	if !ok {
		t.Fatalf("column %q is %T, want *Float64Column", name, c)
	}
	values := make([]float64, fc.Len())
	valid := make([]bool, fc.Len())
	for i := range values {
		values[i], valid[i] = fc.Value(i)
	}
	return values, valid
}

// TestGroupByAgg covers the basic pipeline: groups in first-appearance
// order, per-group Sum/Avg/Count, and the key column keeping its name.
func TestGroupByAgg(t *testing.T) {
	df, err := grizzly.FromStructs([]struct {
		Store string
		Price float64
	}{
		{"north", 1.5}, {"south", 2.5}, {"north", 0.5}, {"south", 1.5}, {"north", 1.0},
	})
	if err != nil {
		t.Fatalf("FromStructs: %v", err)
	}

	out, err := df.GroupBy("Store").Agg(
		grizzly.Sum("Price"),
		grizzly.Avg("Price").As("avg_price"),
		grizzly.Count("Price").As("n"),
	)
	if err != nil {
		t.Fatalf("Agg: %v", err)
	}

	keys, _ := stringValues(t, out, "Store")
	if want := []string{"north", "south"}; !slices.Equal(keys, want) {
		t.Fatalf("keys = %v, want %v (first-appearance order)", keys, want)
	}

	sums, _ := floatValues(t, out, "Price") // Sum keeps the source name
	if want := []float64{3.0, 4.0}; !slices.Equal(sums, want) {
		t.Errorf("sums = %v, want %v", sums, want)
	}
	avgs, _ := floatValues(t, out, "avg_price")
	if want := []float64{1.0, 2.0}; !slices.Equal(avgs, want) {
		t.Errorf("avgs = %v, want %v", avgs, want)
	}
	counts, _ := floatValues(t, out, "n")
	if want := []float64{3, 2}; !slices.Equal(counts, want) {
		t.Errorf("counts = %v, want %v", counts, want)
	}
}

// TestGroupByDeterministicOrder runs the same pipeline many times and
// demands an identical key order each run. The factorize map cannot
// provide this — Go randomizes map iteration on purpose — so this pins
// the firstRows mechanism: a refactor that iterated the map would fail
// here almost surely within 20 runs.
func TestGroupByDeterministicOrder(t *testing.T) {
	df, err := grizzly.FromStructs([]struct {
		K string
		V float64
	}{
		{"c", 1}, {"a", 1}, {"d", 1}, {"b", 1}, {"a", 1}, {"e", 1}, {"b", 1},
	})
	if err != nil {
		t.Fatalf("FromStructs: %v", err)
	}
	want := []string{"c", "a", "d", "b", "e"} // first-appearance order

	for run := 0; run < 20; run++ {
		out, err := df.GroupBy("K").Agg(grizzly.Sum("V"))
		if err != nil {
			t.Fatalf("run %d: Agg: %v", run, err)
		}
		keys, _ := stringValues(t, out, "K")
		if !slices.Equal(keys, want) {
			t.Fatalf("run %d: keys = %v, want %v", run, keys, want)
		}
	}
}

// TestGroupByNullKeys pins the SQL grouping rule: all null keys form ONE
// group (for grouping, null equals null), loaded here through JSON so the
// nulls arrive the way a user would produce them.
func TestGroupByNullKeys(t *testing.T) {
	src := `[
		{"store": "north", "price": 1.5},
		{"store": null,    "price": 2.0},
		{"store": "north", "price": 0.5},
		{"store": null,    "price": 3.0}
	]`
	schema := grizzly.Schema{
		{Name: "store", Type: grizzly.String},
		{Name: "price", Type: grizzly.Float64},
	}
	df, err := grizzly.FromJSONReader(strings.NewReader(src), schema)
	if err != nil {
		t.Fatalf("FromJSONReader: %v", err)
	}

	out, err := df.GroupBy("store").Agg(grizzly.Sum("price"))
	if err != nil {
		t.Fatalf("Agg: %v", err)
	}
	if got := out.NumRows(); got != 2 {
		t.Fatalf("groups = %d, want 2 (all nulls are ONE group)", got)
	}

	keys, valid := stringValues(t, out, "store")
	if keys[0] != "north" || !valid[0] {
		t.Errorf("group 0 key = (%q, %v), want (north, valid)", keys[0], valid[0])
	}
	if valid[1] {
		t.Errorf("group 1 key valid, want null key group")
	}

	sums, _ := floatValues(t, out, "price")
	if want := []float64{2.0, 5.0}; !slices.Equal(sums, want) {
		t.Errorf("sums = %v, want %v", sums, want)
	}
}

// TestGroupByEmptyStringKey verifies the CSV flip side of the null-key
// rule: an empty string cell is a real value, so "" keys form a normal
// group with a valid (empty) key — distinct from a null group.
func TestGroupByEmptyStringKey(t *testing.T) {
	csv := "store,price\nnorth,1.5\n\"\",2.0\nnorth,0.5\n\"\",3.0\n"
	schema := grizzly.Schema{
		{Name: "store", Type: grizzly.String},
		{Name: "price", Type: grizzly.Float64},
	}
	df, err := grizzly.FromCSVReader(strings.NewReader(csv), schema)
	if err != nil {
		t.Fatalf("FromCSVReader: %v", err)
	}

	out, err := df.GroupBy("store").Agg(grizzly.Sum("price"))
	if err != nil {
		t.Fatalf("Agg: %v", err)
	}
	keys, valid := stringValues(t, out, "store")
	if want := []string{"north", ""}; !slices.Equal(keys, want) {
		t.Fatalf("keys = %v, want %v", keys, want)
	}
	if !valid[1] {
		t.Error("\"\" key group is null, want a VALID empty-string key")
	}
}

// TestGroupByAllNullGroup pins the per-group equivalent of
// ErrNoValidValues: a group whose values are all null yields Sum 0 and
// Count 0 (the sum/count of nothing) but a NULL Avg — a group cannot
// return an error, so it returns null.
func TestGroupByAllNullGroup(t *testing.T) {
	src := `[
		{"store": "north", "price": 2.5},
		{"store": "ghost", "price": null},
		{"store": "ghost", "price": null}
	]`
	schema := grizzly.Schema{
		{Name: "store", Type: grizzly.String},
		{Name: "price", Type: grizzly.Float64},
	}
	df, err := grizzly.FromJSONReader(strings.NewReader(src), schema)
	if err != nil {
		t.Fatalf("FromJSONReader: %v", err)
	}

	out, err := df.GroupBy("store").Agg(
		grizzly.Sum("price"),
		grizzly.Avg("price").As("avg"),
		grizzly.Count("price").As("n"),
	)
	if err != nil {
		t.Fatalf("Agg: %v", err)
	}

	sums, sumsValid := floatValues(t, out, "price")
	if sums[1] != 0 || !sumsValid[1] {
		t.Errorf("ghost Sum = (%v, %v), want (0, valid)", sums[1], sumsValid[1])
	}
	_, avgValid := floatValues(t, out, "avg")
	if avgValid[1] {
		t.Error("ghost Avg is valid, want null (no valid values in group)")
	}
	counts, _ := floatValues(t, out, "n")
	if counts[1] != 0 {
		t.Errorf("ghost Count = %v, want 0", counts[1])
	}
}

// TestGroupByBoolKey exercises the dedicated bool factorizer: at most
// {false, true, null} groups, no hash table involved.
func TestGroupByBoolKey(t *testing.T) {
	df, err := grizzly.FromStructs([]struct {
		Sold  bool
		Price float64
	}{
		{true, 1.5}, {false, 2.0}, {true, 0.5},
	})
	if err != nil {
		t.Fatalf("FromStructs: %v", err)
	}
	out, err := df.GroupBy("Sold").Agg(grizzly.Sum("Price"))
	if err != nil {
		t.Fatalf("Agg: %v", err)
	}
	if got := out.NumRows(); got != 2 {
		t.Fatalf("groups = %d, want 2", got)
	}
	sums, _ := floatValues(t, out, "Price")
	if want := []float64{2.0, 2.0}; !slices.Equal(sums, want) { // true first (first appearance)
		t.Errorf("sums = %v, want %v", sums, want)
	}
}

// TestGroupByDeferredErrors pins the deferred-error pattern: GroupBy
// itself returns no error so the chain compiles; every failure mode
// surfaces at Agg, with the usual sentinels where applicable.
func TestGroupByDeferredErrors(t *testing.T) {
	df, err := grizzly.FromStructs([]struct {
		Store string
		Price float64
	}{{"north", 1.5}})
	if err != nil {
		t.Fatalf("FromStructs: %v", err)
	}

	t.Run("unknown key column", func(t *testing.T) {
		_, err := df.GroupBy("nope").Agg(grizzly.Sum("Price"))
		if !errors.Is(err, grizzly.ErrColumnNotFound) {
			t.Errorf("err = %v, want ErrColumnNotFound", err)
		}
	})
	t.Run("multiple key columns", func(t *testing.T) {
		if _, err := df.GroupBy("Store", "Price").Agg(grizzly.Sum("Price")); err == nil {
			t.Error("expected error for two key columns, got nil")
		}
	})
	t.Run("no specs", func(t *testing.T) {
		if _, err := df.GroupBy("Store").Agg(); err == nil {
			t.Error("expected error for zero aggregations, got nil")
		}
	})
	t.Run("unknown agg column", func(t *testing.T) {
		_, err := df.GroupBy("Store").Agg(grizzly.Sum("nope"))
		if !errors.Is(err, grizzly.ErrColumnNotFound) {
			t.Errorf("err = %v, want ErrColumnNotFound", err)
		}
	})
	t.Run("sum over string column", func(t *testing.T) {
		_, err := df.GroupBy("Store").Agg(grizzly.Sum("Store"))
		if !errors.Is(err, grizzly.ErrTypeMismatch) {
			t.Errorf("err = %v, want ErrTypeMismatch", err)
		}
	})
	t.Run("duplicate output names", func(t *testing.T) {
		// Two aggregations of the same column without As collide on the
		// source name.
		if _, err := df.GroupBy("Store").Agg(grizzly.Sum("Price"), grizzly.Avg("Price")); err == nil {
			t.Error("expected duplicate-name error, got nil")
		}
	})
	t.Run("count over any type is fine", func(t *testing.T) {
		if _, err := df.GroupBy("Store").Agg(grizzly.Count("Store").As("n")); err != nil {
			t.Errorf("Count over string column: %v", err)
		}
	})
}
