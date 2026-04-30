# GossamerDB Documentation

This directory holds the design package for GossamerDB. Documents below are produced top-down: each downstream document consumes the upstream one, so reading them in order gives you the full picture from problem statement through implementation slicing.

## Index

| #   | Document                    | Author         | Path                                       | Status                                |
| --- | --------------------------- | -------------- | ------------------------------------------ | ------------------------------------- |
| 1   | Wiki (initial requirements) | Archit Agarwal | [`wiki/gossamerdb.md`](wiki/gossamerdb.md) | Done                                  |
| 2   | PRD (Product Requirements)  | Archit Agarwal | [`prds/gossamerdb.md`](prds/gossamerdb.md) | Draft v1.4 — awaiting design sign-off |
| 3   | HLD (High-Level Design)     | Archit Agarwal | `hld/gossamerdb.md`                        | Pending                               |
| 4   | LLD (Low-Level Design)      | Archit Agarwal | `lld/gossamerdb.md`                        | Pending                               |
| 5   | Epics                       | Archit Agarwal | `epics/gossamerdb/`                        | Pending                               |
| 6   | Stories (≤ 300 LOC each)    | Archit Agarwal | `stories/gossamerdb/<epic>/`               | Pending                               |
| 7   | Migration plans             | Archit Agarwal | `migrations/<feature>.md`                  | Created per feature                   |

## Reading order

1. Start with the **Wiki** for the problem statement, personas, and user flows.
2. Move to the **PRD** for goals, scope cuts, NFR targets, stack decisions, and resolved open questions. The PRD's §11 records how every wiki open question was resolved or deferred.
3. The **HLD** (when written) picks up from the PRD and owns the binding architecture; the **LLD** then expands the HLD into Go package layout, interfaces, and concurrency strategy.
4. **Epics + Stories** are the implementation slicing — each Story is ≤ 300 LOC and is the unit of work for an implementation PR.

## Cross-cutting constraints

- **< 1 ms p99 cache-call SLO** — defined in `../CLAUDE.md` "Performance Gate", enforced by `./scripts/bench-check.sh`. Every design doc must respect it; every implementation PR must pass it.
- **Stack** — Go 1.21+, gRPC, Fiber (REST), PostgreSQL, Redis, OpenTelemetry. See `CLAUDE.md` "Project Overview".
- **Security baseline** — mTLS-by-default on every hop. No `--insecure` flag exists in v1 (PRD FR-6).

## Conventions

- One feature folder per initiative (`gossamerdb` here is the v1 KV product).
- File names mirror the feature slug; epic / story directories use the same slug.
- Documents are append-only in spirit: when the PRD changes, the change is logged in a "Revision History" section rather than silently rewritten.
- Open questions are tracked at the bottom of the document that owns them; resolutions move them up into the body and link back from the open-questions section.
