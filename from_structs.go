package grizzly

import (
	"fmt"
	"reflect"
)

// FromStructs builds a Dataframe from a slice of structs: one column per
// exported struct field, named after the field (or its `grizzly:"..."` tag).
//
//	type Sale struct {
//		Product string  `grizzly:"product"`
//		Price   float64 `grizzly:"price"`
//	}
//	df, err := grizzly.FromStructs([]Sale{...})
//
// This is the row-oriented front door of grizzly: the caller thinks in rows
// (one struct = one row), and this function transposes them into typed
// columns once, at the boundary. No schema is needed — the struct fields
// already declare the types.
//
// Unexported fields are skipped. A field of an unsupported type is an error
// rather than silently dropped: explicit over magic.
func FromStructs[T any](rows []T) (Dataframe, error) {
	t := reflect.TypeFor[T]()
	if t.Kind() != reflect.Struct {
		return Dataframe{}, fmt.Errorf("FromStructs: row type must be a struct, got %s", t.Kind())
	}

	// Plan phase: walk the struct's fields ONCE and prepare, per column, a
	// pre-sized typed slice plus a closure that knows how to copy that field
	// from a row into the slice. The per-row loop below then does no type
	// decisions at all — they were all made here.
	//
	// Columns are built by finish closures AFTER the transpose, not up
	// front: BoolColumn packs its values at construction time, so its
	// slice must be fully filled before the constructor runs.
	var fill []func(rowIdx int, row reflect.Value)
	var finish []func() Column

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}

		name := f.Name
		if tag := f.Tag.Get("grizzly"); tag != "" {
			name = tag
		}

		fieldIdx := i // capture per iteration, each closure needs its own
		switch f.Type.Kind() {
		case reflect.Float64:
			values := make([]float64, len(rows)) // pre-sized: row count is known
			fill = append(fill, func(r int, row reflect.Value) {
				values[r] = row.Field(fieldIdx).Float()
			})
			finish = append(finish, func() Column { return NewFloat64Column(name, values) })
		case reflect.String:
			values := make([]string, len(rows))
			fill = append(fill, func(r int, row reflect.Value) {
				values[r] = row.Field(fieldIdx).String()
			})
			finish = append(finish, func() Column { return NewStringColumn(name, values) })
		case reflect.Bool:
			values := make([]bool, len(rows))
			fill = append(fill, func(r int, row reflect.Value) {
				values[r] = row.Field(fieldIdx).Bool()
			})
			finish = append(finish, func() Column { return NewBoolColumn(name, values) })
		default:
			return Dataframe{}, fmt.Errorf(
				"FromStructs: unsupported type %s for field %q", f.Type, f.Name)
		}
	}

	// Transpose phase: one pass over the rows, filling every column.
	for r := range rows {
		row := reflect.ValueOf(rows[r])
		for _, copyField := range fill {
			copyField(r, row)
		}
	}

	cols := make([]Column, len(finish))
	for i, fn := range finish {
		cols[i] = fn()
	}
	logger.Debug("dataframe loaded from structs", "rows", len(rows), "cols", len(cols))
	return NewDataframe(cols...)
}
