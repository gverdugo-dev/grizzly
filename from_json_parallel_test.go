package grizzly

// White-box tests for the parallel JSON path, mirroring the parallel CSV
// suite: forced worker counts, identical-dataframe equivalence against
// the sequential byte parser, splitter invariants (every boundary on a
// top-level '{'), and canonical errors via the sequential fallback.

import (
	"bytes"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// buildTrickyJSON generates rows designed to break a naive splitter:
// strings containing braces, brackets and escaped quotes, nested ignored
// values spanning would-be boundaries, nulls, and exotic numbers.
func buildTrickyJSON(rows int) []byte {
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i := range rows {
		if i > 0 {
			buf.WriteString(",\n  ")
		}
		fmt.Fprintf(&buf, `{"id": %d, `, i)
		switch {
		case i%7 == 0:
			buf.WriteString(`"note": "braces } { ] [ inside", `)
		case i%11 == 0:
			buf.WriteString(`"note": "esc \" \\ é 😀 end", `)
		case i%13 == 0:
			buf.WriteString(`"note": null, `)
		default:
			fmt.Fprintf(&buf, `"note": "note-%d", `, i)
		}
		if i%5 == 0 {
			buf.WriteString(`"price": null, `)
		} else {
			fmt.Fprintf(&buf, `"price": %se-2, `, strconv.Itoa(i*7))
		}
		if i%17 == 0 {
			// Nested ignored value, with a brace-laden string for spice.
			buf.WriteString(`"meta": {"a": [1, {"b": "}]"}], "c": null}, `)
		}
		fmt.Fprintf(&buf, `"sold": %v}`, i%3 == 0)
	}
	buf.WriteByte(']')
	return buf.Bytes()
}

// TestFromJSONBytesParallelMatchesSeq is the core equivalence test: same
// bytes, any worker count, identical dataframe.
func TestFromJSONBytesParallelMatchesSeq(t *testing.T) {
	data := buildTrickyJSON(3000)

	want, err := fromJSONBytesSeq(data, jsonTestSchema)
	if err != nil {
		t.Fatalf("sequential: %v", err)
	}
	for _, workers := range []int{2, 3, 5, 8, 16} {
		got, err, ok := fromJSONBytesParallel(data, jsonTestSchema, workers)
		if !ok {
			t.Fatalf("parallel(%d workers): not parallelizable", workers)
		}
		if err != nil {
			t.Fatalf("parallel(%d workers): %v", workers, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("parallel(%d workers) differs from sequential", workers)
		}
	}
}

// TestSplitJSONChunks checks the splitter's invariants: chunks tile the
// object region exactly and every boundary lands on a top-level '{'.
func TestSplitJSONChunks(t *testing.T) {
	data := buildTrickyJSON(800)
	dataStart := bytes.IndexByte(data, '[') + 1
	docEnd := len(data) - 1 // the closing ']'

	for _, n := range []int{2, 3, 8, 32} {
		var chunks []jsonChunk
		ok := splitJSONChunks(data, dataStart, docEnd, n,
			func(c jsonChunk) { chunks = append(chunks, c) })
		if !ok || len(chunks) == 0 {
			t.Fatalf("n=%d: split failed (ok=%v, %d chunks)", n, ok, len(chunks))
		}
		if chunks[0].start != dataStart {
			t.Fatalf("n=%d: first chunk starts at %d, want %d", n, chunks[0].start, dataStart)
		}
		if last := chunks[len(chunks)-1]; last.end != docEnd {
			t.Fatalf("n=%d: last chunk ends at %d, want %d", n, last.end, docEnd)
		}
		for i, c := range chunks {
			if i > 0 {
				if c.start != chunks[i-1].end {
					t.Fatalf("n=%d: chunk %d starts at %d, previous ends at %d",
						n, i, c.start, chunks[i-1].end)
				}
				if data[c.start] != '{' {
					t.Fatalf("n=%d: chunk %d starts on %q, want '{'", n, i, data[c.start])
				}
			}
		}
	}
}

// TestFromJSONBytesParallelErrors checks that parallel errors are the
// sequential errors, byte for byte (the fallback re-parse guarantees it —
// this pins the guarantee down).
func TestFromJSONBytesParallelErrors(t *testing.T) {
	docs := map[string][]byte{
		"bad value mid-doc": func() []byte {
			data := buildTrickyJSON(2000)
			// Corrupt one row deep into the document: a string where a
			// number must be.
			return bytes.Replace(data, []byte(`"id": 1500`), []byte(`"id": "x"`), 1)
		}(),
		"trailing comma": func() []byte {
			// Insert the comma right before the final ']' — a Replace of
			// "}]" would hit the decoy strings containing literal "}]".
			data := buildTrickyJSON(2000)
			return append(data[:len(data)-1], ',', ']')
		}(),
		"truncated document":  buildTrickyJSON(2000)[:50_000],
		"objects sans comma":  bytes.Replace(buildTrickyJSON(2000), []byte(",\n  {\"id\": 1000,"), []byte("\n  {\"id\": 1000,"), 1),
		"missing schema keys": bytes.Replace(buildTrickyJSON(2000), []byte(`"sold": true}`), []byte(`}`), 1),
	}
	for name, doc := range docs {
		t.Run(name, func(t *testing.T) {
			_, seqErr := fromJSONBytesSeq(doc, jsonTestSchema)
			if seqErr == nil {
				t.Fatal("sequential: expected an error")
			}
			_, parErr, ok := fromJSONBytesParallel(doc, jsonTestSchema, 4)
			if !ok {
				// Not parallelizable (e.g. truncated frame): the public
				// dispatcher would run sequential — same error by
				// construction. Nothing more to assert.
				return
			}
			if parErr == nil {
				t.Fatal("parallel: expected an error")
			}
			if seqErr.Error() != parErr.Error() {
				t.Errorf("error mismatch:\nsequential: %v\nparallel:   %v", seqErr, parErr)
			}
		})
	}
}

// TestJSONWorkers checks the sizing policy.
func TestJSONWorkers(t *testing.T) {
	tests := []struct {
		size, cores, want int
	}{
		{1 << 10, 8, 1},
		{minParallelJSONBytes - 1, 8, 1},
		{100 << 20, 8, 8},
		{1 << 20, 8, 4},
	}
	for _, tt := range tests {
		if got := jsonWorkers(tt.size, tt.cores); got != tt.want {
			t.Errorf("jsonWorkers(%d, %d) = %d, want %d", tt.size, tt.cores, got, tt.want)
		}
	}
}

// TestFromJSONParallelViaPublicAPI loads a >1MiB document through the
// public dispatcher so the parallel path actually engages, and checks it
// against the stdlib reference path.
func TestFromJSONParallelViaPublicAPI(t *testing.T) {
	rows := 40_000 // ~2MB: comfortably over minParallelJSONBytes
	data := buildTrickyJSON(rows)
	if len(data) < minParallelJSONBytes {
		t.Fatalf("test document too small to engage the parallel path: %d bytes", len(data))
	}

	want, err := FromJSONReader(strings.NewReader(string(data)), jsonTestSchema)
	if err != nil {
		t.Fatalf("stdlib path: %v", err)
	}
	got, err := fromJSONBytes(data, jsonTestSchema)
	if err != nil {
		t.Fatalf("byte path: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Error("byte path (parallel) differs from stdlib path")
	}
}
