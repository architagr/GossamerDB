# GossamerDB — High-Level Design

**Status:** Draft v0.2 — open-question resolution pass off PRD v1.13 (`docs/prds/README.md`, APPROVED 2026-05-03).

**Author:** Archit Agarwal

**Upstream inputs:** `docs/wiki/README.md` (requirements wiki) → `docs/prds/README.md` (PRD) → **this document** → `docs/lld/gossamerdb.md` → `docs/epics/gossamerdb/` → `docs/stories/gossamerdb/<epic>/`

**Project rules:** `CLAUDE.md`

**Binding constraint:** every PRD-level FR/NFR/Q/OD/DM ID cited inline is binding on the HLD; the LLD MAY tighten but MUST NOT relax.

> **Scope contract.** This HLD captures intent, component boundaries, and tradeoffs. Go package layouts, struct fields, method signatures, and full wire schemas live in the LLD. Where the PRD already locks a value (vnode-count constraints, Raft snapshot cadence, fio floor, cache TTL ceiling, etc.), the HLD honours the value verbatim and traces it back; where the PRD leaves a measurable target without an architectural answer (e.g. how a `R=3 of 5` quorum read clears 1 ms p99), this document supplies the architecture and the measurement plan.

### Revision history

| Date       | Rev  | Change                                                                  | Driver         |
| ---------- | ---- | ----------------------------------------------------------------------- | -------------- |
| 2026-05-04 | v0.1 | Initial scaffold from PRD v1.13. All 18 hld-spec-architect sections seeded with PRD-traced content; vnode-count selection locked at 128 vnodes/node (16384 cluster-wide at 128-node ceiling); R1 GET-QUORUM sub-budget plan present; cross-references to `docs/prds/diagrams/*.svg` reused (no new diagrams added in v0.1). | Archit Agarwal |
| 2026-05-09 | v0.2 | **§18 open-question resolution pass.** OQ-1, OQ-2, OQ-5, OQ-6, OQ-8, OQ-9, OQ-10 LOCKED at HLD with rationale + cross-refs; OQ-3, OQ-4, OQ-7 trimmed to bench-bound LLD scope (the architectural shape is locked, only the numeric knob is LLD-bench-driven). Inherited PRD OD list reconciled to PRD v1.10 `needs-by` tags. Adds Appendix B summarising every newly-locked decision so the EM gate sees the §18 → §14/§5/§6 traceability in one place. No requirement-level changes; HLD is still PRD v1.13-bound. | Archit Agarwal |

---

## 1. Context and problem statement

GossamerDB is a cloud-agnostic distributed key-value store. The wiki and PRD frame the problem as: **operators want a Dynamo-style KV cluster they can deploy identically on local, Kubernetes, and multi-region AWS, without giving up sub-millisecond reads or operator control over the consistency / replication / gossip / conflict-resolution knobs.** Existing options force a tradeoff: managed services (DynamoDB, BigTable) lock you to a vendor; etcd / Consul are control-plane sized, not data-plane sized; Cassandra and Riak hand you the knobs but at the cost of operability and tail latency at the < 1 ms p99 scale this project targets.

The PRD (Q1 / NFR-PERF-1a / NFR-SCALE-2) commits to **GET p99 < 1 ms request-level** (not cache-only — full quorum read inclusive of replica fan-out and conflict resolution) and **PUT p99 ≤ 5 ms** at ≥ 1 M ops/s sustained on a 128-node cluster, with `N=5, W=3, R=3` defaults. The architectural problem this HLD solves is therefore **how to clear that latency budget under a Dynamo-style replication scheme without paying for either a hosted control plane or the vendor-lock that Dynamo's managed peers require**.

The HLD answers this by:

- **Splitting the control plane and the data plane** so the per-request path never traverses the consensus tier (FR-7 / FR-8 / Q3).
- **Offering three routing tiers** with the smart Go SDK as the primary one-hop path that earns the < 1 ms budget; coordinator-as-replica fan-out and any-data-node forwarding cover the long tail without adding deployable surface (FR-8).
- **Treating the cache layer as mandatory** for the < 1 ms budget, with an explicit `(key, partition_map_epoch)` key shape so QUORUM reads stay cache-eligible without the vc-hash footgun (FR-12 / NFR-PERF-2 / A1).
- **Making mTLS the only auth surface in v1** so security is a build-time constraint, not a deployment-time toggle (FR-6 / NFR-SEC-1 / Q9).

The < 1 ms p99 commitment is the load-bearing decision; everything else in this HLD is shaped around it.

## 2. Goals and non-goals

### 2.1 Goals (HLD-level — derived from PRD §1.1)

- **HG1.** Clear PRD §4.1 budgets (GET p99 < 1 ms request-level at any consistency; PUT p99 ≤ 5 ms; same budgets at N=3 with W=R=2 per DM-17) with an architecture that survives the bench gate (`./.claude/scripts/bench-check.sh`).
- **HG2.** Keep the Raft Coordinator group strictly off the per-request path (FR-7) so a coordinator-group outage pauses control-plane mutations only, not data-plane traffic.
- **HG3.** Layer gossip protocols (mandatory region-aware SWIM + optional Plumtree) so membership / FD and bulk-dissemination concerns are addressed without coupling them, and so authoring a future strategy is purely additive (FR-3 / NFR-API-2 / Q6).
- **HG4.** Enforce a single mTLS-only listener model with operator-supplied PKI behind a `pki.Source` interface; no `--insecure` flag exists in the binary (FR-6 / NFR-SEC-1 / Q8 / R6).
- **HG5.** Make every cross-cutting tradeoff (cache eligibility, anti-entropy bounds, fan-out parallelism, partition-map epoch handling) explicit and **bench-gated**, not undocumented "good behaviour" (NFR-PERF-1..4 / OD-6).
- **HG6.** Preserve a single binary across all three deployment modes; only configuration differs (NFR-PORT-1).
- **HG7.** Keep all wire-protocol surfaces (gRPC primary, REST via Fiber secondary, admin gRPC, gossip, anti-entropy) **semver-compatible within a major version** (NFR-API-1).
- **HG8.** Carry vector clocks on every write and surface conflict resolution as a configurable strategy (`lww` default, `siblings` opt-in) — never hide concurrent writes (FR-4 / J-A-2 / R3).
- **HG9.** Provide a smart Go SDK that earns the GET p99 < 1 ms budget on the primary path **and** transparently handles `WRONG_OWNER` / `WRONG_REGION` redirects so callers never see topology churn or region failover as a hard error (FR-20 / R9).
- **HG10.** Carry an **OpenTelemetry-Collector-mediated** observability pipeline (OTLP gRPC for metrics + traces + logs) with a three-panel cross-correlated reference Grafana dashboard so operators can answer J-O-5 from telemetry alone (FR-11 / FR-17 / Q16 / NFR-OPS-4).

### 2.2 Non-goals (HLD-level — derived from PRD §1.2 and §3.3)

- **HNG1.** Per-row residency tagging, automatic placement, or runtime "GDPR mode" flag (NG10). The cluster is the residency primitive (NFR-SEC-7).
- **HNG2.** Active-active multi-region writes and automatic region failover (FC-3) — both depend on a cross-region vector-clock merge correctness machinery deferred to v1.2.
- **HNG3.** Durable per-node data backend (Pebble / RocksDB / S3-tiered) (FR-12 / Q7) — v1 is in-memory only.
- **HNG4.** Hot-swap of gossip / conflict strategies (FC-1 / Q13).
- **HNG5.** Non-Go SDKs (FC-2 / Q14) — wire-protocol-only for non-Go callers in v1.
- **HNG6.** Built-in CA — permanently rejected (FR-6 / Q8 rejected-alternatives), not deferred.
- **HNG7.** Stateless router tier as a separate deployable (FR-8 deferred-alternatives).
- **HNG8.** SQL / document / search / queue semantics (NG1).

## 3. Assumptions

The HLD relies on the following assumptions, every one of which has a PRD-traced source. If any of them changes, the HLD must be re-validated.

- **A-Net.** Intra-AZ p99 RTT ≤ 200 µs; inter-region p99 RTT ≤ 100 ms (NFR-SCALE-8 / OD-3, `pre-HLD` confirmation pending). The R1 measurement plan budgets ≤ 600 µs for the slowest-of-3 intra-AZ round trip; a sustained intra-AZ RTT above 250 µs invalidates the GET QUORUM p99 < 1 ms budget.
- **A-Hardware.** Per data node: 4 vCPU, 8 GiB RAM, NVMe-SSD (NFR-SCALE-3). Per coordinator node: same plus the **OD-7 fio floor of ≥ 3,000 IOPS sustained 4 KiB random write with fsync**. Volumes that fail the floor are unsupported and rejected by the FR-13 bootstrap pre-flight.
- **A-Working-set.** v1 cluster fits in RAM at the 128-node / 1 B-key / ≤ 1 MiB-value ceiling (NFR-SCALE-4..6). The LLD publishes the per-node sizing formula (R5 mitigation).
- **A-Workload.** Read/write mix 80/20, hot-key QPS ≤ 1k ops/s sustained per key (NFR-SCALE-3a). Above that the cache layer absorbs reads; writes contend at the home replica set.
- **A-PKI.** Operators provide their own PKI via `disk` or `k8s-secret` `pki.Source` (FR-6). cert-manager users get free integration via the `k8s-secret` source.
- **A-Backup.** One operator-selected backup destination (`s3` or `postgres`) per cluster, shared by data-node snapshots (FR-18) and coordinator Raft snapshots (FR-7) (Q7 / FR-12).
- **A-Time.** Wall-clock skew between nodes ≤ 500 ms is tolerable for OTel timestamp ordering and audit-log ordering. **The vector-clock total order does not depend on wall-clock** (FR-4 / Q5 rejected-alternatives) — wall-clock skew never silently loses writes.
- **A-Single-tenant.** One cluster = one logical workload (NG6 / Q18). No per-tenant authorisation surface to design around in v1.

## 4. System context

Five external systems interact with a GossamerDB cluster:

| External system               | Direction      | Role                                                                                                                | Trust boundary             |
| ----------------------------- | -------------- | ------------------------------------------------------------------------------------------------------------------- | -------------------------- |
| Application clients (Go SDK)  | inbound        | Call `Put` / `Get` / `Delete` on the wire (gRPC primary, REST via Fiber secondary).                                 | mTLS `client.<cluster>`    |
| Application clients (non-Go)  | inbound        | Same wire, no smart routing — go through the L4 LB and the any-data-node forwarding fallback (FR-8 tier 3).         | mTLS `client.<cluster>`    |
| Operators (`gossamerctl`)     | inbound        | Drive admin RPCs (FR-13) — bootstrap, partition map dump, anti-entropy trigger, rolling upgrade, region promotion.   | mTLS `admin.<cluster>`     |
| Backup destination            | outbound       | `s3` (object storage) or `postgres` (`bytea` table). Stores coordinator Raft snapshots and data-node snapshots.      | mTLS / IAM as configured   |
| OpenTelemetry Collector       | outbound       | OTLP gRPC export of metrics + traces + logs; the Collector fans out to Prometheus / Tempo / Loki for Grafana display. | mTLS or in-cluster ServiceAccount |

`docs/prds/diagrams/system-topology.svg` is the authoritative system-topology diagram and is referenced unmodified from the PRD §6.1.1.

The cluster itself is composed of two long-lived node groups (described in §5) plus an optional cross-instance Redis cache. Plain L4 load balancers (NLB on AWS, k8s `Service` in Kubernetes mode) front the data-plane gRPC + REST listeners; **no L7 routing is required or supported in v1** (FR-8 deferred-alternatives — token-aware L7 LBs deferred to v1.x).

## 5. Major components

The HLD names eight major components. Each one is bounded, has a single owner concept, and is the LLD's input for a corresponding package + interface set. Package paths are LLD-bound and not specified here.

### 5.1 Coordinator (3-node embedded-Raft control plane)

- **Owns:** cluster membership canonical view, partition map (epoch-stamped), strategy config (gossip + conflict), rolling-upgrade orchestration, region-promotion / reseed orchestration, admin API (FR-13).
- **Why a 3-node Raft group:** single point of failure unacceptable (NFR-AVAIL-1); external etcd adds an ops dependency the project explicitly rejects (Q2 rejected-alternatives); in-house consensus is malpractice. 3-node Raft tolerates 1-of-3 failures and gives RTO < 30 s / RPO 0 (NFR-AVAIL-3).
- **Strictly off the per-request path** (FR-7 / FR-8 / Q3). A complete coordinator outage pauses control-plane mutations only; foreground reads / writes continue.
- **Library choice:** `hashicorp/raft` or `etcd-io/raft` — selected in the LLD by criteria of (a) snapshot/restore ergonomics for the FR-7 cadence (10k entries or 1 GiB), (b) library mTLS integration cleanliness, (c) maintenance velocity.
- **Durability split:** Raft commit log is local-disk-only on each coordinator (Raft requires synchronous fsync per commit for soundness); snapshots ship to the operator-selected backup destination (FR-7 / FR-12).
- **Cross-references:** `docs/prds/diagrams/coordinator-raft.svg` (PRD §6.1.2).

### 5.2 Data node (in-memory KV + cache + gossip + anti-entropy + client surfaces)

- **Owns:** the in-memory key-value store (sharded `sync.Map` family — exact structure LLD-bound), the in-process cache (sub-component, §5.6), the per-key vector-clock attachment, the conflict-resolution invocation site, the gRPC + Fiber REST surfaces, the gossip participant, the anti-entropy participant.
- **Why in-memory only in v1:** PRD §11 Q7 — cluster fault tolerance via `N=5/W=3/R=3` + Merkle anti-entropy. Single-node restarts hydrate via peer anti-entropy. Full-cluster outage covered by snapshots (FR-18). Pluggable durable backend (Pebble) deferred to v1.x (R5).
- **Per-node RAM caps:** the LLD publishes the sizing formula; HLD-level rule: `replicas_owned × (avg_key + avg_value) + cache_overhead + gossip_overhead + anti-entropy_overhead`. Each component reports its own RAM in metrics (FR-11) so operators can see the budget in real time.

### 5.3 Gossip (region-aware SWIM, mandatory + Plumtree, optional)

- **SWIM (always on, region-aware variant).** Membership + failure detection. Within-region full-fanout probes; cross-region reduced-fanout probes. The `region` tag rides every gossip message so peers apply region-aware policy without a separate WAN serf. LLD locks intra/inter-region probe periods and indirect-probe fanouts.
- **Plumtree (operator-enabled, layered on SWIM's view).** Eager-push spanning tree for bulk dissemination of partition-map updates, strategy-version changes, region-epoch bumps. Lazy-push `IHAVE` for off-tree repair. **No separate HyParView in v1** — Plumtree builds its tree directly over the SWIM membership view. HyParView deferred to v1.2 once cluster sizes or churn justify it (FC-3-adjacent).
- **Layering invariant:** Plumtree is **always layered on top of SWIM, never instead of it** (FR-3). When Plumtree is disabled, partition-map / region-epoch updates ride SWIM piggyback as the v1 fallback.
- **Strategy abstraction:** the gossip "strategy" is a chain of layers (e.g. `["swim"]` or `["swim", "plumtree"]`). Authoring a new layer in v1.x (HyParView, alternative FD) does not break the abstraction.
- **Region-epoch propagation (A7).** Same wire shape as partition-map epoch — a scalar carried on every gossip message. A `gossamerctl promote` bumps `region_epoch`; the bump rides the active layer chain and reaches every node within the same convergence window as a partition-map update (target p99 < 10 s on a 32-node cluster, bench-gated).

### 5.4 Anti-entropy (per-range Merkle tree)

- **Owns:** convergence between replicas without taking the cluster offline. Per-range Merkle trees compared on a configurable cadence (default 5 min with jitter); divergent ranges reconciled via the active conflict-resolution strategy.
- **Bounded background work** (NFR-PERF-3): default ≤ 5% per-node CPU, ≤ 10% NIC. Bench-gated by `BenchmarkAntiEntropyUnderLoad` (DM-2 fix).
- **Hydration-burst exception (A3):** a freshly-rejoined node runs at 50% CPU / 50% NIC for ≤ 10 min (ceiling 30 min), tagging itself `node_role=hydrating` so peers throttle outbound to it. Bench-gated by `BenchmarkAntiEntropyHydrationBurst`.
- **Sibling preservation:** under the `siblings` strategy, anti-entropy preserves all siblings; only a descendant write collapses them (FR-4).

### 5.5 Conflict resolution (`lww` default, `siblings` opt-in)

- **Vector clocks** are attached to every write (per-key `(nodeId, counter)` map). Total order is **lexicographic over sorted entries** (LLD locks the comparator); no wall-clock participates (FR-4 / Q5 rejected-alternatives — wall-clock LWW silently loses writes under skew).
- **`lww`:** highest vector clock wins under the deterministic total order.
- **`siblings`:** divergent values returned in a single payload with their VC contexts; client collapses by writing back with a clock that descends from all siblings (Riak-style).
- **No app-supplied merge function in v1.** `siblings` is the supported path (Q5 rejected-alternatives — keeps the strategy surface minimal).
- **Strategy is bootstrap-only**, restart-pace swap (Q13). Hot-swap deferred to v1.1.

### 5.6 Cache layer (in-memory single-instance + Redis cross-instance)

- **In-memory single-instance** (`sync.Map`-backed, in-process per data node): default TTL **100 ms**, ceiling **500 ms** (A1 / NFR-PERF-2).
- **Redis cross-instance:** same key shape + TTL semantics; covers hot keys served by any data node in the region.
- **Cache key:** `(key, partition_map_epoch)` — never `(key, vector-clock-hash)` because the latter changes on every write and defeats the cache under hot-key traffic at NFR-SCALE-3a (1 k QPS/key).
- **Eligibility:** both `R=ONE` and successful `R=QUORUM` (zero siblings) populate. `siblings`-mode divergent results never populate.
- **Mandatory invalidation paths:** local Put/Delete, gossip DELETE, anti-entropy repair touching the key, partition-map epoch bump (automatic — epoch is part of the key). TTL is the safety net for dropped invalidations.
- **0-allocation contract on cache hit** (NFR-PERF-4): `sync.Pool` buffers, pre-sized maps; bench-gated.

### 5.7 PKI / mTLS (`pki.Source` interface — `disk` + `k8s-secret`)

- **`pki.Source` interface** abstracts certificate / key / trust-bundle lookup + hot-reload. v1 ships **`disk`** (fsnotify-watched files) and **`k8s-secret`** (k8s Secret reference, watch-API-watched). cert-manager integrates "for free" via `k8s-secret`.
- **`gossamerctl dev-pki`** ships for local dev (refuses non-local).
- **Hot-reload contract:** new certs apply at the next TLS handshake; in-flight sessions keep their old creds — zero downtime. Reload events emit to the audit log (FR-16).
- **Identity scheme (SAN-based, cluster-embedded):** `coordinator.<cluster>`, `data.<cluster>.<region>`, `admin.<cluster>`, `client.<cluster>`. Cluster-name SAN check on every handshake prevents cross-cluster cert reuse.
- **Built-in CA permanently rejected** (FR-6 / Q8 — chicken-and-egg trust on bootstrap; CA responsibility is a different product). SPIFFE / Vault deferred to v1.x as additional `pki.Source` implementations (FC-6).

### 5.8 Smart Go SDK (token-aware client routing)

- **Primary path** for the GET p99 < 1 ms request-level budget (FR-8 tier 1 / FR-20).
- **Owns:** local copy of the partition map + cluster epoch + `region_epoch`; key-hash + replica selection; gRPC stream pinning per key range; `WRONG_OWNER(epoch=X, owners=[...])` redirect handling (re-fetch + retry once); `WRONG_REGION(primary=<region>)` redirect handling (transparent retry against the primary); typed named-consistency enum (`ONE` / `QUORUM` / `ALL` — numeric tuples are a compile error, not a runtime check); `partition_map_thrash_total` metric on second-strike `WRONG_OWNER` (A4 fallback to FR-8 tier 3); `region_epoch_lag_seconds` metric (DM-7 convergence SLO ≤ 20 s p99).
- **Hard upstream LLD dependency** (DM-19): the partition-map wire schema (`pkg/api/partition/v1/partition.proto`) and `region_epoch` schema must be locked before any FR-20 story enters the Project Lead's branch-cutting plan. SDK story is **branch-cut-blocked** until the proto is signed off.
- **Connection-context principal pinning** (R10 / FR-20.h): `principal` derived once at TLS handshake in the gRPC interceptor, pinned to the connection context, read on every request as a pointer-equal lookup (zero allocation).

## 6. Data flow

This section describes four canonical flows. SVG diagrams in `docs/prds/diagrams/` are authoritative; the HLD restates the flow in prose plus the architectural budget that justifies the latency claim.

### 6.1 GET request (smart-SDK primary path, R=QUORUM)

`docs/prds/diagrams/get-lifecycle.svg` is the authoritative diagram (PRD §6.2.1).

1. Client SDK hashes the key → picks one of the 5 owning replicas (the **request coordinator** — lowercase, distinct from the Raft Coordinator) → opens / reuses a pre-warmed mTLS gRPC stream. **One client-to-cluster hop.**
2. Request-coordinator validates ownership against the gossip-current partition map. If the SDK's epoch is stale, returns `WRONG_OWNER(epoch=X, owners=[...])` and the SDK refreshes + retries once. If the second attempt also returns `WRONG_OWNER` (topology churn — A4), the SDK falls back to FR-8 tier 3 and increments `partition_map_thrash_total`.
3. **Cache check.** If hit and `R=ONE`, return — sub-budget cache-hit p99 < 500 µs. Otherwise read locally **and in parallel** issue reads to the other 4 owners. **Cache also populates on `R=QUORUM` results with zero siblings** (A1).
4. **Wait for fastest 3 of 5** responses, **cancel slowest 2** (cancellation is what makes the budget — see §6.1.1 below). Reconcile via vector clocks. Apply conflict strategy: under `lww` pick the highest VC; under `siblings` collect divergent values + VC contexts.
5. Populate cache (single-value results only); return. OTel span closed; gRPC trailers carry the VC context back to the client.

#### 6.1.1 R1 measurement plan — how GET p99 < 1 ms clears at R=3 of 5 (binding)

The PRD R1 mitigation budgets the GET request-level path as four explicit substeps. The HLD locks them as bench-gate sub-budgets:

| Substep                                                          | p99 sub-budget | Bench-gated by                                                                                       |
| ---------------------------------------------------------------- | -------------- | ---------------------------------------------------------------------------------------------------- |
| 1. Dispatch (parallel-future creation + connection acquire)      | ≤ 50 µs        | `BenchmarkGetQuorumRequestLevel` substep timing exported as histogram                                |
| 2. Slowest-of-3-of-5 intra-AZ network round-trip                 | ≤ 600 µs       | Same bench, network sub-timing under saturation                                                      |
| 3. Vector-clock reconcile + conflict-resolution invocation        | ≤ 100 µs       | Zero-alloc reconcile (NFR-PERF-4); `O(N)` over the 3 winning clocks under `lww`                       |
| 4. Response marshal + trailer serialisation                      | ≤ 50 µs        | Pre-allocated proto buffers via `sync.Pool`                                                          |
| **Total**                                                        | **≤ 800 µs**   | Headroom against 1 ms ceiling absorbs jitter; gate fails on any p99 over 1 ms                        |

Architectural hard requirements that flow from this plan:

- **Pre-warmed gRPC connection pool** to all 4 peer owners maintained by every data node (eager dial on partition-map epoch bump; LRU close at idle ceiling). LLD locks pool size + idle policy.
- **Parallel dispatch via `Future`-style fan-out** (Go pattern: `chan` of result + `errgroup` with cancellation). The first 3 successful responses cancel the remaining 2 via gRPC `context.Cancel`; cancelled peer reads MUST short-circuit before issuing a downstream read against their own KV map (LLD locks the cancellation propagation point).
- **Zero-allocation reconcile path** via `sync.Pool` for VC merge buffers; pre-sized maps for sibling collection.
- **`R=ONE` skips the fan-out entirely** (cache or local read only) and rides the cache-hit < 500 µs sub-budget.

### 6.2 PUT request (smart-SDK primary path, W=QUORUM)

`docs/prds/diagrams/put-lifecycle.svg` is the authoritative diagram (PRD §6.2.2).

1. SDK hashes key → request coordinator (same as GET).
2. Generate / advance the per-key vector clock (`(nodeId, counter++)`).
3. Write locally **and in parallel** to the other 4 owners. **Wait for fastest 3 commits** (`W=3 of 5`); cancel pending slowest-2 writes — cancelled writes MUST be idempotent against the local KV map (the cancellation arrives after the local write may already have succeeded).
4. **Cache invalidation** in this order: local in-memory cache, gossip DELETE for the cross-instance Redis cache, partition-map-epoch bump (auto-invalidates by virtue of the cache key shape).
5. **Idempotency dedup** at the request-coordinator replica: if the client-supplied `request_id` is in the in-memory dedup map, return the dedup ack with the original VC. Dedup state is in-memory and **does not survive a node restart** (A2 / FR-15) — `idempotency_persisted` metric flagged false; the post-restart contract is "may apply twice within the window," with `idempotency_dedup_miss_total` exported (DM-4).
6. Return. PUT p99 ≤ 5 ms (replication-bound).

### 6.3 Cache invalidation (cross-instance)

`docs/prds/diagrams/cache-invalidation.svg` is the authoritative diagram (PRD §6.3.1). The HLD adds one architectural note: the Redis cross-instance cache is **invalidated, not updated** on Put/Delete. Updating would force a full value to ride gossip; invalidating sends only the key + epoch tuple. The next Get re-populates from the read path. This is what keeps cross-instance gossip cheap under hot-key write traffic.

### 6.4 Anti-entropy repair

Per-range Merkle trees compared on cadence. When a divergence is detected:

1. The pair of peers exchanges the divergent leaf range.
2. Each peer applies the active conflict-resolution strategy to reconcile.
3. The reconciled state is written locally; **the cache is invalidated for every touched key** (FR-12 mandatory invalidation path).
4. Bounded throughput: ≤ 5% CPU / ≤ 10% NIC steady-state; ≤ 50% / ≤ 50% during hydration burst with the `node_role=hydrating` tag.

### 6.5 Cross-region replication (active-passive only in v1)

Primary-region writes commit locally and ack the client; replication to passives is **asynchronous** via the active gossip layer chain (Plumtree when enabled, SWIM piggyback otherwise). Lag target p99 < 2 s under ≤ 100 ms inter-region p99 RTT (NFR-SCALE-8). Backpressure under prolonged passive outage (R12): outbound queue per passive is bounded at **500 MiB or 30 s of writes, whichever is smaller**; on cap breach, drop oldest with audit-log emission (`event_type=cross_region_drop`); recovery from long outage = full reseed (`gossamerctl reseed`), not catch-up replay.

`docs/prds/diagrams/multi-region-topology.svg` (PRD §6.4.1) is the authoritative diagram.

## 7. APIs and integration points

| Surface                | Transport     | Auth                                  | Stability         | Owner section |
| ---------------------- | ------------- | ------------------------------------- | ----------------- | ------------- |
| Client KV (primary)    | gRPC + mTLS   | `client.<cluster>` SAN                | semver, additive within major (NFR-API-1) | §5.2 / §5.8 |
| Client KV (secondary)  | REST via Fiber + mTLS | `client.<cluster>` SAN          | 1-to-1 with gRPC; same semver rule        | §5.2          |
| Admin                  | gRPC + mTLS   | `admin.<cluster>` SAN                 | 15-RPC contract locked in PRD §FR-13 (DM-9 fix); proto in `pkg/api/admin/v1/admin.proto` | §5.1          |
| Inter-node RPC         | gRPC + mTLS   | `data.<cluster>.<region>` SAN          | Internal — semver minor bumps for additive fields | §5.2 / §5.3 |
| Gossip wire            | Custom UDP/TCP framing over mTLS | `data.<cluster>.<region>` SAN | LLD-locked; layered envelope so SWIM and Plumtree share a frame | §5.3          |
| OTLP gRPC export       | OTLP gRPC over mTLS | (collector trust)               | OTel-defined                              | §12           |
| Backup destination     | S3 SDK / Postgres SDK | IAM / DB creds                  | LLD-locked manifest schema                | §5.1 / §5.2   |

**Strategy extension points** (NFR-API-2 — Go module semver, additive interface methods become major bumps):

- `internal/gossip.Strategy` — chain of layers (`["swim"]` / `["swim", "plumtree"]`).
- `internal/conflict.Resolver` — `lww`, `siblings`, future contributions.
- `internal/storage.Backend` — in-memory in v1; Pebble / RocksDB / S3-tiered in v1.x.
- `internal/pki.Source` — `disk`, `k8s-secret` in v1; SPIFFE / Vault in v1.x.
- `internal/backup.Destination` — `s3`, `postgres` in v1; future destinations additive.

`pkg/api/` houses the public wire schemas (`partition/v1`, `admin/v1`, `client/v1`); `internal/...` houses everything else. Detailed package layout is LLD-bound.

## 8. Data model overview

GossamerDB stores **opaque byte values keyed by opaque byte keys**. Keys ≤ 1 KiB, values ≤ 1 MiB (NFR-SCALE-5 / NFR-SCALE-6). No schema, no secondary indexes, no sort order, no namespaces in v1 (NG6). Each record carries:

- The key bytes (immutable).
- The value bytes (mutable on Put; nil-on-Delete with a tombstone).
- A **per-key vector clock** as a sorted `(nodeId, counter)` map.
- A **tombstone flag** for Delete records, retained by anti-entropy long enough to outlive cluster-wide convergence.
- An **arrival epoch** = the partition-map epoch at write time, used in cache key construction (§5.6).

### 8.1 Partition ring

- **Consistent hashing with virtual nodes** (PRD §5 stack table). Every key hashes to a position on the ring; the **N=5 successors** at that position are the replicas.
- **Vnode-count selection (DM-18 PRD §5.1 — locked at HLD level).** The HLD locks **128 vnodes per data node**; at the 128-node ceiling that is **16 384 vnodes cluster-wide**, comfortably inside the PRD constraint `[1024, 32768]`.

  - **Serialised payload calculation (≤ 256 KiB at 128-node ceiling).** Per vnode: `token uint64` (8 B) + `owner_node_id uint32` (4 B) + `replicas []uint32` of `N=5` peers (20 B) = 32 B raw. With group-varint delta encoding on the sorted `token` column and zigzag-varint on the `replicas` column (LLD locks the exact wire encoding), the per-vnode amortised cost lands at **≤ 12 B**. Total: `16 384 × 12 B ≈ 192 KiB`. Headroom against the 256 KiB ceiling: ~25%.
  - **Imbalance analysis (< 1% on single-node loss).** Lose 1 of 128 nodes → its 128 vnodes are absorbed by the other 127 (≈ 1.008 vnodes each on average). Each surviving node moves from 128 vnodes to ~129 vnodes — an added load fraction of ~0.78%. Well under the 1% bar. (For comparison: 64 vnodes/node would land at ~3% and 32 vnodes/node at ~6% — both above the bar.)
  - **Bench gate:** `BenchmarkPartitionMapSerialization` (PRD §5.1) asserts the serialised partition-map at 128 nodes serialises in ≤ 5 ms and lands at ≤ 256 KiB. **GA-blocking.**
  - **Uniform per-node** (no weighted placement in v1 — PRD §5.1 #3).
  - **Bootstrap-time only** — no hot-resize (PRD §5.1 #4); a v1.x effort.

- **Replica placement** is the standard "next N successors on the ring" rule. Region-aware replica preference (e.g., always prefer the 5 closest replicas in the same region for intra-AZ latency) is LLD-locked.

### 8.2 Coordinator metadata model

The Raft Coordinator group's state machine holds:

- Cluster name + cluster ID.
- Membership (canonical view) — node ID, region, role, status (Alive / Suspect / Dead / Hydrating), last-seen, version.
- Partition map (current epoch + last K archived epochs for `WRONG_OWNER` redirect serving).
- Strategy config — gossip strategy chain, conflict strategy, `N`/`W`/`R` numerics, `cross_region.mode`, primary region, passive regions, region-epoch.
- Rolling-upgrade state (in-progress / paused / done; per-node target version).
- Rate-limit config (FR-19, hot-reloadable via `UpdateRateLimit`).
- PKI source identity (where to read certs from — not the certs themselves).

LLD locks the precise Raft-log entry types and snapshot serialisation.

## 9. Scalability expectations

PRD NFR-SCALE-1..8 and NFR-PERF-1..4 are binding ceilings; the HLD restates them with the architectural mechanisms that earn each one:

| Target                                                                  | PRD ID            | Mechanism (HLD)                                                                                                                         |
| ----------------------------------------------------------------------- | ----------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| 128 data nodes per cluster                                              | NFR-SCALE-1       | Partition-map size budget at 128 nodes drives the vnode-count selection (§8.1).                                                          |
| ≥ 1 M ops/s sustained per cluster                                       | NFR-SCALE-2       | Smart-SDK direct routing (§6.1) avoids the L4-LB-then-forward hop that an any-data-node-only architecture would force.                   |
| ≥ 8 k ops/s per node                                                    | NFR-SCALE-3       | Sharded in-memory store (§5.2) + zero-alloc cache hit (§5.6); LLD locks shard count.                                                     |
| ≥ 1 k ops/s per hot key                                                 | NFR-SCALE-3a      | Cache layer in front of the read path keyed by `(key, partition_map_epoch)` (§5.6); writes contend at the home replica set.              |
| ≤ 1 B keys per cluster                                                  | NFR-SCALE-4       | Cluster fits in RAM at the 128-node ceiling under the per-node sizing formula (§5.2 + R5).                                               |
| ≤ 1 MiB value, ≤ 1 KiB key                                              | NFR-SCALE-5/6     | Hard limit at the wire — `INVALID_ARGUMENT` on overflow.                                                                                 |
| `N ∈ {3, 5}`, default 5                                                 | NFR-SCALE-7       | Vector-clock + 3-tier routing both work at either N; benches ship `-tags=n3` variants for the DM-17 acceptance contract.                 |
| ≤ 100 ms inter-region p99 RTT, < 2 s replication lag                    | NFR-SCALE-8       | Async replication via the gossip layer chain (§5.3 / §6.5); lag bench-gated.                                                            |
| GET p99 < 1 ms request-level                                            | NFR-PERF-1a       | R1 measurement plan (§6.1.1).                                                                                                            |
| PUT p99 ≤ 5 ms request-level                                            | NFR-PERF-1b       | Parallel-write-fastest-3-of-5 + cancellation of slowest 2 (§6.2).                                                                        |
| Cache mandatory on < 1 ms paths; explicit invalidation                  | NFR-PERF-2 / A1   | §5.6 + §6.3.                                                                                                                             |
| Anti-entropy & gossip bounded background work                           | NFR-PERF-3        | §5.4 + bench gates `BenchmarkAntiEntropyUnderLoad` / `BenchmarkAntiEntropyHydrationBurst` (DM-2).                                        |
| 0 allocations on cache-hit                                              | NFR-PERF-4        | `sync.Pool` buffers + pre-sized maps (§5.6).                                                                                             |

## 10. Reliability and failure modes

### 10.1 Failure modes (HLD-exhaustive at the cluster level)

| Mode                                              | Detection                                  | Response                                                                                                            | Budget / SLO                                       |
| ------------------------------------------------- | ------------------------------------------ | ------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------- |
| Single data-node crash                            | SWIM Suspect → Dead                        | Replica's slice rebalances on next partition-map epoch; anti-entropy re-converges peers; SDK gets `WRONG_OWNER` once. | NFR-AVAIL-2 (99.99% R=ONE / 99.95% R=QUORUM)       |
| Up to 2 simultaneous replica crashes per range    | SWIM                                       | Cluster keeps serving at `W=3 / R=3` (3-of-5 majority).                                                              | NFR-AVAIL-4                                        |
| Coordinator node crash (1 of 3)                   | Raft heartbeat timeout                     | Election within RTO < 30 s; foreground traffic untouched.                                                            | NFR-AVAIL-1 / NFR-AVAIL-3                          |
| Coordinator group total outage                    | Raft no-leader                             | Control-plane mutations pause; data-plane reads/writes continue.                                                     | Q2 commitment                                      |
| Coordinator Raft-log disk too slow                | Bootstrap pre-flight `fio` probe (FR-13)   | Refuse to initialise; clear error.                                                                                   | OD-7 floor (3 k IOPS sustained 4 KiB)              |
| Smart-SDK partition-map staleness                 | Server `WRONG_OWNER`                       | SDK refresh + retry once; second strike → tier-3 fallback (A4).                                                       | R9 mitigation; ≤ 1.5 ms p99 during churn windows   |
| Cross-region passive outage                       | Replication queue saturation               | Bound queue at 500 MiB / 30 s; drop oldest + audit; recovery via `gossamerctl reseed`.                                | R12 mitigation                                     |
| `WRONG_REGION` redirect storm (post-promote)      | SDK `region_epoch_lag_seconds`             | Transparent retry to new primary; convergence ≤ 20 s p99 (DM-7).                                                     | `TestSDKPostPromoteConvergence` GA gate            |
| Rolling-upgrade incompat                          | Per-PR upgrade-skew CI (R7)                | Pre-merge gate; production-side per-key drain ≤ 5 s (NFR-AVAIL-5).                                                    | R7 mitigation                                      |
| Sustained `WRONG_OWNER` thrash (topology churn)   | `partition_map_thrash_total` metric         | SDK falls back to tier-3 forwarding for that single request; subsequent requests on refreshed map.                    | A4 fallback                                        |
| Anti-entropy starves foreground                   | `BenchmarkAntiEntropyUnderLoad` regression | Bench gate fails the PR before merge.                                                                                | NFR-PERF-3 / DM-2                                  |
| Hot-principal rate-limit contention               | `BenchmarkHotPrincipalGet` regression      | Bench gate; sharded buckets (R11 mitigation).                                                                        | R11 mitigation                                     |
| Idempotency dedup miss after restart              | `idempotency_dedup_miss_total` metric      | Documented as expected (A2 / FR-15); operators size their own application-layer ledger.                              | DM-4 fix                                           |
| Cluster-wide outage / accidental delete-all        | Operator-driven                            | Restore from `gossamerctl snapshot` (FR-18) into the operator-selected backup destination.                            | `TestSnapshotRestoreRTO` (DM-10 — 60 min total)    |

### 10.2 Reliability invariants (HLD-binding)

- **Raft Coordinator is never on the per-request path.** Any LLD that re-introduces a coordinator hop on the data plane is rejected.
- **No write that has been ack'd to the client is silently lost.** Vector-clock total order + `siblings`-mode preservation enforce this; LWW under VC total order is deterministic and never wall-clock-skew-driven (Q5 rejected-alternatives).
- **No read returns silently stale data past the cache TTL ceiling (500 ms).** Explicit invalidation is primary; TTL is the dropped-message safety net (A1).
- **No cross-cluster cert reuse.** Cluster-name SAN check is enforced on every handshake (FR-6).
- **No automatic region failover in v1.** `gossamerctl promote` is operator-gated and fences the old primary (R4 / Q11). Active-active and auto-failover are v1.2 (FC-3).

## 11. Security and compliance considerations

`docs/prds/diagrams/mtls-pki.svg` (PRD §6.7.1) is the authoritative mTLS / PKI flow diagram.

### 11.1 mTLS posture (FR-6 / NFR-SEC-1)

- Every TCP listener requires mTLS. **Plaintext listeners are not buildable into the binary** — there is no `--insecure` flag in v1. The build itself enforces this (a unit test asserts no plaintext listener constructor exists in the binary; LLD-locked).
- `pki.Source` interface (§5.7) abstracts cert lookup. `disk` + `k8s-secret` ship in v1; SPIFFE / Vault are v1.x.
- Hot-reload is **mandatory** for cert rotation — restart is not a documented rotation procedure.
- Identity scheme is SAN-based with cluster-name embedded; cluster-name SAN check on every handshake. The SAN format reserves an OU slot (`/role=<r>`) for v1.1 RBAC — v1 ignores it but cert-issuing operators can pre-tag identities today (Q9 forward-compat hook).
- **`principal` derivation is handshake-time only.** Per-request principal is a pointer-equal connection-context lookup — zero allocation, sub-100 ns (R10 / NFR-PERF-4).

### 11.2 Audit (FR-16)

Distinct OTel log stream `gossamer.audit` carries:

- Admin-API calls.
- mTLS handshake failures (with the rejecting reason: expired cert / unknown CA / SAN mismatch / cluster-name mismatch).
- Config changes, strategy changes, rolling-upgrade events.
- Cert reload events (every fsnotify- or k8s-Secret-driven reload, including new cert NotBefore / NotAfter / SAN list).
- Region promotion / reseed events.
- Every data-plane mutation (`Put`, `Delete`) at sampled cadence — **default sample rate 0%** (off) to keep the < 1 ms GET p99 budget clean.

Stable schema fields per record: `ts`, `event_type`, `principal`, `cluster`, `region`, `key_hash` (never raw key — privacy), `outcome`, `reason`.

### 11.3 Compliance (NFR-SEC-5 / Q10 / OD-2)

- **No certifications in v1.** Public positioning: "audit-ready, not audited."
- `docs/security/controls.md` ships in v1, mapping controls to **SOC2 CC-series**: CC6.1 logical access → mTLS + SAN; CC6.6 encryption-in-transit → FR-6; CC6.7 confidential-info transmission → cluster-name SAN check; CC7.2 system monitoring → FR-16 audit log; CC7.3 anomaly detection → handshake-fail metrics.
- **Cluster is the residency primitive (NFR-SEC-7 / §6.5).** Single-region by default (`cross_region.mode = none` — no WAN traffic); cross-region is opt-in via `cross_region.mode = active_passive` + explicit `passive_regions` list. No per-row residency tagging in v1 (NG10).
- **GDPR right-to-erasure** is the operator's data-model responsibility; v1 supplies the primitives (`Delete(key)` per FR-1, audit attribution per FR-16, tombstone-respecting anti-entropy per FR-5).

### 11.4 Threat model (HLD-level)

| Threat                                                         | Mitigation                                                                                                                            |
| -------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------- |
| Cross-cluster cert reuse                                       | Cluster-name SAN check on every handshake (FR-6 identity scheme).                                                                     |
| Plaintext-listener regression                                  | No-plaintext-constructor build assertion (LLD-locked); `security/no_plaintext_test.go` in CI (J-S-1).                                 |
| Cert-private-key leak via OTel                                 | `no key material in OTel` test + lint rule (NFR-SEC-6).                                                                              |
| Capability-token replay                                        | Out of scope for v1 — capability tokens explicitly rejected (Q9 rejected-alternatives); mTLS identity is the only auth surface.       |
| Rate-limit bucket-contention DoS                                | Sharded buckets per principal (R11 mitigation); `BenchmarkHotPrincipalGet` gate.                                                       |
| Split-brain on region promotion                                | Operator-gated `gossamerctl promote` + fenced old primary until `gossamerctl reseed` (R4 / Q11).                                       |
| Replicated-state divergence after passive-region reseed         | Anti-entropy convergence after reseed; bench-gated; `TestSnapshotRestoreRTO` analogue runs as a §11 acceptance scenario.              |

## 12. Observability

`docs/prds/diagrams/system-topology.svg` shows the OTel export wire; the obs sub-system itself is detailed in PRD §FR-11 / FR-17 / Q16 / NFR-OPS-1 / NFR-OPS-4.

### 12.1 Pipeline (Q16 — locked)

`GossamerDB → OTLP gRPC (single wire for metrics + traces + logs) → OpenTelemetry Collector → fan-out: Prometheus (metrics) + Tempo (traces) + Loki (logs) → Grafana (three-panel cross-correlated dashboard)`. Prometheus `/metrics` scrape kept as a secondary back-compat path. Logs use OTLP logs (OTel-native) with stdout-JSON fallback.

### 12.2 Metrics (HLD-binding minimum set)

Inherited from PRD §FR-11 / FR-19 / FR-20 / FR-21 / DM-4 / DM-7, the cluster MUST emit at minimum:

- **Request path (RED):** `request_duration_seconds{op,consistency,result}`, `request_total{op,consistency,result}`, `request_in_flight`.
- **Cache:** hit-rate, eviction-count, TTL-expiry-count, allocation-count (NFR-PERF-4 evidence).
- **Gossip:** convergence-time, message-count by layer, suspect-state count.
- **Anti-entropy:** bytes-repaired, foreground-CPU-share, NIC-share, `node_role=hydrating` gauge.
- **Coordinator:** Raft term, leader ID, log-index, snapshot-cadence-seconds, fsync-latency-seconds.
- **PKI:** `mtls_cert_expires_in_seconds`, handshake-fail-count by reason.
- **Cross-region:** `cross_region_writes_total`, `cross_region_reads_total`, `cross_region_replication_lag_seconds`, `cross_region_replication_backlog_bytes`, `region_role`, `region_epoch_lag_seconds`.
- **SDK:** `partition_map_thrash_total`, `idempotency_persisted{result}`, `idempotency_dedup_miss_total`.
- **Rate limit:** `rate_limit_tokens_consumed_total{principal}`, `rate_limit_shard_contention_seconds`, `rate_limit_violations_total{principal,action}`.

### 12.3 Traces

W3C Trace Context propagated via gRPC metadata + HTTP headers. One parent span per client request + one child per fan-out leg. Span attributes include `gossamer.principal` (R10), `gossamer.consistency`, `gossamer.partition_map_epoch`, `gossamer.region_epoch`. The reference Grafana dashboard's three-panel layout pivots from a slow span to its logs in one click (J-O-5 / NFR-OPS-4).

### 12.4 Logs

OTLP logs (OTel-native) for operational logs. The audit stream `gossamer.audit` (§11.2) is a distinct logical pipeline routed through the same OTLP wire — operators query it via Grafana / Loki.

### 12.5 Reference deployment (FR-17)

`deploy/observability/` ships docker-compose + Helm sub-chart, the OTel Collector pipeline config, the Grafana 3-panel dashboard, and Prometheus / Tempo / Loki minimal configs. **Configs only — never redistribute upstream binaries.**

## 13. Deployment and environments

The same single binary set (`coordinator` + `datanode` + `gossamerctl`) ships across all three modes; **only configuration differs** (NFR-PORT-1).

| Mode             | Topology                                                       | Mode-specific config                                                                                  |
| ---------------- | -------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| Local            | Single host; ports allocated by config; `gossamerctl dev-pki`. | `cluster.deployment_mode: local`; PKI source `disk` pointing at dev-pki output.                       |
| Kubernetes       | StatefulSet for data nodes; separate StatefulSet for the 3-node coordinator group; headless services for peer discovery. | `cluster.deployment_mode: k8s`; PKI source `k8s-secret`; cert-manager `Certificate` resources optional. |
| Multi-region AWS | EKS-based (k8s mode generalised). Region-aware SWIM gossip; per-cluster `cross_region.mode`. | `cluster.deployment_mode: k8s` + `cluster.cloud=aws` + `cross_region.mode={none, active_passive}`.    |

### 13.1 Bring-up SLOs (J-O-1 — DM-1 GA gate)

- Local: ≤ 60 s (`TestClusterBringUpLocal` — every PR).
- Kubernetes: ≤ 3 min (`TestClusterBringUpK8s` — nightly + pre-merge `develop`).
- Multi-region AWS: ≤ 10 min (`TestClusterBringUpMultiRegionAWS` — pre-tag on `release/*`).

Readiness contract identical across the three: `coordinator_quorum=true ∧ all_data_nodes_alive=true ∧ partition_map_epoch>0 ∧ gossip_converged=true` for two consecutive 5 s polls. Image-pull excluded via pre-pull harness.

### 13.2 Rolling upgrade (FR-10 / NFR-AVAIL-5 / Q12)

`docs/prds/diagrams/rolling-upgrade.svg` (PRD §6.6.1) is the authoritative diagram. N / N+1 minor-version skew is supported; N / N+2 is rejected. Per-key drain ≤ 5 s. The CI gate runs an N ↔ N+1 interop test on every PR that touches a wire-protocol or gossip-message file (R7 mitigation).

### 13.3 Backup / restore

Operator-selected backup destination shared by data and coordinator snapshots. Restore is **offline** (cluster cold-start from snapshot, then anti-entropy reconciles). RTO budget per DM-10:

- 32-node, 50 GiB, `s3`: ≤ 30 min to `Ready`, ≤ 30 min anti-entropy convergence (60 min total). `TestSnapshotRestoreRTO` GA gate.
- `postgres` destination: half throughput (90 min total) — marketed as "dev / control-plane sized."

## 14. Key architectural decisions

This section restates the load-bearing decisions in one place. Each cites the PRD ID(s) it traces to.

| #     | Decision                                                                                              | Rationale                                                                                                    | PRD trace                          |
| ----- | ----------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ | ---------------------------------- |
| KAD-1 | Coordinator is 3-node embedded Raft, strictly off the per-request path                                | RTO < 30 s, RPO 0, no external ops dep, no data-path SPOF                                                    | FR-7 / Q2 / Q3                     |
| KAD-2 | Three-tier routing: smart Go SDK primary, coordinator-as-replica fan-out, any-data-node forwarding   | Earns < 1 ms GET p99 on the primary path; covers tail without adding a deployable                            | FR-8 / FR-20 / Q3                  |
| KAD-3 | Named-only consistency (`ONE`/`QUORUM`/`ALL`) at the wire; cluster owns N/W/R numerics                | Eliminates the Cassandra-style `R:1` footgun                                                                 | FR-2 / Q4                          |
| KAD-4 | Layered gossip: region-aware SWIM mandatory, Plumtree optional layered on SWIM's view                 | Membership / FD and bulk dissemination are different problems; layering keeps each correct                   | FR-3 / Q6                          |
| KAD-5 | Per-key vector clocks + `lww` default + `siblings` opt-in; no app-supplied merge in v1                | Minimal correct surface; `siblings` covers app-merge needs without a second extension point                  | FR-4 / Q5                          |
| KAD-6 | In-memory data nodes, replication-only fault tolerance, snapshots for cluster-wide DR                 | v1 scope; durable backend deferred to v1.x via `storage.Backend`                                              | FR-12 / Q7 / R5                    |
| KAD-7 | Cache mandatory on < 1 ms paths, key shape `(key, partition_map_epoch)`, default TTL 100 ms ceiling 500 ms | Earns < 1 ms GET p99 under hot-key writes without vc-hash defeat                                              | NFR-PERF-2 / FR-12 / A1            |
| KAD-8 | mTLS-only via `pki.Source`; no `--insecure` flag exists; built-in CA permanently rejected             | Security as a build-time constraint                                                                           | FR-6 / NFR-SEC-1 / Q8 / Q9         |
| KAD-9 | OTel-Collector-mediated obs (single OTLP wire); reference stack ships under `deploy/observability/`   | Single export wire keeps the binary slim; Collector lets operators swap backends                              | FR-11 / FR-17 / Q16                |
| KAD-10 | Active-passive multi-region only in v1; manual `gossamerctl promote` with fencing; no auto-failover  | Avoids split-brain without active-active merge machinery                                                      | FR-21 / Q11 / R4 / FC-3            |
| KAD-11 | Vnode count locked at 128 vnodes/node (16 384 cluster-wide at 128-node ceiling)                       | Satisfies PRD §5.1 constraints with headroom: ≤ 192 KiB serialised payload, < 1% imbalance on single-node loss | DM-18 / PRD §5.1                   |
| KAD-12 | R1 GET-QUORUM sub-budget: 50 / 600 / 100 / 50 µs                                                      | Locks substep budgets so the bench gate can attribute regressions                                             | R1 / `BenchmarkGetQuorumRequestLevel` |

## 15. Alternatives considered

The PRD's §11 question table is exhaustive and authoritative for the rejected alternatives. The HLD-level alternatives (architecture-level, not policy-level) the design phase considered and rejected:

| Alt                                                                  | Why rejected                                                                                                              | Cited in                  |
| -------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------- | ------------------------- |
| Coordinator-as-front-door (every request enters the coordinator)     | Adds a hop on the data plane; SPOF; cannot meet < 1 ms GET p99                                                             | Q3 / FR-8                 |
| Cassandra-style ring without a coordinator                           | Loses the "intelligent coordinator" the README promises; partition-map dissemination becomes ad-hoc                        | PRD §5 rejected           |
| External etcd as coordinator metadata store                          | Adds ops dependency for what Raft alone solves                                                                             | Q2 / PRD §5 rejected      |
| L7 / token-aware LB (Envoy / nginx) instead of smart SDK              | Most cloud LBs need scripting to do this; SDK achieves the same routing without a deployable                               | FR-8 deferred-alternatives |
| HyParView under Plumtree in v1                                       | Marginal value at 128 nodes; deferred to v1.2                                                                             | Q6 deferred-alternatives  |
| Naive cross-region SWIM (no region-awareness)                        | Noisy on WAN; latency-skew triggers false-positive failure detections                                                     | Q6 rejected               |
| Per-key-range home region                                            | Cluster-level active-passive matches operator mental model better; per-key-range is a v1.2 active-active concern         | Q11 rejected / OD-4       |
| App-supplied merge function as a v1 conflict strategy                | `siblings` covers the merge use case without a second extension point                                                     | Q5 rejected               |
| Wall-clock LWW                                                       | Clock skew silently loses writes                                                                                          | Q5 rejected               |
| Pebble durable backend in v1                                         | Out of v1 scope; lands in v1.x as the first pluggable durable backend                                                     | Q7 / FR-12                |
| Per-row residency tagging in v1                                      | Application-layer concern; cluster is the residency primitive                                                             | NG10 / NFR-SEC-7          |
| Capability tokens (JWT) as auth surface                              | JWKS distribution + revocation cache fight the < 1 ms GET p99 budget                                                       | Q9 rejected               |
| Bundled Tempo / Loki / Prom binaries                                 | Configs only — pulling upstream images at runtime keeps the binary slim                                                    | Q16 rejected              |
| Built-in CA                                                          | Chicken-and-egg trust on bootstrap; CA responsibility is a different product                                              | Q8 permanently rejected   |
| Cache key `(key, vc-hash)`                                           | vc-hash changes on every write; defeats cache under hot-key traffic at NFR-SCALE-3a                                        | A1                        |

## 16. Risks and mitigations

The PRD §8 risk table (R1..R12) is binding and not duplicated in full here. The HLD adds the following architecture-level risks that arise from this design specifically:

| #         | Risk                                                                                                                                                                                                  | Likelihood | Impact | Mitigation                                                                                                                                                                                                                                                                                                                                                                                                  |
| --------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------- | ------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| HR1       | **Vnode count of 128/node hits the 256 KiB payload ceiling at 128-node scale if the LLD picks a wire encoding less efficient than group-varint deltas + zigzag-varint replicas.**                       | Medium     | High   | Bench gate `BenchmarkPartitionMapSerialization` (PRD §5.1) is GA-blocking and asserts both the 5 ms cost and the 256 KiB ceiling. If the chosen LLD encoding does not fit, drop vnode count to 96/node (12 288 cluster-wide) — re-runs imbalance analysis (still < 1%) without a PRD revision (PRD §5.1 #1 still satisfied).                                                                                       |
| HR2       | **Pre-warmed gRPC connection pool exhausts file descriptors at 128-node scale.** Every data node maintains 4 hot connections to peer owners per request — at scale that's hundreds of connections per node. | Medium     | Medium | Connection pool sized by per-node `ulimit -n` budget; idle-LRU close + re-warm on epoch bump. LLD locks pool size + idle policy. Bench gate (`BenchmarkGetQuorumRequestLevel`) runs at NFR-SCALE-2 numbers and would catch fd-exhaustion regressions.                                                                                                                                                          |
| HR3       | **Cancellation-of-slowest-2 is the load-bearing mechanism for clearing 1 ms p99.** A naive Go fan-out that does not cancel via `context.Cancel` would wait for slowest-of-5 (≈ 1.5–2 ms p99 intra-AZ).        | High       | High   | LLD locks the cancellation propagation point: cancellation MUST short-circuit before issuing the downstream KV-map read on the cancelled peer. Bench-gated by `BenchmarkGetQuorumRequestLevel` measuring the slowest-of-3 sub-budget directly.                                                                                                                                                              |
| HR4       | **OTel Collector single-point-of-failure for the obs pipeline.** If the Collector is unreachable, all three signals back up; logs spill to stdout but metrics + traces are dropped at the export queue.    | Medium     | Medium | Bounded export queue with drop-oldest semantics; saturation alert via `otel_export_queue_dropped_total`; LLD picks the queue cap. Operators run the Collector as a `Deployment` with ≥ 2 replicas in production (documented in `deploy/observability/`).                                                                                                                                                       |
| HR5       | **Smart-SDK partition-map staleness during rolling upgrade.** Upgrading a node bumps the partition-map epoch; SDKs that have not refreshed will get `WRONG_OWNER` on every request to that node for the duration of the SDK's refresh latency. | Medium     | Medium | Refresh latency ≤ 1 RTT to any data node (the refresh response ships the new map); R1's degraded budget of ≤ 1.5 ms p99 during membership churn applies. Per-PR upgrade-skew CI (R7) catches wire-format regressions; no separate HLD mitigation needed beyond the SDK behaviour already specified in §5.8.                                                                                                  |

## 17. Rollout plan

The PRD §7 milestone DAG (`docs/prds/diagrams/milestone-dag.svg`) is binding. The HLD adds the following implementation guidance per milestone for the Project Lead's branch-cutting plan:

| Milestone | HLD-bound implementation guidance                                                                                                                                       |
| --------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| M2        | This document → LLD → Epics → Stories. EM gate between HLD and LLD; story-slicer caps stories at ≤ 300 LOC.                                                              |
| M3        | Foundations: data-node §5.2 sharded in-memory store + cache layer §5.6. Bench-gate evidence on `Get` cache-hit < 1 ms p99 + 0-allocation NFR-PERF-4 contract.            |
| M4        | Single-node KV: §5.2 + mTLS surfaces from §5.7 + gRPC + Fiber REST. `security/no_plaintext_test.go` lands here as a CI gate (J-S-1).                                     |
| M5        | Coordinator §5.1: 3-node Raft, partition map gossip-distributed. Library decision (hashicorp vs etcd raft) made here.                                                    |
| M6        | Gossip §5.3: region-aware SWIM + optional Plumtree. **Parallel workstream with M5 off the M4 base** (DM-16 / §7.2). Both feed M7.                                        |
| M7        | Conflict §5.5: vector clocks + `lww` + `siblings`. Concurrent-write correctness tests on the simulator.                                                                  |
| M8        | Anti-entropy §5.4: per-range Merkle + restart hydration. `BenchmarkAntiEntropyUnderLoad` + hydration-burst variant land as GA gates.                                     |
| M9        | Backup §5.1 + §5.2 + `gossamerctl snapshot` + offline restore. `TestSnapshotRestoreRTO` (DM-10) lands as a named exit criterion.                                          |
| M10       | Rolling upgrade machinery + CLI; per-PR upgrade-skew CI gate stands up (R7).                                                                                              |
| M11       | Cross-region §6.5: active-passive + `gossamerctl promote` + `gossamerctl reseed`. `TestSDKPostPromoteConvergence` (DM-7) and the cross-region-replication bench land here. |
| M12       | Reference observability stack §12.5; Grafana 3-panel dashboard answers J-O-5.                                                                                             |
| M13       | GA candidate: §9 acceptance criteria all green; bench gate green for 7 consecutive days; security review signed off; rolling-upgrade rehearsal green.                    |

### 17.1 Branch-cutting plan (Project Lead-bound)

- M5 and M6 are parallel workstreams off the M4 merge commit (DM-16 / PRD §7.2). The Project Lead cuts `feat/<n>-m5-coordinator-raft` and `feat/<n>-m6-gossip` simultaneously; engineers branch sub-stories off each feature branch per `CLAUDE.md`'s sub-branch convention.
- **FR-20 (smart Go SDK) is branch-cut-blocked** until LLD `pkg/api/partition/v1/partition.proto` is signed off (DM-19 hard upstream dependency).

## 18. Open questions — resolution pass (v0.2)

The PRD §12 outstanding decisions carry into the HLD with their PRD-v1.10 `needs-by` tags (see §18.1 below). v0.1 of this HLD seeded ten architectural questions (OQ-1..OQ-10) with `Default proposal / Needs-by` placeholders. v0.2 converts each to one of:

- **LOCKED at HLD** — the answer is binding. The LLD MAY tighten knobs (e.g. exact byte layout, exact idle timeout) but MUST NOT relax the shape.
- **DEFERRED-bench** — the architectural *shape* is locked at HLD; only the numeric knob is LLD-bench-driven. The closing bench is named.

No item still requires an architectural answer before LLD work can begin. OQ-3/OQ-4/OQ-7 are the only items the LLD must close numerically; the rest are LOCKED.

### 18.1 Inherited PRD outstanding decisions (read-through, no HLD override)

| PRD ID            | Status at HLD v0.2                                                                                                                  | `needs-by` (PRD v1.10)  |
| ----------------- | ----------------------------------------------------------------------------------------------------------------------------------- | ----------------------- |
| OD-2-marketing    | "Audit-ready, not audited" positioning locked in §11.3. Marketing copy review is a non-engineering deliverable.                      | pre-GA                  |
| OD-3              | Inter-region p99 RTT default ≤ 100 ms accepted by the HLD §6.5 / §9 NFR-SCALE-8 lag budget. No HLD-level override needed.            | pre-HLD (closed)        |
| OD-5              | mTLS overlap window 24 h reflected in §11.1 hot-reload contract. PRD-side close.                                                     | pre-GA                  |
| OD-6              | Bench-gate taxonomy (`cache-bound` / `backend-bound`, plus `background-work-under-load`) honoured by every bench named in this HLD. | pre-HLD (closed)        |
| OD-7              | Coordinator Raft-log fio floor 3 k IOPS reflected in §5.1 + §10.1 bootstrap pre-flight. PRD-LLD revisit captured below as OQ-1.b.    | pre-HLD (closed) / pre-LLD |

### 18.2 HLD architectural questions — resolution table

| #      | Question                                                                                                                                | Decision (HLD v0.2)                                                                                                                                                                                                                                                                                                                                                                                                                                | Status         | Cross-ref / closing bench                                                                  |
| ------ | --------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------- | ------------------------------------------------------------------------------------------ |
| OQ-1   | Raft library: `hashicorp/raft` vs `etcd-io/raft`?                                                                                       | **`hashicorp/raft`.** (a) `FSMSnapshot.Persist` / `Restore` map cleanly onto the FR-7 cadence (10 k entries / 1 GiB) and the ≤ 30 s RTO replay budget; (b) `StreamLayer` is a one-line mTLS hookpoint into the §5.7 `pki.Source`; (c) production track record at Consul/Nomad scale is broader than `etcd-io/raft`'s typical embed. `etcd-io/raft` would force us to own the log + snapshotter + transport — extra surface inside the RTO/IOPS budget. | LOCKED         | §5.1 (Coordinator) bullet "Library choice" updated to remove the LLD-defer caveat.          |
| OQ-1.b | Coordinator Raft-log storage backend (BoltDB vs raft-wal vs custom)?                                                                    | **DEFERRED-bench.** Architectural shape is LOCKED: a single fsync-per-commit local WAL meeting OD-7 fio floor (3 k IOPS sustained 4 KiB random-write w/ fsync). Pick among the `hashicorp/raft` storage backends in the LLD; closing bench measures p99 fsync latency at 1× target metadata mutation rate and at 5× spike. If the storage backend cannot hold the floor under spike, escalate the floor to 5–10 k IOPS (per OD-7 escalation path). | DEFERRED-bench | OD-7 follow-on; LLD bench `BenchmarkRaftCommitFsyncLatency`.                                |
| OQ-2   | Partition-map wire encoding: group-varint deltas + zigzag-varint replicas, or a custom proto-buf?                                       | **Group-varint deltas (token column) + zigzag-varint (replicas column), LOCKED.** Sized at ~192 KiB for 16 384 vnodes (≈ 25% headroom against the 256 KiB ceiling). A custom proto-buf saves nothing the encoding doesn't already buy and adds a code-gen dependency on the hot read path of every redirect.                                                                                                                                       | LOCKED         | §8.1 sizing already authoritative; bench `BenchmarkPartitionMapSerialization` enforces ≤ 5 ms / ≤ 256 KiB at 128 nodes (GA-blocking). LLD locks only the byte layout. |
| OQ-3   | Sharded in-memory store — how many shards per data node?                                                                                | **DEFERRED-bench.** Architectural shape LOCKED: shard count is a power of 2, derived from `runtime.NumCPU()`, contention-bench-driven. LLD picks among `1×`, `2×`, `4×`, `8×` of `NumCPU` against `BenchmarkPutShardedThroughput` (write-heavy) and `BenchmarkGetShardedHit` (read-heavy under cache miss); locks the value where p99 PUT under saturation crosses < 5 ms with the smallest shard count (memory hygiene).                            | DEFERRED-bench | §5.2; LLD bench `BenchmarkPutShardedThroughput` + `BenchmarkGetShardedHit`.                 |
| OQ-4   | gRPC connection pool to peer owners (HR2) — static or auto-sized?                                                                       | **Static, one multiplexed HTTP/2 connection per peer-owner, LOCKED.** Eager-dialed on partition-map epoch bump (§6.1.1 "pre-warmed pool" requirement); idle-closed at 5 min (LLD locks the timer); re-warmed lazily on next request. Pool ceiling = `cluster_size − 1` ≤ 127 streams at the 128-node ceiling — well under any realistic ulimit. The v0.1 `min(N-1, ulimit/64)` guardrail is retained as a *refusal* policy: if `cluster_size − 1 > ulimit/64` at startup, refuse to start with a clear error.                                                                                                                                                                       | LOCKED         | §6.1.1 "Pre-warmed gRPC connection pool" bullet refined; LLD locks idle timer + refusal policy. |
| OQ-5   | Anti-entropy schedule jitter — uniform or exponential backoff under load?                                                               | **Uniform ±50% of cadence, LOCKED.** Exponential backoff under load couples scheduling to foreground saturation, which would suppress repair exactly when divergence is rising. Pathological co-scheduling at scale is guarded by `BenchmarkAntiEntropyUnderLoad` (already enforces NFR-PERF-3 caps + foreground latency under repair).                                                                                                            | LOCKED         | §5.4 + §10.1 already binding; bench is the same one DM-2 added.                              |
| OQ-6   | Plumtree fanout — static `log(N)` or adaptive on message-loss rate?                                                                     | **Static `⌈log₂(N)⌉` for v1, LOCKED.** Adaptive fanout requires a stable cluster-wide `gossip_message_loss_rate` metric we do not yet emit and would couple the gossip layer to observability cardinality. Adaptive is deferred to v1.2 alongside HyParView (FC-3 line in §5.3) where the same observability lift is already on the roadmap.                                                                                                       | LOCKED         | §5.3 already names HyParView v1.2; no §5.3 text change needed.                              |
| OQ-7   | Cross-instance Redis cache — at what cluster size does it start paying for itself, and where can operators disable it?                  | **DEFERRED-bench, but operator knob shape LOCKED.** Cache layer always ships; the `cache.cross_instance` config knob accepts `enabled` / `disabled` (default `enabled`). Operators MAY set `disabled` only when ALL of: (a) `cluster_size ≤ 10`, (b) `cross_region.mode = none`, (c) the operator has measured Redis hit-rate < 5% over a representative 24 h window. LLD bench `BenchmarkCrossInstanceCacheBreakeven` numerically locks the 10-node and 5% thresholds against the < 1 ms GET p99 budget at NFR-SCALE-3a (1 k QPS/key). | DEFERRED-bench | §5.6 + §11.3 unchanged; LLD bench named above; threshold becomes ops-runbook material.       |
| OQ-8   | Multi-region replication transport — Plumtree-only or always SWIM-piggyback for cross-region?                                            | **Follow the active gossip layer chain (Plumtree when enabled, SWIM piggyback otherwise), LOCKED.** Already the §6.5 invariant; promoted here so the LLD does not re-litigate it. Bench-gated by the cross-region-lag bench at M11.                                                                                                                                                                                                                | LOCKED         | §6.5 already authoritative; bench is the M11 cross-region-lag bench.                        |
| OQ-9   | `region_epoch` propagation budget at 128 nodes (PRD locks p99 < 10 s at 32 nodes)?                                                       | **LOCKED at p99 ≤ 15 s with Plumtree enabled; p99 ≤ 40 s under SWIM-piggyback fallback.** Plumtree convergence scales as `O(log N)`, so 32 → 128 nodes is `× log₂(128)/log₂(32) = × 7/5 = × 1.4` → ~14 s, with 15 s headroom for jitter. SWIM piggyback is `O(N)`-ish in the worst case, so the 4× node-count factor gives a 4× ceiling (≈ 40 s). The §5.8 SDK alert threshold scales accordingly: `region_epoch_lag_seconds > 30` for Plumtree-on, `> 60` for SWIM-only. | LOCKED         | §5.8 metric threshold updated; LLD bench `BenchmarkRegionEpochPropagation` confirms at M11. |
| OQ-10  | Operator-facing config schema versioning — in-band or by-binary?                                                                         | **In-band, LOCKED.** Every config file carries `config_schema_version: <int>` (starts at `1`); the binary refuses to start if the schema is unknown. By-binary versioning is opaque to operators on rolling upgrade and breaks roll-forward of config-only changes. Upgrade-time migration: every binary version that bumps the schema MUST ship `gossamerctl config-migrate <old> <new>`; CI gate enforces presence of the migrator. | LOCKED         | §5.1 admin API (FR-13) already owns config; LLD names the migrator binary + CI gate.        |

### 18.3 Decisions log roll-up

The seven LOCKED items above are folded into §14 (Key architectural decisions) at HLD merge — adding KAD-12 (Raft library), KAD-13 (partition-map wire encoding), KAD-14 (gRPC peer-pool shape), KAD-15 (anti-entropy jitter shape), KAD-16 (Plumtree fanout v1 fixed), KAD-17 (multi-region transport follows active layer chain), KAD-18 (region-epoch propagation budgets at 128 nodes), KAD-19 (in-band config schema versioning). The three DEFERRED-bench items (OQ-1.b, OQ-3, OQ-4 numerics, OQ-7 numerics) become explicit LLD scope, each with its named closing bench.

> **Gate semantics:** any LLD revision that re-opens a LOCKED OQ requires an HLD revision bump and EM re-review. DEFERRED-bench items are LLD-bench evidence-only — the LLD writes the bench, runs it, and locks the value; no HLD revision needed.

---

## Appendix A — Cross-reference index (PRD ↔ HLD)

| PRD / Wiki ID         | HLD section                                                                 |
| --------------------- | --------------------------------------------------------------------------- |
| FR-1 / NG6            | §5.2, §8                                                                    |
| FR-2 / Q4             | §6.1, §7, §14 KAD-3                                                          |
| FR-3 / Q6             | §5.3, §14 KAD-4                                                              |
| FR-4 / Q5             | §5.5, §14 KAD-5                                                              |
| FR-5 / DM-2           | §5.4, §6.4, §16 inherited                                                    |
| FR-6 / Q8 / NFR-SEC-1..3 | §5.7, §11.1, §14 KAD-8                                                    |
| FR-7 / Q2 / OD-7      | §5.1, §10.1, §14 KAD-1                                                       |
| FR-8 / Q3             | §6.1, §6.2, §14 KAD-2                                                        |
| FR-9 / NFR-PORT-1     | §13                                                                         |
| FR-10 / NFR-AVAIL-5 / Q12 | §13.2                                                                    |
| FR-11 / FR-17 / Q16   | §12, §14 KAD-9                                                                |
| FR-12 / Q7 / A1       | §5.2, §5.6, §6.3, §14 KAD-6 / KAD-7                                          |
| FR-13 (admin RPCs) / DM-9 | §7, §10.1                                                                |
| FR-14                 | §13                                                                         |
| FR-15 / DM-4 / A2     | §6.2, §10.1                                                                  |
| FR-16 / Q9            | §11.2                                                                       |
| FR-18 / DM-10         | §5.1, §13.3, §17                                                             |
| FR-19 / DM-6 / R11    | §7, §10.1                                                                    |
| FR-20 / DM-7 / DM-19  | §5.8, §6.1, §17.1                                                            |
| FR-21 / Q11 / R4 / R12 | §5.3, §6.5, §14 KAD-10                                                      |
| NFR-PERF-1a/1b / R1   | §6.1.1, §9                                                                   |
| NFR-PERF-2..4 / A1    | §5.6                                                                        |
| NFR-AVAIL-1..5        | §10                                                                         |
| NFR-SCALE-1..8 / DM-17 | §9                                                                          |
| NFR-SEC-1..7 / Q9 / Q10 | §11                                                                       |
| NFR-OPS-1..4 / J-O-5  | §12                                                                         |
| §5.1 / DM-18          | §8.1, §14 KAD-11                                                             |
| §6 diagrams           | Cross-referenced inline; not duplicated                                     |
| §7 milestones / DM-16 | §17                                                                         |
| §8 risks R1..R12      | §10.1, §16 (HLD adds HR1..HR5)                                                |
| §9 acceptance         | §17                                                                         |
| §11 Q1..Q20           | §15                                                                         |
| §12 OD-1..OD-7        | §18                                                                         |
| §13 hand-off          | §17                                                                         |

---

## Appendix B — §18 v0.2 decisions roll-up (LOCKED items only)

These will fold into §14 at HLD merge. Listed here so the EM gate can see the §18 → §14/§5/§6 traceability without diffing every section.

| New KAD ID | Decision                                                                                            | Origin OQ | Authoritative HLD section          |
| ---------- | --------------------------------------------------------------------------------------------------- | --------- | ---------------------------------- |
| KAD-12     | Raft library = `hashicorp/raft`.                                                                     | OQ-1      | §5.1                               |
| KAD-13     | Partition-map wire = group-varint deltas (tokens) + zigzag-varint (replicas).                       | OQ-2      | §8.1                               |
| KAD-14     | gRPC peer-owner pool = static, 1 multiplexed HTTP/2 conn per peer, eager-dial on epoch bump.        | OQ-4      | §6.1.1                             |
| KAD-15     | Anti-entropy schedule jitter = uniform ±50% of cadence (no exponential under load).                 | OQ-5      | §5.4 / §10.1                       |
| KAD-16     | Plumtree fanout = static `⌈log₂(N)⌉` in v1; adaptive deferred to v1.2 with HyParView.                | OQ-6      | §5.3                               |
| KAD-17     | Multi-region replication transport = active gossip layer chain (Plumtree if on, SWIM piggyback otherwise). | OQ-8      | §6.5                               |
| KAD-18     | `region_epoch` propagation budget at 128 nodes: p99 ≤ 15 s (Plumtree) / ≤ 40 s (SWIM piggyback). SDK alert thresholds scale to 30 s / 60 s. | OQ-9      | §5.8                               |
| KAD-19     | Operator config schema versioning = in-band `config_schema_version` with refusal on unknown; binary-shipped `gossamerctl config-migrate` for every schema bump. | OQ-10     | §5.1 admin API                     |

DEFERRED-bench items left as explicit LLD scope (no KAD assigned until benched):

- **OQ-1.b** — Coordinator Raft-log storage backend pick under `hashicorp/raft`; closed by `BenchmarkRaftCommitFsyncLatency`.
- **OQ-3** — Sharded in-memory store shard count; closed by `BenchmarkPutShardedThroughput` + `BenchmarkGetShardedHit`.
- **OQ-7** — Cross-instance Redis cache breakeven thresholds (cluster-size + hit-rate); closed by `BenchmarkCrossInstanceCacheBreakeven`.

— end of HLD v0.2 (open-question resolution pass) —
