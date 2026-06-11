package grizzly

// White-box tests for the byte-level JSON parser. The contract: every
// valid document the stdlib path (FromJSONReader) loads, fromJSONBytes
// loads to an identical dataframe — and the stdlib path is also the
// oracle of the fuzz test below. The reverse is deliberately weaker:
// fromJSONBytes may accept some malformed documents the stdlib rejects,
// when the damage hides inside a skipped (non-schema) value.

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

var jsonTestSchema = Schema{
	{Name: "id", Type: Float64},
	{Name: "note", Type: String},
	{Name: "price", Type: Float64},
	{Name: "sold", Type: Bool},
}

// trickyJSON exercises escapes, surrogate pairs, exotic numbers, nulls,
// whitespace and non-schema keys (nested included) in one document.
const trickyJSON = `[
  {"id": 0, "note": "plain", "price": 1.5, "sold": true},
  {"id": 1e2, "note": "esc \" \\ \/ \b \f \n \r \t end", "price": -0.25, "sold": false},
  {"id": -3, "note": "unicode é €", "price": 2e-3, "sold": null},
  {"id": 4.5E+1, "note": "emoji 😀 pair", "price": null, "sold": true},
  {"note": "reordered keys", "sold": false, "price": 0, "id": 5},
  {"id": 6, "note": null, "price": 100, "sold": true, "extra": "ignored"},
  {"id": 7, "note": "nested ignored", "price": 1, "sold": false,
   "meta": {"a": [1, 2, {"b": "}]"}], "c": null}},
  {"id": 8, "note": "", "price": 0.125, "sold": true}
]`

// TestFromJSONBytesMatchesReader is the core equivalence test.
func TestFromJSONBytesMatchesReader(t *testing.T) {
	want, err := FromJSONReader(strings.NewReader(trickyJSON), jsonTestSchema)
	if err != nil {
		t.Fatalf("stdlib path: %v", err)
	}
	got, err := fromJSONBytes([]byte(trickyJSON), jsonTestSchema)
	if err != nil {
		t.Fatalf("byte path: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("byte path differs from stdlib path:\ngot:\n%v\nwant:\n%v", got, want)
	}
}

// TestFromJSONBytesErrors checks the documents BOTH paths must reject —
// and that the byte path rejects them with a useful message.
func TestFromJSONBytesErrors(t *testing.T) {
	tests := []struct {
		name, doc, wantInMsg string
	}{
		{"not an array", `{"id": 1}`, `expected '['`},
		{"missing schema key", `[{"id": 1, "note": "x", "price": 2}]`, "filled 3 of 4"},
		{"string into float", `[{"id": "x", "note": "a", "price": 1, "sold": true}]`, "Float64"},
		{"number into bool", `[{"id": 1, "note": "a", "price": 1, "sold": 3}]`, "Bool"},
		{"nested into column", `[{"id": {"x": 1}, "note": "a", "price": 1, "sold": true}]`, "nested"},
		{"leading zero", `[{"id": 01, "note": "a", "price": 1, "sold": true}]`, "invalid number"},
		{"bare dot fraction", `[{"id": .5, "note": "a", "price": 1, "sold": true}]`, "row 0"},
		{"truncated exponent", `[{"id": 1e, "note": "a", "price": 1, "sold": true}]`, "invalid number"},
		{"unterminated string", `[{"id": 1, "note": "a`, "unterminated"},
		{"control char in string", "[{\"id\": 1, \"note\": \"a\nb\", \"price\": 1, \"sold\": true}]", "control"},
		{"bad escape", `[{"id": 1, "note": "a \x b", "price": 1, "sold": true}]`, "escape"},
		{"missing comma", `[{"id": 1 "note": "a", "price": 1, "sold": true}]`, "expected"},
		{"truncated document", `[{"id": 1, "note": "a", "price": 1, "sold": true}`, "expected"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := FromJSONReader(strings.NewReader(tt.doc), jsonTestSchema); err == nil {
				t.Errorf("stdlib path accepted %q", tt.doc)
			}
			_, err := fromJSONBytes([]byte(tt.doc), jsonTestSchema)
			if err == nil {
				t.Fatalf("byte path accepted %q", tt.doc)
			}
			if !strings.Contains(err.Error(), tt.wantInMsg) {
				t.Errorf("error %q does not mention %q", err, tt.wantInMsg)
			}
		})
	}
}

// TestUnescapeJSON pins the slow path down case by case.
func TestUnescapeJSON(t *testing.T) {
	tests := []struct {
		raw, want string
		wantErr   bool
	}{
		{`plain`, "plain", false},
		{`a\"b`, `a"b`, false},
		{`a\\b`, `a\b`, false},
		{`a\/b`, "a/b", false},
		{`\b\f\n\r\t`, "\b\f\n\r\t", false},
		{`café`, "café", false},
		{`€`, "€", false},
		{`😀`, "😀", false},                  // surrogate pair
		{`\ud83d`, "�", false},             // unpaired high surrogate
		{`\ud83dx`, "�x", false},           // unpaired, then data
		{`\ude00`, "�", false},             // lone low surrogate
		{`ends with backslash\`, "", true}, // truncated escape
		{`\u12`, "", true},                 // truncated \u
		{`\u12zz`, "", true},               // bad hex
		{`\q`, "", true},                   // unknown escape
		{"tab\there", "", true},            // raw control char
	}
	for _, tt := range tests {
		got, err := unescapeJSON([]byte(tt.raw))
		if (err != nil) != tt.wantErr {
			t.Errorf("unescapeJSON(%q): err = %v, wantErr %v", tt.raw, err, tt.wantErr)
			continue
		}
		if err == nil && got != tt.want {
			t.Errorf("unescapeJSON(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

// TestValidJSONNumber pins the strict grammar: everything ParseFloat
// would happily accept but JSON forbids must be rejected.
func TestValidJSONNumber(t *testing.T) {
	valid := []string{"0", "-0", "7", "42", "-13", "0.5", "-0.5", "1.25",
		"1e5", "1E5", "1e+5", "1e-5", "0.5e10", "123.456e-78"}
	invalid := []string{"", "-", ".5", "1.", "01", "+1", "1e", "1e+",
		"0x10", "Inf", "NaN", "1.2.3", "--1", "1..2"}
	for _, s := range valid {
		if !validJSONNumber([]byte(s)) {
			t.Errorf("validJSONNumber(%q) = false, want true", s)
		}
	}
	for _, s := range invalid {
		if validJSONNumber([]byte(s)) {
			t.Errorf("validJSONNumber(%q) = true, want false", s)
		}
	}
}

// FuzzFromJSONBytes uses the stdlib path as the oracle: whatever
// FromJSONReader loads, fromJSONBytes must load identically. The reverse
// is not asserted — the byte path is documented as (slightly) more
// permissive with malformed content inside skipped values.
func FuzzFromJSONBytes(f *testing.F) {
	f.Add([]byte(trickyJSON))
	f.Add([]byte(`[]`))
	f.Add([]byte(`[{"id": 1, "note": "a", "price": 2.5, "sold": true}]`))
	f.Add([]byte(`[{"id": null, "note": null, "price": null, "sold": null}]`))
	f.Add([]byte(`[{"id": 1, "note": "😀", "price": 1e-3, "sold": false, "x": [{}]}]`))
	f.Add([]byte(`[{"id": 0}]`))
	f.Add([]byte(`[{}]`))

	f.Fuzz(func(t *testing.T, data []byte) {
		want, errWant := FromJSONReader(bytes.NewReader(data), jsonTestSchema)
		got, errGot := fromJSONBytes(data, jsonTestSchema)

		if errWant == nil && errGot != nil {
			t.Fatalf("stdlib accepts, byte path rejects: %v\ninput: %q", errGot, data)
		}
		if errWant == nil && errGot == nil && !reflect.DeepEqual(got, want) {
			t.Fatalf("results differ\ninput: %q\nstdlib:\n%v\nbytes:\n%v", data, want, got)
		}
	})
}
