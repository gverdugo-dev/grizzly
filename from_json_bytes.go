package grizzly

// Byte-level JSON parsing: the v0.2.0 sequential JSON rewrite ("B1").
//
// Profiling showed encoding/json's per-value machinery is the JSON
// loader's whole cost: each mid-stream Decode runs an internal scan that
// formats and discards an error per call (~36% CPU, ~43% allocs), every
// Token boxes its value, and the public API offers no way around either
// (the token-per-value experiment made things 11% worse; json/v2 is still
// behind GOEXPERIMENT). So this file stops asking the stdlib's permission
// and scans the bytes directly — the polars way, and the same move
// FromCSV made when it left the streaming reader behind:
//
//   - keys are compared as raw bytes against the schema (no string
//     allocation per key);
//   - numbers go strconv.ParseFloat over the exact token slice, after a
//     strict JSON-grammar check (ParseFloat alone accepts ".5", "01",
//     "+1"... which JSON forbids — looseness here would silently change
//     which files load);
//   - strings take a fast path when they contain no backslash (one
//     validation pass + one copy); escapes (\n, \uXXXX, surrogate pairs)
//     are handled in a slow path that mirrors encoding/json's behavior,
//     unpaired surrogates becoming U+FFFD;
//   - hot scans (closing quotes) hop with bytes.IndexByte (SIMD) instead
//     of walking byte by byte.
//
// FromJSON uses this path; FromJSONReader keeps the stdlib token decoder
// as the streaming-correct reference implementation. One documented
// divergence: values of keys OUTSIDE the schema are skipped structurally
// (quotes and bracket depth) without full validation, so some malformed
// JSON that FromJSONReader rejects loads here if the damage hides inside
// an ignored value. Valid files load identically in both.

import (
	"bytes"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

// jsonKind classifies the value a scan produced; builders switch on it to
// accept or reject the value for their column type.
type jsonKind uint8

const (
	jsonNull jsonKind = iota
	jsonNumber
	jsonString // raw contents between the quotes, escapes still encoded
	jsonTrue
	jsonFalse
)

// String names the kind for error messages ("cannot load a string into a
// Float64 column").
func (k jsonKind) String() string {
	switch k {
	case jsonNull:
		return "null"
	case jsonNumber:
		return "a number"
	case jsonString:
		return "a string"
	case jsonTrue, jsonFalse:
		return "a bool"
	}
	return "an unknown value"
}

// fromJSONBytesSeq parses a whole JSON document (an array of flat
// objects) held in memory, sequentially. It is also the reference the
// parallel path (from_json_parallel.go) falls back to for canonical
// error reporting.
func fromJSONBytesSeq(data []byte, schema Schema) (Dataframe, error) {
	builders, keys, err := jsonBuilders(schema, 0)
	if err != nil {
		return Dataframe{}, err
	}

	p := &jsonParser{data: data}
	if err := p.expectByte('['); err != nil {
		return Dataframe{}, err
	}
	row := 0
	p.skipWS()
	if !p.consume(']') {
		for {
			if err := parseJSONObject(p, builders, keys, row); err != nil {
				return Dataframe{}, err
			}
			row++
			p.skipWS()
			if p.consume(',') {
				continue
			}
			if p.consume(']') {
				break
			}
			return Dataframe{}, fmt.Errorf("row %d: expected ',' or ']'", row)
		}
	}

	cols, err := finishColumns(builders)
	if err != nil {
		return Dataframe{}, err
	}
	return NewDataframe(cols...)
}

// jsonBuilders creates the per-column builders plus their key bytes (for
// allocation-free matching) — the shared setup of the sequential and
// parallel byte-level loaders.
func jsonBuilders(schema Schema, capHint int) ([]columnBuilder, [][]byte, error) {
	builders := make([]columnBuilder, len(schema))
	keys := make([][]byte, len(schema))
	for j, field := range schema {
		b, err := newColumnBuilder(field, capHint)
		if err != nil {
			return nil, nil, err
		}
		builders[j] = b
		keys[j] = []byte(field.Name)
	}
	return builders, keys, nil
}

// parseJSONObject parses one row object — from its '{' (whitespace
// allowed before it) through its '}' — appending each schema column's
// value to its builder. The row number is for error context only. Shared
// by the sequential loader and the parallel chunk workers.
func parseJSONObject(p *jsonParser, builders []columnBuilder, keys [][]byte, row int) error {
	if err := p.expectByte('{'); err != nil {
		return fmt.Errorf("row %d: %w", row, err)
	}
	filled := 0
	p.skipWS()
	if !p.consume('}') {
		for {
			p.skipWS()
			if p.pos >= len(p.data) || p.data[p.pos] != '"' {
				return fmt.Errorf("row %d: expected an object key", row)
			}
			key, err := p.scanString()
			if err != nil {
				return fmt.Errorf("row %d: %w", row, err)
			}
			if err := p.expectByte(':'); err != nil {
				return fmt.Errorf("row %d, key %q: %w", row, key, err)
			}
			if j := matchKey(keys, key); j < 0 {
				// Key not in the schema: skip its value structurally
				// (it may be nested).
				if err := p.skipValue(); err != nil {
					return fmt.Errorf("row %d, key %q: %w", row, key, err)
				}
			} else {
				kind, raw, err := p.scanValue()
				if err != nil {
					return fmt.Errorf("row %d, key %q: %w", row, key, err)
				}
				if err := builders[j].appendJSONValue(kind, raw, row); err != nil {
					return err
				}
				filled++
			}
			p.skipWS()
			if p.consume(',') {
				continue
			}
			if p.consume('}') {
				break
			}
			return fmt.Errorf("row %d: expected ',' or '}'", row)
		}
	}
	// Catches missing keys (and duplicates, which also desync lengths —
	// NewDataframe double-checks those at the end).
	if filled != len(builders) {
		return fmt.Errorf("row %d: filled %d of %d schema columns",
			row, filled, len(builders))
	}
	return nil
}

// matchKey finds the schema column a raw key belongs to, comparing bytes
// directly (no allocation). Keys containing escapes — legal but rare —
// are unescaped first on a slow path.
func matchKey(keys [][]byte, raw []byte) int {
	for j, k := range keys {
		if bytes.Equal(k, raw) {
			return j
		}
	}
	if bytes.IndexByte(raw, '\\') >= 0 {
		if s, err := unescapeJSON(raw); err == nil {
			for j, k := range keys {
				if string(k) == s {
					return j
				}
			}
		}
	}
	return -1
}

// jsonParser is a cursor over the document's bytes. All scanning methods
// move pos forward; none of them allocate on valid input (string copies
// happen later, in the builders).
type jsonParser struct {
	data []byte
	pos  int
}

// skipWS advances past JSON whitespace (space, tab, newline, carriage
// return — the only four the grammar allows).
func (p *jsonParser) skipWS() {
	for p.pos < len(p.data) {
		switch p.data[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

// consume advances over c if it is the current byte, reporting whether it
// did. Callers handle their own whitespace.
func (p *jsonParser) consume(c byte) bool {
	if p.pos < len(p.data) && p.data[p.pos] == c {
		p.pos++
		return true
	}
	return false
}

// expectByte skips whitespace and consumes c, or describes what it found
// instead.
func (p *jsonParser) expectByte(c byte) error {
	p.skipWS()
	if p.pos >= len(p.data) {
		return fmt.Errorf("expected %q, got end of input", c)
	}
	if p.data[p.pos] != c {
		return fmt.Errorf("expected %q, got %q at byte %d", c, p.data[p.pos], p.pos)
	}
	p.pos++
	return nil
}

// expectLit consumes the literal lit ("true", "false", "null") starting
// at the current byte.
func (p *jsonParser) expectLit(lit string) error {
	if !bytes.HasPrefix(p.data[p.pos:], []byte(lit)) {
		return fmt.Errorf("invalid literal at byte %d", p.pos)
	}
	p.pos += len(lit)
	return nil
}

// scanString scans a string starting at the opening quote and returns its
// raw contents (escapes still encoded), leaving pos after the closing
// quote. It finds candidate closing quotes with IndexByte and rejects the
// escaped ones by counting the backslashes before them: an odd run means
// the quote is escaped ("a\"b"), an even run means the LAST backslash is
// itself escaped ("a\\") and the quote is real.
func (p *jsonParser) scanString() ([]byte, error) {
	start := p.pos + 1
	i := start
	for {
		j := bytes.IndexByte(p.data[i:], '"')
		if j < 0 {
			return nil, fmt.Errorf("unterminated string at byte %d", p.pos)
		}
		end := i + j
		backslashes := 0
		for k := end - 1; k >= start && p.data[k] == '\\'; k-- {
			backslashes++
		}
		if backslashes%2 == 0 {
			content := p.data[start:end]
			p.pos = end + 1
			return content, nil
		}
		i = end + 1
	}
}

// scanNumber scans a number token and validates it against JSON's strict
// grammar before returning it. The greedy scan takes every byte a number
// could contain and lets the validator sort it out: "1e5" passes, "1e"
// and "01" do not.
func (p *jsonParser) scanNumber() ([]byte, error) {
	start := p.pos
scan:
	for p.pos < len(p.data) {
		switch c := p.data[p.pos]; {
		case c >= '0' && c <= '9', c == '-', c == '+', c == '.', c == 'e', c == 'E':
			p.pos++
		default:
			break scan
		}
	}
	tok := p.data[start:p.pos]
	if !validJSONNumber(tok) {
		return nil, fmt.Errorf("invalid number %q at byte %d", tok, start)
	}
	return tok, nil
}

// validJSONNumber reports whether tok matches JSON's number grammar:
//
//	-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?
//
// strconv.ParseFloat alone is too permissive (".5", "1.", "01", "+1",
// "Inf" all parse) — accepting those would silently load files the
// reference stdlib path rejects.
func validJSONNumber(tok []byte) bool {
	i := 0
	if i < len(tok) && tok[i] == '-' {
		i++
	}
	switch {
	case i < len(tok) && tok[i] == '0':
		i++
	case i < len(tok) && '1' <= tok[i] && tok[i] <= '9':
		for i < len(tok) && isDigit(tok[i]) {
			i++
		}
	default:
		return false
	}
	if i < len(tok) && tok[i] == '.' {
		i++
		if i >= len(tok) || !isDigit(tok[i]) {
			return false
		}
		for i < len(tok) && isDigit(tok[i]) {
			i++
		}
	}
	if i < len(tok) && (tok[i] == 'e' || tok[i] == 'E') {
		i++
		if i < len(tok) && (tok[i] == '+' || tok[i] == '-') {
			i++
		}
		if i >= len(tok) || !isDigit(tok[i]) {
			return false
		}
		for i < len(tok) && isDigit(tok[i]) {
			i++
		}
	}
	return i == len(tok)
}

func isDigit(c byte) bool { return '0' <= c && c <= '9' }

// scanValue scans one scalar value and classifies it. Nested objects and
// arrays are not values grizzly columns can hold, so they are an error
// here (the skip path handles them for non-schema keys).
func (p *jsonParser) scanValue() (jsonKind, []byte, error) {
	p.skipWS()
	if p.pos >= len(p.data) {
		return 0, nil, fmt.Errorf("unexpected end of input")
	}
	switch c := p.data[p.pos]; {
	case c == '"':
		raw, err := p.scanString()
		return jsonString, raw, err
	case c == 't':
		return jsonTrue, nil, p.expectLit("true")
	case c == 'f':
		return jsonFalse, nil, p.expectLit("false")
	case c == 'n':
		return jsonNull, nil, p.expectLit("null")
	case c == '-' || isDigit(c):
		raw, err := p.scanNumber()
		return jsonNumber, raw, err
	case c == '{' || c == '[':
		return 0, nil, fmt.Errorf("nested %q value", c)
	default:
		return 0, nil, fmt.Errorf("invalid character %q at byte %d", c, p.pos)
	}
}

// skipValue advances past one value of any shape without keeping it —
// the fate of keys outside the schema. Nested objects and arrays are
// skipped by bracket depth, jumping over strings (where brackets are
// data, not structure) with scanString.
func (p *jsonParser) skipValue() error {
	p.skipWS()
	if p.pos >= len(p.data) {
		return fmt.Errorf("unexpected end of input")
	}
	switch c := p.data[p.pos]; {
	case c == '"':
		_, err := p.scanString()
		return err
	case c == 't':
		return p.expectLit("true")
	case c == 'f':
		return p.expectLit("false")
	case c == 'n':
		return p.expectLit("null")
	case c == '-' || isDigit(c):
		_, err := p.scanNumber()
		return err
	case c == '{' || c == '[':
		depth := 0
		for p.pos < len(p.data) {
			switch p.data[p.pos] {
			case '"':
				if _, err := p.scanString(); err != nil {
					return err
				}
				continue // scanString already moved pos
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					p.pos++
					return nil
				}
			}
			p.pos++
		}
		return fmt.Errorf("unterminated value at byte %d", p.pos)
	default:
		return fmt.Errorf("invalid character %q at byte %d", c, p.pos)
	}
}

// unescapeJSON decodes a JSON string's raw contents (the bytes between
// the quotes) into the string they represent.
//
// Fast path: no backslash and valid UTF-8 means the bytes ARE the string —
// one validation pass (raw control characters are invalid JSON inside
// strings) and one copy. Slow path: rebuild through a strings.Builder
// handling the eight simple escapes and \uXXXX, where UTF-16 surrogate
// pairs decode to one rune and unpaired surrogates become U+FFFD. Invalid
// UTF-8 bytes also become U+FFFD instead of erroring. All three behaviors
// mirror encoding/json exactly — the fuzz oracle test is the proof (the
// invalid-UTF-8 coercion was its first catch).
func unescapeJSON(raw []byte) (string, error) {
	if bytes.IndexByte(raw, '\\') < 0 {
		// One pass does double duty: reject raw control characters and
		// detect pure ASCII — which needs no utf8.Valid call at all
		// (profiling showed Valid's per-call cost on short strings).
		ascii := true
		for _, c := range raw {
			if c < 0x20 {
				return "", fmt.Errorf("invalid control character in string")
			}
			if c >= utf8.RuneSelf {
				ascii = false
			}
		}
		if ascii || utf8.Valid(raw) {
			return string(raw), nil
		}
		// Invalid UTF-8: fall through to the rebuilding path, which
		// coerces the bad bytes.
	}

	var b strings.Builder
	b.Grow(len(raw))
	for i := 0; i < len(raw); {
		c := raw[i]
		if c != '\\' {
			switch {
			case c < 0x20:
				return "", fmt.Errorf("invalid control character in string")
			case c < utf8.RuneSelf: // ASCII
				b.WriteByte(c)
				i++
			default:
				r, size := utf8.DecodeRune(raw[i:])
				if r == utf8.RuneError && size == 1 {
					b.WriteRune(utf8.RuneError) // invalid byte → U+FFFD, like stdlib
					i++
				} else {
					b.Write(raw[i : i+size])
					i += size
				}
			}
			continue
		}
		i++
		if i >= len(raw) {
			return "", fmt.Errorf("truncated escape")
		}
		switch raw[i] {
		case '"', '\\', '/':
			b.WriteByte(raw[i])
			i++
		case 'b':
			b.WriteByte('\b')
			i++
		case 'f':
			b.WriteByte('\f')
			i++
		case 'n':
			b.WriteByte('\n')
			i++
		case 'r':
			b.WriteByte('\r')
			i++
		case 't':
			b.WriteByte('\t')
			i++
		case 'u':
			if i+5 > len(raw) {
				return "", fmt.Errorf("truncated \\u escape")
			}
			r1, ok := hex4(raw[i+1 : i+5])
			if !ok {
				return "", fmt.Errorf("invalid \\u escape")
			}
			i += 5
			if utf16.IsSurrogate(r1) {
				// A high surrogate should be followed by \uXXXX with the
				// low half; together they encode one rune above U+FFFF.
				if i+6 <= len(raw) && raw[i] == '\\' && raw[i+1] == 'u' {
					if r2, ok := hex4(raw[i+2 : i+6]); ok {
						if dec := utf16.DecodeRune(r1, r2); dec != unicode.ReplacementChar {
							b.WriteRune(dec)
							i += 6
							continue
						}
					}
				}
				b.WriteRune(unicode.ReplacementChar) // unpaired: U+FFFD, like stdlib
				continue
			}
			b.WriteRune(r1)
		default:
			return "", fmt.Errorf("invalid escape \\%c", raw[i])
		}
	}
	return b.String(), nil
}

// hex4 decodes exactly four hex digits into a rune.
func hex4(h []byte) (rune, bool) {
	var r rune
	for _, c := range h {
		r <<= 4
		switch {
		case '0' <= c && c <= '9':
			r |= rune(c - '0')
		case 'a' <= c && c <= 'f':
			r |= rune(c-'a') + 10
		case 'A' <= c && c <= 'F':
			r |= rune(c-'A') + 10
		default:
			return 0, false
		}
	}
	return r, true
}
