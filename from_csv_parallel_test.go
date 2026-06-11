package grizzly

// White-box tests for the parallel CSV path: they force worker counts and
// inspect chunk boundaries directly, which the public API hides on
// purpose. The contract under test: for ANY worker count, the parallel
// path produces a dataframe identical (reflect.DeepEqual-identical, nil
// bitmaps included) to the sequential path's, and the same errors with
// the same absolute line numbers.

import (
	"bytes"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// parallelTestSchema covers all three dtypes.
var parallelTestSchema = Schema{
	{Name: "id", Type: Float64},
	{Name: "note", Type: String},
	{Name: "price", Type: Float64},
	{Name: "sold", Type: Bool},
}

// buildTrickyCSV generates rows designed to break a naive splitter:
// quoted fields with embedded newlines, escaped quotes (""), commas
// inside quotes, plus nulls in float and bool columns.
func buildTrickyCSV(rows int) []byte {
	var buf bytes.Buffer
	buf.WriteString("id,note,price,sold\n")
	for i := range rows {
		buf.WriteString(strconv.Itoa(i))
		buf.WriteByte(',')
		switch {
		case i%7 == 0:
			// Embedded newlines: the classic chunk-splitter trap.
			fmt.Fprintf(&buf, "%q", "line one\nline two\nline three")
		case i%11 == 0:
			// Escaped quotes and a comma, RFC 4180 style.
			buf.WriteString(`"she said ""hi"", twice"`)
		default:
			fmt.Fprintf(&buf, "note-%d", i)
		}
		buf.WriteByte(',')
		if i%5 != 0 { // every 5th price is null (empty cell)
			buf.WriteString(strconv.FormatFloat(float64(i)*1.25, 'g', -1, 64))
		}
		buf.WriteByte(',')
		if i%13 != 0 { // every 13th sold is null
			buf.WriteString(strconv.FormatBool(i%2 == 0))
		}
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

// TestFromCSVBytesParallelMatchesSequential is the core equivalence test:
// same bytes, any worker count, identical dataframe.
func TestFromCSVBytesParallelMatchesSequential(t *testing.T) {
	data := buildTrickyCSV(2000)

	want, err := FromCSVReader(bytes.NewReader(data), parallelTestSchema)
	if err != nil {
		t.Fatalf("sequential: %v", err)
	}

	for _, workers := range []int{2, 3, 5, 8, 16} {
		got, err := fromCSVBytesParallel(data, parallelTestSchema, workers)
		if err != nil {
			t.Fatalf("parallel(%d workers): %v", workers, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("parallel(%d workers) differs from sequential", workers)
		}
	}
}

// TestSplitCSVChunks checks the structural invariants of the splitter:
// chunks tile data[dataStart:] exactly, every boundary sits right after a
// newline that is outside quotes, and startLine matches the newline count.
func TestSplitCSVChunks(t *testing.T) {
	data := buildTrickyCSV(500)
	dataStart := bytes.IndexByte(data, '\n') + 1

	for _, n := range []int{1, 2, 3, 7, 32} {
		chunks := splitCSVChunks(data, dataStart, n)

		if chunks[0].start != dataStart {
			t.Fatalf("n=%d: first chunk starts at %d, want %d", n, chunks[0].start, dataStart)
		}
		if last := chunks[len(chunks)-1]; last.end != len(data) {
			t.Fatalf("n=%d: last chunk ends at %d, want %d", n, last.end, len(data))
		}
		line := 2
		for i, c := range chunks {
			if i > 0 && c.start != chunks[i-1].end {
				t.Fatalf("n=%d: chunk %d starts at %d, previous ends at %d",
					n, i, c.start, chunks[i-1].end)
			}
			if c.start > dataStart && data[c.start-1] != '\n' {
				t.Fatalf("n=%d: chunk %d starts mid-line at byte %d", n, i, c.start)
			}
			if c.startLine != line {
				t.Fatalf("n=%d: chunk %d startLine = %d, want %d", n, i, c.startLine, line)
			}
			line += bytes.Count(data[c.start:c.end], []byte{'\n'})
		}
	}
}

// TestSplitCSVChunksBalance pins the *distribution* down, not just the
// invariants: quote-free data must split into exactly n chunks of similar
// size. This is the test the first version failed — it produced correct
// but lopsided chunks (1 boundary instead of n-1: a 10%/90% split that
// silently erased the parallelism), which no equivalence test can catch.
func TestSplitCSVChunksBalance(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("id,note,price,sold\n")
	for i := range 10_000 {
		fmt.Fprintf(&buf, "%d,note-%d,%d.5,true\n", i, i, i)
	}
	data := buf.Bytes()
	dataStart := bytes.IndexByte(data, '\n') + 1

	for _, n := range []int{2, 4, 8} {
		chunks := splitCSVChunks(data, dataStart, n)
		if len(chunks) != n {
			t.Fatalf("n=%d: got %d chunks", n, len(chunks))
		}
		target := (len(data) - dataStart) / n
		for i, c := range chunks {
			size := c.end - c.start
			// Each cut lands at the first newline after its target, so a
			// chunk can deviate by at most one record length per side.
			if size < target-64 || size > target+64 {
				t.Errorf("n=%d: chunk %d has %d bytes, want %d±64", n, i, size, target)
			}
		}
	}
}

// TestSplitCSVChunksQuotedRun pins the quote-awareness down: a desired
// cut point that falls inside one huge quoted field must slide past it.
func TestSplitCSVChunksQuotedRun(t *testing.T) {
	// One row whose quoted note is a wall of newlines covering the middle
	// of the file: any even split would want to cut inside it.
	wall := strings.Repeat("x\n", 5000)
	data := []byte("id,note,price,sold\n" +
		"1," + strconv.Quote(wall) + ",2.5,true\n" +
		"2,plain,3.5,false\n")
	dataStart := bytes.IndexByte(data, '\n') + 1

	chunks := splitCSVChunks(data, dataStart, 4)
	for i, c := range chunks {
		got, err := parseCSVChunk(data, c, parallelTestSchema,
			[]int{0, 1, 2, 3}, 4)
		if err != nil {
			t.Fatalf("chunk %d [%d:%d] does not parse: %v", i, c.start, c.end, err)
		}
		_ = got
	}
}

// TestFromCSVBytesParallelErrors checks that errors surface with the same
// message and absolute line number as the sequential path.
func TestFromCSVBytesParallelErrors(t *testing.T) {
	// No embedded newlines here: line numbers must be comparable
	// byte-for-byte with the sequential path's.
	var buf bytes.Buffer
	buf.WriteString("id,note,price,sold\n")
	for i := range 1000 {
		bad := ""
		if i == 800 { // line 802 in the file
			bad = "not-a-number"
		} else {
			bad = strconv.Itoa(i)
		}
		fmt.Fprintf(&buf, "%s,note-%d,%d.5,true\n", bad, i, i)
	}
	data := buf.Bytes()

	_, seqErr := FromCSVReader(bytes.NewReader(data), parallelTestSchema)
	if seqErr == nil {
		t.Fatal("sequential: expected an error")
	}
	_, parErr := fromCSVBytesParallel(data, parallelTestSchema, 4)
	if parErr == nil {
		t.Fatal("parallel: expected an error")
	}
	if seqErr.Error() != parErr.Error() {
		t.Errorf("error mismatch:\nsequential: %v\nparallel:   %v", seqErr, parErr)
	}
	if want := "line 802"; !strings.Contains(parErr.Error(), want) {
		t.Errorf("parallel error %q does not mention %q", parErr, want)
	}
}

// TestCSVWorkers checks the sizing policy: small files stay sequential,
// big files cap at the core count, mid-size files get fewer workers so
// each chunk stays meaningful.
func TestCSVWorkers(t *testing.T) {
	tests := []struct {
		size, cores, want int
	}{
		{1 << 10, 8, 1},                 // tiny: sequential
		{minParallelCSVBytes - 1, 8, 1}, // just under the threshold
		{100 << 20, 8, 8},               // big: all cores
		{1 << 20, 8, 4},                 // 1 MiB / 256 KiB = 4 meaningful chunks
	}
	for _, tt := range tests {
		if got := csvWorkers(tt.size, tt.cores); got != tt.want {
			t.Errorf("csvWorkers(%d, %d) = %d, want %d", tt.size, tt.cores, got, tt.want)
		}
	}
}
