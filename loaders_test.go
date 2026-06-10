package grizzly_test

// Black-box tests for the three row-oriented loaders: FromStructs,
// FromCSVReader and FromJSONReader. The Reader variants are fed from
// strings.NewReader — no temp files needed, which is exactly why the
// loaders accept io.Reader instead of a path.

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/gverdugo-dev/grizzly"
)

// floatValue fetches the float64 column and returns Value(i), failing the
// test on any error. Keeps the null-rule tests focused on the assertions.
func floatValue(t *testing.T, df grizzly.Dataframe, col string, i int) (float64, bool) {
	t.Helper()
	c, err := df.Column(col)
	if err != nil {
		t.Fatalf("Column(%q): %v", col, err)
	}
	fc, ok := c.(*grizzly.Float64Column)
	if !ok {
		t.Fatalf("column %q is %T, want *Float64Column", col, c)
	}
	return fc.Value(i)
}

// TestFromStructs covers the happy path: exported fields become columns
// (renamed by the grizzly tag), unexported fields are skipped, and bool
// fields survive the reflection round-trip.
func TestFromStructs(t *testing.T) {
	type sale struct {
		Product string `grizzly:"product"`
		Price   float64
		Sold    bool
		hidden  int // unexported: must be skipped, not an error
	}
	df, err := grizzly.FromStructs([]sale{
		{Product: "apple", Price: 1.5, Sold: true, hidden: 1},
		{Product: "pear", Price: 2.5, Sold: false, hidden: 2},
	})
	if err != nil {
		t.Fatalf("FromStructs: %v", err)
	}

	if got, want := df.NumCols(), 3; got != want {
		t.Errorf("NumCols = %d, want %d (unexported field must be skipped)", got, want)
	}
	if got, want := df.NumRows(), 2; got != want {
		t.Errorf("NumRows = %d, want %d", got, want)
	}

	// The tag renames Product; the untagged fields keep their Go names.
	if _, err := df.Column("product"); err != nil {
		t.Errorf("tagged column %q missing: %v", "product", err)
	}
	if _, err := df.Column("Price"); err != nil {
		t.Errorf("untagged column %q missing: %v", "Price", err)
	}

	// Bool through reflection: row 0 true, row 1 false, no nulls.
	c, err := df.Column("Sold")
	if err != nil {
		t.Fatalf("Column(Sold): %v", err)
	}
	bc, ok := c.(*grizzly.BoolColumn)
	if !ok {
		t.Fatalf("Sold is %T, want *BoolColumn", c)
	}
	if v, ok := bc.Value(0); !ok || v != true {
		t.Errorf("Sold[0] = (%v, %v), want (true, true)", v, ok)
	}
	if v, ok := bc.Value(1); !ok || v != false {
		t.Errorf("Sold[1] = (%v, %v), want (false, true)", v, ok)
	}
}

// TestFromStructsErrors pins the two explicit-over-magic rejections:
// a non-struct row type and a field of an unsupported type.
func TestFromStructsErrors(t *testing.T) {
	t.Run("non-struct row type", func(t *testing.T) {
		if _, err := grizzly.FromStructs([]int{1, 2}); err == nil {
			t.Error("expected error for []int, got nil")
		}
	})
	t.Run("unsupported field type", func(t *testing.T) {
		type bad struct{ N int } // int is not in the closed DType set
		if _, err := grizzly.FromStructs([]bad{{N: 1}}); err == nil {
			t.Error("expected error for int field, got nil")
		}
	})
}

// TestFromCSVNullRules pins the CSV null rules: an empty cell is a null in
// a Float64 column but a REAL empty string in a String column, and a
// literal "0.0" is a real zero, not a null.
func TestFromCSVNullRules(t *testing.T) {
	csv := "city,temp\nmadrid,0.0\nbilbao,\n,5.5\n"
	schema := grizzly.Schema{
		{Name: "city", Type: grizzly.String},
		{Name: "temp", Type: grizzly.Float64},
	}
	df, err := grizzly.FromCSVReader(strings.NewReader(csv), schema)
	if err != nil {
		t.Fatalf("FromCSVReader: %v", err)
	}

	// "0.0" is a real zero: valid, value 0 — distinguishable from null.
	if v, ok := floatValue(t, df, "temp", 0); !ok || v != 0 {
		t.Errorf("temp[0] = (%v, %v), want (0, true): literal 0.0 is a value", v, ok)
	}
	// Empty float cell is a null.
	if _, ok := floatValue(t, df, "temp", 1); ok {
		t.Error("temp[1] ok = true, want false: empty float cell is null")
	}

	// Empty string cell stays a real "" (deliberately unlike pandas).
	c, err := df.Column("city")
	if err != nil {
		t.Fatalf("Column(city): %v", err)
	}
	if got := c.NullCount(); got != 0 {
		t.Errorf("city NullCount = %d, want 0: empty CSV string cells are values", got)
	}
	sc := c.(*grizzly.StringColumn)
	if v, ok := sc.Value(2); !ok || v != "" {
		t.Errorf("city[2] = (%q, %v), want (\"\", true)", v, ok)
	}
}

// TestFromCSVParseBool feeds every common ParseBool casing plus an empty
// cell (null) and checks the decoded column.
//
// The empty cell is written as a quoted "" on its own line: encoding/csv
// skips fully blank lines (they are record separators, not records), so a
// bare empty line would not produce a row at all.
func TestFromCSVParseBool(t *testing.T) {
	csv := "ok\ntrue\nTRUE\nTrue\nt\n1\nfalse\nFALSE\nf\n0\n\"\"\n"
	schema := grizzly.Schema{{Name: "ok", Type: grizzly.Bool}}
	df, err := grizzly.FromCSVReader(strings.NewReader(csv), schema)
	if err != nil {
		t.Fatalf("FromCSVReader: %v", err)
	}
	c, _ := df.Column("ok")
	bc := c.(*grizzly.BoolColumn)

	want := []bool{true, true, true, true, true, false, false, false, false}
	for i, w := range want {
		if v, ok := bc.Value(i); !ok || v != w {
			t.Errorf("ok[%d] = (%v, %v), want (%v, true)", i, v, ok, w)
		}
	}
	// The trailing empty cell is a null.
	if _, ok := bc.Value(len(want)); ok {
		t.Error("empty bool cell decoded as valid, want null")
	}
}

// TestFromCSVErrors covers the rejection paths: an unparseable cell and a
// schema column missing from the header (which must be ErrColumnNotFound,
// checked with errors.Is so wrapping is allowed to change).
func TestFromCSVErrors(t *testing.T) {
	t.Run("unparseable float cell", func(t *testing.T) {
		csv := "temp\nnot-a-number\n"
		schema := grizzly.Schema{{Name: "temp", Type: grizzly.Float64}}
		if _, err := grizzly.FromCSVReader(strings.NewReader(csv), schema); err == nil {
			t.Error("expected error for unparseable cell, got nil")
		}
	})
	t.Run("schema column missing from header", func(t *testing.T) {
		csv := "a\n1\n"
		schema := grizzly.Schema{{Name: "missing", Type: grizzly.Float64}}
		_, err := grizzly.FromCSVReader(strings.NewReader(csv), schema)
		if !errors.Is(err, grizzly.ErrColumnNotFound) {
			t.Errorf("err = %v, want ErrColumnNotFound", err)
		}
	})
}

// TestFromCSVSchemaSelectsAndOrders verifies that the schema, not the
// stream, decides which columns load and in what order.
func TestFromCSVSchemaSelectsAndOrders(t *testing.T) {
	csv := "a,b,c\n1,x,2\n"
	schema := grizzly.Schema{
		{Name: "c", Type: grizzly.Float64}, // reversed vs the stream
		{Name: "a", Type: grizzly.Float64}, // and b is ignored
	}
	df, err := grizzly.FromCSVReader(strings.NewReader(csv), schema)
	if err != nil {
		t.Fatalf("FromCSVReader: %v", err)
	}
	if got := df.NumCols(); got != 2 {
		t.Fatalf("NumCols = %d, want 2 (column b must be ignored)", got)
	}
	if _, err := df.Column("b"); !errors.Is(err, grizzly.ErrColumnNotFound) {
		t.Errorf("Column(b) err = %v, want ErrColumnNotFound", err)
	}
	if v, ok := floatValue(t, df, "c", 0); !ok || v != 2 {
		t.Errorf("c[0] = (%v, %v), want (2, true)", v, ok)
	}
}

// TestFromJSONNulls pins the JSON null rule: a literal null is data (a
// null row), in every column type.
func TestFromJSONNulls(t *testing.T) {
	src := `[
		{"city": "madrid", "temp": 0.5, "sunny": true},
		{"city": null, "temp": null, "sunny": null}
	]`
	schema := grizzly.Schema{
		{Name: "city", Type: grizzly.String},
		{Name: "temp", Type: grizzly.Float64},
		{Name: "sunny", Type: grizzly.Bool},
	}
	df, err := grizzly.FromJSONReader(strings.NewReader(src), schema)
	if err != nil {
		t.Fatalf("FromJSONReader: %v", err)
	}

	for _, name := range []string{"city", "temp", "sunny"} {
		c, err := df.Column(name)
		if err != nil {
			t.Fatalf("Column(%q): %v", name, err)
		}
		if !c.IsValid(0) {
			t.Errorf("%s[0] invalid, want valid", name)
		}
		if c.IsValid(1) {
			t.Errorf("%s[1] valid, want null", name)
		}
	}
}

// TestFromJSONNullThenValue regression-tests the decoder's re-armed
// pointer: after a null leaves the *T nil, the next row's real value must
// still decode (a stale nil pointer would lose it).
func TestFromJSONNullThenValue(t *testing.T) {
	src := `[{"x": null}, {"x": 2.5}]`
	schema := grizzly.Schema{{Name: "x", Type: grizzly.Float64}}
	df, err := grizzly.FromJSONReader(strings.NewReader(src), schema)
	if err != nil {
		t.Fatalf("FromJSONReader: %v", err)
	}
	if v, ok := floatValue(t, df, "x", 1); !ok || v != 2.5 {
		t.Errorf("x[1] = (%v, %v), want (2.5, true)", v, ok)
	}
}

// TestFromJSONErrors covers the malformed-row rejections: a schema key
// missing from a row (absent key != literal null) and a value of the
// wrong JSON type.
func TestFromJSONErrors(t *testing.T) {
	schema := grizzly.Schema{
		{Name: "city", Type: grizzly.String},
		{Name: "temp", Type: grizzly.Float64},
	}
	tests := []struct {
		name string
		src  string
	}{
		{"missing schema key", `[{"city": "madrid"}]`},
		{"wrong value type", `[{"city": "madrid", "temp": "hot"}]`},
		{"not an array", `{"city": "madrid"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := grizzly.FromJSONReader(strings.NewReader(tt.src), schema); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

// TestFromJSONIgnoresExtraKeys verifies that keys outside the schema are
// skipped, including nested values that span several tokens.
func TestFromJSONIgnoresExtraKeys(t *testing.T) {
	src := `[{"x": 1.5, "extra": {"nested": [1, 2, {"deep": true}]}}]`
	schema := grizzly.Schema{{Name: "x", Type: grizzly.Float64}}
	df, err := grizzly.FromJSONReader(strings.NewReader(src), schema)
	if err != nil {
		t.Fatalf("FromJSONReader: %v", err)
	}
	if v, ok := floatValue(t, df, "x", 0); !ok || v != 1.5 {
		t.Errorf("x[0] = (%v, %v), want (1.5, true)", v, ok)
	}
}

// TestFromCSVFile exercises the path-based wrapper once, end to end, using
// t.TempDir — automatically cleaned up when the test finishes.
func TestFromCSVFile(t *testing.T) {
	path := t.TempDir() + "/data.csv"
	if err := os.WriteFile(path, []byte("x\n1.5\n"), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	df, err := grizzly.FromCSV(path, grizzly.Schema{{Name: "x", Type: grizzly.Float64}})
	if err != nil {
		t.Fatalf("FromCSV: %v", err)
	}
	if got := df.NumRows(); got != 1 {
		t.Errorf("NumRows = %d, want 1", got)
	}
}
