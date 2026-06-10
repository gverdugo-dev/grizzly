package grizzly

// White-box tests for the validity bitmap. This file is in package grizzly
// (not grizzly_test) on purpose: the bitmap helpers are unexported plumbing,
// and the edge cases worth pinning down — the partial last word, the
// trailing-bits-zero invariant, the nil = "no nulls" compaction — are not
// reachable through the public API alone.

import (
	"slices"
	"testing"
)

// TestBitmapWords pins the round-up division: n rows need ceil(n/64) words.
func TestBitmapWords(t *testing.T) {
	tests := []struct {
		name string
		n    int
		want int
	}{
		{"zero rows", 0, 0},
		{"one row", 1, 1},
		{"exactly one word", 64, 1},
		{"one bit past a word", 65, 2},
		{"exactly two words", 128, 2},
		{"129 rows", 129, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bitmapWords(tt.n); got != tt.want {
				t.Errorf("bitmapWords(%d) = %d, want %d", tt.n, got, tt.want)
			}
		})
	}
}

// TestBitmapSetGet exercises set/get across a word boundary: 65 rows span
// two words, so rows 63, 64 and 65-ish catch off-by-one errors in the
// i>>6 / i&63 index math.
func TestBitmapSetGet(t *testing.T) {
	const n = 65
	bm := newBitmap(n)

	// A fresh bitmap is all zeros: every row reads as unset.
	for i := 0; i < n; i++ {
		if bitmapGet(bm, i) {
			t.Fatalf("fresh bitmap: bit %d set, want unset", i)
		}
	}

	// Set a few positions chosen around the word boundary.
	set := []int{0, 1, 62, 63, 64}
	for _, i := range set {
		bitmapSet(bm, i)
	}
	for i := 0; i < n; i++ {
		want := slices.Contains(set, i)
		if got := bitmapGet(bm, i); got != want {
			t.Errorf("bit %d = %v, want %v", i, got, want)
		}
	}

	if got := bitmapCountSet(bm); got != len(set) {
		t.Errorf("bitmapCountSet = %d, want %d", got, len(set))
	}
}

// TestPackBools verifies the byte-per-row → bit-per-row packing, including
// a 65-entry input whose last entry lands alone in the second word.
func TestPackBools(t *testing.T) {
	b := make([]bool, 65)
	b[0], b[63], b[64] = true, true, true

	bm := packBools(b)
	if len(bm) != 2 {
		t.Fatalf("packBools(65 entries) produced %d words, want 2", len(bm))
	}
	for i, v := range b {
		if got := bitmapGet(bm, i); got != v {
			t.Errorf("bit %d = %v, want %v", i, got, v)
		}
	}
}

// TestValidityFromBools pins the compaction rule: an all-true mask must
// collapse to a nil bitmap (the "no nulls" fast path), and any false entry
// must force a real bitmap.
func TestValidityFromBools(t *testing.T) {
	t.Run("all true collapses to nil", func(t *testing.T) {
		valid := []bool{true, true, true}
		if got := validityFromBools(valid); got != nil {
			t.Errorf("validityFromBools(all true) = %v, want nil", got)
		}
	})

	t.Run("empty mask collapses to nil", func(t *testing.T) {
		if got := validityFromBools(nil); got != nil {
			t.Errorf("validityFromBools(nil) = %v, want nil", got)
		}
	})

	t.Run("one false forces a bitmap", func(t *testing.T) {
		valid := []bool{true, false, true}
		bm := validityFromBools(valid)
		if bm == nil {
			t.Fatal("validityFromBools with a false entry returned nil")
		}
		for i, v := range valid {
			if got := bitmapGet(bm, i); got != v {
				t.Errorf("bit %d = %v, want %v", i, got, v)
			}
		}
	})
}

// TestClearTrailingBits verifies the invariant restorer: bits past the
// logical length are zeroed, bits within it are untouched.
func TestClearTrailingBits(t *testing.T) {
	t.Run("partial last word", func(t *testing.T) {
		// 65 rows, both words all-ones (as a whole-word NOT would leave them).
		bm := []uint64{^uint64(0), ^uint64(0)}
		clearTrailingBits(bm, 65)

		if bm[0] != ^uint64(0) {
			t.Errorf("word 0 changed: %#x", bm[0])
		}
		// Only bit 0 of word 1 is within the logical length.
		if bm[1] != 1 {
			t.Errorf("word 1 = %#x, want 0x1", bm[1])
		}
	})

	t.Run("exact word multiple is untouched", func(t *testing.T) {
		// n&63 == 0 means there is no partial word: nothing to clear.
		bm := []uint64{^uint64(0)}
		clearTrailingBits(bm, 64)
		if bm[0] != ^uint64(0) {
			t.Errorf("word 0 changed: %#x", bm[0])
		}
	})
}

// TestSetBits verifies the set-bit iterator yields positions in ascending
// order across a word boundary, and that early termination (break) works —
// the yield-returns-false contract of iter.Seq.
func TestSetBits(t *testing.T) {
	bm := newBitmap(65)
	want := []int{3, 63, 64}
	for _, i := range want {
		bitmapSet(bm, i)
	}

	got := slices.Collect(setBits(bm))
	if !slices.Equal(got, want) {
		t.Errorf("setBits = %v, want %v", got, want)
	}

	t.Run("early break stops the walk", func(t *testing.T) {
		var first []int
		for i := range setBits(bm) {
			first = append(first, i)
			break
		}
		if !slices.Equal(first, []int{3}) {
			t.Errorf("after break collected %v, want [3]", first)
		}
	})
}
