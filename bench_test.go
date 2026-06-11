package grizzly_test

// The v0.2.0 performance regression harness (docs/v0.2.0-principles.md,
// principle 1: measure before touching). Benchmarks cover the three areas —
// load, kernels, write — over one shared synthetic dataset, so every
// optimization can show a before/after number with:
//
//	go test -bench . -count=10 | tee new.txt
//	benchstat old.txt new.txt
//
// All benchmarks use b.Loop (Go 1.24+): it handles the timer around the
// loop and keeps the loop body's inputs and results alive, so the compiler
// cannot optimize the benchmarked call away.
//
// The dataset is deterministic (seeded PCG): every run, every machine, the
// exact same bytes — otherwise benchstat would compare different inputs.

import (
	"bytes"
	"io"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/gverdugo-dev/grizzly"
)

const (
	benchRows   = 100_000
	benchStores = 10 // distinct GroupBy keys
	nullEvery   = 20 // 1 of every 20 prices is null (5%)
)

var benchSchema = grizzly.Schema{
	{Name: "id", Type: grizzly.Float64},
	{Name: "store", Type: grizzly.String},
	{Name: "price", Type: grizzly.Float64},
	{Name: "sold", Type: grizzly.Bool},
}

// benchInputs holds the raw columns every benchmark input is rendered
// from: the CSV text, the JSON text and the in-memory Dataframe all carry
// exactly this data.
type benchInputs struct {
	ids        []float64
	stores     []string
	prices     []float64 // placeholder 0 where null
	priceValid []bool
	sold       []bool
}

// benchData generates the shared dataset once, lazily. sync.OnceValue
// gives us "compute on first call, cache forever" without init-time cost:
// plain unit-test runs never pay for benchmark data they don't use.
var benchData = sync.OnceValue(func() *benchInputs {
	// PCG seeded with constants: deterministic across runs and machines.
	rng := rand.New(rand.NewPCG(14, 42))

	in := &benchInputs{
		ids:        make([]float64, benchRows),
		stores:     make([]string, benchRows),
		prices:     make([]float64, benchRows),
		priceValid: make([]bool, benchRows),
		sold:       make([]bool, benchRows),
	}
	for i := range benchRows {
		in.ids[i] = float64(i)
		in.stores[i] = "store-" + strconv.Itoa(rng.IntN(benchStores))
		if i%nullEvery == 0 {
			continue // null price: placeholder 0, valid false
		}
		// Two-decimal prices: realistic cell widths in the rendered text.
		in.prices[i] = math.Round(rng.Float64()*10_000) / 100
		in.priceValid[i] = true
		in.sold[i] = rng.IntN(2) == 1
	}
	return in
})

// benchCSV renders the dataset as CSV text (nulls = empty cells).
var benchCSV = sync.OnceValue(func() []byte {
	in := benchData()
	var buf bytes.Buffer
	buf.WriteString("id,store,price,sold\n")
	var scratch []byte
	for i := range benchRows {
		scratch = strconv.AppendFloat(scratch[:0], in.ids[i], 'g', -1, 64)
		buf.Write(scratch)
		buf.WriteByte(',')
		buf.WriteString(in.stores[i])
		buf.WriteByte(',')
		if in.priceValid[i] {
			scratch = strconv.AppendFloat(scratch[:0], in.prices[i], 'g', -1, 64)
			buf.Write(scratch)
		}
		buf.WriteByte(',')
		buf.WriteString(strconv.FormatBool(in.sold[i]))
		buf.WriteByte('\n')
	}
	return buf.Bytes()
})

// benchJSON renders the dataset as a JSON array of objects (nulls =
// literal null) — the exact shape FromJSONReader loads.
var benchJSON = sync.OnceValue(func() []byte {
	in := benchData()
	var buf bytes.Buffer
	buf.WriteByte('[')
	var scratch []byte
	for i := range benchRows {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.WriteString(`{"id":`)
		scratch = strconv.AppendFloat(scratch[:0], in.ids[i], 'g', -1, 64)
		buf.Write(scratch)
		buf.WriteString(`,"store":"`)
		buf.WriteString(in.stores[i]) // store names need no JSON escaping
		buf.WriteString(`","price":`)
		if in.priceValid[i] {
			scratch = strconv.AppendFloat(scratch[:0], in.prices[i], 'g', -1, 64)
			buf.Write(scratch)
		} else {
			buf.WriteString("null")
		}
		buf.WriteString(`,"sold":`)
		buf.WriteString(strconv.FormatBool(in.sold[i]))
		buf.WriteByte('}')
	}
	buf.WriteByte(']')
	return buf.Bytes()
})

// benchDF builds the in-memory Dataframe the kernel and writer benchmarks
// operate on. Kernels never mutate, so sharing one instance is safe.
var benchDF = sync.OnceValue(func() grizzly.Dataframe {
	in := benchData()
	price, err := grizzly.NewFloat64ColumnWithNulls("price", in.prices, in.priceValid)
	if err != nil {
		panic(err)
	}
	df, err := grizzly.NewDataframe(
		grizzly.NewFloat64Column("id", in.ids),
		grizzly.NewStringColumn("store", in.stores),
		price,
		grizzly.NewBoolColumn("sold", in.sold),
	)
	if err != nil {
		panic(err)
	}
	return df
})

// --- Load ---

// BenchmarkFromCSV measures the file-path loader — the parallel chunked
// path when the file is big enough (it is: ~2.4MB > 1MiB threshold). The
// file is written once to the benchmark's temp dir; the OS page cache
// keeps re-reads cheap, mirroring the external benchmark's warm-cache
// setup.
func BenchmarkFromCSV(b *testing.B) {
	data, schema := benchCSV(), benchSchema
	path := filepath.Join(b.TempDir(), "bench.csv")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := grizzly.FromCSV(path, schema); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFromCSVReader(b *testing.B) {
	data, schema := benchCSV(), benchSchema
	b.SetBytes(int64(len(data))) // reports MB/s next to ns/op
	b.ReportAllocs()
	for b.Loop() {
		if _, err := grizzly.FromCSVReader(bytes.NewReader(data), schema); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkFromJSON measures the file-path loader — the byte-level
// parser (from_json_bytes.go), vs the stdlib token decoder kept in
// FromJSONReader below.
func BenchmarkFromJSON(b *testing.B) {
	data, schema := benchJSON(), benchSchema
	path := filepath.Join(b.TempDir(), "bench.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := grizzly.FromJSON(path, schema); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFromJSONReader(b *testing.B) {
	data, schema := benchJSON(), benchSchema
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := grizzly.FromJSONReader(bytes.NewReader(data), schema); err != nil {
			b.Fatal(err)
		}
	}
}

// --- Kernels ---

func BenchmarkSum(b *testing.B) {
	df := benchDF()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := df.Sum("price"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkFilterWhere measures the full user-facing filter pipeline:
// two comparators, one Kleene And, one Where materialization.
func BenchmarkFilterWhere(b *testing.B) {
	df := benchDF()
	b.ReportAllocs()
	for b.Loop() {
		gt, err := df.Gt("price", 25.0)
		if err != nil {
			b.Fatal(err)
		}
		lt, err := df.Lt("price", 75.0)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := df.Where(gt.And(lt)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGroupByAgg(b *testing.B) {
	df := benchDF()
	b.ReportAllocs()
	for b.Loop() {
		_, err := df.GroupBy("store").Agg(
			grizzly.Sum("price"),
			grizzly.Avg("price").As("avg"),
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSort(b *testing.B) {
	df := benchDF()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := df.Sort("price"); err != nil {
			b.Fatal(err)
		}
	}
}

// --- Write ---

func BenchmarkToCSVWriter(b *testing.B) {
	df := benchDF()
	// SetBytes wants the output size; render once to measure it.
	var out bytes.Buffer
	if err := df.ToCSVWriter(&out); err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(out.Len()))
	b.ReportAllocs()
	for b.Loop() {
		if err := df.ToCSVWriter(io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkToJSONWriter(b *testing.B) {
	df := benchDF()
	var out bytes.Buffer
	if err := df.ToJSONWriter(&out); err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(out.Len()))
	b.ReportAllocs()
	for b.Loop() {
		if err := df.ToJSONWriter(io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}
