package grizzly

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// FromJSON builds a Dataframe from a JSON file containing an array of
// objects (one object = one row). It opens the file and delegates to
// FromJSONReader; see that function for the format, schema and null rules.
func FromJSON(path string, schema Schema) (Dataframe, error) {
	f, err := os.Open(path)
	if err != nil {
		return Dataframe{}, fmt.Errorf("FromJSON: %w", err)
	}
	defer f.Close()

	df, err := FromJSONReader(bufio.NewReaderSize(f, readerBufSize), schema)
	if err != nil {
		return Dataframe{}, fmt.Errorf("FromJSON %s: %w", path, err)
	}
	logger.Debug("dataframe loaded from json", "path", path, "rows", df.NumRows())
	return df, nil
}

// FromJSONReader builds a Dataframe from a stream containing a JSON array
// of objects (one object = one row), using the given schema to decide each
// column's type and the column order — JSON objects are unordered, so the
// stream cannot provide an order even if it wanted to.
//
// It decodes at token level, streaming: rows are never materialized as
// map[string]any. The first benchmark showed that intermediate layout
// (1M heap-allocated maps, 10M values boxed in any) made JSON ~6x slower
// than our own CSV path. Values are decoded through a reused per-column
// pointer, so they go stream → typed slice with no per-value allocation.
//
// A JSON null becomes a null row in that column (validity bit 0). Schema
// columns missing from a row, or holding a value of the wrong JSON type,
// are still errors: explicit over magic — an absent key is a malformed
// row, a literal null is data.
func FromJSONReader(r io.Reader, schema Schema) (Dataframe, error) {
	dec := json.NewDecoder(r)

	// One builder per schema field (see column_builder.go), reachable by
	// key for the per-row loop and in schema order for the final build.
	// All type decisions happen here, outside the per-row loop.
	builders := make([]columnBuilder, len(schema))
	byKey := make(map[string]columnBuilder, len(schema))
	for j, field := range schema {
		b, err := newColumnBuilder(field, 0) // streaming: row count unknown
		if err != nil {
			return Dataframe{}, err
		}
		builders[j] = b
		byKey[field.Name] = b
	}

	// Walk the tokens: '[' then one '{...}' per row.
	if err := expectDelim(dec, '['); err != nil {
		return Dataframe{}, err
	}
	row := 0
	for dec.More() {
		if err := expectDelim(dec, '{'); err != nil {
			return Dataframe{}, fmt.Errorf("row %d: %w", row, err)
		}
		filled := 0
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return Dataframe{}, fmt.Errorf("row %d: %w", row, err)
			}
			key, _ := keyTok.(string) // inside an object, keys are always strings
			b, ok := byKey[key]
			if !ok {
				// Key not in the schema: skip its value (it may be nested,
				// so let Decode consume it whole).
				var skip json.RawMessage
				if err := dec.Decode(&skip); err != nil {
					return Dataframe{}, fmt.Errorf("row %d, key %q: %w", row, key, err)
				}
				continue
			}
			if err := b.appendJSON(dec, row); err != nil {
				return Dataframe{}, err
			}
			filled++
		}
		if _, err := dec.Token(); err != nil { // consume the closing '}'
			return Dataframe{}, fmt.Errorf("row %d: %w", row, err)
		}
		// Catches missing keys (and duplicates, which also desync lengths —
		// NewDataframe double-checks those at the end).
		if filled != len(schema) {
			return Dataframe{}, fmt.Errorf("row %d: filled %d of %d schema columns",
				row, filled, len(schema))
		}
		row++
	}

	cols, err := finishColumns(builders)
	if err != nil {
		return Dataframe{}, err
	}
	return NewDataframe(cols...)
}

// expectDelim consumes the next token and verifies it is the wanted
// delimiter ('[', '{', ...).
func expectDelim(dec *json.Decoder, want json.Delim) error {
	t, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := t.(json.Delim); !ok || d != want {
		return fmt.Errorf("expected %q, got %v", want, t)
	}
	return nil
}
