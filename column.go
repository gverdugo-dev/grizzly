package grizzly

// DType identifies the data type of a column. Grizzly supports a closed set
// of column types; every Column implementation reports exactly one DType.
//
// Using a small named type instead of bare strings gives us compile-time
// safety wherever a function expects "a column type" rather than "any string".
type DType string

// The supported column data types.
const (
	Float64 DType = "float64"
	String  DType = "string"
)

// Column is the contract every typed column satisfies.
//
// A column stores its values in a contiguous typed slice (e.g. []float64),
// which is what makes columnar processing fast: sequential memory access,
// no per-value heap allocations, no type assertions in hot loops.
//
// The interface intentionally exposes only type-agnostic behavior. Anything
// that needs the concrete values (Sum, Filter...) type-switches on the
// concrete implementation (*Float64Column, *StringColumn...) to reach the
// underlying slice directly.
type Column interface {
	// Name returns the column's name, unique within a Dataframe.
	Name() string
	// Len returns the number of values in the column.
	Len() int
	// DType returns the column's data type.
	DType() DType
}

// Float64Column is a column of float64 values backed by a contiguous slice.
type Float64Column struct {
	name   string
	values []float64
}

// NewFloat64Column returns a Float64Column with the given name and values.
//
// The slice is stored as-is (not copied): the caller must not mutate it
// after construction.
func NewFloat64Column(name string, values []float64) *Float64Column {
	return &Float64Column{name: name, values: values}
}

// Name returns the column's name.
func (c *Float64Column) Name() string { return c.name }

// Len returns the number of values in the column.
func (c *Float64Column) Len() int { return len(c.values) }

// DType returns Float64.
func (c *Float64Column) DType() DType { return Float64 }

// StringColumn is a column of string values backed by a contiguous slice.
type StringColumn struct {
	name   string
	values []string
}

// NewStringColumn returns a StringColumn with the given name and values.
//
// The slice is stored as-is (not copied): the caller must not mutate it
// after construction.
func NewStringColumn(name string, values []string) *StringColumn {
	return &StringColumn{name: name, values: values}
}

// Name returns the column's name.
func (c *StringColumn) Name() string { return c.name }

// Len returns the number of values in the column.
func (c *StringColumn) Len() int { return len(c.values) }

// DType returns String.
func (c *StringColumn) DType() DType { return String }
