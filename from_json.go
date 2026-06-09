package grizzly

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

// FromJSON builds a Dataframe from a JSON file containing an array of
// objects (one object = one row), using the given schema to decide each
// column's type and the column order — JSON objects are unordered, so the
// file cannot provide an order even if it wanted to.
//
// It decodes at token level, streaming: rows are never materialized as
// map[string]any. The first benchmark showed that intermediate layout
// (1M heap-allocated maps, 10M values boxed in any) made JSON ~6x slower
// than our own CSV path. Decode(&v) writes straight into a typed variable,
// so values go file → typed slice with no boxing in between.
//
// Schema columns missing from a row, or holding a value of the wrong JSON
// type, are errors: explicit over magic.
func FromJSON(path string, schema Schema) (Dataframe, error) {
	f, err := os.Open(path)
	if err != nil {
		return Dataframe{}, fmt.Errorf("FromJSON: %w", err)
	}
	defer f.Close()

	dec := json.NewDecoder(bufio.NewReaderSize(f, 256<<10))

	// One fill closure per schema field: decodes the value at the decoder's
	// cursor straight into that column's typed slice. finish closures wrap
	// the final slices into Columns once all rows are read.
	fills := make(map[string]func(row int) error, len(schema))
	finish := make([]func() Column, 0, len(schema))
	for _, field := range schema {
		name := field.Name
		switch field.Type {
		case Float64:
			var values []float64
			fills[name] = func(row int) error {
				var v float64
				if err := dec.Decode(&v); err != nil {
					return fmt.Errorf("row %d, key %q: %w", row, name, err)
				}
				values = append(values, v)
				return nil
			}
			finish = append(finish, func() Column { return NewFloat64Column(name, values) })
		case String:
			var values []string
			fills[name] = func(row int) error {
				var v string
				if err := dec.Decode(&v); err != nil {
					return fmt.Errorf("row %d, key %q: %w", row, name, err)
				}
				values = append(values, v)
				return nil
			}
			finish = append(finish, func() Column { return NewStringColumn(name, values) })
		default:
			return Dataframe{}, fmt.Errorf("FromJSON %s: unsupported dtype %q for column %q",
				path, field.Type, field.Name)
		}
	}

	// Walk the tokens: '[' then one '{...}' per row.
	if err := expectDelim(dec, '['); err != nil {
		return Dataframe{}, fmt.Errorf("FromJSON %s: %w", path, err)
	}
	row := 0
	for dec.More() {
		if err := expectDelim(dec, '{'); err != nil {
			return Dataframe{}, fmt.Errorf("FromJSON %s: row %d: %w", path, row, err)
		}
		filled := 0
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return Dataframe{}, fmt.Errorf("FromJSON %s: row %d: %w", path, row, err)
			}
			key, _ := keyTok.(string) // inside an object, keys are always strings
			fill, ok := fills[key]
			if !ok {
				// Key not in the schema: skip its value (it may be nested,
				// so let Decode consume it whole).
				var skip json.RawMessage
				if err := dec.Decode(&skip); err != nil {
					return Dataframe{}, fmt.Errorf("FromJSON %s: row %d, key %q: %w", path, row, key, err)
				}
				continue
			}
			if err := fill(row); err != nil {
				return Dataframe{}, fmt.Errorf("FromJSON %s: %w", path, err)
			}
			filled++
		}
		if _, err := dec.Token(); err != nil { // consume the closing '}'
			return Dataframe{}, fmt.Errorf("FromJSON %s: row %d: %w", path, row, err)
		}
		// Catches missing keys (and duplicates, which also desync lengths —
		// NewDataframe double-checks those at the end).
		if filled != len(schema) {
			return Dataframe{}, fmt.Errorf("FromJSON %s: row %d: filled %d of %d schema columns",
				path, row, filled, len(schema))
		}
		row++
	}

	cols := make([]Column, len(finish))
	for i, fn := range finish {
		cols[i] = fn()
	}
	logger.Debug("dataframe loaded from json", "path", path, "rows", row, "cols", len(cols))
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
