package grizzly

// Parallel JSON parsing ("B2"): the chunk model from from_csv_parallel.go
// applied to a JSON array of objects. Same shape — one cheap scan finds
// safe boundaries, one goroutine per chunk parses into private builders,
// builders merge in order — with two JSON-specific twists:
//
//   - A safe boundary is the start of a top-level object: a '{' at
//     bracket depth 1 (directly inside the array), outside any string.
//     CSV's quote *parity* trick does not transfer: JSON escapes quotes
//     with a backslash (\"), not by doubling, so the splitter reuses
//     scanString (backslash-run logic included) to jump over strings,
//     and tracks {} [] depth in between. Far segments update depth with
//     four SIMD bytes.Count passes; only segments near a desired cut are
//     walked byte by byte.
//
//   - Error reporting falls back to the sequential parser. Workers know
//     only chunk-relative row numbers, so instead of translating them,
//     any worker error triggers one sequential re-parse of the whole
//     document, which reproduces the canonical error (same message, same
//     absolute row) at a cost paid only on the error path.

import (
	"bytes"
	"fmt"
	"runtime"
	"sync"
)

// Parallelism thresholds, mirroring the CSV ones.
const (
	minParallelJSONBytes = 1 << 20   // 1 MiB
	minJSONChunkBytes    = 256 << 10 // 256 KiB
)

// jsonChunk is one worker's byte range: it starts at a top-level '{' and
// ends right before another (or before the array's closing bracket).
type jsonChunk struct {
	start, end int
}

// fromJSONBytes parses a whole JSON document held in memory, in parallel
// when it is big enough. It is the engine behind FromJSON.
func fromJSONBytes(data []byte, schema Schema) (Dataframe, error) {
	if workers := jsonWorkers(len(data), runtime.GOMAXPROCS(0)); workers > 1 {
		if df, err, ok := fromJSONBytesParallel(data, schema, workers); ok {
			return df, err
		}
		// Could not frame the document as a splittable array: let the
		// sequential parser produce the result (or the canonical error).
	}
	return fromJSONBytesSeq(data, schema)
}

// jsonWorkers decides the level of parallelism for a document of the
// given size: capped by cores, floored so every worker gets a meaningful
// chunk.
func jsonWorkers(size, cores int) int {
	if size < minParallelJSONBytes {
		return 1
	}
	return min(cores, size/minJSONChunkBytes)
}

// fromJSONBytesParallel is the parallel path: frame, split, fan out,
// merge. ok=false means the document could not be framed for splitting
// (not an array, empty array, no safe cuts...) and the caller should run
// the sequential path instead.
func fromJSONBytesParallel(data []byte, schema Schema, workers int) (Dataframe, error, bool) {
	// Frame the array: '[' after leading whitespace, ']' before trailing
	// whitespace. Anything else is the sequential path's problem.
	p := &jsonParser{data: data}
	p.skipWS()
	if !p.consume('[') {
		return Dataframe{}, nil, false
	}
	dataStart := p.pos
	docEnd := len(data)
	for docEnd > dataStart && isJSONSpace(data[docEnd-1]) {
		docEnd--
	}
	if docEnd == dataStart || data[docEnd-1] != ']' {
		return Dataframe{}, nil, false
	}
	docEnd-- // the chunks span the objects, not the closing bracket

	// Fan out as a pipeline: each chunk's worker launches the moment the
	// splitter finds its end, so the (sequential) boundary scan overlaps
	// the (parallel) parsing instead of delaying it — profiling showed an
	// up-front split gating all the workers behind it. Same lock-free
	// shape as the CSV path otherwise: index-addressed results, the
	// WaitGroup as the only synchronization.
	results := make([][]columnBuilder, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	count := 0
	emit := func(c jsonChunk) {
		i := count // emit runs only on this goroutine: no race on count
		count++
		wg.Go(func() {
			results[i], errs[i] = parseJSONChunk(data, c, schema, c.end == docEnd)
		})
	}
	ok := splitJSONChunks(data, dataStart, docEnd, workers, emit)
	wg.Wait()
	if !ok || count <= 1 {
		// Malformed framing or no safe cuts: the sequential path decides
		// (any half-done worker results are simply dropped).
		return Dataframe{}, nil, false
	}

	for _, e := range errs[:count] {
		if e != nil {
			// Canonical error reporting: re-parse sequentially. Workers
			// only know chunk-relative rows; the sequential pass hits the
			// same problem with the absolute row number in the message.
			// Costs one extra parse, on the error path only.
			df, err := fromJSONBytesSeq(data, schema)
			return df, err, true
		}
	}

	merged := make([]columnBuilder, len(schema))
	parts := make([]columnBuilder, count)
	for j := range schema {
		for i := range parts {
			parts[i] = results[i][j]
		}
		b, err := mergeColumn(parts)
		if err != nil {
			return Dataframe{}, err, true
		}
		merged[j] = b
	}
	cols, err := finishColumns(merged)
	if err != nil {
		return Dataframe{}, err, true
	}
	df, err := NewDataframe(cols...)
	return df, err, true
}

// isJSONSpace reports whether c is one of JSON's four whitespace bytes.
func isJSONSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// splitJSONChunks cuts data[dataStart:docEnd] (the objects of the array,
// brackets excluded) into up to n chunks, each starting at a top-level
// '{' — a '{' at bracket depth 1, outside any string. Each chunk is
// handed to emit the moment its end is known, so callers can pipeline
// work behind the scan. With no safe cuts nothing is emitted (the caller
// falls back to sequential); ok=false means the scan hit something
// malformed.
//
// Mechanics: strings are jumped with scanString (escaped quotes
// included); every byte outside strings goes through one tight
// switch that tracks depth. A first version skipped far-from-target
// segments in bulk with bytes.Count, but JSON's segments between strings
// (key, value, key...) are so short that per-call overhead on tiny
// slices cost more than this loop — measured, not guessed.
func splitJSONChunks(data []byte, dataStart, docEnd, n int, emit func(jsonChunk)) bool {
	target := (docEnd - dataStart) / n

	p := &jsonParser{data: data[:docEnd], pos: dataStart}
	depth := 1 // directly inside the top-level array
	prev := dataStart
	for k := 1; k < n; k++ {
		want := max(dataStart+k*target, p.pos)
		cut := -1
		for cut == -1 && p.pos < docEnd {
			switch p.data[p.pos] {
			case '"':
				if _, err := p.scanString(); err != nil {
					return false
				}
				continue // scanString already moved past the string
			case '{':
				if depth == 1 && p.pos >= want {
					cut = p.pos
				}
				depth++
			case '[':
				depth++
			case '}', ']':
				depth--
			}
			p.pos++
		}
		if cut == -1 {
			break // no safe cut left: the tail stays one chunk
		}
		emit(jsonChunk{start: prev, end: cut})
		prev = cut
	}
	if prev == dataStart {
		return true // zero cuts: nothing emitted, caller goes sequential
	}
	emit(jsonChunk{start: prev, end: docEnd})
	return true
}

// parseJSONChunk parses one chunk's objects into a fresh set of builders.
// A chunk holds whole objects separated by commas; every chunk but the
// last ends with the comma that separates it from the next chunk's first
// object. Row numbers are chunk-relative — fine, because any error here
// triggers the canonical sequential re-parse anyway.
//
// The comma bookkeeping (sep) keeps the chunk grammar exactly as strict
// as the sequential loop's: two objects without a comma, or a trailing
// comma before the closing bracket (last chunk only), are errors — a
// lenient worker would make the parallel path accept documents the
// sequential path rejects.
func parseJSONChunk(data []byte, c jsonChunk, schema Schema, last bool) ([]columnBuilder, error) {
	// Pre-size with the chunk's '{' count: rows plus nested objects in
	// skipped keys — at worst a harmless over-reserve (same idea as the
	// CSV chunks' newline counts).
	capHint := bytes.Count(data[c.start:c.end], []byte{'{'})
	builders, keys, err := jsonBuilders(schema, capHint)
	if err != nil {
		return nil, err
	}

	p := &jsonParser{data: data[:c.end], pos: c.start}
	sep := true // the '[' (or the previous chunk's comma) precedes the first object
	for row := 0; ; row++ {
		p.skipWS()
		if p.pos >= c.end {
			if last && sep && row > 0 {
				return nil, fmt.Errorf("trailing comma before ']'")
			}
			break
		}
		if !sep {
			return nil, fmt.Errorf("expected ',' between objects")
		}
		if err := parseJSONObject(p, builders, keys, row); err != nil {
			return nil, err
		}
		p.skipWS()
		sep = p.consume(',')
	}
	return builders, nil
}
