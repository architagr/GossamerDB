# GossamerDB — Business Analyst Wiki

**Owner:** `agent-ba` (Business Analyst)
**Status:** Draft v0.1 — first pass from the README one-paragraph product brief
**Next consumer:** `agent-delivery-manager` (will turn this into a PRD at `docs/prds/gossamerdb.md`)
**Source of intent:** `<projectDIR>/GossamerDB/README.md` (single paragraph; no other product brief exists yet)

> ⚠️ This wiki is intentionally written _only_ from the README. Anything not stated in the README is captured below as an **assumption** or an **open question** and must be resolved by the Delivery Manager (or escalated back to the BA) before the PRD is finalized. The BA has not yet had a clarification round with the user; the Delivery Manager is expected to drive that next.

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
- **NG3.** No client SDKs in arbitrary languages are promised by the README; SDK scope is an **open question** for the Delivery Manager.
- **NG4.** No managed control plane (hosted offering) is implied; deployment is operator-driven.
- **NG5.** Cross-store federation (e.g., bridging to S3, DynamoDB, etcd) is out of scope.

---

## 3. Actors / Personas

The README implies three classes of stakeholders. None of them are named in the README — these are the BA's inferences and **must be confirmed** by the Delivery Manager.

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

Each flow below states **trigger → what the system does → success signal**. None of these flows is yet validated against the user's intent — they are derived from the README plus the existing `internal/` package layout. The Delivery Manager should confirm each flow's "happy path" before turning them into PRD acceptance criteria.

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

The following are stated using the IETF MUST / SHOULD / COULD convention. They are **high-level**; the Delivery Manager will decompose them into PRD-level acceptance criteria.

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

- **NFR-SCALE-1.** The cluster must scale across multi-region AWS. Concrete numerical targets (nodes per cluster, keys per node, peak QPS, peak payload size) are **all open questions** that the Delivery Manager must extract from the user.

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

### 7.2 Out of scope (until reopened by the user)

- SQL / document / search / queue semantics on top of the KV store.
- Hosted / managed service offering.
- Non-Go client SDKs (subject to the open question on language coverage).
- Federation with external KV systems.
- A web UI for cluster administration in v1 (a CLI is a "could-have").

---

## 8. Stated Assumptions

These are explicit BA assumptions made because the README does not say. Each one is the Delivery Manager's to confirm or revise.

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

## 9. Open Questions for the Delivery Manager

These are the **must-resolve** items before the PRD can be finalized. Each one was inferred to be missing from the README and is the BA's hand-off list to the Delivery Manager. Any item not resolved here will block the Architect downstream.

1. **Workload shape & scale targets.** What are the v1 numerical targets? Concretely: peak QPS per cluster, peak QPS per key, p50 / p95 / p99 latency targets for `Get` and `Put` at each consistency level, max value size, max key cardinality, and replication factor defaults.
2. **Coordinator HA model.** Is the coordinator a leader-elected cluster (Raft? gossip-elected?), an active-active set, or a stateless front-door over a consensus group? What is its failover RTO / RPO?
3. **Coordinator vs data-node responsibilities.** Does the coordinator sit on the request path for every `Get` / `Put`, or is it strictly a control-plane role that data-nodes hit out-of-band? This decision is load-bearing for the < 1 ms p99 cache-call SLO.
4. **Quorum semantics & defaults.** What are the supported consistency levels (e.g., `ONE`, `QUORUM`, `ALL`, custom `R`+`W`), what is the default, and is consistency selectable per request, per client, or only per cluster?
5. **Conflict-resolution strategies — which ones ship in v1?** Last-Write-Wins? CRDT-based? Application-supplied merge function? What is the default? What is the migration story when an operator changes the strategy?
6. **Gossip strategies — which ones ship in v1?** SWIM? Plumtree? HyParView? Periodic full-state? What is the default, and what are the trade-offs documented to operators?
7. **Storage backend pluggability.** The repo has `internal/storage`; is the backend pluggable in v1, and if so, which backends ship (in-memory, BoltDB-style, RocksDB, S3-backed, Postgres)? The `CLAUDE.md` tech stack mentions Postgres and Redis — what role do they play (durable storage? cache only? metadata?)?
8. **mTLS / PKI source of truth.** Operator-supplied PKI? Built-in CA? SPIFFE / SPIRE? Vault-issued certs? What is the expected rotation cadence and operator workflow?
9. **Authentication & authorization for clients.** Beyond mTLS at the transport layer, is there per-key or per-namespace authorization (RBAC, ACLs, capability tokens)? Or is mTLS identity the only auth surface in v1?
10. **Compliance posture.** Are SOC2 / GDPR / HIPAA / FedRAMP in scope for v1? Data-residency guarantees in multi-region AWS?
11. **Multi-region semantics.** Are writes single-region with async cross-region replication, or active-active with vector-clock-merged conflicts? What is the expected cross-region write latency?
12. **Rolling-upgrade contract.** What is the version-skew window the cluster guarantees? How are gossip / conflict strategy changes versioned?
13. **Strategy hot-swap.** Is hot-swap of gossip / conflict strategies a v1 promise, or is restart-time swap acceptable? (See flow §4.6.)
14. **Client surface.** Which languages get a first-class client SDK in v1? Go-only? Go + one other? Or just the wire protocol?
15. **Edge deployment shape.** The README names "edge landscape" — what does an edge node look like? Is it a full GossamerDB node, a read-through cache, or a thin proxy?
16. **Observability backends.** Beyond OpenTelemetry-as-a-format, which collectors / dashboards / SLO definitions ship in v1 (Prometheus? Tempo? a reference Grafana dashboard)?
17. **Disaster recovery & backup.** Is there an explicit snapshot / restore tool, or is anti-entropy + replication considered DR-sufficient?
18. **Tenancy.** Single-tenant per cluster, or multi-tenant with namespace isolation?
19. **Public API stability commitment.** What is the versioning policy for the gRPC / REST surface and the strategy extension points?
20. **Tooling & migration path.** Is there a migration story for users coming from etcd / Consul / Redis / DynamoDB, or is GossamerDB a clean-slate adoption?

---

## 10. Hand-off

- **This document:** `<projectDIR>/GossamerDB/docs/wiki/gossamerdb.md`
- **Next agent:** `agent-delivery-manager`
- **Expected next artefact:** `docs/prds/gossamerdb.md` — the PRD must (a) drive a clarification round with the user against the open questions in §9, (b) translate the high-level F-\

16. **Observability backends.** Beyond OpenTelemetry-as-a-format, which collectors / dashboards / SLO definitions ship in v1 (Prometheus? Tempo? a reference Grafana dashboard)?
17. **Disaster recovery & backup.** Is there an explicit snapshot / restore tool, or is anti-entropy + replication considered DR-sufficient?
18. **Tenancy.** Single-tenant per cluster, or multi-tenant with namespace isolation?
19. **Public API stability commitment.** What is the versioning policy for the gRPC / REST surface and the strategy extension points?
20. **Tooling & migration path.** Is there a migration story for users coming from etcd / Consul / Redis / DynamoDB, or is GossamerDB a clean-slate adoption?

---

## 10. Hand-off

- **This document:** `<projectDIR>/GossamerDB/docs/wiki/gossamerdb.md`
- **Next agent:** `agent-delivery-manager`
- **Expected next artefact:** `docs/prds/gossamerdb.md` — the PRD must (a) drive a clarification round with the user against the open questions in §9, (b) translate the high-level F-\* requirements in §5 into testable acceptance criteria, and (c) carry the < 1 ms p99 cache-call SLO from `CLAUDE.md` forward as a hard NFR for the Architect.
- **EM gate reminder:** per `.claude/TEAM_WORKFLOW.md` step 4, the Engineering Manager will only sign off after the Architect's HLD + LLD + Epics + Stories are drafted on top of the PRD; this wiki is the upstream input.
