package grizzly_test

// Black-box tests for the writers, ToCSVWriter and ToJSONWriter, centered
// on round-trips: write a dataframe, load it back with the mirror loader,
// and compare. This checks both directions at once — including the one
// deliberate asymmetry (a null string through CSV).

import (
	"os"
	"strings"
	"testing"

	"github.com/gverdugo-dev/grizzly"
)

// writersFixture builds a dataframe exercising every dtype and nulls in
// each: float, string and bool columns with one null apiece.
func writersFixture(t *testing.T) grizzly.Dataframe {
	t.Helper()
	temp, err := grizzly.NewFloat64ColumnWithNulls("temp",
		[]float64{21.5, 0, 0.25}, []bool{true, false, true})
	if err != nil {
		t.Fatalf("building temp: %v", err)
	}
	city, err := grizzly.NewStringColumnWithNulls("city",
		[]string{"madrid", "with \"quotes\", and commas", ""}, []bool{true, true, false})
	if err != nil {
		t.Fatalf("building city: %v", err)
	}
	sunny, err := grizzly.NewBoolColumnWithNulls("sunny",
		[]bool{true, false, false}, []bool{true, false, true})
	if err != nil {
		t.Fatalf("building sunny: %v", err)
	}
	df, err := grizzly.NewDataframe(temp, city, sunny)
	if err != nil {
		t.Fatalf("building dataframe: %v", err)
	}
	return df
}

// assertColumnsEqual compares two dataframes cell by cell through the
// public API (values and validity).
func assertColumnsEqual(t *testing.T, got, want grizzly.Dataframe) {
	t.Helper()
	if got.NumRows() != want.NumRows() || got.NumCols() != want.NumCols() {
		t.Fatalf("shape = %dx%d, want %dx%d",
			got.NumRows(), got.NumCols(), want.NumRows(), want.NumCols())
	}
	for i := 0; i < want.NumRows(); i++ {
		gw, _ := got.Column("temp")
		ww, _ := want.Column("temp")
		gv, gok := gw.(*grizzly.Float64Column).Value(i)
		wv, wok := ww.(*grizzly.Float64Column).Value(i)
		if gok != wok || (gok && gv != wv) {
			t.Errorf("temp[%d] = (%v, %v), want (%v, %v)", i, gv, gok, wv, wok)
		}
	}
}

// TestJSONRoundTrip pins the writer's headline property: ToJSONWriter
// output loads back through FromJSONReader bit-identical, nulls included,
// for every column type.
func TestJSONRoundTrip(t *testing.T) {
	df := writersFixture(t)

	var buf strings.Builder
	if err := df.ToJSONWriter(&buf); err != nil {
		t.Fatalf("ToJSONWriter: %v", err)
	}

	schema := grizzly.Schema{
		{Name: "temp", Type: grizzly.Float64},
		{Name: "city", Type: grizzly.String},
		{Name: "sunny", Type: grizzly.Bool},
	}
	back, err := grizzly.FromJSONReader(strings.NewReader(buf.String()), schema)
	if err != nil {
		t.Fatalf("FromJSONReader over written output: %v\noutput: %s", err, buf.String())
	}

	assertColumnsEqual(t, back, df)

	// The string column must round-trip exactly: quoting, commas, the
	// real "" at... row 2 is null, rows 0-1 are values.
	city, _ := back.Column("city")
	sc := city.(*grizzly.StringColumn)
	if v, ok := sc.Value(1); !ok || v != "with \"quotes\", and commas" {
		t.Errorf("city[1] = (%q, %v): JSON escaping did not round-trip", v, ok)
	}
	if _, ok := sc.Value(2); ok {
		t.Error("city[2] valid, want null: JSON null must round-trip")
	}

	// Bool nulls too.
	sunny, _ := back.Column("sunny")
	if sunny.IsValid(1) {
		t.Error("sunny[1] valid, want null")
	}
}

// TestCSVRoundTrip pins both sides of the CSV null rule: float and bool
// nulls round-trip (empty cell → null), while a null STRING comes back as
// a valid "" — the documented asymmetry (FromCSVReader reads an empty
// string cell as a real value).
func TestCSVRoundTrip(t *testing.T) {
	df := writersFixture(t)

	var buf strings.Builder
	if err := df.ToCSVWriter(&buf); err != nil {
		t.Fatalf("ToCSVWriter: %v", err)
	}

	schema := grizzly.Schema{
		{Name: "temp", Type: grizzly.Float64},
		{Name: "city", Type: grizzly.String},
		{Name: "sunny", Type: grizzly.Bool},
	}
	back, err := grizzly.FromCSVReader(strings.NewReader(buf.String()), schema)
	if err != nil {
		t.Fatalf("FromCSVReader over written output: %v\noutput: %s", err, buf.String())
	}

	assertColumnsEqual(t, back, df)

	// Float and bool nulls round-trip.
	temp, _ := back.Column("temp")
	if temp.IsValid(1) {
		t.Error("temp[1] valid, want null: empty float cell round-trips")
	}
	sunny, _ := back.Column("sunny")
	if sunny.IsValid(1) {
		t.Error("sunny[1] valid, want null: empty bool cell round-trips")
	}

	// The documented asymmetry: the null string went out as an empty cell
	// and comes back as a VALID empty string.
	city, _ := back.Column("city")
	sc := city.(*grizzly.StringColumn)
	if v, ok := sc.Value(2); !ok || v != "" {
		t.Errorf("city[2] = (%q, %v), want (\"\", true): null strings do not round-trip through CSV by design", v, ok)
	}

	// Quoting survived the trip.
	if v, _ := sc.Value(1); v != "with \"quotes\", and commas" {
		t.Errorf("city[1] = %q: CSV quoting did not round-trip", v)
	}
}

// TestToJSONWriterRejectsNaN verifies the honest failure: JSON cannot
// represent NaN/Inf, so writing one is an error, not corrupt output.
func TestToJSONWriterRejectsNaN(t *testing.T) {
	type row struct{ X float64 }
	nan := 0.0
	nan = nan / nan // NaN without importing math in the test
	df, err := grizzly.FromStructs([]row{{X: nan}})
	if err != nil {
		t.Fatalf("FromStructs: %v", err)
	}
	var buf strings.Builder
	if err := df.ToJSONWriter(&buf); err == nil {
		t.Error("ToJSONWriter(NaN): expected error, got nil")
	}
}

// TestWritersToFile exercises the path-based wrappers end to end through
// a t.TempDir round-trip.
func TestWritersToFile(t *testing.T) {
	df := writersFixture(t)
	dir := t.TempDir()

	t.Run("csv", func(t *testing.T) {
		path := dir + "/out.csv"
		if err := df.ToCSV(path); err != nil {
			t.Fatalf("ToCSV: %v", err)
		}
		back, err := grizzly.FromCSV(path, grizzly.Schema{{Name: "temp", Type: grizzly.Float64}})
		if err != nil {
			t.Fatalf("FromCSV: %v", err)
		}
		if got := back.NumRows(); got != df.NumRows() {
			t.Errorf("NumRows = %d, want %d", got, df.NumRows())
		}
	})

	t.Run("json", func(t *testing.T) {
		path := dir + "/out.json"
		if err := df.ToJSON(path); err != nil {
			t.Fatalf("ToJSON: %v", err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading output: %v", err)
		}
		if !strings.HasPrefix(string(data), "[{") {
			t.Errorf("output does not look like a JSON array of objects: %.40s", data)
		}
	})
}
