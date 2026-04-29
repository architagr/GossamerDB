# GossamerDB — Product Requirements Document

**Author:** Archit Agarwal
**Status:** Draft v1.4 — derived from `docs/wiki/gossamerdb.md`; confirmed scale & latency targets, Coordinator HA model, data-plane routing topology, and quorum semantics (cluster-level configurable N/R/W with per-request named consistency `{ONE, QUORUM, ALL}` only — Option C) folded in 2026-04-28
**Upstream input:** `docs/wiki/gossamerdb.md`
**Project rules:** `CLAUDE.md`
**Next document:** the HLD at `docs/hld/gossamerdb.md` (HLD → LLD → Epics → Stories), pending design sign-off
**Release target:** v1.0 GA (single coordinated release; v1.x increments listed in §10)

### Revision history

| Date       | Rev  | Change                                                                                                                                                                                                                                                                                          | Driver           |
| ---------- | ---- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------- |
| 2026-04-27 | v1.0 | Initial PRD from the requirements wiki; 20/20 open questions resolved with delivery-realistic defaults.                                                                                                                                                                                                      | Archit Agarwal |
| 2026-04-28 | v1.1 | Q1 (workload shape & scale) confirmed by user with concrete numbers: 1M cluster QPS, 1k per-key QPS, GET p50/p95/p99 = 100 µs / 200 µs / < 1 ms (request-level), PUT p50/p95/p99 = 500 µs / 1 ms / 5 ms, replication `N=5, W=3, R=3`. Cascaded into FR-2, NFR-PERF-1a/1b, NFR-SCALE-2/3/3a/7, NFR-AVAIL-4, the §4.1 budget table, and the Q4 row. GET p99 budget moved from cache-only to **request-level**. | Archit Agarwal |
| 2026-04-28 | v1.2 | Q2 (Coordinator HA model) confirmed and expanded by user: 3-node embedded Raft, strictly control-plane (off the data path), RTO < 30 s, RPO 0, 99.95% availability, total coordinator outage pauses control-plane mutations only — reads/writes continue. Cascaded into the §11 Q2 row; FR-7 / FR-8 / NFR-AVAIL-1 / NFR-AVAIL-3 already captured this stance and are referenced from Q2.                                                                          | Archit Agarwal |
| 2026-04-28 | v1.3 | Q3 (Coordinator vs data-node responsibilities — data-plane routing topology) confirmed by user. Three-tier preference order locked: (1) **smart Go client SDK** with token-aware routing direct to an owning replica, (2) **coordinator-as-replica** fan-out with fastest-3-of-5 wins, (3) **any-data-node forwarding fallback** for REST / non-Go callers. Stateless router tier deferred to v1.x. Cascaded into FR-8 (rewritten), new **FR-20** (smart Go client SDK), §6.2 Request lifecycle (rewritten), §8 risk **R9** (partition-map staleness), and the §11 Q3 row.                                | Archit Agarwal |
| 2026-04-28 | v1.4 | Q4 (Quorum semantics & defaults) confirmed by user — **Option C** chosen: cluster config owns the numerics (`N`, `W`, `R`); per-request consistency is **named only** (`{ONE, QUORUM, ALL}`), no arbitrary numeric R/W tuples allowed at the API surface. Default per-request consistency = `QUORUM`. Default cluster numerics = `N=5, W=3, R=3` (already locked in v1.1). Cascaded into FR-2 (rewritten — "explicit R/W tuples" clause dropped), J-A-1 (request-option syntax updated), FR-20 (SDK accepts named consistency), and the §11 Q4 row. Wiki §9.1 expanded; §9.2 renumbered to 16 items.                                | Archit Agarwal |

> This PRD resolves the 20 open questions raised in `docs/wiki/gossamerdb.md §9`. Every resolution is recorded inline in §11 with the rejected alternatives. Items still requiring business confirmation are listed in §12 ("Outstanding Decisions") with a recommended default and a `needs-by` deadline tag.

---

## 1. Goals & Non-Goals

### 1.1 Goals (v1)

- **G1.** Ship a single distributed key-value binary set (`coordinator` + `datanode`) that runs identically on **local**, **Kubernetes**, and **multi-region AWS** with mode-specific behaviour driven only by configuration.
- **G2.** Expose `Put` / `Get` / `Delete` over both **gRPC** (primary) and **REST via Fiber** (secondary) with **per-request tunable quorum**.
- **G3.** Use a **layered, lightweight gossip stack** for membership and metadata dissemination: **region-aware SWIM** (default, mandatory) for membership + failure detection, plus optional **Plumtree** for efficient bulk dissemination of partition-map / strategy-version updates. Layers are selectable per cluster via configuration.
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

The wiki's three primary personas (Olivia / Adi / Sam) and one tertiary (Contributor) are confirmed. Refined journeys below — each has a measurable success signal that becomes the acceptance criterion for the design phase.

### 2.1 Olivia — Cluster Operator (primary)

- **J-O-1. First cluster bring-up.** Olivia points at a config file describing the gossip strategy, conflict strategy, replication factor (`N=5` default with `W=3`, `R=3` for quorum), and mTLS material. She runs the coordinator binary on three nodes and at least `N` data-node binaries. **Success:** the cluster reaches `Ready` over the admin API within **60 s** for local, **3 min** for k8s, and **10 min** for multi-region AWS (cold start, image already pulled).
- **J-O-2. Scale out / replace a failed node.** Olivia adds (or replaces) a data node. **Success:** gossip propagates membership within **10 s p99**; rebalancing completes for the affected ranges within the configured tolerance and no key drops below its target replica count once stabilized.
- **J-O-3. Strategy change.** Olivia changes the gossip or conflict strategy via the coordinator admin API. **Success in v1:** restart-time swap on a rolling-restart pace; hot-swap is deferred (§11 Q13).
- **J-O-4. Rolling upgrade.** Olivia rolls a new minor version. **Success:** the cluster serves traffic throughout; cluster supports **N and N+1 minor versions running simultaneously** for the duration of the upgrade window; rollback is one rolling-restart away.
- **J-O-5. Observability driven triage.** Olivia answers "is the cluster healthy / where is the latency / which node is the outlier" entirely from the OTel pipeline plus the reference Grafana dashboard, without shelling into nodes.

### 2.2 Adi — Application Engineer (primary)

- **J-A-1. Per-request consistency.** Adi calls `Put(key, value, {consistency: QUORUM})` or `Get(key, {consistency: ONE})` to dial latency vs consistency on the request line. The API accepts only the three named levels — `ONE`, `QUORUM`, `ALL` — and maps them to the cluster-configured `N/W/R` numerics (FR-2). Omitting the option defaults to `QUORUM`. **Success:** the chosen level is honoured; SLO budgets in §3 are met for each level; passing a numeric `R`/`W` returns `INVALID_ARGUMENT`.
- **J-A-2. Conflict surfacing.** When concurrent writers touch the same key, Adi either gets the deterministically-picked winner (`lww` default — highest vector clock wins under the FR-4 total order) or gets **all siblings + their vector-clock contexts in a single `Get` response** (Riak-style `siblings` mode), depending on cluster configuration. App-supplied merge functions are not offered (`siblings` is the supported path). **Success:** vector-clock context round-trips correctly; in `siblings` mode the response carries every concurrent value (no silent collapse), and writing back with a clock that descends from all siblings collapses them.
- **J-A-3. Idempotent retries.** On gRPC `UNAVAILABLE` / `DEADLINE_EXCEEDED`, Adi retries safely. **Success:** server treats retries with the same client-supplied request ID as idempotent for `Put` and `Delete`.

### 2.3 Sam — Security & Compliance Reviewer (secondary)

- **J-S-1. mTLS posture.** Sam verifies that no node accepts a plaintext connection on any port and that client certs are validated against the configured trust bundle. **Success:** integration test `security/no_plaintext_test.go` is part of CI and green.
- **J-S-2. Cert rotation.** Sam rotates the cluster CA. **Success:** rotation is online; no traffic is dropped; old + new certs are accepted simultaneously for the configured overlap window (default **24 h**).
- **J-S-3. Audit trail.** Sam reviews structured audit logs for admin-API calls, mTLS handshake failures, and configuration changes. **Success:** audit events are emitted as a distinct OTel log stream with stable schema.

### 2.4 Contributor (tertiary)

- **J-C-1. New strategy authoring.** A contributor implements a new gossip or conflict strategy by satisfying a small Go interface, adds it to a strategy registry, and selects it via cluster config. The bench gate (`./.claude/scripts/bench-check.sh`) is the contract that proves their strategy does not regress hot-path latency.

---

## 3. Functional Requirements (v1)

The wiki's `F-MUST-*` / `F-SHOULD-*` / `F-COULD-*` items are restated here as PRD-level acceptance criteria. **Cuts** from the wiki list are noted explicitly so the design phase knows what is _not_ in v1.

### 3.1 MUST (v1.0)

- **FR-1. Core KV API.** `Put(key, value, opts)`, `Get(key, opts)`, `Delete(key, opts)` over **gRPC** (primary) and **Fiber-backed REST** (secondary). Both surfaces are 1-to-1; REST is a thin translation layer. Wire schemas live under `pkg/api/`.
- **FR-2. Tunable quorum (named-only, Option C).** Cluster configuration owns the numerics: operators set **`N`, `W`, `R`** at cluster level (default **`N=5, W=3, R=3`** — 3-of-5 strict majority; satisfies `R+W>N` for read-your-writes under non-concurrent updates). The API surface exposes **only three named consistency levels** per request: **`ONE`**, **`QUORUM`** (default, applied when the caller omits the option), **`ALL`**. The cluster maps each name to its configured numerics: `ONE → 1`, `QUORUM → R` (for reads) / `W` (for writes), `ALL → N`. **Arbitrary numeric R/W tuples are not part of the v1 wire protocol** — apps that need a specific number must change the cluster config, not the request. This intentionally rules out the Cassandra-style `Get(k, {R:2})` footgun. Per-client SDK default mirrors the cluster default unless explicitly overridden by name.
- **FR-3. Gossip — layered SWIM + Plumtree.** Two complementary protocols ship in v1, both selectable at cluster bootstrap and **not hot-swappable** (restart-pace change only — see FC-1 / Q13). The protocols solve different layers of the gossip problem and are not alternatives:
  - **`swim` (default, mandatory, region-aware variant).** Membership + failure detection. Always-on — every cluster runs SWIM. The v1 implementation is **region-aware**: each node probes peers **within its own region at full fanout** and probes **cross-region at a reduced fanout** to bound WAN message load and avoid latency-skew-induced false positives (LLD locks exact intra/inter-region probe periods and indirect-probe fanouts). Suspect-state and Lifeguard-style awareness extensions are LLD candidates. Multi-region SWIM emits `region` as a tag on every gossip message so peers can apply region-aware policy without a separate WAN serf.
  - **`plumtree` (operator-enabled, optional second layer).** Efficient bulk dissemination of partition-map updates, strategy-version changes, and other small but cluster-wide payloads. Builds an eager-push spanning tree over the **SWIM-maintained membership view** (no separate HyParView in v1 — deferred to v1.2 once cluster sizes or churn justify it), with lazy-push `IHAVE` repair off-tree. When disabled, those payloads ride SWIM piggyback (the v1 fallback). Plumtree is **always layered on top of SWIM, never instead of it.**
  - **Strategy abstraction.** `internal/gossip.Strategy` is the contract and accepts a layer chain (e.g., `["swim"]` or `["swim", "plumtree"]`). Authoring a new strategy in v1.x (HyParView, alternative FD) does not require an API change.
- **FR-4. Vector clocks + conflict resolution.** Per-key vector clocks attached to every write. Two strategies ship in v1, **selectable at cluster bootstrap and not hot-swappable** (restart-pace change only — see FC-1 / Q13). **Application-supplied merge functions are not a v1 strategy** — `siblings` is the supported path for teams that need application-level merge semantics.
  - **`lww` (default):** on concurrent writes, **highest vector clock wins** under a deterministic total order over the clock (lexicographic over sorted `(nodeId, counter)` entries — LLD locks the exact comparator). No wall-clock timestamps participate in the decision; this prevents clock-skew-driven write loss.
  - **`siblings` (Riak-style):** when a `Get` finds divergent values for a key, the response carries **all sibling values + their vector-clock contexts in a single payload** (no separate `GetSiblings` call). The client picks/merges and writes back with a clock that descends from all siblings, which collapses them on the next write. Anti-entropy preserves siblings; only a descendant write collapses them.
- **FR-5. Merkle anti-entropy.** Per-range Merkle trees compared on a configurable cadence (default **5 min**, with jitter). Divergent ranges reconciled via the active conflict-resolution strategy. Anti-entropy is **bounded** in CPU and bandwidth (see NFR-PERF-3).
- **FR-6. mTLS by default.** Every TCP listener requires mTLS. Plaintext listeners are not buildable into the binary — there is no `--insecure` flag in v1 (Sam-friendly). Cert source is **operator-supplied PKI loaded from disk or from a Kubernetes Secret**; built-in CA / SPIFFE / Vault are deferred (§11 Q8).
- **FR-7. Coordinator HA (control plane).** The Coordinator runs as a **3-node embedded-Raft group** owning cluster metadata (membership, partition map, strategy config, rolling-upgrade orchestration). **Strictly control-plane** — never on the per-request data path (see FR-8). Coordinator failure pauses control-plane mutations only; foreground reads and writes continue uninterrupted. **Durability split:** the **Raft commit log** lives on **each coordinator node's local disk** (Raft requires a synchronous local fsync per commit for soundness — non-negotiable). **Raft snapshots and the partition-map archive** ship to the **operator-selected backup destination** (`s3` or `postgres`, see FR-12). The local Raft log is bounded by snapshot cadence; the snapshot is the durable backstop for full-coordinator-group rebuild.
- **FR-8. Data-plane routing (three-tier preference order).** Clients reach data on a layered path designed to minimise network hops under the < 1 ms GET p99 budget. Tier ordering, in preference:
  1. **Smart Go client SDK (primary path).** The SDK owns a gossip-propagated copy of the partition map (refreshed on epoch change — see FR-20) and routes each `Get` / `Put` directly to one of the **5 owning replicas** by hashing the key. **One client-to-cluster network hop.** This is the path that must clear NFR-PERF-1a / NFR-PERF-1b.
  2. **Coordinator-as-replica fan-out.** The chosen owner replica acts as the **request coordinator** (lowercase — explicitly distinct from the Raft Coordinator group in FR-7). Its local read counts toward the `R=3` quorum; it issues **parallel** reads/writes to the other 4 owners and returns on the **fastest 3 responses** (R=3 of 5 for GET, W=3 of 5 for PUT), reconciling via vector clocks before applying the active conflict-resolution strategy. Maximum **2 parallel cross-replica hops**, not 3 sequential.
  3. **Any-data-node forwarding fallback.** Every data node accepts any request and forwards to the correct owner if it is not itself an owner. Costs one extra intra-AZ hop. Used by REST callers via Fiber, non-Go gRPC clients, and any Go client that has not adopted the smart SDK. The fronting load balancer is a **plain L4 LB (NLB / k8s Service)**; routers are stateless, no session stickiness required.
  **Explicitly NOT in v1:** a separate stateless router tier; token-aware L7 LBs (Envoy/nginx with custom routing). Both are deferred to v1.x by demand — the any-data-node forwarding fallback covers the same use case without adding a deployable. **The Raft Coordinator is never on the per-request path** — this is load-bearing for the < 1 ms GET p99 SLO (§11 Q3).
- **FR-9. Deployment modes.** A single binary runs in three modes; only configuration differs:
  - **Local:** single-host, ports allocated by config.
  - **Kubernetes:** Helm chart + StatefulSet for data nodes, separate StatefulSet for the 3-node coordinator group, headless services for peer discovery.
  - **Multi-region AWS:** EKS-based (k8s mode generalised) with one home region per key range, async cross-region replication, and per-region gossip tiers (see §11 Q11).
- **FR-10. Rolling upgrades.** Coordinator orchestrates a one-at-a-time data-node upgrade with health gates. **N / N+1 minor-version skew is supported.** Rollback = roll the same upgrade in reverse on the previous binary.
- **FR-11. OpenTelemetry instrumentation.** Traces (W3C Trace Context propagated via gRPC metadata and HTTP headers), metrics (RED on the request path; queue depths, gossip round-trip, anti-entropy bytes-repaired on the background paths), and structured logs. **OTLP exporter** is the single supported transport in v1 (§11 Q16).
- **FR-12. Storage — in-memory data nodes + operator-selected backup destination.** **Data-node storage in v1 is in-memory only** (Go `sync.Map` / sharded map; LLD locks the exact structure). No durable per-node data backend ships in v1. The cluster relies on `N=5 / W=3 / R=3` replication and Merkle anti-entropy (FR-5) for in-cluster fault tolerance. **Single-node restarts hydrate via anti-entropy from peers** — no per-node disk persistence is required for the data plane. **A simultaneous loss of all 5 replicas of a key range = data loss in v1**, mitigated only by the snapshot tool (FR-18). Pluggable durable per-node data backend (Pebble, RocksDB, etc.) deferred to v1.x.
  - **Operator-selected backup destination** (cluster-bootstrap config): exactly one of **`s3`** (S3-compatible object storage) or **`postgres`** (PostgreSQL `bytea` table). The chosen destination is shared by **both** consumers — data-node snapshots from `gossamerctl snapshot` (FR-18) and coordinator Raft snapshots / partition-map archive (FR-7). Pluggable via `backup.Destination`; further destinations in v1.x without API breaks.
  - **Sizing guidance.** `s3` recommended for clusters with > ~50 GiB total state or > 32 nodes; `postgres` recommended for small clusters / dev / control-plane-only. LLD locks the exact threshold and operator-warning behaviour.
  - **Redis** is **not** a data backend — it is the **cross-instance read cache** in front of the read path (see NFR-PERF-1). **PostgreSQL** is **not** a hot-path data backend either; it appears only as an optional backup destination as described above.
- **FR-13. Admin API.** gRPC surface on the coordinator Raft leader: membership inspection, partition map dump, anti-entropy trigger, rolling-upgrade orchestration, strategy change (restart-pace). Authenticated via mTLS client cert with an `admin` SAN.
- **FR-14. CLI.** `gossamerctl` wraps the admin API. Ships in v1 (covers all admin RPCs); web UI does not.
- **FR-15. Idempotent writes.** `Put` and `Delete` accept an optional client-supplied request ID; duplicates within a configurable window (default **5 min**) are de-duplicated at the responsible coordinator-replica.
- **FR-16. Audit logging.** Distinct OTel log stream `gossamer.audit` for: admin-API calls, mTLS handshake failures, config changes, strategy changes, rolling-upgrade events, certificate rotation events.

### 3.2 SHOULD (v1.0 — best-effort, may slip to v1.x without breaking GA)

- **FR-17. Reference Grafana dashboard** + **Prometheus scrape config** shipped under `deploy/observability/`. (Wiki F-SHOULD-1 / Q16.)
- **FR-18. Snapshot / restore tool** (`gossamerctl snapshot`) — coordinator-orchestrated **point-in-time per-range snapshot** of in-memory data-node state. **Destination = the cluster's operator-selected backup target (FR-12)**: `s3` for object storage or `postgres` for a `bytea`-backed table. **Restore is offline** (cluster cold-start from snapshot, then anti-entropy reconciles divergence). Same destination is shared with FR-7 coordinator snapshots so operators configure backup once. (§11 Q17.)
- **FR-19. Per-cluster rate limits** on the data plane (token-bucket per client cert SAN) to protect the < 1 ms SLO under abuse.
- **FR-20. Smart Go client SDK — token-aware routing.** The Go SDK keeps a local copy of the partition map and the cluster epoch. It MUST: (a) fetch the partition map at startup from any data node and refresh it on epoch bump; (b) hash each operation's key to identify the owning replicas (consistent-hash + replica count `N`); (c) pick one owner as the connection target and pin the gRPC stream for that key range; (d) handle the server-side `WRONG_OWNER(epoch=X, owners=[...])` redirect by re-fetching the partition map and retrying once; (e) expose the partition-map epoch via a metric so operators can detect staleness; (f) accept the per-request consistency option as one of the three named levels `{ONE, QUORUM, ALL}` (default `QUORUM`) and reject any numeric R/W tuple at compile time (typed enum, not int). Non-Go clients fall back to FR-8 tier 3 (any-data-node forwarding) and use the same named-consistency wire enum. Wire schema for partition-map fetch + redirect + consistency enum lives under `pkg/api/`.

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

All targets below are **measurable** and become bench-gate / SLO-test inputs for the design phase.

### 4.1 Performance — SLO budgets

The project-wide **< 1 ms p99 cache-call SLO** from `CLAUDE.md` is carried forward as a hard gate. Below is the explicit scoping of which call paths are bound by it and which are not.

| Path                                                | Consistency             | p50 budget | p95 budget | p99 budget                | Bound by < 1 ms cache-call SLO?     |
| --------------------------------------------------- | ----------------------- | ---------- | ---------- | ------------------------- | ----------------------------------- |
| **`Get` (any consistency, end-to-end)**             | `R=ONE` or `R=QUORUM=3` | **100 µs** | **200 µs** | **< 1 ms**                | **YES** — bench gate enforces       |
| `Get` cache hit (Redis cross-instance or in-memory) | `R=ONE`                 | < 80 µs    | < 150 µs   | < 500 µs                  | YES — sub-budget of the row above   |
| **`Put` (end-to-end, intra-AZ)**                    | `W=QUORUM=3 of N=5`     | **500 µs** | **1 ms**   | **5 ms**                  | No — replication cost               |
| `Put`/`Get` `R/W=ALL`                               | `ALL`, N=5              | < 2 ms     | < 8 ms     | < 25 ms intra-AZ          | No — slowest-of-5-replicas bound    |
| Cross-region replicated write commit                | async                   | n/a        | n/a        | < 2 s p99 (async)         | No — propagation, not request-bound |
| Gossip round-trip convergence                       | n/a                     | n/a        | n/a        | < 10 s p99 for membership | No — background                     |
| Anti-entropy repair of a 1 MB divergent range       | n/a                     | n/a        | n/a        | < 30 s p99                | No — background                     |

> **GET budget is request-level, not cache-only.** The commitment is **GET p99 < 1 ms regardless of consistency level**, including `R=QUORUM=3`. That tightens this PRD versus prior drafts: every `Get` path — cache hit, cache miss, quorum read — must come in under 1 ms p99 intra-AZ. The cache layer is therefore mandatory on the read path (NFR-PERF-2) and the bench gate now applies to **the full GET request, not just the cache-hit sub-path**. The HLD must show how a `R=3 of 5` quorum read clears 1 ms p99 — likely via parallel replica fan-out, fastest-3-wins, and pre-warmed connection pools. **PUT p99 = 5 ms** is the matching write commitment under `W=3 of 5`.

- **NFR-PERF-1.** Every feature contributes a `*_bench_test.go` file with `b.ReportAllocs()`. The bench gate (`./.claude/scripts/bench-check.sh`) runs in CI and locally before PR ready-for-review.
- **NFR-PERF-1a. GET request budget (request-level, intra-AZ).** p50 ≤ **100 µs**, p95 ≤ **200 µs**, p99 ≤ **1 ms** at `N=5`, `R=3` (quorum) or `R=1`. The bench gate enforces the 1 ms p99 ceiling on the full GET path, not only the cache-hit sub-path.
- **NFR-PERF-1b. PUT request budget (request-level, intra-AZ).** p50 ≤ **500 µs**, p95 ≤ **1 ms**, p99 ≤ **5 ms** at `N=5`, `W=3` (quorum). The bench gate ledger tracks PUT separately and fails on regression; PUT p99 is **not** subject to the 1 ms cache-call SLO (it is replication-bound).
- **NFR-PERF-2.** The cache layer (Redis cross-instance, in-memory LRU / `sync.Map` single-instance) is mandatory for any path subject to the < 1 ms gate. Cache invalidation must be **explicit and tested** (mandatory invalidation test per cache).
- **NFR-PERF-3.** Anti-entropy and gossip are **bounded background work**: each is capped at a per-node CPU share (default 5%) and outbound bandwidth share (default 10% of NIC) — both configurable. They MUST NOT pre-empt foreground request budgets.
- **NFR-PERF-4.** Allocations on the hot path: `Get` cache-hit must hit **0 allocations** in steady state (sync.Pool buffers, pre-sized maps).

### 4.2 Availability & resilience

- **NFR-AVAIL-1.** **Coordinator availability target: 99.95%** (43 m 49 s/month downtime budget). Achieved via 3-node Raft; tolerates 1 of 3 coordinator failures. (§11 Q2.)
- **NFR-AVAIL-2.** **Data-plane availability target: 99.99%** at `R=ONE` reads, **99.95%** at `R=QUORUM` reads/writes (intra-region). Multi-region availability is best-effort during partitions — the cluster must keep serving in-region traffic during a regional partition (Wiki A10).
- **NFR-AVAIL-3.** **Coordinator failover RTO: < 30 s.** RPO: **0** (Raft commits before ack).
- **NFR-AVAIL-4.** **Data-node failure**: zero data loss as long as RF (default `N=5`) is honoured. Tolerates up to **2 simultaneous replica failures per range** while still meeting `W=3` / `R=3` (3-of-5 quorum).
- **NFR-AVAIL-5.** **Rolling upgrades**: zero full-cluster downtime; per-key unavailability bounded to **< 5 s** during the per-node drain window.

### 4.3 Scale targets (v1.0 GA)

These are the minimum proven targets the bench harness and the system test will demonstrate. Higher numbers may be possible — these are the **commitments**.

- **NFR-SCALE-1. Cluster size:** up to **128 data nodes** per cluster, **3 coordinator nodes** (fixed); up to **3 AWS regions**.
- **NFR-SCALE-2. Throughput per cluster:** **≥ 1,000,000 ops/s** sustained at `N=5, W=3, R=3`, mixed 80/20 read/write, intra-region. (Was 250k in earlier drafts; raised per the v1 commitment of 2026-04-28.)
- **NFR-SCALE-3. Throughput per node:** **≥ 8k ops/s** sustained on a 4-vCPU / 8 GiB / NVMe-SSD node at the same workload (1M cluster ops/s ÷ 128 nodes ≈ 7.8k/node; the harness asserts 8k for headroom).
- **NFR-SCALE-3a. Throughput per key (hot key):** **≥ 1,000 ops/s** sustained on a single key without falling out of the GET / PUT latency budgets (NFR-PERF-1a/1b). Above 1k QPS on a single key, behaviour is best-effort and the cache layer absorbs reads; writes contend at the home replica set.
- **NFR-SCALE-4. Key cardinality:** **≤ 1 B keys per cluster** in v1.
- **NFR-SCALE-5. Value size:** **≤ 1 MiB per value**, hard limit (rejected with `INVALID_ARGUMENT` over budget). Default soft warn at 256 KiB.
- **NFR-SCALE-6. Key size:** **≤ 1 KiB per key**, hard.
- **NFR-SCALE-7. Replication factor:** N ∈ {3, 5}; **default 5** with `W=3` / `R=3`. (`N=1` was dropped from the supported set — it cannot satisfy the availability targets.)
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
| Coordinator consensus            | **Embedded Raft** (`hashicorp/raft` or `etcd-io/raft` — picked in the HLD)                  | 3-node group, well-trodden Go libs, no external dependency.                                                                 |
| Coordinator metadata persistence | **Local Raft commit log on disk** + snapshots/archive to operator-selected backup destination | Raft log local for fsync soundness; snapshots ship to the same `s3` / `postgres` destination operators choose for data backups (FR-7, FR-12). |
| Hot-path cache                   | **Redis (cross-instance)** + **in-memory LRU / sync.Map (single-instance)**                 | Project stack; mandatory for the < 1 ms p99 budget.                                                                         |
| Storage backend (data)           | **In-memory only in v1** (sharded `sync.Map`); durable backend deferred to v1.x             | User-confirmed v1 scope (§11 Q7). Cluster fault-tolerance via `N=5/W=3/R=3` + Merkle anti-entropy; cluster-wide outage covered by snapshots. |
| Backup destination               | **Operator-selected: `s3` or `postgres`** (one per cluster); pluggable via `backup.Destination` | Same destination serves both data-node snapshots and coordinator Raft snapshots — single config knob (§11 Q7, Q17, FR-12, FR-18). |
| Gossip protocol                  | **Layered: region-aware SWIM (mandatory) + optional Plumtree**                              | SWIM for membership/FD with within-region full fanout + cross-region reduced fanout; Plumtree as opt-in bulk-dissemination layer over the SWIM view. |
| Conflict resolution              | **`lww`** (default) + **`siblings`**                                                        | LWW is the simplest correct default for KV; siblings unblock CRDT-style apps without forcing CRDTs into v1.                 |
| Anti-entropy                     | **Per-range Merkle tree**, scheduled comparison                                             | Wiki MUST.                                                                                                                  |
| Partitioning                     | **Consistent hashing with virtual nodes**                                                   | Standard for Dynamo-style KV; enables smooth rebalancing on join/leave. (Vnode count is fixed in the HLD.)                  |
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

## 6. Architecture Sketch (informational — the binding architecture lives in the HLD)

### 6.1 Node roles

- **Coordinator group (3 nodes, Raft).** Owns: cluster membership canonical view, partition map, strategy config, rolling-upgrade orchestration, admin API. **Not on the data path.**
- **Data node (N ≥ 3).** Owns: a subset of the partition ring, the **in-memory key-value store** (no per-node disk persistence in v1 — see FR-12), the cache layer, the gossip participant, the anti-entropy participant, the client-facing gRPC + Fiber REST surfaces, vector-clock attachment, and conflict resolution. State on a restarting node hydrates via Merkle anti-entropy from peers; durable backstop for full-cluster outage is `gossamerctl snapshot` to the operator-selected backup destination.

### 6.2 Request lifecycle (Get / Put)

**Primary path (smart Go SDK, FR-8 tier 1 + FR-20):**

1. Client SDK hashes the key → picks one of the 5 owning replicas as the **request coordinator** (lowercase, distinct from the Raft Coordinator) → opens / reuses an mTLS-authenticated gRPC stream to that node. **One client-to-cluster hop.**
2. The request-coordinator replica validates its own ownership against the partition map (gossip-current). If the SDK's epoch is stale, it returns `WRONG_OWNER(epoch=X, owners=[...])` and the SDK re-fetches + retries once (FR-20.d).
3. **Get path:** check cache → if hit and `R=ONE`, return (cache-bound p99 < 500 µs sub-budget). Otherwise: read locally **and in parallel** issue reads to the other 4 owners; return on the **fastest 3 responses** (`R=3` of 5); reconcile via vector clocks; apply conflict strategy; populate cache; return. **Two parallel cross-replica hops max.**
4. **Put path:** generate / advance the vector clock; write locally **and in parallel** to the other 4 owners; ack on the **fastest 3 commits** (`W=3` of 5); invalidate cache; return.
5. OTel span covers the whole call (one parent + one child per fan-out leg); gRPC trailers carry the vector-clock context back to the client.

**Fallback path (REST / non-Go / dumb client, FR-8 tier 3):**

1. Client → L4 LB → arbitrary data node A.
2. If A is one of the 5 owners for the key, A becomes the request coordinator and the flow continues as steps 3–5 above. **One extra hop avoided.**
3. If A is **not** an owner, A forwards once to a chosen owner B (one extra ~150–300 µs hop), and B becomes the request coordinator. The fallback path therefore costs at most one additional intra-AZ hop relative to the primary path; it is not subject to the GET p99 < 1 ms request-level budget for these callers but is still gated by the per-component bench harness.

### 6.3 What the cache layer caches

- **In-memory single-instance:** recent `Get` results keyed by `(key, vector-clock-hash)`; explicit invalidation on local `Put`/`Delete`/repair.
- **Redis cross-instance:** `R=ONE` results across data nodes for hot keys, with TTL + explicit invalidation on `Put`/`Delete`.

The HLD must demonstrate that the cache-hit path stays under **1 ms p99** with `b.ReportAllocs()` on a representative payload distribution, or the feature does not ship.

---

## 7. Delivery Milestones & Dependencies

The project follows the GitFlow-inspired model in `CLAUDE.md`. Milestones below are PRD-level; the Epics break these down further.

| M#      | Milestone                                                     | Exit criteria                                                                                  | Depends on |
| ------- | ------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- | ---------- |
| **M1**  | PRD signed off                                                | This document approved; design phase started                                                   | —          |
| **M2**  | HLD + LLD + Epics + Stories drafted                           | Design package complete; sign-off recorded on PRD                                              | M1         |
| **M3**  | Foundations: in-memory data store + cache layer               | Bench gate green; `Get` cache-hit < 1 ms p99 demonstrated; sharded in-memory store survives 1M-key load test | M2         |
| **M4**  | Single-node KV with mTLS + gRPC + REST                        | `Put`/`Get`/`Delete` E2E; mTLS-only listeners; OTel spans on all RPCs                          | M3         |
| **M5**  | Coordinator Raft group + partition map                        | 3-node coordinator survives 1-node loss; partition map gossip-distributed                      | M4         |
| **M6**  | Gossip (region-aware SWIM + optional Plumtree) + membership + FD | Membership convergence within p99 10 s on 32-node cluster; Plumtree dissemination of partition-map updates demonstrated | M4         |
| **M7**  | Vector clocks + LWW + siblings                                | Concurrent-write correctness tests green on simulator                                          | M5, M6     |
| **M8**  | Merkle anti-entropy + single-node restart hydration           | 1 MiB divergent range repaired in p99 30 s, bounded CPU/BW; restarting node refills its keys via anti-entropy with cluster QPS unaffected | M7         |
| **M9**  | Backup destinations (`s3` + `postgres`) + `gossamerctl snapshot` + offline restore | Snapshot/restore round-trip green for both destinations; same destination drives coordinator Raft snapshots; cluster cold-start from snapshot reaches `Ready` | M3, M5     |
| **M10** | Rolling upgrade machinery + CLI                               | N / N+1 skew demonstrated on 32-node cluster                                                   | M5, M9     |
| **M11** | Multi-region async replication                                | 3-region demo with < 2 s p99 cross-region lag                                                  | M7, M9     |
| **M12** | Reference Grafana + Prometheus dashboards                     | Triage SLO met from telemetry alone                                                            | M4–M11     |
| **M13** | v1.0 GA candidate                                             | All NFR targets met; bench gate green; security review passed; rolling-upgrade rehearsal green | M3–M12     |

External dependencies: hashicorp/raft (or etcd raft), gRPC, Fiber, OpenTelemetry SDK, Cobra, AWS SDK (S3 backup destination), Postgres driver (Postgres backup destination). All are well-maintained Go libraries already implied by the stack. **Note:** Pebble drops out of v1's external-dependency list now that the data-node backend is in-memory — it returns in v1.x as the first pluggable durable backend.

---

## 8. Risks & Mitigations

| #      | Risk                                                                                                                                                          | Likelihood | Impact                                        | Mitigation                                                                                                                                                                                                                                       |
| ------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------- | --------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **R1** | The < 1 ms p99 cache-call SLO bleeds into non-cache paths (e.g., a Get cache miss accidentally gated to 1 ms by a junior contributor wiring the bench wrong). | Medium     | High — false gate failures stall PRs.         | Bench harness explicitly tags **cache-bound** vs **backend-bound** benchmarks; only the cache-bound set is gated at 1 ms. The LLD must define this taxonomy and the bench harness must enforce it.                                   |
| **R2** | Coordinator HA via embedded Raft is harder than it looks (split-brain on misconfigured peers, snapshot bugs).                                                 | Medium     | High — cluster control plane goes down.       | Use a battle-tested library (`hashicorp/raft` or `etcd raft`) — no in-house consensus. Chaos test: kill 1-of-3 coordinator nodes mid-upgrade as a recurring CI job.                                                                              |
| **R3** | Vector-clock siblings semantics leak into application code in surprising ways (clients that don't merge).                                                     | Medium     | High — silent data loss on concurrent writes. | LWW is the **default**; siblings is opt-in per cluster. Documentation includes worked examples. Client SDK exposes a typed `Siblings` return; non-merging clients get a compile error, not silent collapse.                                      |
| **R4** | Multi-region async replication causes operator-confusing read-after-write violations on cross-region reads.                                                   | High       | Medium                                        | Document explicitly that v1 is **single-home-region per key range**; cross-region reads are eventually consistent. Surface region affinity in the partition-map view. (The active-active deferral in §11 Q11 is a direct response to this risk.) |
| **R5** | **In-memory-only data backend blows the per-node RAM budget** (full key set fits in RAM at v1 scale numbers; replication fan-out and cache layer compete for the same memory). | Medium-High | High — fails the throughput NFR or OOMs the node. | Per-component RAM caps (NFR-PERF-3); load test at NFR-SCALE-3 numbers gating M13. **LLD must publish a per-node RAM sizing formula** (`replicas_owned × avg_key + avg_value + cache_overhead`). README explicitly markets v1 as "fits in RAM"; clusters that exceed RAM are a v1.x durable-backend use case. Backup-destination Postgres path is sized for control-plane-only / dev — sizing-guidance check fires a warning if the operator picks `postgres` for a cluster larger than the threshold (FR-12). |
| **R6** | mTLS-only with no `--insecure` flag makes local-dev painful and contributors quietly disable it via a fork.                                                   | Medium     | Medium                                        | Ship a one-shot `gossamerctl dev-pki` that generates a localhost CA + node certs in 1 command.                                                                                                                                                   |
| **R7** | Rolling-upgrade N / N+1 skew window invites compatibility bugs the team can't catch in tests.                                                                 | Medium     | High — production upgrade breaks.             | Mandatory upgrade-skew integration test in CI: every PR touching a wire-protocol or gossip-message file triggers an automatic v(N) ↔ v(N+1) interop test.                                                                                        |
| **R8** | Open question Q8 (cert source) and Q11 (multi-region) get re-litigated late, blocking the design phase.                                                              | High       | High                                          | Defaults documented in §11; the override window closes `pre-HLD`. Design phase proceeds on documented defaults.                                                                                                                                     |
| **R9** | Smart-client partition-map staleness (epoch lag): the Go SDK routes a `Get` / `Put` to a node that is no longer an owner after a topology change, costing a forced redirect on every in-flight request and inflating GET p99 above 1 ms during membership churn. | Medium     | High — direct hit on the GET p99 SLO during scale-out / node replacement. | Server returns `WRONG_OWNER(epoch=X, owners=[...])` on epoch mismatch (FR-20.d); SDK refreshes the partition map and retries **once**. The redirect responder is local — no additional fan-out. Bench harness includes a "topology-churn" benchmark that injects an epoch bump every N requests and asserts GET p99 stays under 1.5 ms during the churn window (degraded budget acknowledged during membership change). Operators see partition-map epoch drift as a metric (FR-20.e) and a Grafana panel. |

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

For each of the 20 open questions raised in the wiki §9, the resolution below is **the v1 commitment**. Items left in **Outstanding Decisions** (§12) are duplicated there with a `needs-by` deadline.

| #       | Wiki Question                                      | Resolution                                                                                                                                                                                                                                                  | Rejected alternatives                                                                                                                                                                                         |
| ------- | -------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Q1**  | Workload shape & scale targets                     | **Confirmed 2026-04-28.** Cluster QPS ≥ **1M ops/s**; per-key QPS ≥ **1k ops/s**; **GET** p50 100 µs / p95 200 µs / p99 < 1 ms (request-level); **PUT** p50 500 µs / p95 1 ms / p99 5 ms; replication **N=5** with **W=3, R=3** quorum. Cluster sized for 128 nodes / 1 B keys / 1 MiB value / 1 KiB key. Cross-region p99 RTT ≤ 100 ms assumed. See NFR-PERF-1a/1b and NFR-SCALE-1..8.                                                                                                                                           | "Set targets after first beta" — rejected: the design phase cannot size partitioning / cache without numbers. Earlier draft default of N=3 / 250k cluster QPS / 1 ms cache-only — superseded by the request-level budgets and N=5 commitment.                                                                                                        |
| **Q2**  | Coordinator HA model                               | **Confirmed 2026-04-28.** **3-node embedded Raft group (leader-elected consensus).** Strictly a **control-plane role** — not on the data path, not active-active, not a stateless front door. Owns membership, partition map, strategy config, and rolling-upgrade orchestration. **RTO < 30 s** (remaining nodes hold a Raft election to restore control-plane availability). **RPO = 0** (control-plane mutations are committed to a Raft quorum before ack — no committed metadata is lost on failover). **Availability target 99.95%**, tolerates 1-of-3 node loss. A complete coordinator-group outage **pauses control-plane mutations only** (node adds, strategy changes); foreground reads and writes continue uninterrupted because the data path does not flow through the coordinator. See FR-7, FR-8, NFR-AVAIL-1, NFR-AVAIL-3. | Single coordinator + cold standby (rejected: RTO too long); gossip-elected leader (rejected: weaker correctness than Raft); external etcd (rejected: extra ops dependency); active-active coordinator set (rejected: complicates metadata-mutation correctness with no data-path benefit since coordinator is off the request path); stateless coordinator over external consensus (rejected: no operational gain over embedded Raft and adds a third dependency to deploy).                                   |
| **Q3**  | Coordinator on the request path? / data-plane routing topology | **Confirmed 2026-04-28.** Raft Coordinator is **never** on the per-request path. Data-plane routing follows a three-tier preference order (FR-8): **(1) smart Go client SDK** with token-aware routing direct to one of the 5 owning replicas — primary path, one client-to-cluster hop, must clear NFR-PERF-1a/1b; **(2) coordinator-as-replica fan-out** — chosen owner becomes the request coordinator (lowercase), local read counts toward `R=3`, parallel fastest-3-of-5 to peers, max 2 cross-replica hops; **(3) any-data-node forwarding fallback** for REST / non-Go / dumb clients via a plain L4 LB, one extra intra-AZ hop. Smart Go SDK responsibilities (partition-map fetch, epoch refresh, key hashing, `WRONG_OWNER` redirect handling) are FR-20. See FR-7 / FR-8 / FR-20, §6.2 Request lifecycle, and risk R9 (partition-map staleness). | LB → "master data nodes" → consistent-hash forward to owner (rejected: extra hop on every request when the master node is not the owner; probability of a random master being in the owner set is N/cluster-size, which shrinks as the cluster grows — incompatible with GET p99 < 1 ms). Coordinator-as-front-door (rejected: SPOF on the data path; cannot meet < 1 ms with an extra hop). Token-aware L7 LB (Envoy/nginx with custom routing logic) (deferred: most cloud LBs cannot do this without scripting, and the smart Go SDK achieves the same routing without that complexity). Stateless router tier as a separate deployable in v1 (deferred to v1.x by demand: the any-data-node forwarding fallback covers the same use case for free). |
| **Q4**  | Quorum semantics & defaults                        | **Confirmed by user 2026-04-28 — Option C (named-only, cluster owns numerics).** Cluster configuration owns `N`, `W`, `R` (default `N=5, W=3, R=3`, 3-of-5 strict majority; `R+W>N` for read-your-writes). Per-request consistency is a **named enum** of `{ONE, QUORUM, ALL}`; omitting it defaults to `QUORUM`. The cluster maps names to its numerics (`ONE→1`, `QUORUM→R for reads / W for writes`, `ALL→N`). **Arbitrary numeric R/W per request is not allowed** at the wire level — apps that need a specific number change the cluster config. SDK enforces the enum via a typed Go type so misuse is a compile error, not a runtime footgun. See FR-2, FR-20, J-A-1.        | **Option A: cluster-only, no per-request override** (rejected: status-page reads / presence pings / cache-warmth checks legitimately want `ONE`; forcing them through `QUORUM` blows the latency budget on workloads that don't need consistency). **Option B: per-request arbitrary numeric R/W tuples (Cassandra-classic)** (rejected: every "we got the consistency we deserved" production incident in distributed-DB history starts here — `Get(k, {R:1})` on the billing path is the textbook footgun). **Option D: per-keyspace N/R/W** (deferred: keyspaces are not a v1 concept; v1 has a flat keyspace). **Add `LOCAL_QUORUM`** (deferred to v1.2 alongside active-active multi-region). **`N=3, W=2, R=2`** (rejected: user committed to `N=5` on 2026-04-28 to tolerate 2 simultaneous replica failures while keeping 3-way quorum). |
| **Q5**  | Conflict-resolution strategies in v1               | **Confirmed by user 2026-04-29.** Two strategies ship: **`lww` (default)** and **`siblings`**, configurable at the cluster level, vector clocks always attached. **`lww`** resolves concurrent writes by **highest vector clock** under a deterministic total order over the clock (lexicographic over sorted `(nodeId, counter)` entries — LLD locks the comparator); no wall-clock timestamps participate. **`siblings` is Riak-style:** a `Get` over a divergent key returns **all sibling values + vector-clock contexts in a single response payload**, and the client collapses them by writing back with a clock that descends from all siblings. **Strategy is set at cluster bootstrap and is not hot-swappable** (rolling-restart swap only — see Q13). Migration between strategies = rolling restart + re-converge via anti-entropy under the new strategy. **README markets `lww` as the default and `siblings` as the operator-selectable alternative for teams that need to surface concurrent writes.** See FR-4, J-A-2, FC-1. | **App-supplied merge function as a v1 strategy** (rejected: `siblings` is the supported path for teams that need app-level merge — keeps the v1 strategy surface minimal and avoids a second extension point with its own correctness/perf risks; revisitable post-v1 only if siblings proves insufficient). **CRDT default** (rejected: too prescriptive — apps that want CRDT semantics build them on top of `siblings`). **Wall-clock timestamps as the LWW comparator** (rejected: clock skew silently loses writes — vector-clock total order avoids this). **Hot-swap of strategies in v1** (deferred to v1.1 — see Q13). |
| **Q6**  | Gossip strategies in v1                            | **Confirmed by user 2026-04-29 — Option B: layered SWIM + Plumtree.** Two complementary protocols ship: **`swim` (default, mandatory, region-aware variant)** for membership + failure detection, and **`plumtree` (operator-enabled, optional)** as a second layer for efficient bulk dissemination of partition-map and strategy-version updates. They are **not interchangeable** — Plumtree is **always layered on top of SWIM**, never instead of it. **Region-aware SWIM** probes within-region at full fanout and cross-region at reduced fanout, with `region` tagged on every gossip message; LLD locks exact intra/inter-region probe periods and indirect-probe fanouts. Plumtree builds its eager-push spanning tree over SWIM's membership view (no separate HyParView in v1). When Plumtree is disabled, partition-map / strategy-version updates ride SWIM piggyback. Strategy is bootstrap-only and not hot-swappable (Q13). See FR-3, J-O-3. | **`plumtree`-only in v1** (rejected: Plumtree is a broadcast protocol, not a failure detector — would force us to invent an FD; SWIM is battle-tested via Hashicorp Memberlist / Consul / Nomad / CockroachDB). **HyParView in v1** (deferred to v1.2: peer-sampling overlay's value is marginal at the 128-node target; Plumtree on the SWIM view is sufficient until churn or scale justifies it). **Naive cross-region SWIM (no region awareness)** (rejected: noisy on WAN, latency-skew triggers false-positive failure detections — Cassandra and Consul both learned this the hard way and added region/datacenter awareness retroactively). **Periodic full-state anti-entropy** (rejected: O(N²) message volume — incompatible with NFR-PERF-3's bounded-background-work rule). **Phi accrual / Lifeguard as a separate strategy** (rolled into SWIM as LLD-time enhancements, not a separate top-level strategy). |
| **Q7**  | Storage backend pluggability + which backends      | **Confirmed by user 2026-04-29.** **Data-node storage is in-memory only in v1** (no durable per-node data backend). Cluster relies on `N=5/W=3/R=3` + Merkle anti-entropy for in-cluster fault tolerance; single-node restarts hydrate via peer anti-entropy. **Cluster-wide outage = data loss for affected key ranges**, mitigated only by snapshots. **Operator-selected backup destination** at cluster bootstrap: exactly one of **`s3`** (object storage) or **`postgres`** (`bytea` table). Same destination serves BOTH data-node snapshots (FR-18) and coordinator Raft snapshots / partition-map archive (FR-7). Backup is pluggable via a `backup.Destination` interface; additional destinations in v1.x without API breaks. **Raft commit log stays on each coordinator's local disk** (Raft requires it). **Redis = cross-instance read cache only**, never a backend. **Pluggable durable per-node data backend (Pebble / RocksDB / S3-tiered) deferred to v1.x.** README explicitly markets v1 as "in-memory clustered KV store; durable persistence ships in v1.x." See FR-7, FR-12, FR-18, NFR-PERF-1. | **Pebble durable backend in v1 (prior PRD draft)** (rejected: user opted for in-memory-only v1 to keep scope focused; Pebble lands in v1.x as the first pluggable durable backend). **RocksDB via CGO** (rejected: CGO complicates cross-platform builds — Pebble is the Go-native equivalent for v1.x). **BoltDB** (rejected: single-writer constraint hurts throughput). **S3-backed live data backend** (deferred to v1.x as a tier; v1 uses S3 only as a backup destination). **Single hard-coded backup destination (S3-only, prior PRD draft)** (rejected: user wants operator choice — small clusters / dev want Postgres, large clusters want S3). **Per-node disk persistence to bound restart loss** (rejected: anti-entropy from peers is sufficient for single-node restart; adds disk-management complexity for marginal benefit). |
| **Q8**  | mTLS / PKI source of truth                         | **Operator-supplied PKI from disk or k8s Secret in v1.** Rotation cadence is operator policy; default cert overlap window 24 h.                                                                                                                             | Built-in CA (deferred: chicken-and-egg trust); SPIFFE/SPIRE (deferred to v1.x); Vault-issued (deferred to v1.x).                                                                                              |
| **Q9**  | Authn/Authz beyond mTLS                            | **mTLS identity is the only auth surface in v1.** Admin role identified by SAN match (`admin.<cluster>` SAN).                                                                                                                                               | Per-key ACLs (deferred to v1.1); RBAC with cluster-roles (deferred to v1.1); capability tokens (deferred to v1.x).                                                                                            |
| **Q10** | Compliance posture                                 | **No certifications in v1.** Controls (audit log, mTLS, cert rotation, access boundary) are designed to be SOC2-friendly. **`needs-by: pre-GA`** for any explicit compliance commitment — see §12.                                                          | Commit to SOC2 in v1 (rejected: 6-12 month audit lead time); commit to FedRAMP (rejected: not relevant to current ICP).                                                                                       |
| **Q11** | Multi-region semantics                             | **v1 = single-home-region per key range with async cross-region replication.** Active-active deferred to v1.2. Cross-region p99 lag target < 2 s under ≤ 100 ms inter-region RTT.                                                                           | Active-active in v1 (deferred: vector-clock cross-region merge correctness needs longer soak); single-region only (rejected: README explicitly names multi-region AWS).                                       |
| **Q12** | Rolling-upgrade contract                           | **N / N+1 minor-version skew supported.** Wire protocol semver; gossip and conflict strategy versions carried in gossip; mismatched-major nodes refuse to join.                                                                                             | N / N+2 skew (deferred: cost of two-version compat tests too high); same-version-only (rejected: violates rolling-upgrade goal).                                                                              |
| **Q13** | Strategy hot-swap                                  | **Restart-pace swap in v1** (rolling restart; coordinator orchestrates). Hot-swap deferred to v1.1.                                                                                                                                                         | Hot-swap in v1 (deferred: requires per-call strategy negotiation; not a v1 budget item).                                                                                                                      |
| **Q14** | Client SDK languages                               | **Go SDK only in v1.** Other languages use the gRPC/REST wire protocol directly. Java or Python SDK in v1.1 by demand.                                                                                                                                      | Go + Java + Python in v1 (deferred: SDK matrix doubles the test surface).                                                                                                                                     |
| **Q15** | Edge deployment shape                              | **Edge nodes = full data nodes** running in a separate region tag. **No thin proxy / read-through cache in v1.**                                                                                                                                            | Read-through edge cache (deferred to v1.x); thin proxy (rejected: violates "identical binary across modes").                                                                                                  |
| **Q16** | Observability backends                             | **OTel SDK + OTLP gRPC exporter** as the v1 wire format. **Prometheus scrape** on `/metrics` for back-compat. **Reference Grafana dashboard** ships at `deploy/observability/grafana/`. Tempo/Jaeger optional but un-bundled.                               | Bundle Tempo + Loki (rejected: out-of-scope for a KV store binary); Prometheus only (rejected: traces are first-class per CLAUDE.md).                                                                         |
| **Q17** | DR & backup                                        | **Confirmed by user 2026-04-29.** Anti-entropy + `N=5` (3-of-5 replication) covers in-cluster replica loss. **`gossamerctl snapshot` ships in v1** for operator-triggered point-in-time per-range snapshots, written to the **operator-selected backup destination (`s3` or `postgres`, see Q7 / FR-12)** — same destination as coordinator Raft snapshots, so operators configure backup once. Restore is offline. Snapshots are the explicit DR path for accidental delete-all and full-cluster outage in an in-memory-only v1.                                                                            | Anti-entropy alone considered DR-sufficient (rejected: doesn't survive accidental delete-all or full-cluster outage — even more critical now that v1 has no durable data backend); single hard-coded backup destination (rejected per Q7: operator chooses); continuous PITR (deferred to v1.x — point-in-time snapshots are the v1 floor).                                                                                  |
| **Q18** | Tenancy                                            | **Single-tenant per cluster in v1.** Multi-tenancy with namespace isolation is v1.1.                                                                                                                                                                        | Multi-tenant in v1 (deferred: requires the authz work in Q9).                                                                                                                                                 |
| **Q19** | Public API stability                               | gRPC and REST = **semver at the protocol level**, additive within a major. Strategy interfaces (`gossip.Strategy`, `conflict.Resolver`, `storage.Backend`) follow Go module semver.                                                                         | "Stable from v0" (rejected: locks out learning); no commitment until v2 (rejected: scares early adopters).                                                                                                    |
| **Q20** | Migration path from etcd / Consul / Redis / Dynamo | **None in v1 — clean-slate adoption.** Importers post-GA (v1.x).                                                                                                                                                                                            | Ship a Redis-RDB importer in v1 (deferred: scope creep).                                                                                                                                                      |

**Punch-list summary: all 20 questions resolved with v1 defaults. Two are flagged for confirmatory user sign-off in §12 (Q1 scale numbers, Q10 compliance posture).**

---

## 12. Outstanding Decisions

These are the items where I have set a **delivery-realistic default** but the business should confirm before a hard commit. Each has a `needs-by` tag.

- **OD-1. Scale targets exact numbers (Q1).** _Default:_ 128 nodes / 250k ops/s cluster / 5k ops/s node / 1 B keys / 1 MiB value / 1 KiB key. _Why a default:_ the design phase needs concrete partitioning + cache sizing to start. _Needs-by:_ `pre-HLD` (i.e., before the partition / cache design is locked).
- **OD-2. Compliance commitment (Q10).** _Default:_ no certifications in v1; SOC2-friendly controls only. _Why a default:_ certification timelines (6-12 months) are incompatible with v1 GA. _Needs-by:_ `pre-GA` (i.e., before public marketing).
- **OD-3. Multi-region cross-region RTT assumption (Q11/NFR-SCALE-8).** _Default:_ ≤ 100 ms inter-region p99 RTT. _Why a default:_ the cross-region replication lag SLO depends on it. _Needs-by:_ `pre-HLD`.
- **OD-4. Per-region home-region key allocation policy (Q11).** _Default:_ operator picks home region per key prefix at cluster init; no automatic placement. _Why a default:_ automated placement is a v1.2 active-active concern. _Needs-by:_ `pre-HLD`.
- **OD-5. Cert overlap window (FR-6 / NFR-SEC-2).** _Default:_ 24 h. _Why a default:_ matches typical operator rotation cadence. _Needs-by:_ `pre-GA`.
- **OD-6. Bench-gate scope clarification (R1 mitigation).** _Default:_ every benchmark in the repo is tagged as `cache-bound` (gated at 1 ms) or `backend-bound` (not gated). _Why a default:_ prevents false gate failures. _Needs-by:_ `pre-LLD` (must be in the LLD before any Story is sliced).

---

## 13. Hand-off

- **This document:** `docs/prds/gossamerdb.md`
- **Upstream wiki:** `docs/wiki/gossamerdb.md`
- **Next documents** (in order):
  1. `docs/hld/gossamerdb.md` — High-Level Design.
  2. `docs/lld/gossamerdb.md` — Low-Level Design (consumes the HLD).
  3. `docs/epics/gossamerdb/` — Epics breakdown.
  4. `docs/stories/gossamerdb/<epic>/` — implementation stories, ≤ 300 LOC each.
- **Sign-off:** the full design package (HLD + LLD + Epics + Stories + this PRD) is reviewed end-to-end and the approval date is recorded as `APPROVED: <date>` at the top of this PRD before implementation starts.
- **Carry-forward SLO:** the **< 1 ms p99 cache-call** budget from `CLAUDE.md` is binding on every cache-bound path the design covers. Bypassing the bench gate is not permitted.
