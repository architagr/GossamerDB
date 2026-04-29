# GossamerDB

> A distributed, cloud-agnostic, **in-memory** key-value store with sub-millisecond reads, tunable quorum, and secure-by-default operation across local, Kubernetes, and multi-region AWS — all from a single binary.

GossamerDB combines a region-aware SWIM gossip protocol (with an optional Plumtree dissemination layer), Merkle-tree anti-entropy repair, per-key vector clocks with operator-selectable conflict resolution (`lww` default, `siblings` opt-in), and tunable quorum semantics to deliver strong consistency, fast convergence, and predictable latency at scale. Operators run the same binary everywhere; behaviour is driven entirely by configuration.

> **Status: pre-1.0, in active design.** APIs and config surfaces will change before the v1.0 GA release. Treat this as an evaluation target, not a production deployment. See [Roadmap](#roadmap) for current state.

> **v1 is in-memory.** Data-node state lives in RAM and replicates 5 ways across the cluster (`N=5/W=3/R=3`). Single-node restarts heal automatically via Merkle anti-entropy. **A simultaneous loss of all 5 replicas of a key range = data loss in v1**, mitigated by operator-triggered snapshots to S3 or PostgreSQL (your choice). **Pluggable durable per-node persistence ships in v1.x.** Pick GossamerDB v1 if you need a clustered, replicated, in-memory KV store with strong consistency on demand; pick a durable store (or wait for v1.x) if your data must survive a full-cluster outage without operator intervention.

## Why GossamerDB

| If you need...                                                          | GossamerDB gives you                                                                                                                  |
| ----------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------- |
| **Sub-millisecond reads at the request level** under quorum consistency | GET p99 < 1 ms with `N=5, R=3` via parallel fastest-of-5 fan-out and a mandatory cache layer.                                         |
| **Tunable consistency without the footgun**                             | Per-request named levels — `ONE`, `QUORUM`, `ALL` — that map to operator-controlled numerics. No raw `R=1` calls on the billing path. |
| **Operate the same way on a laptop, a k8s cluster, or 3 AWS regions**   | One binary. Mode is config. No cloud-specific primitives on the request path.                                                         |
| **Secure by default**                                                   | mTLS on every listener; plaintext is not buildable. Cert rotation online with a 24 h overlap window.                                  |
| **Resilient cluster ops**                                               | Rolling upgrades with N / N+1 minor-version skew. Node loss healed by Merkle anti-entropy without taking traffic offline.             |
| **First-class observability**                                           | OpenTelemetry traces, metrics, and logs across the request path, gossip, and anti-entropy. Reference Grafana dashboard ships in v1.   |

## Performance commitments (v1.0 GA)

These are the published numbers the bench gate enforces. They are commitments — not aspirations.

| Operation                      | p50    | p95    | p99     | Notes                                                   |
| ------------------------------ | ------ | ------ | ------- | ------------------------------------------------------- |
| **GET** (any consistency)      | 100 µs | 200 µs | < 1 ms  | Request-level, intra-AZ, `N=5, R=3 of 5`.               |
| **PUT** (quorum write)         | 500 µs | 1 ms   | 5 ms    | `N=5, W=3 of 5`, intra-AZ.                              |
| Cluster throughput             | —      | —      | —       | ≥ **1,000,000 ops/s** sustained, 80/20 read/write mix.  |
| Hot-key throughput             | —      | —      | —       | ≥ **1,000 ops/s** on a single key without budget loss.  |
| Cross-region replication lag   | —      | —      | < 2 s   | Async; assumes ≤ 100 ms inter-region p99 RTT.           |
| Coordinator failover (RTO)     | —      | —      | < 30 s  | RPO 0 (Raft-quorum-committed metadata).                 |

## Features

- **Core API.** `Put`, `Get`, `Delete` over **gRPC** (primary) and **REST via Fiber** (secondary). Wire-compatible across both surfaces.
- **Tunable consistency.** `{ONE, QUORUM, ALL}` per request. Default is `QUORUM`. Cluster operator owns the `N`, `W`, `R` numerics (default `N=5, W=3, R=3`).
- **Smart Go client SDK.** Token-aware partition-map cache, epoch-driven refresh, single-retry on topology change. One client-to-cluster network hop on the happy path.
- **Conflict resolution.** Per-key vector clocks with two strategies: `lww` (default) and `siblings` (client-side merge with returned vector clocks).
- **Anti-entropy.** Per-range Merkle trees compared on a configurable cadence (default 5 min, jittered). CPU and bandwidth bounded.
- **In-memory data nodes (v1).** Sharded in-memory KV store; no per-node disk persistence in v1. Cluster-level fault tolerance via `N=5/W=3/R=3` replication + Merkle anti-entropy. Single-node restarts hydrate from peers. Pluggable durable backend (Pebble / RocksDB / S3-tiered) ships in v1.x.
- **Operator-selected backup destination.** `gossamerctl snapshot` writes point-in-time snapshots to **either S3-compatible object storage or PostgreSQL** — operator picks one at cluster bootstrap. The same destination also stores Coordinator Raft snapshots, so backup is configured once for the whole cluster. Redis is the cross-instance read cache only — never a backend.
- **Gossip stack.** Region-aware SWIM for membership and failure detection (within-region full fanout, cross-region reduced fanout) with optional Plumtree as a second layer for efficient bulk dissemination of partition-map and strategy-version updates.
- **Cluster security.** mTLS required end-to-end. Operator-supplied PKI loaded from disk or a Kubernetes Secret.
- **Rolling upgrades.** N / N+1 minor-version skew supported. Per-key unavailability bounded to < 5 s during the per-node drain window.

## Quick start

> Coming with v1.0 GA. Today the project is in design phase — the binaries below are placeholders for the eventual published artefacts.

### Run a 3-node local cluster

```sh
# Generate dev mTLS material
gossamerctl dev-pki --out ./pki

# Start the Coordinator group (3 nodes)
gossamer coordinator --config ./examples/coordinator.yaml

# Start data nodes
gossamer datanode --config ./examples/datanode.yaml
```

### Use it from Go (smart client SDK)

```go
import "github.com/architagr/GossamerDB/pkg/client"

c, err := client.New(client.Config{
    Endpoints: []string{"gossamer-1.local:7100", "gossamer-2.local:7100"},
    TLS:       client.TLSFromFiles("./pki/ca.pem", "./pki/client.pem", "./pki/client-key.pem"),
})
if err != nil { /* handle */ }
defer c.Close()

// Default consistency is QUORUM (3-of-5).
if err := c.Put(ctx, []byte("user:42"), userBytes); err != nil { /* handle */ }

// Override per call when latency matters more than freshness.
val, vc, err := c.Get(ctx, []byte("user:42"), client.WithConsistency(client.ONE))
```

### Use it from any language (gRPC)

```sh
grpcurl -cacert ca.pem -cert client.pem -key client-key.pem \
  -d '{"key":"dXNlcjo0Mg==", "consistency":"QUORUM"}' \
  gossamer-1.local:7100 gossamer.v1.KV/Get
```

### Use it from any language (REST via Fiber)

```sh
curl --cacert ca.pem --cert client.pem --key client-key.pem \
  "https://gossamer-1.local:7443/v1/kv/user:42?consistency=QUORUM"
```

## Deployment modes

A single binary set; the deployment mode is selected by configuration, not by separate builds.

- **Local.** `gossamer coordinator` + `gossamer datanode` on the same host or LAN. Cluster reaches `Ready` within 60 s.
- **Kubernetes.** A Helm chart deploys the Coordinator group as a `StatefulSet` (3 replicas) and the data tier as a `StatefulSet` with anti-affinity. Cluster reaches `Ready` within 3 minutes.
- **Multi-region AWS.** Same binary; one home region per key range with async cross-region replication. Cluster reaches `Ready` within 10 minutes (cold start, image pre-pulled).

## Architecture at a glance

- **Coordinator (capital C).** A 3-node embedded-Raft group owning cluster metadata: membership, partition map, strategy config, rolling-upgrade orchestration. **Strictly control-plane** — never on the per-request data path. A complete Coordinator outage pauses control-plane mutations only; reads and writes continue uninterrupted.
- **Data nodes.** Hold a slice of the partition ring, the **in-memory KV store** (no per-node disk persistence in v1), the cache layer, gossip and anti-entropy participants, and the gRPC + REST surfaces. State on a restarting node hydrates via Merkle anti-entropy from peers; durable backstop for full-cluster outage is `gossamerctl snapshot` to the operator-selected backup destination.
- **Request routing (no extra hops).** The smart Go client routes directly to one of the 5 owning replicas using its local copy of the partition map. That node becomes the *request coordinator* (lowercase) — its local read counts toward the `R=3` quorum, and it issues two parallel reads to peers, returning on the fastest 3 responses. REST and non-Go clients use a stateless any-data-node forwarding fallback through a plain L4 load balancer.

## Documentation

| Document                          | Path                                                 |
| --------------------------------- | ---------------------------------------------------- |
| Initial requirements (Wiki)       | [`docs/wiki/gossamerdb.md`](docs/wiki/gossamerdb.md) |
| Product Requirements (PRD v1.4)   | [`docs/prds/gossamerdb.md`](docs/prds/gossamerdb.md) |
| Documentation index               | [`docs/README.md`](docs/README.md)                   |
| Project rules & conventions       | [`CLAUDE.md`](CLAUDE.md)                             |

The HLD, LLD, Epic breakdown, and per-story implementation plans land under `docs/hld/`, `docs/lld/`, `docs/epics/`, and `docs/stories/` once the design phase completes.

## Roadmap

- **v1.0 GA (target).** Single-region core, **in-memory data tier**: KV API, `N=5/W=3/R=3` quorum, region-aware SWIM gossip + optional Plumtree, vector clocks (LWW + siblings), Merkle anti-entropy, mTLS-by-default, embedded-Raft Coordinator, rolling upgrades, OpenTelemetry, snapshot/restore to operator-selected backup destination (S3 or Postgres), smart Go SDK.
- **v1.1.** **Pluggable durable per-node data backend (Pebble first)** so cluster state survives full-cluster outage without operator-triggered snapshots; strategy hot-swap; per-key / per-namespace authorization (RBAC); one additional non-Go SDK (Java or Python by demand).
- **v1.2.** Active-active multi-region (`LOCAL_QUORUM`); web admin UI; HyParView peer-sampling overlay under Plumtree at thousand-node scale.
- **v1.x.** Additional storage backends (RocksDB, S3-tiered); additional backup destinations; edge read-through cache; SPIFFE / Vault PKI sources; migration importers from etcd / Consul / Redis / DynamoDB.

## For contributors

Build and test commands:

```sh
go build ./...                       # build all packages
go test ./...                        # run tests
golangci-lint run                    # lint
go mod tidy                          # tidy modules
./.claude/scripts/bench-check.sh     # enforce the < 1 ms p99 GET SLO
```

Branching, PR cadence, the draft-PR pattern, and the bench gate are documented in [`CLAUDE.md`](CLAUDE.md).

Requires Go 1.21+.

## License

See [`LICENSE`](LICENSE).
