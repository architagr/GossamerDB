# Merkle anti-entropy (per-range tree build + repair)

## Why this topic

FR-5 commits per-range Merkle trees on a 5-min cadence with bounded CPU/BW (NFR-PERF-3, DM-2 fix). The repository already has `internal/config/merkle_tree.go` and an anti-entropy strategy stub in `internal/gossip/anti_entropy_strategy.go` — these resources tighten the design before either is re-shaped. Cassandra is the practical reference because it is the most-deployed Merkle-repair system in production today.

## Pre-reqs

- [Vector Clocks](vector-clocks.md) — repair invokes the conflict resolver on every divergent key.
- [LWW](lww.md) **and** [Siblings](siblings.md) — both because anti-entropy must do the right thing under whichever strategy is active.

## Resources

### 1. The Last Pickle — "A Look At Cassandra Repairs" + "Anti-entropy in Cassandra"

- **Where:** `thelastpickle.com/blog` (now part of DataStax engineering blog; original posts mirrored).
- **Why:** The single best practical writeup on Merkle-repair operations: tree depth tradeoffs, validation cost, streaming the diff, throttling. Real ops scars from years of running this in production.
- **Read for:** tree depth selection, the validation phase versus the streaming phase, the cost of full vs incremental repair, and why subrange repair was added to Cassandra.

### 2. Aphyr — Jepsen "Cassandra Repair"

- **Where:** `jepsen.io/analyses/cassandra` (the repair-focused sections).
- **Why:** Adversarial analysis of Cassandra's incremental repair and where it breaks under partition. Code-annotated. Calibrates how much you should trust a "repair complete" signal.
- **Read for:** the failure modes — when repair lies, when it silently drops a range, and what the operator-visible signal must be to detect this. Directly relevant to the `BenchmarkAntiEntropyUnderLoad` gate (DM-2).

### 3. `github.com/cbergoon/merkletree` source + Apache Cassandra source for `org.apache.cassandra.repair`

- **Where:** GitHub.
- **Why:** `cbergoon/merkletree` is a clean Go reference implementation in ~200 lines — read it end-to-end. Cassandra's `repair` package is the production-scale equivalent (Java, but the algorithms transfer); JIRA ticket **CASSANDRA-5351** documents the original design with discussion. Pair both with `internal/config/merkle_tree.go` to spot the delta.
- **Read for:** the leaf-hash content (do you hash values or hashes-of-values?), the comparison protocol (does the requester send the whole tree or only the root-and-walk?), and how range boundaries align with vnode boundaries.

## Go-deep checks

1. What does a leaf in your Merkle tree hash — the value, or the `(vector clock, value-hash)` tuple? (The PRD does not lock this; the LLD must. Hashing the tuple makes the tree sensitive to clock changes alone, which is what you want for "are these replicas in sync".)
2. How does the tree-comparison protocol work over the wire — do you ship the whole tree (small clusters) or walk it depth-first with one round-trip per layer (large clusters)? Both are valid; pick one and document.
3. Tree depth: 16 levels = 65 K leaves = ~256 keys per leaf at 1 B keys / 128 nodes / 5 vnodes per node. Is 16 the right depth for GossamerDB? (LLD decision; show the math.)
4. Repair invokes the active conflict resolver per-divergent-key. Under `siblings`, repair must collapse the sibling union (see [Siblings](siblings.md) check 4); under `lww`, repair picks the comparator winner. The repair code path must be strategy-aware.
5. NFR-PERF-3 caps repair CPU at 5% steady-state and 50% in hydration burst. How does your repair scheduler enforce this? (Token bucket on hashes-per-second + token bucket on bytes-streamed-per-second. The bench-gate in `BenchmarkAntiEntropyUnderLoad` (DM-2 fix) asserts both caps under foreground load.)
6. Hydration burst (A3): a freshly-rejoined node tags itself `node_role=hydrating` and runs at 50/50. Peers must throttle outbound to it. How does a peer detect the role change? (Gossip metadata, FR-3.)

## Scratch-implementation prompt

```go
type Range struct {
    StartToken, EndToken uint64
    Keys map[string]Versioned // small; one vnode-shard worth
}

type Tree struct{ /* hash + children */ }

func Build(r Range, depth int) Tree
func Diff(a, b Tree) []KeyRange // ranges where roots disagree
func Repair(local, remote Range, divergent []KeyRange, resolve func(a, b Versioned) Versioned)
```

Test cases:

- Identical ranges → `Diff` returns empty.
- One key differs → `Diff` narrows down to a single leaf-range.
- 1 % of 10 K keys differ → `Diff` cost is `O(divergent count × log(tree depth))`, not `O(total keys)`.
- Repair under `lww` resolver → both replicas converge to the same single-value-per-key state.
- Repair under `siblings` resolver → both replicas converge to the same sibling-set-per-key state.

If repair is idempotent (running it twice on already-converged ranges is a no-op) and bounded (CPU/BW caps enforced), the gate will hold.
