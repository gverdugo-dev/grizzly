package grizzly

// Field declares one column of a Schema: its name in the source and its type.
type Field struct {
	Name string
	Type DType
}

// Schema declares the columns to load from an untyped source (CSV, JSON):
// which ones, their types, and their order in the resulting Dataframe.
//
// grizzly never guesses types — a "08001" zip code stays a string because
// the user said so, not an int because it looked like one. The schema is a
// slice (not a map) so the user also controls column order, which untyped
// sources can't provide reliably: JSON objects are unordered by definition.
//
//	schema := grizzly.Schema{
//		{Name: "product", Type: grizzly.String},
//		{Name: "price", Type: grizzly.Float64},
//	}
type Schema []Field
