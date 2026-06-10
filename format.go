package grizzly

import (
	"fmt"
	"strconv"
	"strings"
)

// maxPrintRows caps how many rows String renders, so printing a huge
// dataframe stays readable (pandas does the same).
const maxPrintRows = 10

// String renders the dataframe as an aligned text table.
//
// Implementing this single method makes Dataframe satisfy fmt.Stringer, so
// the whole fmt package picks it up automatically: fmt.Println(df) prints
// this table. Output is truncated at maxPrintRows.
func (d Dataframe) String() string {
	if len(d.cols) == 0 {
		return "empty dataframe"
	}

	rows := min(d.NumRows(), maxPrintRows)

	// First pass: render every cell and track each column's widest string,
	// so the second pass can pad cells into aligned columns.
	cells := make([][]string, len(d.cols)) // [col][row]
	widths := make([]int, len(d.cols))
	for j, c := range d.cols {
		widths[j] = len(c.Name())
		cells[j] = make([]string, rows)
		for i := 0; i < rows; i++ {
			s := cellString(c, i)
			cells[j][i] = s
			widths[j] = max(widths[j], len(s))
		}
	}

	var b strings.Builder
	for j, c := range d.cols {
		writeCell(&b, c.Name(), widths[j], j == len(d.cols)-1)
	}
	for i := 0; i < rows; i++ {
		for j := range d.cols {
			writeCell(&b, cells[j][i], widths[j], j == len(d.cols)-1)
		}
	}
	if hidden := d.NumRows() - rows; hidden > 0 {
		fmt.Fprintf(&b, "… (%d more rows)\n", hidden)
	}
	return b.String()
}

// Info returns a structural summary of the dataframe, in the spirit of
// pandas' DataFrame.info(): one line per column with its name, dtype,
// non-null count and approximate memory footprint.
func (d Dataframe) Info() string {
	var b strings.Builder
	fmt.Fprintf(&b, "grizzly.Dataframe: %d rows × %d columns\n", d.NumRows(), d.NumCols())

	nameW := len("name")
	for _, c := range d.cols {
		nameW = max(nameW, len(c.Name()))
	}

	// Width of the non-null count column: "<rows> non-null" at its widest.
	countW := len(fmt.Sprintf("%d non-null", d.NumRows()))

	fmt.Fprintf(&b, " #   %-*s  %-8s  %-*s  %s\n", nameW, "name", "dtype", countW, "non-null", "memory")
	var total int
	for j, c := range d.cols {
		mem := colMemory(c)
		total += mem
		nonNull := fmt.Sprintf("%d non-null", c.Len()-c.NullCount())
		fmt.Fprintf(&b, " %-3d %-*s  %-8s  %-*s  %s\n",
			j, nameW, c.Name(), c.DType(), countW, nonNull, formatBytes(mem))
	}
	fmt.Fprintf(&b, "total memory: %s\n", formatBytes(total))
	return b.String()
}

// cellString renders the value at row i of a column as text.
//
// This is the package's single point of "concrete type knowledge" for
// rendering: adding a new column type means adding one case here.
func cellString(c Column, i int) string {
	// Null check first, through the type-agnostic interface: printing the
	// placeholder (0.00 for a null price) would be lying about the data.
	if !c.IsValid(i) {
		return "null"
	}
	switch c := c.(type) {
	case *Float64Column:
		// 'g' with precision -1: shortest representation that round-trips.
		return strconv.FormatFloat(c.values[i], 'g', -1, 64)
	case *StringColumn:
		return c.values[i]
	case *BoolColumn:
		// cellString already checked validity, so the bit is a real value.
		return strconv.FormatBool(bitmapGet(c.values, i))
	default:
		return "?"
	}
}

// colMemory estimates the bytes a column's data occupies in memory.
//
// For strings it counts the slice's string headers (16 bytes each on 64-bit:
// a pointer + a length) plus the byte content they point to on the heap —
// a concrete reminder that strings are indirection, floats are not.
// The validity bitmap, when present, adds 8 bytes per word (its ~0.4%
// overhead made visible).
func colMemory(c Column) int {
	switch c := c.(type) {
	case *Float64Column:
		return 8*len(c.values) + 8*len(c.validity)
	case *StringColumn:
		mem := 16*len(c.values) + 8*len(c.validity)
		for _, s := range c.values {
			mem += len(s)
		}
		return mem
	case *BoolColumn:
		// Both buffers are packed words: a 6-row column costs 8+8 bytes,
		// a 1M-row one ~125 KB + 125 KB — the 8x win over []bool.
		return 8*len(c.values) + 8*len(c.validity)
	default:
		return 0
	}
}

// writeCell writes one padded cell, ending the line after the last column.
func writeCell(b *strings.Builder, s string, width int, last bool) {
	if last {
		fmt.Fprintf(b, "%-s\n", s)
		return
	}
	fmt.Fprintf(b, "%-*s  ", width, s)
}

// formatBytes renders a byte count human-friendly (B, KB, MB...).
func formatBytes(n int) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := unit, 0
	for n/div >= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
