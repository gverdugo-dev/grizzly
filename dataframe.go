package grizzly

import "errors"

var ErrColumnNotFound = errors.New("column does not exist in dataframe")

type Dataframe struct {
	columns []Column
}

func NewDataframe(cols ...Column) Dataframe {
	return Dataframe{columns: cols}
}

func (d Dataframe) Sum(cn string) error {
	return nil
}

type Column struct {
	Name string
	Type string
	Data []any
}
