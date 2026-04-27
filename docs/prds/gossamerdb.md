# GossamerDB — Product Requirements Document

**Owner:** `agent-delivery-manager`
**Status:** Draft v1.0 — derived from `docs/wiki/gossamerdb.md`
**Upstream input:** `<projectDIR>/GossamerDB/docs/wiki/gossamerdb.md`
**Project rules:** `<projectDIR>/GossamerDB/CLAUDE.md`
**Next consumer:** `agent-architect` (HLD → LLD → Epics → Stories), gated by `agent-engineering-manager`
**Release target:** v1.0 GA (single coordinated release; v1.x increments listed in §10)

> This PRD resolves the 20 open questions left by the BA in `docs/wiki/gossamerdb.md §9`. Every resolution is recorded inline in §11 with the rejected alternatives. Items still requiring user / business sign-off are listed in §12 ("Outstanding Decisions") with a recommended default and a `needs-by` deadline tag.

---

## 1. Goals & Non-Goals

### 1.1 Goals (v1)

- **G1.** Ship a single distributed key-value binary set (`coordinator` + `datanode`) that runs identically on **local**, **Kubernetes**, and **multi-region AWS** with mode-specific behaviour driven only by configuration.
- **G2.** Expose `Put` / `Get` / `Delete` over both **gRPC** (primary) and **REST via Fiber** (secondary) with **per-request tunable quorum**.
- **G3.** Use a **lightweight gossip protocol (SWIM-style)** for membership and failure detection, with the strategy selectable per cluster via configuration.
- **G4.** Resolve concurrent writes via **per-key vector clocks** with a **pluggable conflict-resolution strategy** (LWW default; sibling-return alternative ships in v1).
- **G5.** Run **Merkle-tree anti-entropy** as bounded background work to converge replicas without taking the cluster offline.
- **G6.** Make the cluster secure by default: **mTLS required** on every node-to-node and client-to-node hop. Plaintext is refused.
- **G7.** Provide an **HA coordinator** (3-node Raft group, embedded — see §6.1) so that a single coordinator failure does not stop the cluster.
- **G8.** Emit **OpenTelemetry** traces, metrics, and logs covering the request path, gossip, anti-entropy, and security events.
- **G9.** Support **rolling upgrades** with a one-minor-version skew window, no full-cluster downtime, and a documented rollback path.
- **G10.** Honor the project-wide **< 1 ms p99 cache-call SLO** from `CLAUDE.md` on every request-scoped read path that flows through the cache layer. Enforced by `./.claude/scripts/bench-check.sh` as a hard gate.

### 1.2 Non-Goals (v1)

- **NG1.** Not a SQL / document / search / queue product. KV semantics only.
- **NG2.** No hosted/managed offering. Self-deploy only.
- **NG3.** No client SDKs beyond **Go** in v1. Other languages can use the wire protocol (gRPC/REST) directly.
- **NG4.** No web admin UI. A CLI ships in v1; a UI is deferred.
- **NG5.** No federation with external KV systems (etcd, DynamoDB, etc.).
- **NG6.** No multi-tenant namespace isolation in v1 — single-tenant per cluster (§11 Q18).
- **NG7.** No active-active multi-region writes in v1 — multi-region is async replication with one home region per key range (§11 Q11).
- **NG8.** No runtime-loadable plugins. Strategies are compiled into the binary and selected by config (§11 Q5/Q6, per Wiki A8).
- **NG9.** No formal certifications (SOC2 / HIPAA / FedRAMP) in v1 — controls are designed to be audit-friendly but the audit itself is post-GA (§11 Q10).

---

## 2. Personas & Primary User Journeys

The wiki's three primary personas (Olivia / Adi / Sam) and one tertiary (Contributor) are confirmed. Refined journeys below — each has a measurable success signal that becomes the acceptance criterion for the Architect.

### 2.1 Olivia — Cluster Operator (primary)

- **J-O-1. First cluster bring-up.** Olivia points at a config file describing the gossip strategy, conflict strategy, replication factor (`N=3` default), and mTLS material. She runs the coordinator binary on three nodes and `N×replication-factor` data-node binaries. **Success:** the cluster reaches `Ready` over the admin API within **60 s** for local, **3 min** for k8s, and **10 min** for multi-region AWS (cold start, image already pulled).
- **J-O-2. Scale out / replace a failed node.** Olivia adds (or replaces) a data node. **Success:** gossip propagates membership within **10 s p99**; rebalancing completes for the affected ranges within the configured tolerance and no key drops below its target replica count once stabilized.
- **J-O-3. Strategy change.** Olivia changes the gossip or conflict strategy via the coordinator admin API. **Success in v1:** restart-time swap on a rolling-restart pace; hot-swap is deferred (§11 Q13).
- **J-O-4. Rolling upgrade.** Olivia rolls a new minor version. **Success:** the cluster serves traffic throughout; cluster supports **N and N+1 minor versions running simultaneously** for the duration of the upgrade window; rollback is one rolling-restart away.
- **J-O-5. Observability driven triage.** Olivia answers "is the cluster healthy / where is the latency / which node is the outlier" entirely from the OTel pipeline plus the reference Grafana dashboard, without shelling into nodes.

### 2.2 Adi — Application Engineer (primary)

- **J-A-1. Per-request consistency.** Adi calls `Put(key, value, {W:QUORUM})` or `Get(key, {R:ONE})` to dial latency vs consistency on the request line. **Success:** consistency level is honoured; SLO budgets in §3 are met for each level.
- **J-A-2. Conflict surfacing.** When concurrent writers touch the same key, Adi either gets the merged value (LWW default) or gets siblings + vector-clock context to merge in application code, depending on cluster configuration. **Success:** vector-clock context round-trips correctly; sibling-return mode actually returns siblings, not a single arbitrary value.
- **J-A-3. Idempotent retries.** On gRPC `UNAVAILABLE` / `DEADLINE_EXCEEDED`, Adi retries safely. **Success:** server treats retries with the same client-supplied request ID as idempotent for `Put` and `Delete`.

### 2.3 Sam — Security & Compliance Reviewer (secondary)

- **J-S-1. mTLS posture.** Sam verifies that no node accepts a plaintext connection on any port and that client certs are validated against the configured trust bundle. **Success:** integration test `security/no_plaintext_test.go` is part of CI and green.
- **J-S-2. Cert rotation.** Sam rotates the cluster CA. **Success:** rotation is online; no traffic is dropped; old + new certs are accepted simultaneously for the configured overlap window (default **24 h**).
- **J-S-3. Audit trail.** Sam reviews structured audit logs for admin-API calls, mTLS handshake failures, and configuration changes. **Success:** audit events are emitted as a distinct OTel log stream with stable schema.

### 2.4 Contributor (tertiary)

- **J-C-1. New strategy authoring.** A contributor implements a new gossip or conflict strategy by satisfying a small Go interface, adds it to a strategy registry, and selects it via cluster config. The bench gate (`./.claude/scripts/bench-check.sh`) is the contract that proves their strategy does not regress hot-path latency.

---

## 3. Functional Requirements (v1)

The wiki's `F-MUST-*` / `F-SHOULD-*` / `F-COULD-*` items are restated here as PRD-level acceptance criteria. **Cuts** from the wiki list are noted explicitly so the Architect knows what is _not_ in v1.

### 3.1 MUST (v1.0)

- **FR-1. Core KV API.** `Put(key, value, opts)`, `Get(key, opts)`, `Delete(key, opts)` over **gRPC** (primary) and **Fiber-backed REST** (secondary). Both surfaces are 1-to-1; REST is a thin translation layer. Wire schemas live under `pkg/api/`.
- **FR-2. Tunable quorum.** Named consistency levels `ONE`, `QUORUM`, `ALL` plus explicit `R`/`W` tuples. Selectable **per request** (request option), with a **per-client default** and a **per-cluster default-of-default**. Default cluster setting: `R=QUORUM`, `W=QUORUM`, `N=3`.
- **FR-3. Gossip — SWIM strategy.** Ships in v1 as the default and only gossip strategy. The strategy abstraction (`internal/gossip.Strategy`) exists so that an HyParView/Plumtree alternative can be added in v1.x without an API change.
- **FR-4. Vector clocks + conflict resolution.** Per-key vector clocks attached to every write. Two strategies ship in v1:
  - **`lww` (default):** last-write-wins by vector-clock-tiebreaking + deterministic node-ID tiebreak.
  - **`siblings`:** return all concurrent values to the client with their vector clocks; the client merges and writes back with the merged context.
- **FR-5. Merkle anti-entropy.** Per-range Merkle trees compared on a configurable cadence (default **5 min**, with jitter). Divergent ranges reconciled via the active conflict-resolution strategy. Anti-entropy is **bounded** in CPU and bandwidth (see NFR-PERF-3).
- **FR-6. mTLS by default.** Every TCP listener requires mTLS. Plaintext listeners are not buildable into the binary — there is no `--insecure` flag in v1 (Sam-friendly). Cert source is **operator-supplied PKI loaded from disk or from a Kubernetes Secret**; built-in CA / SPIFFE / Vault are deferred (§11 Q8).
- **FR-7. Coordinator HA.** The coordinator runs as a **3-node embedded-Raft group** owning cluster metadata (membership, partition map, strategy config). The data path does **not** flow through the coordinator (see FR-8) — coordinator failure pauses control-plane mutations only, not reads/writes.
- **FR-8. Data-plane routing.** Clients connect directly to data nodes. Each data node holds a recent copy of the partition map (gossip-propagated) and forwards/coordinates the request to the responsible replicas. **The coordinator is not on the per-request path** — this is load-bearing for the < 1 ms cache-call SLO (§11 Q3).
- **FR-9. Deployment modes.** A single binary runs in three modes; only configuration differs:
  - **Local:** single-host, ports allocated by config.
  - **Kubernetes:** Helm chart + StatefulSet for data nodes, separate StatefulSet for the 3-node coordinator group, headless services for peer discovery.
  - **Multi-region AWS:** EKS-based (k8s mode generalised) with one home region per key range, async cross-region replication, and per-region gossip tiers (see §11 Q11).
- **FR-10. Rolling upgrades.** Coordinator orchestrates a one-at-a-time data-node upgrade with health gates. **N / N+1 minor-version skew is supported.** Rollback = roll the same upgrade in reverse on the previous binary.
- **FR-11. OpenTelemetry instrumentation.** Traces (W3C Trace Context propagated via gRPC metadata and HTTP headers), metrics (RED on the request path; queue depths, gossip round-trip, anti-entropy bytes-repaired on the background paths), and structured logs. **OTLP exporter** is the single supported transport in v1 (§11 Q16).
- **FR-12. Storage backend — pluggable, two ship.** A `storage.Backend` interface is the contract. v1 ships:
  - **`memory`:** sync.Map-backed; for local dev and tests.
  - **`pebble`:** Pebble (Go-native LSM, RocksDB-compatible) for durable single-node storage.
  - **PostgreSQL is not a hot-path data backend.** It is used only for **coordinator metadata persistence** (Raft snapshot store + partition map archive). Redis is **not** a backend either — it is the **cross-instance cache** in front of the read path (see NFR-PERF-1).
- **FR-13. Admin API.** gRPC surface on the coordinator Raft leader: membership inspection, partition map dump, anti-entropy trigger, rolling-upgrade orchestration, strategy change (restart-pace). Authenticated via mTLS client cert with an `admin` SAN.
- **FR-14. CLI.** `gossamerctl` wraps the admin API. Ships in v1 (covers all admin RPCs); web UI does not.
- **FR-15. Idempotent writes.** `Put` and `Delete` accept an optional client-supplied request ID; duplicates within a configurable window (default **5 min**) are de-duplicated at the responsible coordinator-replica.
- **FR-16. Audit logging.** Distinct OTel log stream `gossamer.audit` for: admin-API calls, mTLS handshake failures, config changes, strategy changes, rolling-upgrade events, certificate rotation events.

### 3.2 SHOULD (v1.0 — best-effort, may slip to v1.x without breaking GA)

- **FR-17. Reference Grafana dashboard** + **Prometheus scrape config** shipped under `deploy/observability/`. (Wiki F-SHOULD-1 / Q16.)
- **FR-18. Snapshot / restore tool** (`gossamerctl snapshot`) — coordinator-orchestrated point-in-time per-range snapshot to S3-compatible object storage. (§11 Q17.)
- **FR-19. Per-cluster rate limits** on the data plane (token-bucket per client cert SAN) to protect the < 1 ms SLO under abuse.

### 3.3 COULD (deferred to v1.x or later — explicitly cut from v1.0)

- **FC-1. Hot-swap of gossip / conflict strategies.** Wiki §4.6 / Q13 — restart-pace swap is acceptable for v1; hot-swap deferred to v1.1.
- **FC-2. Non-Go SDKs.** Q14 — gRPC/REST wire protocol is the v1 contract. Java / Python / TS SDKs are post-GA.
- **FC-3. Active-active multi-region writes.** Q11 — v1 is async cross-region replication with home-region writes; active-active deferred to v1.2+.
- **FC-4. Web admin UI.** Q14-adjacent — CLI is the v1 contract.
- **FC-5. Per-key / per-namespace authorization.** Q9 — mTLS identity is the only auth surface in v1; ACL/RBAC is v1.1.
- **FC-6. SPIFFE / Vault cert sources.** Q8 — operator-supplied PKI on disk only in v1.
- **FC-7. Migration importers from etcd / Consul / Redis / Dynamo.** Q20 — v1 is clean-slate; importers are post-GA.
- **FC-8. Compliance certifications.** Q10 — controls are designed audit-ready; the audits themselves are post-GA.

---

## 4. Non-Functional Requirements

All targets below are **measurable** and become bench-gate / SLO-test inputs for the Architect.

### 4.1 Performance — SLO budgets

The project-wide **< 1 ms p99 cache-call SLO** from `CLAUDE.md` is carried forward as a hard gate. Below is the explicit scoping of which call paths are bound by it and which are not.

| Path                                                | Consistency     | p50 budget       | p99 budget                | Bound by < 1 ms cache-call SLO?     |
| --------------------------------------------------- | --------------- | ---------------- | ------------------------- | ----------------------------------- |
| `Get` cache hit (Redis cross-instance or in-memory) | `R=ONE`         | < 200 µs         | **< 1 ms**                | **YES** — bench gate enforces       |
| `Get` cache miss → backend read                     | `R=ONE`         | < 2 ms           | < 5 ms                    | No — backend-bound                  |
| `Get` quorum read                                   | `R=QUORUM`, N=3 | < 5 ms intra-AZ  | < 15 ms intra-AZ          | No — quorum coordination cost       |
| `Put` quorum write                                  | `W=QUORUM`, N=3 | < 8 ms intra-AZ  | < 25 ms intra-AZ          | No — replication cost               |
| `Put`/`Get` `R/W=ALL`                               | `ALL`, N=3      | < 12 ms intra-AZ | < 40 ms intra-AZ          | No — slowest-replica bound          |
| Cross-region replicated write commit                | async           | n/a              | < 2 s p99 (async)         | No — propagation, not request-bound |
| Gossip round-trip convergence                       | n/a             | n/a              | < 10 s p99 for membership | No — background                     |
| Anti-entropy repair of a 1 MB divergent range       | n/a             | n/a              | < 30 s p99                | No — background                     |

- **NFR-PERF-1.** Every feature contributes a `*_bench_test.go` file with `b.ReportAllocs()`. The bench gate (`./.claude/scripts/bench-check.sh`) runs in CI and locally before PR ready-for-review.
- **NFR-PERF-2.** The cache layer (Redis cross-instance, in-memory LRU / `sync.Map` single-instance) is mandatory for any path subject to the < 1 ms gate. Cache invalidation must be **explicit and tested** (mandatory invalidation test per cache).
- **NFR-PERF-3.** Anti-entropy and gossip are **bounded background work**: each is capped at a per-node CPU share (default 5%) and outbound bandwidth share (default 10% of NIC) — both configurable. They MUST NOT pre-empt foreground request budgets.
- **NFR-PERF-4.** Allocations on the hot path: `Get` cache-hit must hit **0 allocations** in steady state (sync.Pool buffers, pre-sized maps).

### 4.2 Availability & resilience

- **NFR-AVAIL-1.** **Coordinator availability target: 99.95%** (43 m 49 s/month downtime budget). Achieved via 3-node Raft; tolerates 1 of 3 coordinator failures. (§11 Q2.)
- **NFR-AVAIL-2.** **Data-plane availability target: 99.99%** at `R=ONE` reads, **99.95%** at `R=QUORUM` reads/writes (intra-region). Multi-region availability is best-effort during partitions — the cluster must keep serving in-region traffic during a regional partition (Wiki A10).
- **NFR-AVAIL-3.** **Coordinator failover RTO: < 30 s.** RPO: **0** (Raft commits before ack).
- **NFR-AVAIL-4.** **Data-node failure**: zero data loss as long as RF (default 3) is honoured and ≤ N/2 replicas fail simultaneously per range.
- **NFR-AVAIL-5.** **Rolling upgrades**: zero full-cluster downtime; per-key unavailability bounded to **< 5 s** during the per-node drain window.

### 4.3 Scale targets (v1.0 GA)

These are the minimum proven targets the bench harness and the system test will demonstrate. Higher numbers may be possible — these are the **commitments**.

- **NFR-SCALE-1. Cluster size:** up to **128 data nodes** per cluster, **3 coordinator nodes** (fixed); up to **3 AWS regions**.
- **NFR-SCALE-2. Throughput per cluster:** **≥ 250k ops/s** sustained at `R=QUORUM, W=QUORUM, N=3`, mixed 80/20 read/write, intra-region.
- **NFR-SCALE-3. Throughput per node:** **≥ 5k ops/s** sustained on a 4-vCPU / 8 GiB / NVMe-SSD node at the same workload.
- **NFR-SCALE-4. Key cardinality:** **≤ 1 B keys per cluster** in v1.
- **NFR-SCALE-5. Value size:** **≤ 1 MiB per value**, hard limit (rejected with `INVALID_ARGUMENT` over budget). Default soft warn at 256 KiB.
- **NFR-SCALE-6. Key size:** **≤ 1 KiB per key**, hard.
- **NFR-SCALE-7. Replication factor:** N ∈ {1, 3, 5}; default 3.
- **NFR-SCALE-8. Cross-region:** ≤ **100 ms** inter-region p99 RTT assumed; cross-region replication lag p99 < 2 s under that assumption.

### 4.4 Security

- **NFR-SEC-1.** mTLS on every listener; plaintext refused; no `--insecure` flag in v1.
- **NFR-SEC-2.** Cert rotation is online; default overlap window 24 h.
- **NFR-SEC-3.** Cert source = operator-supplied PKI from disk or k8s Secret. Built-in CA / SPIFFE / Vault deferred (§11 Q8).
- **NFR-SEC-4.** No per-key authorization in v1 — mTLS identity is the auth boundary; admin role is identified by SAN match (§11 Q9).
- **NFR-SEC-5.** Compliance: controls are designed to be SOC2-friendly (audit logs, access boundary, encryption-in-transit). Formal certification is post-GA (§11 Q10).
- **NFR-SEC-6.** Secrets handling: cert private keys read once at startup, never logged, never traced. Verified by an explicit lint rule + a "no key material in OTel" test.

### 4.5 Operability

- **NFR-OPS-1.** OTel-first: traces, metrics, logs. **OTLP gRPC** is the single supported export transport in v1.
- **NFR-OPS-2.** Strategy changes are configuration, not code rebuild (Wiki A8 / NFR-OPS-2).
- **NFR-OPS-3.** Rolling upgrades are the only supported upgrade path; full-cluster restart is not a documented procedure.
- **NFR-OPS-4.** A reference Grafana dashboard ships at `deploy/observability/grafana/gossamer.json` (v1.0 GA).

### 4.6 Portability

- **NFR-PORT-1.** Identical binary across local / k8s / multi-region AWS. Only configuration differs.
- **NFR-PORT-2.** No cloud-specific primitives in the request path. AWS-specific helpers (e.g., IMDSv2 for region detection) are optional and gated behind a `cloud=aws` config block.

### 4.7 API stability

- **NFR-API-1.** gRPC and REST surfaces follow **semver** at the _protocol_ level. Within a major version, only additive changes; no field renames, no field-number reuse.
- **NFR-API-2.** Strategy extension points (`gossip.Strategy`, `conflict.Resolver`, `storage.Backend`) follow Go module semver — additive interface methods become a major bump.

---

## 5. Stack & Tooling Decisions

Anchored in `CLAUDE.md`. Every choice below has a one-line rationale.

| Concern                          | Decision (v1)                                                                               | Rationale                                                                                                                   |
| -------------------------------- | ------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| Language                         | **Go 1.21+**                                                                                | Project rule.                                                                                                               |
| Inter-node RPC                   | **gRPC over mTLS**                                                                          | Project stack; binary-safe; streaming for anti-entropy.                                                                     |
| Client REST                      | **Fiber**                                                                                   | Project stack; fastest Go HTTP framework matching the < 1 ms target.                                                        |
| Coordinator consensus            | **Embedded Raft** (`hashicorp/raft` or `etcd-io/raft` — Architect picks)                    | 3-node group, well-trodden Go libs, no external dependency.                                                                 |
| Coordinator metadata persistence | **PostgreSQL** + Raft log on local disk                                                     | Postgres named in CLAUDE.md; used only for snapshots + partition-map archive (control plane only — never on the data path). |
| Hot-path cache                   | **Redis (cross-instance)** + **in-memory LRU / sync.Map (single-instance)**                 | Project stack; mandatory for the < 1 ms p99 budget.                                                                         |
| Storage backend (data)           | **Pebble** (default) + **memory** (dev/test)                                                | Go-native LSM, RocksDB-compat, no CGO; memory backend for tests.                                                            |
| Gossip protocol                  | **SWIM-style** (one strategy in v1)                                                         | Most-cited lightweight membership protocol; bounded message volume.                                                         |
| Conflict resolution              | **`lww`** (default) + **`siblings`**                                                        | LWW is the simplest correct default for KV; siblings unblock CRDT-style apps without forcing CRDTs into v1.                 |
| Anti-entropy                     | **Per-range Merkle tree**, scheduled comparison                                             | Wiki MUST.                                                                                                                  |
| Partitioning                     | **Consistent hashing with virtual nodes**                                                   | Standard for Dynamo-style KV; enables smooth rebalancing on join/leave. (Architect confirms vnode count.)                   |
| Observability                    | **OpenTelemetry** (OTLP gRPC export) + **Prometheus scrape** for /metrics                   | Project stack; OTLP is the wire format, Prom-scrape is for backwards-compat with existing dashboards.                       |
| CLI                              | `gossamerctl` (Cobra) wrapping the admin gRPC                                               | Standard Go CLI tooling.                                                                                                    |
| Build / lint / bench             | `go build`, `go test`, `golangci-lint`, `./.claude/scripts/bench-check.sh`                  | Per `CLAUDE.md`.                                                                                                            |
| Deployment artefacts             | Single static binary; Helm chart for k8s; Terraform module for AWS (post-v1.0 if necessary) | Per project goal of cloud-agnosticism.                                                                                      |

**Explicitly considered and rejected:**

- **etcd as the coordinator metadata store** — adds an external dependency for what Raft + Postgres already give us.
- **Cassandra-style ring without a coordinator** — loses the "intelligent coordinator" the README explicitly promises.
- **gRPC-Web** as a REST replacement — Fiber is already in the stack and gives us native REST.
- **Per-region active-active in v1** — vector clocks + cross-region merge is correct but operationally complex; deferred (§11 Q11).
- **CRDT default conflict resolution** — too prescriptive for a KV store; LWW + siblings covers 95% and lets the app opt into CRDT semantics via siblings.

---

## 6. Architecture Sketch (informational — Architect owns the binding HLD)

### 6.1 Node roles

- **Coordinator group (3 nodes, Raft).** Owns: cluster membership canonical view, partition map, strategy config, rolling-upgrade orchestration, admin API. **Not on the data path.**
- **Data node (N ≥ 3).** Owns: a subset of the partition ring, the storage backend, the cache layer, the gossip participant, the anti-entropy participant, the client-facing gRPC + Fiber REST surfaces, vector-clock attachment, and conflict resolution.

### 6.2 Request lifecycle (Get / Put)

1. Client → data node A (mTLS-authenticated).
2. Data node A consults its (gossip-current) partition map → identifies replicas R1..Rn.
3. **Get path:** check cache → if hit and `R=ONE`, return (this is the < 1 ms p99 path); else fan out to R1..Rn per the configured `R`, reconcile via vector clocks, apply conflict strategy, populate cache, return.
4. **Put path:** generate / advance vector clock, fan out to R1..Rn per the configured `W`, ack on quorum, invalidate cache, return.
5. OTel span covers the whole call; gRPC trailers carry the vector-clock context for the client.

### 6.3 What the cache layer caches

- **In-memory single-instance:** recent `Get` results keyed by `(key, vector-clock-hash)`; explicit invalidation on local `Put`/`Delete`/repair.
- **Redis cross-instance:** `R=ONE` results across data nodes for hot keys, with TTL + explicit invalidation on `Put`/`Delete`.

The Architect must demonstrate that the cache-hit path stays under **1 ms p99** with `b.ReportAllocs()` on a representative payload distribution, or the feature does not ship.

---

## 7. Delivery Milestones & Dependencies

The project follows the GitFlow-inspired model in `CLAUDE.md`. Milestones below are PRD-level; the Architect's Epics break these down further.

| M#      | Milestone                                                     | Exit criteria                                                                                  | Depends on |
| ------- | ------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- | ---------- |
| **M1**  | PRD signed off                                                | This document approved by EM; Architect engaged                                                | —          |
| **M2**  | HLD + LLD + Epics + Stories drafted                           | Architect output complete; EM approval recorded on PRD                                         | M1         |
| **M3**  | Foundations: storage interface + memory backend + cache layer | Bench gate green; `Get` cache-hit < 1 ms p99 demonstrated                                      | M2         |
| **M4**  | Single-node KV with mTLS + gRPC + REST                        | `Put`/`Get`/`Delete` E2E; mTLS-only listeners; OTel spans on all RPCs                          | M3         |
| **M5**  | Coordinator Raft group + partition map                        | 3-node coordinator survives 1-node loss; partition map gossip-distributed                      | M4         |
| **M6**  | Gossip (SWIM) + membership + failure detection                | Membership convergence within p99 10 s on 32-node cluster                                      | M4         |
| **M7**  | Vector clocks + LWW + siblings                                | Concurrent-write correctness tests green on simulator                                          | M5, M6     |
| **M8**  | Merkle anti-entropy                                           | 1 MiB divergent range repaired in p99 30 s, bounded CPU/BW                                     | M7         |
| **M9**  | Pebble durable backend                                        | Crash-recovery tested; bench gate green                                                        | M3         |
| **M10** | Rolling upgrade machinery + CLI                               | N / N+1 skew demonstrated on 32-node cluster                                                   | M5, M9     |
| **M11** | Multi-region async replication                                | 3-region demo with < 2 s p99 cross-region lag                                                  | M7, M9     |
| **M12** | Reference Grafana + Prometheus dashboards                     | Triage SLO met from telemetry alone                                                            | M4–M11     |
| **M13** | v1.0 GA candidate                                             | All NFR targets met; bench gate green; security review passed; rolling-upgrade rehearsal green | M3–M12     |

External dependencies: Pebble, hashicorp/raft (or etcd raft), gRPC, Fiber, OpenTelemetry SDK, Cobra, Postgres driver. All are well-maintained Go libraries already implied by the stack.

---

## 8. Risks & Mitigations

| #      | Risk                                                                                                                                                          | Likelihood | Impact                                        | Mitigation                                                                                                                                                                                                                                       |
| ------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------- | --------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **R1** | The < 1 ms p99 cache-call SLO bleeds into non-cache paths (e.g., a Get cache miss accidentally gated to 1 ms by a junior contributor wiring the bench wrong). | Medium     | High — false gate failures stall PRs.         | Bench harness explicitly tags **cache-bound** vs **backend-bound** benchmarks; only the cache-bound set is gated at 1 ms. The Architect's LLD must define this taxonomy and the bench harness must enforce it.                                   |
| **R2** | Coordinator HA via embedded Raft is harder than it looks (split-brain on misconfigured peers, snapshot bugs).                                                 | Medium     | High — cluster control plane goes down.       | Use a battle-tested library (`hashicorp/raft` or `etcd raft`) — no in-house consensus. Chaos test: kill 1-of-3 coordinator nodes mid-upgrade as a recurring CI job.                                                                              |
| **R3** | Vector-clock siblings semantics leak into application code in surprising ways (clients that don't merge).                                                     | Medium     | High — silent data loss on concurrent writes. | LWW is the **default**; siblings is opt-in per cluster. Documentation includes worked examples. Client SDK exposes a typed `Siblings` return; non-merging clients get a compile error, not silent collapse.                                      |
| **R4** | Multi-region async replication causes operator-confusing read-after-write violations on cross-region reads.                                                   | High       | Medium                                        | Document explicitly that v1 is **single-home-region per key range**; cross-region reads are eventually consistent. Surface region affinity in the partition-map view. (The active-active deferral in §11 Q11 is a direct response to this risk.) |
| **R5** | Pebble + Raft + cache + gossip + anti-entropy together exceed the per-node memory / CPU budget under load.                                                    | Medium     | High — fails the throughput NFR.              | Per-component CPU/BW caps (NFR-PERF-3); load test at NFR-SCALE-3 numbers gating M13.                                                                                                                                                             |
| **R6** | mTLS-only with no `--insecure` flag makes local-dev painful and contributors quietly disable it via a fork.                                                   | Medium     | Medium                                        | Ship a one-shot `gossamerctl dev-pki` that generates a localhost CA + node certs in 1 command.                                                                                                                                                   |
| **R7** | Rolling-upgrade N / N+1 skew window invites compatibility bugs the team can't catch in tests.                                                                 | Medium     | High — production upgrade breaks.             | Mandatory upgrade-skew integration test in CI: every PR touching a wire-protocol or gossip-message file triggers an automatic v(N) ↔ v(N+1) interop test.                                                                                        |
| **R8** | Open question Q8 (cert source) and Q11 (multi-region) get re-litigated late, blocking Architect.                                                              | High       | High                                          | Defaults documented in §11; user has until `pre-HLD` to override. Architect proceeds on documented defaults.                                                                                                                                     |

**Top 3 risks (carried up to the parent's punch list):** R1, R2, R3.

---

## 9. Acceptance Criteria for v1.0 GA

GA means **all of**:

- All FR-1..FR-16 implemented with PRD-traceable acceptance tests in `internal/.../*_test.go`.
- All NFR-PERF / NFR-AVAIL / NFR-SCALE / NFR-SEC targets demonstrated by the system-test rig at the documented numbers.
- `./.claude/scripts/bench-check.sh` green on `develop` for 7 consecutive days at the GA candidate SHA.
- `golangci-lint run` clean; `go test ./...` green.
- A 32-node-cluster, 3-region rolling-upgrade rehearsal completes with **zero** data-plane downtime > 5 s per key.
- A security review of the mTLS posture, audit-log surface, and cert-rotation procedure is signed off by the security persona's reference reviewer.
- The reference Grafana dashboard answers J-O-5 ("is the cluster healthy / where is the latency / which node is the outlier") on a synthetic outage drill.

---

## 10. Phased Release Plan

- **v1.0 (GA target).** Everything in §3.1 + §3.2 (best effort).
- **v1.1.** Strategy hot-swap (FC-1), per-key/namespace authz (FC-5), one additional non-Go SDK (Java or Python — pick by demand, FC-2).
- **v1.2.** Active-active multi-region (FC-3), web admin UI (FC-4), additional gossip strategies (HyParView/Plumtree).
- **v1.x+.** SPIFFE/Vault cert sources (FC-6), migration importers (FC-7), compliance certifications (FC-8).

---

## 11. Resolution of Wiki §9 Open Questions

For each of the 20 open questions raised by the BA, the resolution below is **the v1 commitment**. Items left in **Outstanding Decisions** (§12) are duplicated there with a `needs-by` deadline.

| #       | Wiki Question                                      | Resolution                                                                                                                                                                                                                                                  | Rejected alternatives                                                                                                                                                                                         |
| ------- | -------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Q1**  | Workload shape & scale targets                     | NFR-SCALE-1..8: 128 nodes / 250k ops/s cluster / 5k ops/s node / 1 B keys / 1 MiB value / 1 KiB key / N∈{1,3,5} default 3. Cross-region p99 RTT ≤ 100 ms assumed.                                                                                           | "Set targets after first beta" — rejected: Architect cannot size partitioning / cache without numbers.                                                                                                        |
| **Q2**  | Coordinator HA model                               | **3-node embedded Raft group**, RTO < 30 s, RPO 0, availability 99.95% target.                                                                                                                                                                              | Single coordinator + cold standby (rejected: RTO too long); gossip-elected leader (rejected: weaker correctness than Raft); external etcd (rejected: extra ops dependency).                                   |
| **Q3**  | Coordinator on the request path?                   | **No.** Coordinator is control-plane only. Data path goes client → data node → replicas. Load-bearing for the < 1 ms cache-call SLO.                                                                                                                        | Coordinator-as-front-door (rejected: SPOF on the data path; can't meet < 1 ms with an extra hop).                                                                                                             |
| **Q4**  | Quorum semantics & defaults                        | Levels: `ONE`, `QUORUM`, `ALL` + explicit `R`/`W`. **Per-request** selectable; per-client default; per-cluster default-of-default `R=QUORUM`, `W=QUORUM`, `N=3`.                                                                                            | Per-cluster only (rejected: too rigid for FR-2); add `LOCAL_QUORUM` (deferred to v1.2 with active-active).                                                                                                    |
| **Q5**  | Conflict-resolution strategies in v1               | **`lww` (default)** + **`siblings`**. Strategy is configuration-driven; vector clocks always attached. Migration between strategies = restart + re-converge via anti-entropy under the new strategy.                                                        | App-supplied merge function as a v1 strategy (deferred to v1.1 with a typed callback API); CRDT default (rejected: too prescriptive).                                                                         |
| **Q6**  | Gossip strategies in v1                            | **SWIM** only in v1; `gossip.Strategy` interface ships so HyParView/Plumtree can be added in v1.2 without breaking changes.                                                                                                                                 | Periodic full-state (rejected: O(N²) message volume); ship two strategies in v1 (deferred: v1 budget).                                                                                                        |
| **Q7**  | Storage backend pluggability + which backends      | **Pluggable via `storage.Backend` interface.** Ships: **`pebble`** (default, durable) + **`memory`** (dev/test). **Postgres = coordinator metadata only** (Raft snapshots + partition-map archive). **Redis = cache only** (cross-instance hot-read cache). | RocksDB via CGO (rejected: CGO complicates cross-platform builds — Pebble is the Go-native equivalent); BoltDB (rejected: single-writer constraint hurts throughput); S3-backed (deferred to v1.x as a tier). |
| **Q8**  | mTLS / PKI source of truth                         | **Operator-supplied PKI from disk or k8s Secret in v1.** Rotation cadence is operator policy; default cert overlap window 24 h.                                                                                                                             | Built-in CA (deferred: chicken-and-egg trust); SPIFFE/SPIRE (deferred to v1.x); Vault-issued (deferred to v1.x).                                                                                              |
| **Q9**  | Authn/Authz beyond mTLS                            | **mTLS identity is the only auth surface in v1.** Admin role identified by SAN match (`admin.<cluster>` SAN).                                                                                                                                               | Per-key ACLs (deferred to v1.1); RBAC with cluster-roles (deferred to v1.1); capability tokens (deferred to v1.x).                                                                                            |
| **Q10** | Compliance posture                                 | **No certifications in v1.** Controls (audit log, mTLS, cert rotation, access boundary) are designed to be SOC2-friendly. **`needs-by: pre-GA`** for any explicit compliance commitment — see §12.                                                          | Commit to SOC2 in v1 (rejected: 6-12 month audit lead time); commit to FedRAMP (rejected: not relevant to current ICP).                                                                                       |
| **Q11** | Multi-region semantics                             | **v1 = single-home-region per key range with async cross-region replication.** Active-active deferred to v1.2. Cross-region p99 lag target < 2 s under ≤ 100 ms inter-region RTT.                                                                           | Active-active in v1 (deferred: vector-clock cross-region merge correctness needs longer soak); single-region only (rejected: README explicitly names multi-region AWS).                                       |
| **Q12** | Rolling-upgrade contract                           | **N / N+1 minor-version skew supported.** Wire protocol semver; gossip and conflict strategy versions carried in gossip; mismatched-major nodes refuse to join.                                                                                             | N / N+2 skew (deferred: cost of two-version compat tests too high); same-version-only (rejected: violates rolling-upgrade goal).                                                                              |
| **Q13** | Strategy hot-swap                                  | **Restart-pace swap in v1** (rolling restart; coordinator orchestrates). Hot-swap deferred to v1.1.                                                                                                                                                         | Hot-swap in v1 (deferred: requires per-call strategy negotiation; not a v1 budget item).                                                                                                                      |
| **Q14** | Client SDK languages                               | **Go SDK only in v1.** Other languages use the gRPC/REST wire protocol directly. Java or Python SDK in v1.1 by demand.                                                                                                                                      | Go + Java + Python in v1 (deferred: SDK matrix doubles the test surface).                                                                                                                                     |
| **Q15** | Edge deployment shape                              | **Edge nodes = full data nodes** running in a separate region tag. **No thin proxy / read-through cache in v1.**                                                                                                                                            | Read-through edge cache (deferred to v1.x); thin proxy (rejected: violates "identical binary across modes").                                                                                                  |
| **Q16** | Observability backends                             | **OTel SDK + OTLP gRPC exporter** as the v1 wire format. **Prometheus scrape** on `/metrics` for back-compat. **Reference Grafana dashboard** ships at `deploy/observability/grafana/`. Tempo/Jaeger optional but un-bundled.                               | Bundle Tempo + Loki (rejected: out-of-scope for a KV store binary); Prometheus only (rejected: traces are first-class per CLAUDE.md).                                                                         |
| **Q17** | DR & backup                                        | Anti-entropy + RF=3 covers replica loss. **`gossamerctl snapshot` ships in v1** for explicit point-in-time snapshot to S3-compatible object storage (FR-18). Restore is offline.                                                                            | Anti-entropy considered DR-sufficient (rejected: doesn't survive accidental delete-all); continuous PITR (deferred to v1.x).                                                                                  |
| **Q18** | Tenancy                                            | **Single-tenant per cluster in v1.** Multi-tenancy with namespace isolation is v1.1.                                                                                                                                                                        | Multi-tenant in v1 (deferred: requires the authz work in Q9).                                                                                                                                                 |
| **Q19** | Public API stability                               | gRPC and REST = **semver at the protocol level**, additive within a major. Strategy interfaces (`gossip.Strategy`, `conflict.Resolver`, `storage.Backend`) follow Go module semver.                                                                         | "Stable from v0" (rejected: locks out learning); no commitment until v2 (rejected: scares early adopters).                                                                                                    |
| **Q20** | Migration path from etcd / Consul / Redis / Dynamo | **None in v1 — clean-slate adoption.** Importers post-GA (v1.x).                                                                                                                                                                                            | Ship a Redis-RDB importer in v1 (deferred: scope creep).                                                                                                                                                      |

**Punch-list summary: all 20 questions resolved with v1 defaults. Two are flagged for confirmatory user sign-off in §12 (Q1 scale numbers, Q10 compliance posture).**

---

## 12. Outstanding Decisions

These are the items where I have set a **delivery-realistic default** but the business should confirm before a hard commit. Each has a `needs-by` tag.

- **OD-1. Scale targets exact numbers (Q1).** _Default:_ 128 nodes / 250k ops/s cluster / 5k ops/s node / 1 B keys / 1 MiB value / 1 KiB key. _Why a default:_ the Architect needs concrete partitioning + cache sizing to start. _Needs-by:_ `pre-HLD` (i.e., before Architect locks the partition / cache design).
- **OD-2. Compliance commitment (Q10).** _Default:_ no certifications in v1; SOC2-friendly controls only. _Why a default:_ certification timelines (6-12 months) are incompatible with v1 GA. _Needs-by:_ `pre-GA` (i.e., before public marketing).
- **OD-3. Multi-region cross-region RTT assumption (Q11/NFR-SCALE-8).** _Default:_ ≤ 100 ms inter-region p99 RTT. _Why a default:_ the cross-region replication lag SLO depends on it. _Needs-by:_ `pre-HLD`.
- **OD-4. Per-region home-region key allocation policy (Q11).** _Default:_ operator picks home region per key prefix at cluster init; no automatic placement. _Why a default:_ automated placement is a v1.2 active-active concern. _Needs-by:_ `pre-HLD`.
- **OD-5. Cert overlap window (FR-6 / NFR-SEC-2).** _Default:_ 24 h. _Why a default:_ matches typical operator rotation cadence. _Needs-by:_ `pre-GA`.
- **OD-6. Bench-gate scope clarification (R1 mitigation).** _Default:_ the Architect tags every benchmark in the repo as `cache-bound` (gated at 1 ms) or `backend-bound` (not gated). _Why a default:_ prevents false gate failures. _Needs-by:_ `pre-LLD` (must be in the LLD before any Story is sliced).

---

## 13. Hand-off

- **This document:** `<projectDIR>/GossamerDB/docs/prds/gossamerdb.md`
- **Upstream wiki:** `<projectDIR>/GossamerDB/docs/wiki/gossamerdb.md`
- **Next agent:** `agent-architect`. Expected outputs in this order:
  1. `docs/hld/gossamerdb.md` (using `hld-spec-architect`)
  2. `docs/lld/gossamerdb.md` (using `go-lld-designer`, consumes the HLD)
  3. `docs/epics/gossamerdb/` (using `superpowers:writing-plans`)
  4. `docs/stories/gossamerdb/<epic>/` (using `task-slicer`, ≤ 300 LOC each)
- **Gate:** `agent-engineering-manager` reviews the Architect's full draft (HLD + LLD + Epics + Stories + this PRD), records `EM-APPROVED: <date>` at the top of this PRD, and only then does Phase 2 begin.
- **Carry-forward SLO:** the **< 1 ms p99 cache-call** budget from `CLAUDE.md` is binding on every cache-bound path the Architect designs. Bypassing the bench gate is not permitted.
