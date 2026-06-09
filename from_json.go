package grizzly

import (
	"encoding/json"
	"fmt"
	"os"
)

// FromJSON builds a Dataframe from a JSON file containing an array of
// objects (one object = one row), using the given schema to decide each
// column's type and the column order — JSON objects are unordered, so the
// file cannot provide an order even if it wanted to.
//
// Schema columns missing from a row, or holding a value of the wrong JSON
// type, are errors: explicit over magic.
func FromJSON(path string, schema Schema) (Dataframe, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Dataframe{}, fmt.Errorf("FromJSON: %w", err)
	}

	// Each row lands as map[string]any: the JSON shape is unknown at
	// compile time, so values arrive boxed. This is exactly the layout we
	// rejected for storage — acceptable here because it lives only during
	// loading, at the boundary; the data's final home is typed columns.
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		return Dataframe{}, fmt.Errorf("FromJSON %s: %w", path, err)
	}

	cols := make([]Column, 0, len(schema))
	for _, field := range schema {
		switch field.Type {
		case Float64:
			values := make([]float64, len(rows))
			for i, row := range rows {
				// encoding/json decodes EVERY JSON number as float64 when
				// the destination is `any` — there are no ints in JSON.
				v, err := jsonValue[float64](row, field.Name, i)
				if err != nil {
					return Dataframe{}, fmt.Errorf("FromJSON %s: %w", path, err)
				}
				values[i] = v
			}
			cols = append(cols, NewFloat64Column(field.Name, values))
		case String:
			values := make([]string, len(rows))
			for i, row := range rows {
				v, err := jsonValue[string](row, field.Name, i)
				if err != nil {
					return Dataframe{}, fmt.Errorf("FromJSON %s: %w", path, err)
				}
				values[i] = v
			}
			cols = append(cols, NewStringColumn(field.Name, values))
		default:
			return Dataframe{}, fmt.Errorf("FromJSON %s: unsupported dtype %q for column %q",
				path, field.Type, field.Name)
		}
	}

	logger.Debug("dataframe loaded from json", "path", path, "rows", len(rows), "cols", len(cols))
	return NewDataframe(cols...)
}

// jsonValue extracts row[name] as type T, with errors that say which row
// and which key — generic so the two extractions above share one
// implementation instead of duplicating the lookup + assertion dance.
func jsonValue[T any](row map[string]any, name string, i int) (T, error) {
	var zero T
	raw, ok := row[name]
	if !ok {
		return zero, fmt.Errorf("row %d: %w: %q", i, ErrColumnNotFound, name)
	}
	// Type assertion with comma-ok: the runtime check that `raw` really
	// holds a T. Without the ok it would panic on mismatch.
	v, ok := raw.(T)
	if !ok {
		return zero, fmt.Errorf("row %d, key %q: expected %T, got %T", i, name, zero, raw)
	}
	return v, nil
}
