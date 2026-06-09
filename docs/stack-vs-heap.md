# Stack vs Heap (and why grizzly stores columns as typed slices)

Learning note. Context: deciding the internal `Column` representation — `[]float64`
(contiguous, stack-friendly) vs `[]any` (every value boxed on the heap).

## The two memories of a running program

**The stack** is each function's working memory. Every function call pushes a *frame*
holding its local variables; when the function returns, the whole frame is discarded at
once.

- Extremely cheap to manage: allocate = bump a pointer up, free = bump it down.
  One CPU instruction.
- Automatic: data lifetime equals function lifetime, nothing to clean up.
- Limited: each goroutine starts with a small stack (~8KB, grows on demand), and data
  dies when the function returns.

**The heap** is long-lived memory: data whose size isn't known at compile time, or that
must outlive the function that created it.

- Slow to manage: the allocator must *search* for a free slot of the right size and do
  bookkeeping.
- Needs a garbage collector: nothing frees the heap automatically, so Go's GC must
  periodically trace which objects are still alive and sweep the dead ones — CPU time
  stolen from your program.
- Fragmented: objects end up scattered wherever the allocator found room.

## Escape analysis: how Go decides

In C you choose (`malloc` = heap). In Go **the compiler** chooses, via *escape analysis*:
if it can prove a value doesn't outlive its function, it goes on the stack (cheap). If
the value *escapes* — returned by pointer, stored in a long-lived structure, put into an
interface — it goes to the heap.

See it yourself:

```bash
go build -gcflags='-m' ./...
# ./dataframe.go:42: v escapes to heap
```

## Why `any` means heap (boxing)

A `float64` is an 8-byte value living wherever it's declared. Inside a `[]float64`,
the bytes are *inline*, one after another:

```
[]float64:  [ 3.14 | 2.71 | 1.41 | 9.81 ]   ← 32 contiguous bytes
```

But `any` (alias for `interface{}`) is internally a pair of pointers: one to the type,
one **to the data**. Storing a `float64` in an `any` forces Go to copy the value to the
heap and keep the pointer. That's *boxing*. A `[]any` looks like this:

```
[]any:  [ (*type,*ptr) | (*type,*ptr) | (*type,*ptr) | (*type,*ptr) ]
                ↓             ↓             ↓             ↓
heap:      3.14 (here)   2.71 (there)  1.41 (yonder)  9.81 (...)
```

Three costs:

1. **Allocation** — every value = one heap allocation (slow) + future GC work.
2. **Indirection** — every read follows a pointer = one extra memory access.
3. **Loss of cache locality** — the killer for a dataframe (next section).

## Cache locality

The CPU doesn't read RAM byte by byte: it reads 64-byte blocks (*cache lines*) into
ultra-fast caches (L1 ≈ 1ns vs RAM ≈ 100ns — a **100x** gap). A *prefetcher* detects
sequential access and fetches upcoming blocks before you ask for them.

- With a contiguous `[]float64`: one cache line brings 8 floats at once, the prefetcher
  stays ahead, and summing 1M values runs at sequential-RAM speed.
- With `[]any`: every value sits at a random heap address. Every read is a potential
  ~100ns cache miss. The same `Sum()` over the same data can be **10–100x slower**.

This is the entire reason columnar storage exists, and why grizzly's columns will be
typed contiguous slices.

## The simple version

You work at a desk (stack) and there's a warehouse at the back of the building (heap).

- The **desk** is small but right in front of you: you drop things on it, use them, and
  sweep everything off in one motion when the task is done. Instant.
- The **warehouse** is huge and keeps things forever, but storing something means finding
  a free shelf, and retrieving it means walking over there. And every so often the
  cleaner (the garbage collector) has to inspect the WHOLE warehouse to see what can be
  thrown out — interrupting you while it does.

A `[]float64` is like having 1,000 numbers written on a single sheet on your desk: you
read them straight through.

A `[]any` is like having a sheet with **1,000 post-its saying "shelf 47B", "shelf
102A"...** — summing them means 1,000 trips to the warehouse, one per number.

Same data; one reads a list, the other spends the day walking.

## Further reading

- [Language Mechanics On Escape Analysis](https://www.ardanlabs.com/blog/2017/05/language-mechanics-on-escape-analysis.html)
  — William Kennedy (Ardan Labs). Walks through real Go code and compiler output
  (`-gcflags -m`) to show exactly *when* and *why* a value escapes to the heap —
  in particular how sharing a value up the call stack (returning a pointer) forces the
  escape. Part of a series on Go memory mechanics; great next step after this note.
