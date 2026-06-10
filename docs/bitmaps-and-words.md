# Bitmaps and machine words

Learning note. Context: grizzly's validity bitmap (see
[Nulls in Go](nulls-in-go.md)) packs one bit per row into `[]uint64`, and the
planned `BoolColumn` may pack its *values* the same way. Both rest on the same
low-level vocabulary — words, bit arithmetic, popcount — and on one subtle
consequence: a packed column can no longer derive its row count from
`len(values)`. This note explains that vocabulary from the ground up.

## What a word is

A **machine word** is the natural unit of work of a CPU: the size of its
registers, the chunk it moves, compares, ANDs and ORs in a single
instruction. On a 64-bit machine, a word is **64 bits = one `uint64`**.

When grizzly's bitmap code says "word", it means one element of the
`[]uint64` slice:

```go
validity []uint64
//          ↑ each element = 1 word = 64 bits = 64 rows
```

The CPU never reads "one bit" from memory — it reads at least a word and
extracts the bit with arithmetic. That is why bitmaps are stored as word
slices instead of some hypothetical "bit slice" (which Go, like the
hardware, does not have).

## Packing rows into words

A bitmap stores row i's flag in word `i/64`, at bit position `i%64`.
Because 64 is a power of two, both operations collapse into single-cycle
bit arithmetic, written conventionally as:

```go
word := bm[i>>6]              // i >> 6  ==  i / 64  (shift right 6 = divide by 2⁶)
bit  := uint64(1) << (i & 63) // i & 63  ==  i % 64  (mask the low 6 bits)
set  := word&bit != 0
```

The compiler would rewrite the division anyway; bitmap code spells the
shift-and-mask form because it *is* the mental model: high bits of the
index choose the word, low 6 bits choose the bit inside it.

Memory cost: 1 bit per row — 125 KB for 1M rows, vs 1 MB for `[]bool`
(a Go `bool` occupies one full byte: CPUs address bytes, not bits).

## Word-level superpowers

Packing is not only about memory. Once 64 rows live in one word, one CPU
instruction processes all 64 at once:

- **Count set bits — popcount.** `bits.OnesCount64(word)` returns how many
  of the word's 64 bits are 1, in a single `POPCNT` instruction. Counting
  the valid rows of a 1M-row column is a ~15.6K-iteration loop over words,
  not a 1M-iteration loop over rows. This is `NullCount`.
- **Visit only set bits.** `bits.TrailingZeros64(word)` finds the position
  of the lowest set bit (one instruction), and `word &= word - 1` clears
  it (subtracting 1 flips everything up to and including the lowest set
  bit; the AND erases it — Kernighan's trick). Together they loop exactly
  k times for a word with k set bits, never touching the other 64-k rows.
  This is how `validValues` skips nulls without reading placeholders.
- **Combine masks.** ANDing two bitmaps word by word intersects two row
  sets 64 rows per instruction — the machinery a future `Filter` will use
  to combine conditions.

## The trailing-bits invariant

100 rows need `(100+63)/64 = 2` words = 128 bits. The last 28 bits exist
physically but correspond to no row. grizzly keeps them **always 0**, so
that popcount over the whole slice equals the valid-row count with no
special case for the partial last word. The invariant is free: `make`
returns zeroed memory, and only real rows ever get their bit set.

## Logical length vs buffer size

In a `Float64Column`, 1 slice element = 1 row, so `len(c.values)` happens
to equal the row count. Packing breaks that coincidence:

```
100 rows, packed:
values []uint64:  [ word 0: rows 0..63 ][ word 1: rows 64..99 + 28 spare bits ]
len(values) = 2   ← words, not rows
```

Nobody can recover "100" from a 2-word slice — it could be anything from
65 to 128. A packed column must therefore carry an explicit `length int`
field: the **logical length** (how many rows exist) as opposed to the
**buffer size** (how many words store them). Arrow makes exactly this
distinction — every Arrow array records its `length` separately from its
buffers — and grizzly's `BoolColumn` will need the same field the moment
its values are packed.

`bitmapWords(n) = (n + 63) >> 6` converts one into the other, rounding
up: 64 rows → 1 word, 65 rows → 2.

## The simple version

A word is an egg box holding exactly 64 eggs, and the CPU is a worker who
only ever lifts whole boxes — never a single egg. Storing one yes/no per
egg slot instead of one per crate (a byte) makes the warehouse 8x smaller,
and the worker gets superpowers from lifting boxes: counting the eggs in a
box at a glance (popcount), spotting the first occupied slot instantly
(trailing zeros), comparing two boxes slot-by-slot in one move (AND). The
fine print: if you have 100 eggs you still need 2 whole boxes, and the
second one is mostly empty padding — so you must write "100 eggs" on a
label (the `length` field), because counting boxes only tells you "between
65 and 128".

## Further reading

- [Word (computer architecture)](https://en.wikipedia.org/wiki/Word_(computer_architecture))
  — what a machine word is, why register and word size define an
  architecture, and how word sizes evolved to today's 64 bits; the
  hardware grounding for everything in this note.
- [Bit Twiddling Hacks](https://graphics.stanford.edu/~seander/bithacks.html)
  — the classic catalog of bit manipulation tricks, including Kernighan's
  `v &= v - 1` set-bit iteration and several ways to count set bits; the
  place to go when a bitmap operation feels like it should be one
  instruction cheaper.
