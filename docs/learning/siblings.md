# Siblings (Riak-style divergent reads + client merge)

## Why this topic

PRD §11 Q5 commits `siblings` as the operator-selectable alternative to `lww`. It is the harder of the two strategies to implement well — most engineering disasters in this space are sibling-related. Riak is the reference implementation; almost all practical material is Riak-derived. Embrace that.

## Pre-reqs

- [Vector Clocks](vector-clocks.md) — required.
- [LWW](lww.md) — useful for contrast (siblings is the "don't pick a winner — return both" alternative).

## Resources

### 1. Riak docs — "Conflict Resolution" + "Vector Clocks"

- **Where:** `docs.riak.com` (archived; reachable via Wayback Machine) and `github.com/basho/basho_docs` (mirror).
- **Why:** Wire-protocol-level examples — request flags (`return_body`, `R=2`, `W=2`), the `X-Riak-Vclock` header, and the read → resolve → write-back-with-vclock loop in Java/Ruby/Go. This is the contract a sibling-aware client must implement.
- **Read for:** the read API shape (sibling list + per-sibling vector clock), the write-back shape (single value + merged vector clock), and the SDK responsibilities the application carries.

### 2. Russell Brown — "Sibling explosion" series + Basho engineering posts on dotted version vectors

- **Where:** Basho engineering blog (archived) and Russell's personal posts on `russellbrown.io` / GitHub.
- **Why:** The dirty-secret post on **why siblings grow without bound** if the client doesn't merge faithfully. Without server-side bounds you get a 50 KB key turning into 5 MB of siblings over a weekend. Read this **before** locking the wire schema.
- **Read for:** server-side sibling-cap strategies, dotted version vectors (the modern replacement for naive vector clocks specifically because they fix sibling-explosion), and how anti-entropy must collapse siblings during repair.

### 3. Sean Cribbs / Justin Sheehy / Bryan Fink — RICON talks

- **Where:** YouTube — "RICON 2012/2013/2014 sibling resolution" and "Memos from the Chairman" (Sheehy).
- **Why:** The original Basho engineers walking through both the application-level merge contract and the server-side bookkeeping. Sheehy's "Memos" is short, pungent, and clarifies why siblings exist as a strategy at all.
- **Read for:** the pragmatic decision matrix — when do you ship `siblings` to your users vs `lww`? Apps that need shopping-cart-style merge → siblings. Apps that need "last writer wins, accept the loss" → LWW.

## Go-deep checks

1. A `Get` returns three siblings, each with a different vector clock. The client merges them into one value. The write-back vector clock must be what? (The pointwise max of all three sibling clocks plus an increment for the writing actor. Anything else loses information.)
2. What happens if two clients read the same three siblings, both merge, both write back independently? (Two new siblings descend from the original three but are concurrent with each other → sibling count grows. Without a cap this is unbounded.)
3. Why does the PRD §6.3 cache contract (A1) mark `siblings` divergent results as **non-cacheable**? (Caching one sibling-set under a key would mask later divergence; the cache cannot represent "this key has several values right now" without changing its API. Either the cache layer learns siblings or siblings bypass it. PRD picks the latter.)
4. Anti-entropy hashes a key and finds two replicas disagree. The active strategy is `siblings`. What is the correct repair action? (Both replicas converge to the **union** of the sibling sets, deduplicated by vector-clock equality. Repair does not pick a winner — that is the application's job.)
5. What is your maximum sibling count per key? Document it. The default Riak ceiling was 100. Going past is a runtime error or a forced-LWW collapse. Pick one.

## Scratch-implementation prompt

```go
type Sibling struct {
    Value []byte
    Clock VClock
}

// Read returns all siblings of a key. Caller merges and writes back.
func Read(key string) (siblings []Sibling, err error)

// Write commits a value with a merged vector clock that descends from every input clock.
func Write(key string, value []byte, mergedClock VClock) error

// AntiEntropyMerge unifies sibling sets from two replicas without picking a winner.
func AntiEntropyMerge(left, right []Sibling) []Sibling
```

Test cases:

- Two writes with concurrent clocks → `Read` returns 2 siblings.
- Client merges + writes back with descendant clock → `Read` returns 1 sibling.
- Three writes with one ancestor and two concurrent leaves → `Read` returns 2 siblings (the ancestor is dominated and dropped).
- Anti-entropy across two replicas with overlapping sibling sets → result has each unique vector clock once.

If `AntiEntropyMerge` is associative and commutative, repair will converge under churn.
