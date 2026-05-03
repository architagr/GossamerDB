# SWIM (membership + failure detection + region-aware variant)

## Why this topic

PRD §11 Q6 commits a region-aware SWIM variant as the default and mandatory gossip protocol. Hashicorp's `memberlist` is the de-facto Go reference implementation — most production Go gossip systems (Consul, Nomad, Serf, CockroachDB) wrap it. Read the source, not the SWIM paper.

## Pre-reqs

None hard, but reading [Vector Clocks](vector-clocks.md) first gives you the partial-order intuition that helps when reasoning about gossip-state propagation.

## Resources

### 1. Armon Dadgar — `memberlist` introduction blog post + GopherCon talks

- **Where:** `hashicorp.com/blog` and YouTube — search "Armon Dadgar memberlist" or "Armon Dadgar Serf".
- **Why:** Armon co-authored memberlist. The talks walk SWIM message types (`PING` / `PINGREQ` / `ACK` / `SUSPECT` / `CONFIRM`) with code on screen, and he covers the design choices Hashicorp made to depart from the original SWIM paper (push-pull anti-entropy, suspicion-state machine).
- **Read for:** the message-type walk-through and the "what we changed from the paper" sections. Those are the production-grade decisions you want to copy.

### 2. `github.com/hashicorp/memberlist` source + the Lifeguard blog post

- **Where:** GitHub for the source (~2000 LOC; read `state.go`, `net.go`, `memberlist.go`); `hashicorp.com/blog/lifeguard-swim-with-situational-awareness` for the Lifeguard enhancements.
- **Why:** Lifeguard is region-/load-aware probe-period tuning — directly relevant to the region-aware SWIM variant in PRD §11 Q6. Reading the source line-by-line is the fastest path to feeling competent at gossip code.
- **Read for:** the suspicion timeout math, indirect-probe fanout selection, and how Lifeguard's "Self-awareness" + "Dogpile detection" concepts map to your region tag.

### 3. Asim Aslam — "Building a gossip protocol in Go" series + `go-micro/gossip`

- **Where:** `medium.com/@asim` and his `go-micro/gossip` source on GitHub.
- **Why:** Smaller scope than memberlist, easier to read end-to-end. Good first pass before tackling the production code. He walks through the same primitives (membership, failure detection, message dissemination) at a reading-friendly scale.
- **Read for:** structure, then graduate to memberlist for production-grade detail.

**Bonus** — Brendan Gregg's blog on phi-accrual failure detection. Useful when you're deciding the SWIM `SUSPECT` decision math.

## Go-deep checks

1. SWIM probe period and timeout are not independent. If your probe period is 1 s and your timeout is 200 ms, what is the cluster's failure-detection latency? (At minimum 1 s + 1 indirect-probe round = ~2 s. Cluster-convergence is multi-round.)
2. Indirect-probe fanout `k` defends against transient network blips. What is `k` in memberlist's default config? What is the trade-off between `k=3` and `k=5`? (More indirect probes → fewer false positives but more bandwidth. PRD locks region-aware fanout — intra ≠ inter.)
3. Region-aware SWIM (PRD A7): the region tag rides every gossip message. How is the region tag piggybacked — separate field or piggyback in the metadata blob? (LLD decision — the field-versus-blob choice has wire-protocol stability implications.)
4. SWIM convergence is `O(log N)` rounds with high probability. At 128 nodes with a 1 s probe period, what is the expected convergence time? (~7 s — comfortably within the 10 s NFR commitment.)
5. What is your suspicion-timeout policy when a region's network gets slow but not partitioned? (Naive SWIM marks half the cluster `SUSPECT` on the slow side. Lifeguard-style self-awareness backs off probe periods on the slow side. PRD region-aware variant must do this.)

## Scratch-implementation prompt

This one is bigger than the others — give yourself a weekend.

```go
type Member struct {
    ID     string
    Addr   net.Addr
    Region string
    State  State // {Alive, Suspect, Confirm, Dead}
    Incarnation uint64
}

type Memberlist interface {
    Join(seeds []net.Addr) error
    Leave() error
    Members() []Member
    OnMembershipChange(func(Event))
}
```

Implement:

- The 5 message types (`PING`, `PINGREQ`, `ACK`, `SUSPECT`, `ALIVE`).
- A 1-s probe loop with random member selection.
- Indirect probes with `k=3` fanout.
- Suspicion + incarnation-number machinery.
- A simulator that runs 32 in-process members on `localhost`, kills 1 of them, and asserts cluster convergence within 10 rounds.

Don't ship this code. Throw it away. The scratch is to feel the failure modes before the real implementation lands.
