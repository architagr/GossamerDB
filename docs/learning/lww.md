# LWW conflict resolution (vector-clock-based, no wall clock)

## Why this topic

PRD §11 Q5 commits `lww` as the default conflict strategy with a deterministic comparator over sorted `(nodeId, counter)` entries — explicitly **not** wall-clock LWW. Most LWW writeups in the wild describe the timestamp variant Cassandra uses; the resources below either critique that variant or describe the vector-clock variant directly. Pick from these, not from generic "LWW explained" posts that conflate the two.

## Pre-reqs

[Vector Clocks](vector-clocks.md) — non-negotiable. Without the partial-order model the comparator design will not make sense.

## Resources

### 1. Aphyr (Kyle Kingsbury) — Jepsen "Cassandra" + Jepsen "Riak"

- **Where:** `jepsen.io/analyses/cassandra` + `jepsen.io/analyses/riak`.
- **Why:** The Cassandra post is the canonical "wall-clock LWW silently loses writes under skew" demonstration with reproducible chaos-test code. The Riak post is its vector-clock-LWW counterpart and is closer to what GossamerDB ships. Read both back-to-back.
- **Read for:** the failure modes the comparator must defend against. The PRD's "no wall-clock timestamps" decision is exactly this scar tissue.

### 2. CRDT.tech — LWW-Register entry + "Conflict-free Replicated Data Types" Bartosz Sypytkowski post

- **Where:** `crdt.tech` (concept reference) + `bartoszsypytkowski.com` (LWW-Register entry of the CRDT series).
- **Why:** The LWW-Register CRDT is mathematically what GossamerDB's `lww` strategy implements. Treat the CRDT spec as your spec. The Bartosz post has working code for the merge function.
- **Read for:** the formal merge / join definition, idempotence + commutativity proofs (sketches), and how the timestamp generalizes to "any total order" — which lets you swap wall-clock for vector-clock-derived total order without changing the algebra.

### 3. Martin Kleppmann — "CRDTs: The Hard Parts" YouTube talk

- **Where:** YouTube — search "CRDTs the hard parts Kleppmann".
- **Why:** Frames LWW vs OR-Set vs sibling-collapse trade-offs in one sitting. Bridges this topic to the next ([Siblings](siblings.md)) — both PRD strategies in one head.
- **Read for:** the trade-off discussion at the end. Why does Riak default to siblings while Cassandra defaults to LWW? Same data model, different operator preference.

## Go-deep checks

1. Two writes land with vector clocks `{n1: 3}` and `{n2: 3}`. Both clocks are concurrent and have the same "size". What does your comparator return? (Must be deterministic — pick the lexicographic winner over the sorted entries. Document the rule.)
2. A read sees `{n1: 5, n2: 2}` and `{n1: 5, n2: 2}` with different values. Algebra says `Equal` but the values differ. What happened? (Bug — same vector clock cannot have two values. Either an actor ID was reused or the writer didn't bump on overwrite. Both are bugs the runtime must reject.)
3. Why is `R + W > N` necessary but not sufficient for read-your-writes under LWW? (Necessary: ensures read overlaps with write quorum. Insufficient: concurrent writes from different clients still arrive in different orders at different replicas; LWW resolves but the "winner" is operator-visible.)
4. Document the comparator as a single function: given two `(value, vclock)` pairs, return one. Test with the cases in (1) and (2). This is your `pkg/conflict/lww/comparator.go` spec.

## Scratch-implementation prompt

```go
type Versioned struct {
    Value  []byte
    Clock  VClock
    Origin string // node id
}

// Resolve picks the LWW winner. Must be deterministic for any pair.
func Resolve(a, b Versioned) Versioned
```

Property test:

- `Resolve(a, b) == Resolve(b, a)` (commutative — order-independent).
- `Resolve(Resolve(a, b), c) == Resolve(a, Resolve(b, c))` (associative).
- `Resolve(a, a) == a` (idempotent).
- For descendant clocks: `Resolve(parent, child) == child` always.
- For concurrent clocks: the result is one of the two inputs and the choice is stable across runs.

If property tests pass on 100k random inputs, the comparator is sound. Move on.
