package grizzly

// Parallel CSV parsing: the v0.2.0 "parallelize by chunks" milestone
// (docs/v0.2.0-principles.md, principle 3: parallelism at the boundaries).
//
// The model is the polars one. Parsing is CPU-bound — every byte triggers
// tokenizing, ParseFloat, null rules — so splitting the file into chunks
// and parsing them on all cores scales near-linearly, unlike the
// memory-bound columnar kernels. The shape:
//
//  1. one cheap sequential scan finds safe chunk boundaries (record
//     starts), because a chunk may only begin where a record begins;
//  2. one goroutine per chunk parses its byte range into its own private
//     columnBuilders — no locks, no sharing, no false sharing;
//  3. the builders are concatenated in chunk order and finished once, so
//     the resulting columns are bit-for-bit identical to the sequential
//     path's (principle 4: semantics do not change).
//
// The subtlety lives in step 1: a newline inside a quoted field
//
//	"line one\nline two",42
//
// is data, not a record boundary. Splitting there would hand a worker half
// a record. So the boundary scan tracks quote parity — inside/outside
// quotes — and only newlines *outside* quotes qualify. RFC 4180 escapes a
// quote inside a quoted field by doubling it (""), which flips the parity
// twice and lands back where it was, so plain parity counting stays
// correct without parsing the CSV for real.

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"runtime"
	"slices"
	"sync"
)

// Parallelism thresholds. Below minParallelCSVBytes the file is parsed
// sequentially: goroutine setup and builder merging would cost more than
// they save. Each chunk gets at least minCSVChunkBytes so workers do not
// starve on small files.
const (
	minParallelCSVBytes = 1 << 20   // 1 MiB
	minCSVChunkBytes    = 256 << 10 // 256 KiB
)

// csvChunk is one worker's slice of the file: a byte range that starts at
// the beginning of a record and ends right after the end of another (or at
// EOF), plus the absolute line number of its first record for error
// messages that match the sequential path's.
type csvChunk struct {
	start, end int
	startLine  int // 1-based; the header is line 1
	rows       int // newline count: estimated records, used to pre-size builders
}

// fromCSVBytes parses a whole CSV file held in memory, in parallel when it
// is big enough. It is the engine behind FromCSV; FromCSVReader keeps the
// sequential streaming path (an io.Reader cannot be split — byte ranges
// need random access).
func fromCSVBytes(data []byte, schema Schema) (Dataframe, error) {
	workers := csvWorkers(len(data), runtime.GOMAXPROCS(0))
	if workers <= 1 {
		return FromCSVReader(bytes.NewReader(data), schema)
	}
	return fromCSVBytesParallel(data, schema, workers)
}

// csvWorkers decides the level of parallelism for a file of the given
// size: capped by cores, floored so every worker gets a meaningful chunk.
func csvWorkers(size, cores int) int {
	if size < minParallelCSVBytes {
		return 1
	}
	return min(cores, size/minCSVChunkBytes)
}

// fromCSVBytesParallel is the parallel path: split, fan out, merge.
func fromCSVBytesParallel(data []byte, schema Schema, workers int) (Dataframe, error) {
	// The header is parsed once, sequentially, exactly like the
	// sequential path does: it is one line and it decides the source
	// index of every schema column.
	cr := csv.NewReader(bytes.NewReader(data))
	header, err := cr.Read()
	if err != nil {
		return Dataframe{}, fmt.Errorf("reading header: %w", err)
	}
	// InputOffset reports how many bytes the reader has consumed: the
	// first byte after the header line, where the data records begin.
	dataStart := int(cr.InputOffset())

	colIdx := make(map[string]int, len(header))
	for i, name := range header {
		colIdx[name] = i
	}
	src := make([]int, len(schema))
	for j, field := range schema {
		i, ok := colIdx[field.Name]
		if !ok {
			return Dataframe{}, fmt.Errorf("%w: %q", ErrColumnNotFound, field.Name)
		}
		src[j] = i
	}

	chunks := splitCSVChunks(data, dataStart, workers)

	// Fan out: one goroutine per chunk, each with its own builders and
	// its own error slot — index-addressed slices instead of channels, so
	// no synchronization is needed beyond the WaitGroup itself (each
	// goroutine writes only to its own index).
	results := make([][]columnBuilder, len(chunks))
	errs := make([]error, len(chunks))
	var wg sync.WaitGroup
	for i, c := range chunks {
		wg.Go(func() {
			results[i], errs[i] = parseCSVChunk(data, c, schema, src, len(header))
		})
	}
	wg.Wait()

	// First error in chunk order — deterministic, and the same error the
	// sequential path would have hit first.
	for _, e := range errs {
		if e != nil {
			return Dataframe{}, e
		}
	}

	// Merge: concatenate every chunk's builders for each column into the
	// first chunk's, in order, then finish once. Values and validity are
	// still plain slices at this point ([]float64, []bool...), so merging
	// is appends — no bitmap surgery, and finishColumns compacts validity
	// exactly as the sequential path does.
	merged := make([]columnBuilder, len(schema))
	parts := make([]columnBuilder, len(chunks))
	for j := range schema {
		for i := range results {
			parts[i] = results[i][j]
		}
		b, err := mergeColumn(parts)
		if err != nil {
			return Dataframe{}, err
		}
		merged[j] = b
	}
	cols, err := finishColumns(merged)
	if err != nil {
		return Dataframe{}, err
	}
	return NewDataframe(cols...)
}

// splitCSVChunks cuts data[dataStart:] into up to n chunks whose
// boundaries fall right after record-ending newlines. It scans quote
// parity by hopping between '"' bytes with bytes.IndexByte (SIMD-fast)
// instead of walking byte by byte: only the segments *between* quotes are
// searched for newlines, and only when the parity says "outside quotes".
func splitCSVChunks(data []byte, dataStart, n int) []csvChunk {
	size := len(data) - dataStart
	target := size / n

	bounds := []int{dataStart}
	pos := dataStart  // scan cursor
	inQuotes := false // current quote parity
	// nextQuote caches the absolute offset of the first '"' at or after
	// pos (len(data) when none is left), so the file is scanned for
	// quotes once in total — not once per boundary. It is refreshed only
	// when the cursor moves past it.
	nextQuote := -1
	for k := 1; k < n; k++ {
		// The k-th boundary wants to be near this offset; the real cut
		// happens at the first record-ending newline at or after it.
		want := max(dataStart+k*target, pos)
		cut := -1
		for cut == -1 && pos < len(data) {
			if nextQuote < pos {
				if q := bytes.IndexByte(data[pos:], '"'); q >= 0 {
					nextQuote = pos + q
				} else {
					nextQuote = len(data)
				}
			}
			// The segment runs from the cursor to the next quote (or
			// EOF); its parity is uniform, decided by inQuotes.
			segEnd := nextQuote
			if !inQuotes && segEnd > want {
				from := max(pos, want)
				if nl := bytes.IndexByte(data[from:segEnd], '\n'); nl >= 0 {
					cut = from + nl + 1 // first byte after the newline
					pos = cut           // resume here: same segment, same parity
					break
				}
			}
			if segEnd >= len(data) {
				pos = len(data) // no quotes and no usable newline left
				break
			}
			pos = segEnd + 1 // step over the quote
			inQuotes = !inQuotes
		}
		if cut == -1 || cut >= len(data) {
			break // no safe cut left: the tail stays one chunk
		}
		bounds = append(bounds, cut)
	}
	bounds = append(bounds, len(data))

	// Absolute first-line numbers, computed incrementally: the chunk
	// starting at bounds[i] begins after every newline before it.
	// bytes.Count is SIMD-fast, and each byte is counted once.
	chunks := make([]csvChunk, 0, len(bounds)-1)
	line := 2 // data starts on line 2; the header is line 1
	for i := 0; i+1 < len(bounds); i++ {
		nl := bytes.Count(data[bounds[i]:bounds[i+1]], []byte{'\n'})
		chunks = append(chunks, csvChunk{
			start:     bounds[i],
			end:       bounds[i+1],
			startLine: line,
			rows:      nl, // over-counts embedded newlines: a harmless over-reserve
		})
		line += nl
	}
	return chunks
}

// parseCSVChunk parses one chunk into a fresh set of builders — the same
// loop FromCSVReader runs, minus the header (already consumed) and with
// the line counter starting at the chunk's absolute first line.
func parseCSVChunk(data []byte, c csvChunk, schema Schema, src []int, fields int) ([]columnBuilder, error) {
	builders := make([]columnBuilder, len(schema))
	for j, field := range schema {
		b, err := newColumnBuilder(field, c.rows)
		if err != nil {
			return nil, err
		}
		builders[j] = b
	}

	cr := csv.NewReader(bytes.NewReader(data[c.start:c.end]))
	cr.ReuseRecord = true
	// The chunk has no header to learn the field count from: pin it
	// explicitly so a short or long record fails here exactly as it
	// would have failed against the header in the sequential path.
	cr.FieldsPerRecord = fields

	for line := c.startLine; ; line++ {
		record, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// csv.Reader's own errors carry line numbers relative to the
			// chunk; shift them so they are absolute like everything else.
			var pe *csv.ParseError
			if errors.As(err, &pe) {
				pe.Line += c.startLine - 1
				pe.StartLine += c.startLine - 1
			}
			return nil, err
		}
		for j, b := range builders {
			if err := b.appendCSV(record[src[j]], line); err != nil {
				return nil, err
			}
		}
	}
	return builders, nil
}

// mergeColumn concatenates one column's per-chunk builders into the
// first one and returns it. All parts come from the same schema field, so
// their concrete types always match. Merging happens at the builder
// level, before finish, while everything is still plain slices:
// concatenating two finished BoolColumns or validity bitmaps would mean
// shifting packed bits across word boundaries; concatenating []bool is an
// append. slices.Grow reserves the final size up front, so the
// destination reallocates once instead of once per chunk.
func mergeColumn(parts []columnBuilder) (columnBuilder, error) {
	switch d := parts[0].(type) {
	case *float64Builder:
		extra := 0
		for _, p := range parts[1:] {
			extra += len(p.(*float64Builder).values)
		}
		d.values = slices.Grow(d.values, extra)
		d.valid = slices.Grow(d.valid, extra)
		for _, p := range parts[1:] {
			s := p.(*float64Builder)
			d.values = append(d.values, s.values...)
			d.valid = append(d.valid, s.valid...)
		}
		return d, nil
	case *stringBuilder:
		extra := 0
		for _, p := range parts[1:] {
			extra += len(p.(*stringBuilder).values)
		}
		d.values = slices.Grow(d.values, extra)
		d.valid = slices.Grow(d.valid, extra)
		for _, p := range parts[1:] {
			s := p.(*stringBuilder)
			d.values = append(d.values, s.values...)
			d.valid = append(d.valid, s.valid...)
		}
		return d, nil
	case *boolBuilder:
		extra := 0
		for _, p := range parts[1:] {
			extra += len(p.(*boolBuilder).values)
		}
		d.values = slices.Grow(d.values, extra)
		d.valid = slices.Grow(d.valid, extra)
		for _, p := range parts[1:] {
			s := p.(*boolBuilder)
			d.values = append(d.values, s.values...)
			d.valid = append(d.valid, s.valid...)
		}
		return d, nil
	default:
		return nil, fmt.Errorf("mergeColumn: unsupported builder %T", parts[0])
	}
}
