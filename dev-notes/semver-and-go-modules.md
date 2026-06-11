# Semver and Go modules: what a version number promises

Why this matters for grizzly: when planning the `Join` milestone the question came up
of releasing it as `v0.2.1` instead of `v0.3.0` — it "felt smaller" than the v0.2.0
performance push. This note explains what each number in a version actually measures,
how Go's module tooling *acts* on those numbers, and the versioning strategies real
projects use. It records the decision: **`Join` ships as `v0.3.0`**.

## What the three numbers measure

Semantic versioning (`MAJOR.MINOR.PATCH`) is a contract about the **public API
surface**, not about effort, size, or how long something took:

| Bump | Meaning | grizzly example |
|------|---------|-----------------|
| **MAJOR** (`1.2.3` → `2.0.0`) | Incompatible API changes — existing callers break | Renaming `Avg` to `Mean`, changing `Value(i)` signatures |
| **MINOR** (`1.2.3` → `1.3.0`) | New functionality, backward compatible — callers gain, nobody breaks | Adding `Join`, adding `Sort` |
| **PATCH** (`1.2.3` → `1.2.4`) | Bug fixes only, no new API — "the same thing, but correct" | Fixing the missing-`]` strictness bug in `FromJSONReader` |

The key reframe: **the number tracks what happens to callers, not how hard you
worked.** v0.2.0 ("fast") took 13+ commits, profilers, fuzzers and a parser written
by hand — and changed almost no API. `Join` might be one file — and it is the largest
API addition since `GroupBy`. By semver's measure, `Join` is the *bigger* release.

A second reframe: 0.x numbers are not a scarce resource. There is no prize for
reaching 1.0 with a low minor number; polars went through twenty 0.x minor series
before its 1.0. Burning a minor per milestone costs nothing.

## The pre-1.0 caveat

Semver carves out major version zero explicitly: *"Major version zero (0.y.z) is for
initial development. Anything MAY change at any time."* While grizzly is on 0.x it is
allowed to break API between minors — and the roadmap already says so ("`v0.x` means
the API can still change freely between minors"). That laxity is about *compatibility
guarantees*, though, not about what the numbers mean: a patch is still a fix and a
minor is still a feature, even before 1.0. Keeping that discipline in 0.x means
nothing has to be relearned (by us or by users) when 1.0 lands.

## Go modules take the numbers literally

In many ecosystems semver is social convention. In Go, the toolchain **executes**
it — this is why getting the numbers right matters beyond aesthetics:

- **`go get -u=patch`** updates dependencies to their latest *patch* release only.
  A consumer running it expects to receive fixes and nothing else. If `Join` shipped
  as `v0.2.1`, that command would silently hand them a whole new relational feature
  set when they asked for bugfixes.
- **Minimal Version Selection (MVS)** — Go's dependency resolver — compares versions
  semantically to pick, for each module, the *minimum* version that satisfies every
  requirement in the build graph. The whole algorithm presumes the numbers are honest.
- **Tags are the release mechanism.** A Go "release" is exactly a git tag shaped like
  `vX.Y.Z` (the `v` prefix is mandatory). There is no separate publish step — which is
  why grizzly's milestones end with "tag and push".
- **Untagged commits get pseudo-versions** (`v0.2.1-0.20260611...-7027f9ba1d2c`),
  derived from the latest tag plus timestamp plus commit hash. We already met these in
  the external benchmark's stale-`@main` trap (see
  [Parsing JSON by hand](json-byte-parser.md)) — checking the pseudo-version in
  `go.mod` is how we caught the proxy serving yesterday's commit.
- **Major versions ≥ 2 change the import path.** `v2.0.0` requires the module to
  become `github.com/gverdugo-dev/grizzly/v2` — a new module path, effectively a new
  identity. This makes major bumps *expensive* in Go, and is the strongest argument
  for staying on 0.x until the API has truly settled.

## Strategies in the wild: options, pros and cons

Pre-1.0 projects answer "what do my 0.x numbers mean?" in a few distinct ways.

### A. Strict semver inside 0.x — grizzly's strategy

Patch = fixes, minor = features (and, while on 0.x, also any breaking change),
1.0 when the API has stabilized. This is the most direct strategy: one set of rules
from day one, and it is what Go's own documentation models.

- ✅ The numbers carry real information; `go get -u=patch` behaves as consumers expect.
- ✅ Nothing changes at 1.0 except the compatibility promise — no relearning.
- ✅ Matches Go ecosystem expectations (the toolchain's interpretation *is* this one).
- ❌ Within 0.x, a minor bump is ambiguous: it may be a pure addition (`v0.3.0`,
  Join) or carry breakage. Consumers must read release notes. Acceptable: 0.x
  *means* "read the notes".

### B. Shifted-down semver — the Rust/Cargo convention (polars' 0.x era)

In the Rust world, Cargo's resolver treats the leftmost *non-zero* number as the
major: in `0.y.z`, a `y` bump is breaking and `z` carries both features and fixes.
Polars lived this way for four years (2020 → July 2024), reaching 0.20.x: each 0.N
series was a breaking release, and features landed in patch releases. Since 1.0,
polars follows plain semver (breaking = major, features = minor, rest = patch) with
a formal deprecation cycle.

- ✅ Breaking changes are unambiguous — the thing strategy A leaves fuzzy.
- ✅ Sustainable for a long 0.x adolescence with frequent breakage (polars shipped
  twenty breaking series under it).
- ❌ The patch number lies by semver's definition (features in patches).
- ❌ **Go's tooling does not share this interpretation** — `go get -u=patch` would
  happily pull those feature-patches. A convention borrowed from Cargo misleads Go
  consumers. Wrong fit for grizzly.

### C. Go 1.0 early, version honestly from there

Skip the 0.x phase; every breaking change is a major bump.

- ✅ Strongest stability signal; full semver semantics immediately.
- ❌ Freezes the API before it has been lived in. grizzly *just* learned from v0.2.0
  that designs change on contact with reality (the falsified token-per-value
  experiment); Join may force revisiting GroupBy machinery.
- ❌ In Go specifically, each major after v1 costs a new import path (`/v2`, `/v3`).
  Premature 1.0 turns every design correction into an ecosystem event.

### D. Perpetual 0.x ("ZeroVer")

Never commit; stay 0.x indefinitely (a pattern common enough that a satirical
"0ver" spec exists).

- ✅ Total freedom forever.
- ❌ Signals immaturity forever, and consumers get no anchor at all. Not a real
  strategy — listed because it is the default you drift into by *not* deciding.

### The decision

grizzly uses **strategy A**: strict semver meaning inside 0.x, one milestone per
minor (`v0.1.0` load-look-ask → `v0.2.0` fast → `v0.3.0` relational), patches
reserved for fixes, and 1.0 deferred until the core API (through at least Join)
has survived contact with reality. Hence **`Join` = `v0.3.0`, not `v0.2.1`**.

## The simple version

Version numbers are like the label on a food package, and the three numbers answer
three different questions for the person *eating*, not the cook. Patch: "same
recipe, we fixed the typo in the cooking time." Minor: "new flavor added, your old
favorite still tastes the same." Major: "we changed the recipe — taste before you
serve it to guests." How many hours the cook spent in the kitchen appears nowhere
on the label — a year of perfecting the old recipe is still a patch, and a new
flavor stirred up in an afternoon is still a minor.

And in Go, the supermarket *reads the label for you*: ask it for "only fixes"
(`go get -u=patch`) and it restocks anything whose label says patch. Put a new
flavor under a fix label and you've put it straight into someone's pantry uninvited.

## Further reading

- [Semantic Versioning 2.0.0](https://semver.org/) — the specification itself: the
  MAJOR/MINOR/PATCH rules, the major-version-zero caveat, precedence, and an FAQ
  that answers "when should I 1.0?" directly. Short, and the vocabulary everyone
  else builds on.
- [Go: Module version numbering](https://go.dev/doc/modules/version-numbers) — how
  the Go toolchain interprets each component, pseudo-versions for untagged commits,
  what v0 does (and doesn't) promise, and the `/v2` module-path rule for majors.
  The Go-specific half of this note, from the source.
