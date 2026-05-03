# GossamerDB Learning Track — Distributed Systems Deep Dive

Curated practical resources for the six core strategies GossamerDB v1 ships. **Whitepapers excluded by design** — these are blog posts, conference talks, code walk-throughs, and reference implementations. The bias is "hands-on" first.

## How to use

Run the topics in dependency order:

```
1. Vector Clocks (fundamentals)
        ↓
2. LWW comparator   3. Siblings + client merge   (parallel)
        ↓                   ↓
4. Merkle anti-entropy (consumes 2 + 3)
        ↓
5. SWIM (independent — start anytime after 1)
        ↓
6. Plumtree (depends on 5 — never read alone)
```

Budget 2–3 days deep read + scratch-implementation per topic. Total ~3 weeks for the full set sequentially.

## Topics

| # | Topic | Primary mapping in GossamerDB |
| - | ----- | ----------------------------- |
| 1 | [Vector Clocks](vector-clocks.md) | `internal/config/vector_clock.go`, conflict resolvers, FR-4 |
| 2 | [LWW conflict resolution](lww.md) | `internal/conflict/lww` (planned), FR-4 / Q5 default |
| 3 | [Siblings (Riak-style)](siblings.md) | `internal/conflict/siblings` (planned), FR-4 / Q5 alt |
| 4 | [Merkle anti-entropy](merkle-anti-entropy.md) | `internal/config/merkle_tree.go`, FR-5 / NFR-PERF-3 |
| 5 | [SWIM gossip](swim.md) | `internal/gossip/*`, FR-3 / Q6 default |
| 6 | [Plumtree epidemic broadcast](plumtree.md) | `internal/gossip/push_spread_strategy.go` (likely re-target), FR-3 / Q6 optional |

## Conventions

Each topic file has the same shape:

- **Why this topic** — one paragraph mapping to the PRD/code.
- **Pre-reqs** — what to read first.
- **Resources (2–3)** — title, source, why it's worth reading. Code/talk bias.
- **Go-deep checks** — concrete questions you must be able to answer before writing the LLD section.
- **Scratch-implementation prompt** — a tight throwaway exercise to lock the concept.

## Scope discipline

These resources are for **building the right mental model**, not for the LLD itself. The LLD locks the concrete algorithms after you've internalized the trade-offs. Don't paste resource quotes into the LLD; paraphrase from a working understanding.
