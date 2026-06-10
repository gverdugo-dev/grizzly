package grizzly

import (
	"iter"
	"math/bits"
)

// This file implements the validity bitmap shared by all column types:
// one bit per row, 1 = valid, 0 = null, packed into []uint64 words
// (Arrow-style). A nil bitmap means "no nulls — every row is valid",
// so null-free columns pay no memory and a single predictable branch.
//
// Invariant: bits past the column length are always 0. New bitmaps come
// from make (zeroed memory) and only valid rows ever get their bit set,
// so popcount over the whole slice counts valid rows with no special
// case for the partial last word.
//
// Row i lives in word i/64 at bit i%64. The code writes these as i>>6
// and i&63 — the conventional power-of-two shortcuts in bitmap code
// (the compiler performs this rewrite anyway).

// bitmapWords returns the number of uint64 words needed to hold n bits.
//
// The +63 makes the integer division round up instead of down: 64 rows
// need 1 word, 65 rows need 2.
func bitmapWords(n int) int { return (n + 63) >> 6 }

// newBitmap returns an all-zero (all-null) bitmap sized for n rows.
// Callers mark rows valid with bitmapSet as values arrive.
func newBitmap(n int) []uint64 { return make([]uint64, bitmapWords(n)) }

// bitmapGet reports whether bit i is set. The caller guarantees that
// i is within the bitmap's logical length.
func bitmapGet(bm []uint64, i int) bool {
	return bm[i>>6]&(1<<(i&63)) != 0
}

// bitmapSet sets bit i (marks row i valid).
func bitmapSet(bm []uint64, i int) {
	bm[i>>6] |= 1 << (i & 63)
}

// packBools packs a []bool (one byte per entry) into bitmap words (one
// bit per entry): bit i = b[i]. Used both for validity masks and for
// BoolColumn's packed values.
func packBools(b []bool) []uint64 {
	bm := newBitmap(len(b))
	for i, v := range b {
		if v {
			bitmapSet(bm, i)
		}
	}
	return bm
}

// validityFromBools compacts the ergonomic boundary representation
// ([]bool, one byte per row) into a validity bitmap (one bit per row).
//
// It returns nil when every entry is true, preserving the nil = "no
// nulls" fast path: callers can pass a fully-valid mask and still get
// a bitmap-free column.
func validityFromBools(valid []bool) []uint64 {
	for _, ok := range valid {
		if !ok {
			return packBools(valid)
		}
	}
	return nil
}

// clearTrailingBits zeroes the bits past logical length n in the last
// word, restoring the trailing-bits-zero invariant after a whole-word
// operation (like NOT) that may have set them.
func clearTrailingBits(bm []uint64, n int) {
	if r := n & 63; r != 0 && len(bm) > 0 {
		bm[len(bm)-1] &= (1 << r) - 1
	}
}

// setBits returns an iterator over the positions of the set bits in bm,
// in ascending order — the reusable form of the TrailingZeros64 +
// word &= word-1 walk.
func setBits(bm []uint64) iter.Seq[int] {
	return func(yield func(int) bool) {
		for w, word := range bm {
			for word != 0 {
				if !yield(w<<6 + bits.TrailingZeros64(word)) {
					return
				}
				word &= word - 1
			}
		}
	}
}

// bitmapCountSet returns the number of set bits (valid rows) in bm.
//
// bits.OnesCount64 is a compiler intrinsic on most architectures: a
// single POPCNT instruction counts 64 rows at once, so this loop runs
// len(bm) iterations, not one per row.
func bitmapCountSet(bm []uint64) int {
	n := 0
	for _, word := range bm {
		n += bits.OnesCount64(word)
	}
	return n
}
