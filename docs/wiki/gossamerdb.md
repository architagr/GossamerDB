# GossamerDB — Requirements Wiki

**Author:** Archit Agarwal
**Status:** Draft v0.1 — first pass from the original one-paragraph product brief
**Source of intent:** the original product brief (one paragraph; the rest is derived below)
**Next document:** the PRD at `docs/prds/gossamerdb.md` (already drafted)

> ⚠️ This wiki is intentionally written _only_ from the original brief. Anything not stated there is captured below as an **assumption** or an **open question** to be resolved before the PRD is finalised. The PRD picks up these items in its §11 resolution table.

---

## 1. Problem statement

Modern application teams need a key-value store that survives the realities of multi-cloud and edge deployments — variable network latency, partial partitions, mixed-region operators, churning fleets, and security regimes that demand encryption-in-transit by default — **without** giving up either strong consistency on demand or operational simplicity. Existing options force a hard pick: AP systems (Cassandra/Dynamo-style) sacrifice strong consistency, CP systems (etcd/ZooKeeper) sacrifice scale and edge-friendliness, and most require a heavy operator footprint to run safely across regions. Operators also lack a single store whose replication, conflict-resolution, and gossip behaviour can be **tuned per cluster** rather than baked in at compile time.

GossamerDB exists to close that gap: a distributed KV store that combines a lightweight gossip protocol, Merkle-tree anti-entropy repair, vector-clock conflict resolution, and **tunable** quorum semantics behind an intelligent coordinator, so operators can dial the consistency / availability trade-off per cluster while the system handles convergence, repair, security (mTLS by default), and rolling upgrades automatically across local, Kubernetes, and multi-region AWS deployments.

---

## 2. Goals & Non-Goals

### 2.1 Goals (derived directly from the README)

- **G1.** Provide a distributed key-value API that runs identically in three deployment modes: **local**, **Kubernetes**, and **multi-region AWS**.
- **G2.** Offer **tunable quorum** semantics so the same cluster can be configured for strong consistency or for higher availability per workload.
- **G3.** Use a **lightweight, resilient gossip protocol** for membership, failure detection, and metadata dissemination.
- **G4.** Use **Merkle-tree-based anti-entropy** to repair divergent replicas in the background without a stop-the-world reconciliation.
- **G5.** Resolve concurrent writes via **vector clocks** with a **pluggable conflict-resolution strategy** that can be selected per cluster.
- **G6.** Allow operators to **swap gossip and conflict strategies at the cluster level** without forking the binary.
- **G7.** Ship **mTLS by default** for all node-to-node and client-to-node traffic.
- **G8.** Provide built-in **observability hooks** (per the project's stated OpenTelemetry stack) and **rolling-upgrade** support that does not require taking the cluster offline.
- **G9.** Run all of the above behind an **intelligent coordinator node** that owns cluster management, high-availability, and orchestration of the strategies above.
- **G10.** Inherit the project-wide **< 1 ms p99 cache-call SLO** from `CLAUDE.md` for any request-scoped read path that flows through the cache layer. (See §6.)

### 2.2 Non-Goals (until the user says otherwise)

- **NG1.** GossamerDB is **not** a SQL database, document store, full-text search engine, or message broker. The README scopes it to a key-value store.
- **NG2.** GossamerDB is **not** a multi-tenant SaaS service in v1; it is a system operators deploy into their own infrastructure.
- **NG3.** No client SDKs in arbitrary languages are promised by the original brief; SDK scope is an **open question** to be resolved in the PRD.
- **NG4.** No managed control plane (hosted offering) is implied; deployment is operator-driven.
- **NG5.** Cross-store federation (e.g., bridging to S3, DynamoDB, etcd) is out of scope.

---

## 3. Actors / Personas

The original brief implies three classes of stakeholders. None of them are named there — these are my inferences and **must be confirmed** before the PRD locks them in.

### 3.1 Cluster Operator ("Olivia") — primary persona

- **Who:** SRE / platform engineer who deploys, scales, secures, and upgrades GossamerDB clusters in their organization.
- **Cares about:** reliable cluster bring-up across local / k8s / multi-region AWS, observability, mTLS bootstrap, rolling upgrades, replacing failed nodes, switching gossip / conflict strategies per cluster, and capacity planning.
- **Interfaces with:** the coordinator node's admin API, configuration files, deployment manifests / Terraform, and the OpenTelemetry pipeline.

### 3.2 Application Engineer ("Adi") — primary persona

- **Who:** backend engineer at a consuming team who writes code that reads/writes keys.
- **Cares about:** a clean client API (gRPC + REST per the project stack), predictable read/write latency, choosing per-request consistency (tunable quorum), conflict-resolution semantics, and migration tooling for in-place upgrades.
- **Interfaces with:** the data nodes (and/or the coordinator) via gRPC and REST, and emits / consumes telemetry through the project's OpenTelemetry conventions.

### 3.3 Security & Compliance Reviewer ("Sam") — secondary persona

- **Who:** security engineer or auditor signing off on production deployment.
- **Cares about:** mTLS posture, certificate rotation, key custody, audit logs, rolling-upgrade safety, and that the cluster's defaults are secure.
- **Interfaces with:** mTLS configuration, certificate sources, and the audit / observability outputs.

### 3.4 Internal: GossamerDB Contributor — tertiary persona

- **Who:** an engineer extending GossamerDB itself (writing a new gossip strategy, a new conflict-resolution strategy, a new storage backend).
- **Cares about:** clear extension points, stable interfaces between coordinator and data nodes, the < 1 ms cache-call SLO, and the bench gate.
- **Interfaces with:** the Go module boundaries (`internal/gossip`, `internal/conflict`, `internal/storage`, etc.) — these names exist in the repo today and inform the persona, but the public extension contract is an **open question**.

---

## 4. Primary User Flows

Each flow below states **trigger → what the system does → success signal**. These flows are derived from the original brief plus the existing `internal/` package layout. The PRD turns each "happy path" into testable acceptance criteria.

### 4.1 Cluster bring-up

- **Trigger:** Operator deploys the coordinator and N data nodes (local binary, k8s manifest, or multi-region AWS deployment).
- **System:** coordinator boots, loads cluster config (gossip strategy, conflict strategy, quorum defaults, mTLS material), discovers data nodes, establishes mTLS between all nodes, and reaches steady-state membership through the gossip protocol.
- **Success:** every node is `Healthy` in the gossip view; the cluster reports `Ready` over the admin / observability surface.

### 4.2 Write path (Put)

- **Trigger:** Application engineer issues a `Put(key, value)` against the configured consistency level (e.g., `W=quorum`).
- **System:** coordinator (or a data node, depending on the topology — **open question**) routes the write to the responsible replicas based on the partitioning scheme, attaches a vector-clock context, awaits the configured write quorum, and acknowledges.
- **Success:** the call returns within the cluster's configured budget; replicas converge through Merkle anti-entropy if any replica was unreachable at write time.

### 4.3 Read path (Get)

- **Trigger:** Application engineer issues a `Get(key)` at a chosen consistency level (e.g., `R=quorum` for strong reads, `R=1` for fast reads).
- **System:** request is routed to replicas, responses are reconciled via vector clocks, and any divergent values are surfaced (or auto-merged, depending on the conflict strategy in force).
- **Success:** read returns within the relevant latency target. Cache-served reads must satisfy the project-wide **< 1 ms p99** budget (see §6).

### 4.4 Node join / leave

- **Trigger:** A new data node is added (scale-out) or an existing node is decommissioned / fails.
- **System:** gossip propagates the membership change; coordinator updates the partitioning / hash-ring view; affected key ranges are rebalanced; Merkle anti-entropy heals any divergence introduced during transition.
- **Success:** ownership view is consistent across the cluster; no key is left without its target replica count once stabilized.

### 4.5 Anti-entropy repair

- **Trigger:** scheduled Merkle-tree comparison between replicas, or post-partition healing.
- **System:** replicas exchange Merkle root / subtree hashes, identify divergent ranges, and reconcile via the active conflict-resolution strategy.
- **Success:** replicas reach the same Merkle root for the affected range without taking traffic offline.

### 4.6 Strategy hot-swap (gossip / conflict)

- **Trigger:** Operator changes the configured gossip or conflict strategy at the cluster level.
- **System:** coordinator validates the new strategy, propagates the change through gossip, and switches over without a full restart.
- **Success:** all nodes report the new strategy as active; in-flight requests continue to be served. **Open question:** is hot-swap actually supported, or only restart-time swap?

### 4.7 Rolling upgrade

- **Trigger:** Operator deploys a new GossamerDB version.
- **System:** coordinator drains and upgrades nodes one at a time (or per a configured pace), validating health at each step; gossip and Merkle anti-entropy keep the cluster usable throughout.
- **Success:** every node reaches the new version; no key is unavailable longer than the configured tolerance; rollback path exists if a node fails to come up healthy.

### 4.8 Security / mTLS bootstrap

- **Trigger:** First-time cluster bring-up, or cert rotation event.
- **System:** every node loads its certificate material, establishes mTLS with peers and the coordinator, refuses any non-mTLS connection.
- **Success:** no plaintext traffic exists between nodes or from clients; rotation does not cause downtime. **Open question:** what is the certificate source of truth (operator-supplied PKI vs built-in CA vs SPIFFE)?

### 4.9 Observability

- **Trigger:** Operator wires the cluster to their telemetry backend.
- **System:** every node emits OpenTelemetry traces, metrics, and logs covering the request path, gossip events, anti-entropy runs, and security events.
- **Success:** operator can answer "is the cluster healthy / where is the latency / which node is the outlier" entirely from the telemetry surface, without shelling into nodes.

---

## 5. Functional Requirements (high-level)

The following are stated using the IETF MUST / SHOULD / COULD convention. They are **high-level**; the PRD decomposes them into PRD-level acceptance criteria.

### 5.1 MUST

- **F-MUST-1.** Provide a key-value API with at minimum `Put`, `Get`, and `Delete`, exposed over both gRPC and REST (REST via the project's stated `fiber` stack).
- **F-MUST-2.** Support **tunable quorum** with explicit `R` and `W` parameters (or equivalent named consistency levels) selectable per request or per client.
- **F-MUST-3.** Use a **gossip protocol** for membership and failure detection. The strategy must be selectable at the cluster level via configuration.
- **F-MUST-4.** Use **vector clocks** to track causality on writes, and apply a **pluggable conflict-resolution strategy** when concurrent writes are detected. The strategy must be selectable at the cluster level.
- **F-MUST-5.** Run **Merkle-tree-based anti-entropy** in the background to converge replicas without taking the cluster offline.
- **F-MUST-6.** Enforce **mTLS by default** on all node-to-node and client-to-node traffic. Plaintext traffic must be refused.
- **F-MUST-7.** Provide an **intelligent coordinator node** that owns membership, partitioning, strategy distribution, and orchestration of rolling upgrades. The coordinator must itself be highly available (no single point of failure) — **the exact HA mechanism is an open question**.
- **F-MUST-8.** Run identically in **local**, **Kubernetes**, and **multi-region AWS** deployment modes; configuration differences must be expressible without rebuilding the binary.
- **F-MUST-9.** Emit OpenTelemetry traces, metrics, and logs covering the request path, gossip, anti-entropy, and security events.
- **F-MUST-10.** Support **rolling upgrades** with no required full-cluster downtime.

### 5.2 SHOULD

- **F-SHOULD-1.** Provide an **admin API** on the coordinator for membership inspection, strategy hot-swap, manual anti-entropy triggers, and rolling-upgrade orchestration.
- **F-SHOULD-2.** Ship at least one **reference gossip strategy** and one **reference conflict-resolution strategy** that work safely out of the box.
- **F-SHOULD-3.** Provide **structured audit logging** for security-sensitive events (mTLS handshake failures, configuration changes, admin actions).
- **F-SHOULD-4.** Allow the **storage backend** behind a node to be pluggable (the existing `internal/storage` package implies this; the README itself does not state it explicitly — see open questions).
- **F-SHOULD-5.** Provide a **bench harness** per feature that satisfies the project's `./.claude/scripts/bench-check.sh` gate.

### 5.3 COULD

- **F-COULD-1.** Provide first-class **client SDKs** beyond the wire protocol (Go is implied by the codebase; other languages are an open question).
- **F-COULD-2.** Provide **multi-region active-active** semantics beyond the basic multi-region AWS deployment mode.
- **F-COULD-3.** Provide a **CLI** for operators that wraps the admin API.
- **F-COULD-4.** Provide **export / import** tooling for snapshots and disaster recovery beyond what anti-entropy already gives.

---

## 6. Non-Functional Requirements (high-level)

These NFRs are inherited from `CLAUDE.md` and from the README's framing. They must flow into the PRD verbatim.

### 6.1 Performance

- **NFR-PERF-1.** **< 1 ms p99 cache-call latency** is a project-wide hard SLO, enforced by `./.claude/scripts/bench-check.sh`. Any GossamerDB request path that is served out of cache (Redis cross-instance or in-memory LRU / `sync.Map` single-instance) must meet this budget. Bypassing the gate is not permitted.
- **NFR-PERF-2.** Every feature must ship a `*_bench_test.go` benchmark with `b.ReportAllocs()` enabled, covering the request-scoped path end-to-end with realistic input volumes.
- **NFR-PERF-3.** Anti-entropy and gossip must be **bounded background work** — they must not pre-empt foreground request budgets.

### 6.2 Availability & resilience

- **NFR-AVAIL-1.** The coordinator must be highly available — single coordinator failure must not take the cluster offline. **Exact target (e.g., 99.95% / 99.99%) is an open question.**
- **NFR-AVAIL-2.** A single data-node failure must not cause data loss as long as the configured replication factor is honoured.
- **NFR-AVAIL-3.** Rolling upgrades must not require taking the cluster offline.

### 6.3 Scale

- **NFR-SCALE-1.** The cluster must scale across multi-region AWS. Concrete numerical targets (nodes per cluster, keys per node, peak QPS, peak payload size) are **all open questions** to be locked in the PRD.

### 6.4 Security & compliance

- **NFR-SEC-1.** mTLS is on by default for all traffic; plaintext traffic must be refused.
- **NFR-SEC-2.** Cert rotation must not require downtime.
- **NFR-SEC-3.** Compliance posture (SOC2, ISO 27001, FedRAMP, GDPR data-residency, etc.) is **not stated in the README** — captured as an open question.

### 6.5 Operability

- **NFR-OPS-1.** First-class OpenTelemetry: traces, metrics, logs across the request path, gossip, anti-entropy, and security events.
- **NFR-OPS-2.** Strategy changes must be expressible in configuration, not code. The "modular design" the README calls out is the contract.
- **NFR-OPS-3.** Rolling upgrades must be the default upgrade path; the binary must support running mixed versions during the upgrade window.

### 6.6 Portability

- **NFR-PORT-1.** Identical binary must run in local, Kubernetes, and multi-region AWS modes; mode-specific behavior must be configuration-driven.
- **NFR-PORT-2.** Cloud-agnosticism is a stated project goal in `CLAUDE.md`. No hard dependency on a single cloud's primitives is acceptable in the request path.

---

## 7. In-Scope / Out-of-Scope

### 7.1 In scope (v1, derived from the README)

- Coordinator node and data-node binaries.
- gRPC + REST surface for `Put`, `Get`, `Delete` with tunable quorum.
- Pluggable gossip strategy with at least one reference implementation.
- Pluggable conflict-resolution strategy (vector-clock-driven) with at least one reference implementation.
- Merkle-tree anti-entropy.
- mTLS by default with a documented cert-bootstrap story.
- Local / Kubernetes / multi-region AWS deployment.
- OpenTelemetry instrumentation.
- Rolling upgrade machinery.
- Bench gate compliance per `CLAUDE.md`.

### 7.2 Out of scope (until reopened)

- SQL / document / search / queue semantics on top of the KV store.
- Hosted / managed service offering.
- Non-Go client SDKs (subject to the open question on language coverage).
- Federation with external KV systems.
- A web UI for cluster administration in v1 (a CLI is a "could-have").

---

## 8. Stated Assumptions

These are explicit assumptions made because the original brief does not say. Each one is for the PRD to confirm or revise.

- **A1.** "Strong consistency" in the README means _configurable_ strong consistency at the request level, not blanket linearizability across the whole cluster.
- **A2.** The "intelligent coordinator" is an addressable role (one or more processes), not a metaphor — i.e., it is a real binary with an API surface (the repo confirms this; `cmd/coordinator/` exists).
- **A3.** The data-node binary (`cmd/datanode/`) is the unit of horizontal scaling.
- **A4.** Storage durability is the responsibility of the underlying storage backend, not of GossamerDB itself.
- **A5.** "Cloud-agnostic" is satisfied by _not depending on cloud-specific primitives in the request path_. AWS is the named reference deployment; other clouds are not blocked but not promised.
- **A6.** The cache layer mentioned in `CLAUDE.md` is the project-wide answer to the < 1 ms p99 SLO and applies to GossamerDB's hot read paths.
- **A7.** Vector clocks operate per-key, not per-cluster.
- **A8.** "Pluggable" means configuration-driven selection of compiled-in strategies in v1, not loadable plugins at runtime.
- **A9.** REST is exposed via `fiber` (per `CLAUDE.md`); gRPC is the primary inter-node protocol.
- **A10.** Multi-region AWS implies WAN-tolerant gossip and replica placement, but the v1 multi-region SLO is **not** "five-nines globally" — it is "the cluster keeps working across regional partitions and converges when they heal."

---

## 9. Open Questions

These were the must-resolve items before the PRD could be finalised. Resolved questions are removed from the open list and recorded in the PRD's §11 resolution table; only the still-open ones remain below.

### 9.1 Resolved (closed by the PRD or by direct user input)

The following questions are **closed**. See `docs/prds/gossamerdb.md §11` for the binding resolution and the rejected alternatives.

- ~~**Workload shape & scale targets.**~~ Resolved 2026-04-28 — see PRD NFR-PERF-1a/1b and NFR-SCALE-1..8.
- ~~**Coordinator HA model.**~~ Resolved 2026-04-28 — 3-node embedded Raft, control-plane only, RTO < 30 s, RPO 0, 99.95% availability target. See PRD §11 Q2.
- ~~**Coordinator vs data-node responsibilities (data-plane routing topology).**~~ Resolved 2026-04-28 — Raft Coordinator stays off the per-request path; data-plane routing uses a three-tier preference order (smart Go client SDK → coordinator-as-replica fan-out → any-data-node forwarding fallback). Stateless router tier deferred. See PRD §11 Q3, FR-8, and FR-20.
- ~~**Quorum semantics & defaults.**~~ Resolved 2026-04-28 — **Option C**: cluster config owns the numerics (`N=5, W=3, R=3` default); per-request consistency is a named enum `{ONE, QUORUM, ALL}` only (default `QUORUM`); arbitrary numeric R/W per request is not allowed at the wire level. See PRD §11 Q4, FR-2, FR-20, J-A-1.
- ~~**Conflict-resolution strategies.**~~ Resolved 2026-04-29 — Two strategies ship in v1, configurable at the cluster level: **`lww` (default)** and **`siblings`**. Vector clocks are always attached. `lww` resolves concurrent writes by **highest vector clock** under a deterministic total order (LLD locks the exact comparator). `siblings` is **Riak-style**: a `Get` that finds divergent values returns **all sibling values + their vector-clock contexts in a single response**; the client picks/merges and writes back with a clock that descends from all siblings, collapsing them. **Strategy is set at cluster bootstrap and cannot be hot-swapped** (rolling-restart swap only — see PRD Q13). **Application-supplied merge functions are explicitly rejected for v1** — the `siblings` strategy is the supported escape hatch for teams that need application-level merge semantics. README markets `lww` as the default with `siblings` as the operator-selectable alternative for teams that need to surface concurrent writes. See PRD §11 Q5, FR-4, J-A-2.
- ~~**Gossip strategies.**~~ Resolved 2026-04-29 — Two strategies ship in v1 as **layered, complementary protocols** (not interchangeable like the conflict strategies): **`swim` (default, region-aware variant)** for membership + failure detection, and **`plumtree`** as an **operator-enabled second layer** for efficient bulk dissemination of partition-map and strategy-version updates. **`swim`** is mandatory and always-on (it is the failure detector); **`plumtree`** rides on top of SWIM's membership view (HyParView deferred to v1.2 — not earned at v1's 128-node target). **Region-aware SWIM** probes within-region at full fanout and cross-region at reduced fanout to keep WAN load bounded and avoid latency-skew false positives (LLD locks the exact ratios and timings). Strategy is set at cluster bootstrap and follows the same no-hot-swap rule as conflict strategies (PRD Q13). Rejected: **`plumtree`-only** (no failure detector, would need to be reinvented), **periodic full-state** (O(N²) message volume), **HyParView in v1** (deferred — Plumtree on the SWIM view is sufficient at v1 scale). See PRD §11 Q6 and FR-3.
- ~~**Strategy hot-swap.**~~ Resolved 2026-04-29 — **No hot-swap of gossip or conflict strategies in v1.** Strategy changes are restart-pace only, orchestrated by the coordinator across a rolling restart. Hot-swap deferred to v1.1 (requires per-call strategy negotiation, not a v1 budget item). Wiki flow §4.6 success criterion is therefore narrowed to "rolling-restart swap completes without taking the cluster offline." See PRD §11 Q13, FC-1, FR-4, FR-3.
- ~~**Storage backend pluggability.**~~ Resolved 2026-04-29 — **Data-node storage in v1 is in-memory only.** No durable per-node data backend ships in v1; the cluster relies on `N=5 / W=3 / R=3` replication and Merkle anti-entropy for in-cluster fault tolerance. **Single-node restarts hydrate via anti-entropy from peers** (no per-node disk persistence). **Cluster-wide outage = data loss for affected key ranges** in v1; the README markets this positioning explicitly ("in-memory clustered KV store; durable persistence ships in v1.x"). **Operator-selected backup destination** ships in v1 covering BOTH data-node snapshots AND coordinator metadata: operator picks **`s3`** or **`postgres`** at cluster bootstrap; both consumers (data-node `gossamerctl snapshot` and coordinator Raft snapshot/archive) write to the same destination. The Raft **commit log** stays on each coordinator's local disk (Raft requires it for soundness); only the **snapshot/archive** is configurable. Backup destination is pluggable via a `backup.Destination` interface so additional destinations can be added in v1.x without API breaks. Durable per-node data backend (Pebble, RocksDB, etc.) deferred to v1.x. **Redis** = cross-instance read cache only, never a backend. See PRD §11 Q7, Q17, FR-7, FR-12, FR-18.
- ~~**Disaster recovery & backup.**~~ Resolved 2026-04-29 — `gossamerctl snapshot` ships in v1 as an operator-triggered, point-in-time per-range snapshot tool. Destination = the cluster's operator-selected backup target (`s3` or `postgres`, see Storage entry above). Restore is offline. Anti-entropy + RF=5 covers in-cluster replica loss; snapshots are the explicit DR path against accidental delete-all and full-cluster outage. Continuous PITR deferred to v1.x. See PRD §11 Q17, FR-18.

### 9.2 Still open

1. **mTLS / PKI source of truth.** Operator-supplied PKI? Built-in CA? SPIFFE / SPIRE? Vault-issued certs? What is the expected rotation cadence and operator workflow?
2. **Authentication & authorization for clients.** Beyond mTLS at the transport layer, is there per-key or per-namespace authorization (RBAC, ACLs, capability tokens)? Or is mTLS identity the only auth surface in v1?
3. **Compliance posture.** Are SOC2 / GDPR / HIPAA / FedRAMP in scope for v1? Data-residency guarantees in multi-region AWS?
4. **Multi-region semantics.** Are writes single-region with async cross-region replication, or active-active with vector-clock-merged conflicts? What is the expected cross-region write latency?
5. **Rolling-upgrade contract.** What is the version-skew window the cluster guarantees? How are gossip / conflict strategy changes versioned?
6. **Client surface.** Which languages get a first-class client SDK in v1? Go-only? Go + one other? Or just the wire protocol?
7. **Edge deployment shape.** The README names "edge landscape" — what does an edge node look like? Is it a full GossamerDB node, a read-through cache, or a thin proxy?
8. **Observability backends.** Beyond OpenTelemetry-as-a-format, which collectors / dashboards / SLO definitions ship in v1 (Prometheus? Tempo? a reference Grafana dashboard)?
9. **Tenancy.** Single-tenant per cluster, or multi-tenant with namespace isolation?
10. **Public API stability commitment.** What is the versioning policy for the gRPC / REST surface and the strategy extension points?
11. **Tooling & migration path.** Is there a migration story for users coming from etcd / Consul / Redis / DynamoDB, or is GossamerDB a clean-slate adoption?

---

## 10. Hand-off

- **This document:** `docs/wiki/gossamerdb.md`
- **Next document:** `docs/prds/gossamerdb.md` — the PRD (a) drives a clarification round against the open questions in §9.2, (b) translates the high-level F-\* requirements in §5 into testable acceptance criteria, and (c) carries the < 1 ms p99 cache-call SLO from `CLAUDE.md` forward as a hard NFR for the design phase.
- **Design sign-off:** the HLD + LLD + Epics + Stories will be drafted on top of the PRD; this wiki is the upstream input that those documents trace back to.
