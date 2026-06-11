# Pairwise summation: floating-point addition is not associative

Learning note. Context: the last item of v0.2.0 — `Sum` (and `Avg` by
composition) switched from a naive sequential loop to pairwise summation,
closing the checksum mismatch the first benchmark documented (grizzly's
`…03102` vs pandas/polars' `…03099`: theirs was the more accurate answer,
and now ours matches it). It also came out 21% *faster*.

## Why (a+b)+c ≠ a+(b+c)

A float64 has 53 bits of mantissa. Every single addition rounds its true
result to those 53 bits — a tiny relative error of at most ε ≈ 1.1e-16 per
operation. Real-number addition is associative; *rounded* addition is not,
because each grouping rounds different intermediate values:

```
big   = 1e16
small = 1.0
(big + small) - big  =  0      // small vanished into big's rounding
big + (small - big)  ... and reorderings give different last bits
```

So "the sum of a column" is not one number — it is a family of numbers, one
per summation order, all within an error band. The question is how wide the
band grows with n.

## O(ε·n) vs O(ε·log n): the shape of the error

- **Sequential loop**: `((((v0+v1)+v2)+v3)+…)`. The running total
  participates in every one of the n−1 additions, so its accumulated
  rounding compounds linearly: worst-case error **O(ε·n)**. By 1M values,
  that is visible in the 6th-7th significant decimal — exactly where the
  benchmark checksums diverged.
- **Pairwise**: split the range in halves, sum each half independently,
  add the two results. Each value's error now travels through only the
  **depth of a tree** — log₂(n) additions — so the worst case is
  **O(ε·log n)**. For 1M values that is ~20 levels instead of ~1M steps.

grizzly's implementation (`pairwiseSum` in dataframe.go) is NumPy's design:
recurse down to **128-element leaves** and sum those sequentially. Within
128 values the sequential error is negligible, and the tight leaf loop is
what the hardware wants — the tree costs almost nothing on top
(~n/128 extra additions).

The tree shape has a second virtue, noted for the future: the two halves
are independent, so pairwise summation parallelizes naturally — unlike a
sequential accumulator. `Sum` is memory-bound today, so it stays
single-threaded.

## Proving accuracy: a 200-bit oracle

How do you *test* "more accurate" when every float sum is approximate? With
a reference that is effectively exact: `math/big.Float` at 200 bits of
precision accumulates 500k float64 values without observable rounding. The
test (`TestSumPairwiseAccuracy`) then measures both algorithms against it:

- pairwise: error **0** (it lands on the correctly-rounded float64)
- sequential: error 3.0e-6

Same oracle pattern as the JSON fuzzing — when your output can't be checked
by inspection, check it against an implementation whose correctness rests
on different grounds. And the external benchmark now prints
`499828455.03099`, bit-identical with pandas and polars: three independent
engines, one answer.

This is the one documented exception to "an optimization must not change
results" (principle 4 of [v0.2.0-principles.md](v0.2.0-principles.md)): the
result changed *toward the truth*, with a test proving the direction.

## The bonus: accuracy and speed were not enemies

`Sum` got 21% faster (92.6µs → 73.0µs, still 0 allocs). Not from the tree —
from the leaves: the old loop ranged over the `validValues()` iterator
(range-over-func, a function call per element); the leaf loops read the
slice and bitmap directly. The iterator stays as the readable default for
non-hot paths (`Min`/`Max` still use it); the hot path earns the manual
loop. A reminder that "more accurate algorithms are slower" is a prejudice,
not a law — measure.

## The simple version

Adding a grain of sand to a truck doesn't change what the truck's scale
reads — the grain is lost in the rounding. Sum a million grains one by one
onto the truck and you lose almost all of them: that's the sequential loop.
Instead, weigh the grains in small piles first (the 128-element leaves),
combine piles into bigger piles (the tree), and only at the end put
comparable weights together: nothing gets lost in a much-bigger neighbor.
Same grains, same scale, far truer total — and it turned out the pile
system was also faster than the one-by-one queue.

## Further reading

- [Pairwise summation](https://en.wikipedia.org/wiki/Pairwise_summation) —
  the O(ε·log n) worst-case (and O(ε·√log n) average) error bounds, the
  comparison with Kahan's O(ε), and the note that it is NumPy's default
  precisely because a large base case makes it as fast as the naive loop.
- [What Every Computer Scientist Should Know About Floating-Point
  Arithmetic](https://docs.oracle.com/cd/E19957-01/806-3568/ncg_goldberg.html)
  — Goldberg's classic: ulps, rounding, guard digits, catastrophic
  cancellation, and the IEEE 754 model underneath everything this note
  takes for granted. The canonical next step for floats-as-they-really-are.
