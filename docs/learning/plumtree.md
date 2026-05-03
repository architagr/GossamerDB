# Plumtree (epidemic broadcast tree over SWIM)

## Why this topic

PRD §11 Q6 ships Plumtree as an operator-enabled second layer over SWIM, used for efficient bulk dissemination of partition-map and strategy-version updates. The design constraint is **Plumtree always layered on SWIM, never replacing it** — Plumtree is a broadcast protocol, not a failure detector. Practical material on Plumtree is scarce; what exists is either Erlang-flavored (the original implementation lineage) or in the Lasp/Partisan project. Plan for fewer resources but deeper reading per resource.

## Pre-reqs

[SWIM](swim.md) — required. Plumtree assumes a working membership view from the underlying gossip layer.

## Resources

### 1. Bartosz Sypytkowski — "Plumtree" + "Optimizing state-based CRDTs"

- **Where:** `bartoszsypytkowski.com` — the Plumtree post specifically.
- **Why:** The best practical Plumtree writeup outside the Erlang ecosystem. F# code samples; the tree-construction and graft/prune protocol port directly to Go.
- **Read for:** eager-push vs lazy-push, the IHAVE / IWANT message exchange that detects missing messages, and the graft/prune tree-repair protocol.

### 2. `github.com/lasp-lang/partisan` — `partisan_plumtree_broadcast.erl`

- **Where:** GitHub.
- **Why:** A production-quality Plumtree implementation with benchmarks and scaling experiments. Erlang is annoying to read but the structure is clearer than the paper. The companion Helium project (`github.com/helium/erlang-plumtree`) is the older / smaller reference implementation.
- **Read for:** the message types and state machine. Both implementations diverge from the paper in interesting ways — note them.

### 3. João Leitão — RICON / Erlang Factory talks

- **Where:** YouTube, InfoQ — search "João Leitão Plumtree" or "epidemic broadcast trees".
- **Why:** Leitão is the Plumtree author. He is clear about what Plumtree assumes from the underlying peer-sampling layer (HyParView in the original; SWIM-membership-view in our PRD). Watch before you wire Plumtree on top of SWIM.
- **Read for:** the assumptions about peer-sampling quality. If your underlying gossip layer gives biased peer samples, Plumtree's tree degrades. SWIM's full-membership view satisfies the assumption — but Lifeguard's region-bias does not, fully. Worth thinking through.

## Go-deep checks

1. Why is Plumtree alone not a failure detector? (It is a dissemination protocol — messages go out along a tree, but if a node disappears Plumtree only learns when an IHAVE goes unanswered. That is too late for membership decisions. SWIM does the FD; Plumtree consumes its view.)
2. The eager-push tree is a spanning tree of the membership graph. What happens when it fragments — i.e., a tree edge corresponds to a now-failed node? (Graft messages from descendants attach to surviving ancestors. Until graft completes, dissemination relies on the lazy-push IHAVE/IWANT path.)
3. PRD says Plumtree is **optional** (operator-enabled). When Plumtree is disabled, partition-map and strategy-version updates ride SWIM piggyback. What is the dissemination-latency cost of this fallback? (SWIM piggyback is `O(log N)` rounds; Plumtree is also `O(log N)` rounds but with much lower per-round bandwidth. The operator-visible difference is bandwidth, not latency.)
4. Plumtree builds its tree on top of SWIM's membership view — what happens during a SWIM convergence event (e.g., 5 nodes joining at once)? (Tree repair via graft until SWIM converges. Operator-visible metric: tree-repair-rate.)
5. What is the maximum payload size for a Plumtree message in GossamerDB? (PRD §5.1 DM-18 fix: serialized partition-map ≤ 256 KiB at 128-node ceiling. Plumtree must fit within this; LLD locks the chunking strategy if it doesn't.)

## Scratch-implementation prompt

This is the hardest scratch prompt in the set. Plumtree on top of an in-memory mock membership oracle:

```go
type Membership interface {
    Members() []NodeID            // current alive view from SWIM
    Subscribe(func(MembershipEvent))
}

type Plumtree struct {
    membership Membership
    eagerPush  []NodeID            // tree edges
    lazyPush   []NodeID            // non-tree edges
}

func (p *Plumtree) Broadcast(payload []byte)
func (p *Plumtree) handleEager(from NodeID, msg Message)
func (p *Plumtree) handleIHave(from NodeID, msgID MessageID)
func (p *Plumtree) handleIWant(from NodeID, msgID MessageID)
func (p *Plumtree) handleGraft(from NodeID, msgID MessageID)
func (p *Plumtree) handlePrune(from NodeID)
```

Test harness: 32 in-process Plumtree instances over a mock SWIM oracle. Broadcast 1 K messages. Assert:

- All 32 receive every message within `O(log 32)` ≈ 5 rounds.
- Killing 3 nodes mid-broadcast triggers graft + completes within 2 additional rounds.
- Bandwidth per node is `O(messages × fanout)`, not `O(messages × N)` — that is the whole point.

If the harness is stable under churn, you have the protocol right. Go read the Partisan source one more time before the LLD.
