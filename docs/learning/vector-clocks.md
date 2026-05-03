# Vector Clocks — fundamentals (causality, comparator, GC)

## Why this topic

Every conflict-resolution strategy in GossamerDB sits on top of vector clocks (FR-4 + PRD §11 Q5). Both `lww` and `siblings` resolve via vector-clock comparison; anti-entropy repair (FR-5) re-runs the resolver against divergent clocks. The comparator and the GC story are PRD-level decisions you cannot defer to LLD without scope risk. Code already touches `internal/config/vector_clock.go` — these resources sharpen the model before that file gets re-shaped.

## Pre-reqs

None. This is the foundation.

## Resources

### 1. Bartosz Sypytkowski — vector clock + CRDT series

- **Where:** `bartoszsypytkowski.com` — see "An introduction to state-based CRDTs" and the linked vector-clock posts.
- **Why:** Best clean intro to the partial order, the join (`max` per actor), and why it is a lattice. Working F#/Rust snippets, easy to port.
- **Read for:** the merge operator, the partial-order intuition, and how the lattice property gives you idempotent / commutative / associative merge for free.
- **Articles**
  - State-based CRDTs (and the introduction of Version Vectors)
    Link: [An introduction to state-based CRDTs](https://www.bartoszsypytkowski.com/the-state-of-a-state-based-crdts/)
    Why you need it: GossamerDB's siblings conflict strategy is heavily inspired by Riak and CRDTs. In this article, Bartosz explains how distributed nodes track causality using counters and node IDs, which is the foundational concept of a Vector Clock (technically a "Version Vector" when used to track data replica updates).
  - Dotted Version Vectors
    Link: https://bartoszsypytkowski.com/dotted-version-vectors/
    Why you need it: Traditional vector clocks can grow indefinitely and cause massive overhead (which would threaten GossamerDB's strict < 1 ms GET p99 SLO). "Dotted Version Vectors" are the modern optimization used by Riak and others to keep the payload size small while perfectly tracking sibling divergence. If you are implementing the siblings conflict resolver, this is mandatory reading.
  - Operation-based CRDTs & Causal Delivery
    Link: https://bartoszsypytkowski.com/crdt-operations/
    Why you need it: This dives deeper into how vector clocks are sent over the wire (like GossamerDB's gossip and anti-entropy layers) to ensure messages aren't processed out of order, and how to safely merge concurrent updates.

### 2. Sean Cribbs — "Why Vector Clocks Are Easy" + "Why Vector Clocks Are Hard"

- **Where:** Basho engineering blog (archived; mirrored on Wayback Machine and `github.com/basho`). Plus his RICON talks on YouTube.
- **Why:** Riak's chief vector-clock voice. The "Hard" post covers the GC problem head-on — actor-id retirement, dotted version vectors, and the failure modes you hit at scale (1M ops/s in our case).
- **Read for:** GC strategies and why naive vector clocks blow up under churn — directly relevant to the in-memory-only data plane (PRD FR-12) where node restarts retire actor IDs.

### 3. Martin Kleppmann — "Designing Data-Intensive Applications" Ch. 5 + Ch. 9 + "A Critique of CAP Theorem" talk

- **Where:** book (O'Reilly) + YouTube for the talk.
- **Why:** The "Detecting Concurrent Writes" worked example with a shopping cart is the cleanest end-to-end walkthrough in print. The CAP talk frames why your design choices (`R+W>N`, vector-clock comparator, no wall-clock LWW) are forced moves, not preferences.
- **Read for:** the worked shopping-cart example — copy it onto a whiteboard once and the model sticks.

## Go-deep checks

Before you touch the LLD, you must be able to answer:

1. Given two vector clocks `A = {n1: 3, n2: 2}` and `B = {n1: 3, n2: 2, n3: 1}`, what is the relationship — descendant, ancestor, equal, or concurrent? (B descends from A.)
2. What does your comparator return when `A = {n1: 2, n2: 1}` and `B = {n1: 1, n2: 2}`? (Concurrent — true tie. LWW must break this; siblings must surface it.)
3. When node `n5` is decommissioned and a new node takes its place under a fresh ID, what happens to old vector clocks that still carry `n5: 7`? (They never advance — GC-eligible. PRD must specify the GC trigger.)
4. Why does the PRD reject wall-clock timestamps as the LWW comparator? (See the Aphyr Cassandra Jepsen post — clock skew silently loses writes.)
5. What is the maximum size of a vector clock in your system? (Bounded by active actor IDs in the cluster — at 128 nodes that's fine; at 1B keys it's not per-key state, it's per-key state per recent writer.)

## Scratch-implementation prompt

In a throwaway Go file, implement:

```go
type VClock map[string]uint64

func (a VClock) Compare(b VClock) Ordering // {Before, After, Equal, Concurrent}
func (a VClock) Merge(b VClock) VClock     // pointwise max
func (a VClock) Increment(actor string) VClock
```

Write a fuzz test that generates random vector-clock pairs and asserts:

- `Merge` is commutative, associative, and idempotent.
- `a.Compare(a.Merge(b))` is `Before` or `Equal`.
- If `a.Compare(b) == Concurrent`, then `b.Compare(a) == Concurrent`.

If the fuzz passes, you have the algebra right. Throw the code away after — the LLD lives in `pkg/clock/`.
