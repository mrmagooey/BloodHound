# KNexus Golden Test Mismatch Investigation

## Current State (as of 2026-04-16)

The KNexus golden tests show **2 remaining mismatches** out of 53 queries after accounting for stale golden values. AD (49/49) and Azure (14/14) golden tests pass with zero mismatches.

| Query | Golden (old) | Golden (regenerated) | kglite | Status |
|---|---|---|---|---|
| Total nodes | 11203 | 11203 | 11909 (+706) | **Genuine bug** — transaction isolation |
| Total relationships | 61746 | 63546 | 64937 (+1391) | **Genuine bug** — transaction isolation |
| Distinct relationship types | 210 | 211 | 211 | **Stale golden** — resolved by regeneration |

**Root cause: transaction isolation divergence.** kglite's endpoint resolver can see nodes flushed within the same batch (no isolation), while Neo4j's resolver gets a separate read session that only sees committed data. kglite resolves more edges → creates more stub nodes → 706 extra nodes and ~1391 extra relationships.

All individually-typed edge count queries (DCSync, MemberOf, GenericAll, etc.) match exactly. The extra relationships come from edges involving the 706 extra stub nodes.

**Recommended fix: Option C — pre-built resolver snapshot map** (low-medium complexity, low risk, no FFI changes). See "Recommended Fix" section below.

---

## Root Cause Analysis

### The 706 extra nodes

Diagnostic output (`TestKNexusDiagnostic`) shows the distribution:

| Copies per objectid | Count |
|---|---|
| 1 | 4634 |
| 2 | 2604 |
| 3 | 689 |

All top-20 most-duplicated objectids have 3 copies with labels: `[Okta Okta_User]` + `[Base Okta_User]` + `[SCIM]`. The 689 objectids with 3 copies are Okta users from `preview2-okta-graph.json` also referenced by SCIM edges in `githound_enterprise_scim_E_kgDOAAiv9g.json`. In Neo4j, these users have only 2 copies (Okta + Base) — the SCIM edges are dropped because the resolver can't see uncommitted nodes.

### How the 3 copies are created in kglite

1. **`:Okta` node** — `preview2-okta-graph.json` (sourceKind="Okta") creates Okta_User nodes via `IngestNode` with identityKind=Okta
2. **`:Base` stub** — `okta-graph-hybrid.json` (no sourceKind) references these users as edge endpoints with kind=Okta_User. `endpointIdentityKind(EmptyKind, "Okta_User")` returns "Base". Creates `MERGE (:Base {objectid: X})`
3. **`:SCIM` stub** — `githound_enterprise_scim_E_kgDOAAiv9g.json` (sourceKind="SCIM") references these users as SCIM_Provisioned edge start endpoints with empty kind. `endpointIdentityKind("SCIM", EmptyKind)` returns "SCIM". Creates `MERGE (:SCIM {objectid: X})`

In Neo4j, step 3 never happens because the endpoint resolver (running a separate read session) can't see the Okta nodes created in steps 1-2 (still in the batch's uncommitted write transaction). The SCIM edges fail to resolve → they are dropped → no `:SCIM` stubs.

### Transaction isolation: the fundamental divergence

**kglite**: No transaction isolation. `ReadTransaction` and `BatchOperation` share the same `KnowledgeGraph` instance. After `flush()`, writes are immediately visible to concurrent reads. Thread safety via Rust Mutex, but zero isolation.

**Neo4j**: `BatchOperation` opens a write session. `ReadTransaction` opens a **separate** read session (fresh Bolt connection) with read-committed isolation. Uncommitted writes from the write session are invisible to the read session.

The ingestion call chain:

```
ProcessIngestFile (tasks.go)
  └─ graphdb.BatchOperation(ctx, func(batch) {
       for each file:
         processSingleFile → ReadFileForIngest → IngestRelationships
           └─ endpoint.ResolveAll(ctx, resolver, rels)
                └─ resolver.Start() → spawns 6 goroutines
                     └─ each goroutine: s.db.ReadTransaction(ctx, s.dbLoop)
                          └─ tx.Nodes().Filter(...) → Cypher against KnowledgeGraph
     })
```

The resolver's `ReadTransaction` goroutines query the same `*kglite.KnowledgeGraph` that the batch writes to. The `Resolver` is constructed once at service startup (`service.go:56`) and reused across all ingest runs with the same `graph.Database` reference.

### Contributing factor: `endpointIdentityKind` change

Commit `95f32225` (April 9) changed `endpointIdentityKind` so that when `endpointKind == EmptyKind` and `sourceKind != EmptyKind`, it returns `sourceKind` (e.g., "SCIM") instead of "Base". Before this change, SCIM edge stubs used `:Base` and merged with existing `:Base` stubs from hybrid edges → 2 copies per user. After, they use `:SCIM` → 3 copies. This change exposed the transaction isolation issue but is not the root cause.

---

## Hypotheses Explored

### 1. kglite MERGE creates duplicates across batch flush boundaries

**Status: Ruled out**

`TestFlushBoundaryMergeDuplication` creates 2000 users across multiple flush boundaries (flushSize=5000) with Okta + Base + SCIM identity kinds in a single BatchOperation. Result: 0 duplicates, all counts correct. The batch flush boundary mechanism correctly deduplicates via `oidToIdx` cache and `pendingLookups`.

### 2. Stale golden file

**Status: Partially confirmed**

Golden regenerated from Neo4j on 2026-04-15. Node count unchanged (11203) — the golden was correct for nodes. Relationship types changed (210→211, AZExecuteCommand added) and relationship count changed (61746→63546) — the golden was partially stale for these two queries.

### 3. Transaction isolation difference

**Status: Confirmed as root cause (2026-04-16)**

Architecture investigation confirmed: kglite's `Driver` holds a single `*kglite.KnowledgeGraph` pointer shared by all operations. Both `ReadTransaction` and `BatchOperation` call methods on the same instance. Neo4j opens separate sessions. The divergence is entirely from the `match_by: "name"` resolution path (resolver.go:108-140) seeing in-batch nodes.

### 4. `endpointIdentityKind` returning "SCIM" instead of "Base"

**Status: Contributing factor, not root cause**

Confirmed as the mechanism creating the 3rd copy per user. But Neo4j doesn't create the 3rd copy because the SCIM edges are dropped by the resolver (transaction isolation), not because of different MERGE behavior. Both backends generate the same MERGE statements — they just don't process the same set of edges.

### 5. kglite property-only MERGE fallback

**Status: Confirmed — exists in commit `1faa6c9` but REMOVED in dirty kglite-ffi working tree**

The fallback in `kglite-ffi/src/graph/cypher/executor.rs` (`try_match_merge_pattern()`) added cross-label property-only matching to MERGE. With it enabled, `MERGE (:SCIM {objectid: X})` finds an existing `(:Okta {objectid: X})` node. Currently removed in uncommitted kglite-ffi changes. This was a workaround that made kglite MERGE non-standard — the proper fix should be at the Go/application layer.

---

## Approaches Tried and Rejected

### Option A: Cross-label resolution at Go layer (batch.go)

**Status: Rejected — too aggressive (2026-04-16)**

Added `lookupIdxAnyLabel` to `batch.go` for cross-label oidToIdx cache resolution. Collapses ALL cross-label stubs, not just SCIM duplicates. `:Base` stubs from hybrid edges are legitimate (Neo4j creates them too) but Option A collapsed them.

| Metric | Golden (Neo4j) | kglite (before) | kglite (Option A) |
|---|---|---|---|
| Total nodes | 11203 | 11909 (+706) | **8620 (-2583)** |
| Total relationships | 61746 | 64937 (+3191) | 64937 (+3191, unchanged) |

Removed 3289 nodes when only ~706 should have been removed. Reverted.

---

## Recommended Fix: Option C — Pre-built Resolver Snapshot Map

**Layer**: Application (endpoint resolver + ingestion tasks)
**Complexity**: Low-medium | **Risk**: Low | **FFI changes**: None

The 706-node divergence is entirely from the `match_by: "name"` resolution path. Option C freezes the resolver's view of the graph at batch start:

1. Before `BatchOperation`, issue a read transaction: `MATCH (n) WHERE n.name IS NOT NULL RETURN toLower(n.name), n.objectid`
2. Populate a `map[string]string{lowerName: objectid}` snapshot
3. During the batch, resolver consults only this frozen map — never calls `db.ReadTransaction`
4. If a name isn't in the snapshot → return `ErrNoResultsFound` (edge dropped), matching Neo4j

### Implementation details

- The `Resolver` is constructed once at service startup (`service.go:56`) and reused across ingest runs. The snapshot must be rebuilt per `BatchOperation` — either construct a new resolver or add a `Reset(snapshotData)` method.
- The resolver already has a 500K-entry `cache.NewSieve` (`resolver.go:51`). The snapshot map replaces DB round-trips with frozen-state lookups.
- Changes confined to `cmd/api/src/services/graphify/endpoint/` and `cmd/api/src/services/graphify/tasks.go`.

### Design options assessed (full matrix)

| Option | Layer | Complexity | Risk | FFI changes | Neo4j parity |
|---|---|---|---|---|---|
| A: Cross-label batch.go resolution | Batch | Medium | **Overcorrects** | No | Broken (rejected) |
| B1: kglite MVCC snapshot | kglite/Rust | Very high | High | Yes | Full |
| B2: ID-boundary filter in Go | Transaction rewrite | High | High | No | Approximate |
| **C: Pre-built resolver map** | **Application** | **Low-medium** | **Low** | **No** | **Full (for name resolution)** |
| D: Accept divergence | Tests only | Very low | Low (impl) | No | None |

---

## Reproduction Tests

| Test | File | What it tests |
|---|---|---|
| `TestSCIMCrossPlatformNodeCount` | `repro/scim_cross_platform_test.go` | 5-user dataset through full ReadFileForIngest pipeline |
| `TestSCIMCrossPlatformSingleBatch` | `repro/scim_cross_platform_test.go` | Same 5 users, all files in single BatchOperation |
| `TestSCIMCrossPlatformStepByStep` | `repro/scim_cross_platform_test.go` | Same 5 users, separate BatchOperation per file, counts after each step |
| `TestFlushBoundaryMergeDuplication` | `repro/flush_boundary_test.go` | 2000 users with Okta+Base+SCIM across flush boundaries |

Test data: `cmd/api/src/test/e2e/testdata/repro/scim_cross_platform/` (3 files, ~26KB total).

## Key Data Files

The KNexus zip contains 34 data files. The relevant ones:

| File | Size | sourceKind | Role |
|---|---|---|---|
| `preview2-okta-graph.json` | 1.5MB | Okta | 846 nodes including 689 Okta_User nodes referenced by SCIM |
| `okta-graph.json` | 138MB | Okta | 3206 nodes (main Okta dataset, different user population) |
| `okta-graph-hybrid.json` | 640KB | (none) | 3141 edges with explicit endpoint kinds (Okta_User, etc.) |
| `githound_enterprise_scim_E_kgDOAAiv9g.json` | 1.1MB | SCIM | 701 nodes + 1701 edges (701 SCIM_Provisioned + 1000 SCIM_MemberOf) |
| `githound_enterprise_saml_E_kgDOAAiv9g.json` | 1.7MB | (none) | 3460 edges including 1384 with match_by: "name" endpoints |

---

## Option C: Implementation and Test Results (2026-04-16)

**Status: Implemented, tested, zero effect. The resolver snapshot does not affect the 706-node gap.**

Option C (pre-built resolver snapshot map) was implemented in `resolver.go` and `tasks.go`. Golden test results:

| Metric | Golden | kglite (before) | kglite (Option C) |
|---|---|---|---|
| Total nodes | 11203 | 11909 (+706) | **11909 (+706, unchanged)** |
| Total relationships | 63546 | 64937 (+1391) | **64937 (+1391, unchanged)** |

**Why it failed**: All SCIM edges use `match_by: "id"` — they **bypass the endpoint resolver entirely** and go straight to `UpdateRelationshipBy` → triple-MERGE. The resolver snapshot only affects `match_by: "name"` endpoints. The transaction isolation hypothesis was correct about the architectural difference but **wrong about which code path creates the 706 extra nodes**.

Additionally, the e2e test fixtures (`ingest_test.go:114-133`) call `endpoint.NewResolver(db)` and `db.BatchOperation()` directly — they never call `GraphifyService.ProcessIngestFile`, so the snapshot is never populated even for name-based edges.

## Revised Root Cause (2026-04-16)

The 706 extra SCIM stubs are created by `UpdateRelationshipBy` triple-MERGE using `match_by: "id"`. Both kglite and Neo4j receive the same MERGE statements via the same Go code. The difference must be in how each backend handles these identical statements.

**New hypothesis**: Neo4j's Go-side `nodeUpdateByBuffer` (in the Neo4j DAWGS batch driver) buffers writes before sending to Neo4j. This buffer may deduplicate or coalesce objectids across labels, preventing Neo4j from receiving the `:SCIM` stub MERGEs. Alternatively, Neo4j's batch may process files in a different order or skip certain edges due to its write-commit cycle.

This needs investigation at the Neo4j DAWGS driver layer (`packages/go/dawgs/drivers/neo4j/`), specifically the batch buffering mechanism.

---

## Proposed Diagnostic Queries (2026-04-16)

New diagnostic queries added to `knexusPresetQueries` in `knexus_comparison_test.go`. These can be:
- Run against Neo4j via `make test-comparison` (requires Docker)
- Added to the golden file via `make golden-generate` for future comparison

### Category 1: Per-label counts (where are the 706 extra nodes?)

| Query | Purpose |
|---|---|
| `MATCH (n:SCIM) RETURN count(n)` | How many `:SCIM` nodes exist in each backend? If Neo4j=0, stubs were never created |
| `MATCH (n:Okta) RETURN count(n)` | Okta node count — should match |
| `MATCH (n:Base) RETURN count(n)` | Base stub count — should match |
| `MATCH (n:SCIM_User) RETURN count(n)` | SCIM_User (actual SCIM nodes, not stubs) — should match |
| `MATCH (n:Okta_User) RETURN count(n)` | Okta_User count — may differ if stubs inflate |

**Key insight**: If Neo4j has 0 `:SCIM` nodes and kglite has ~689, the SCIM stubs are the divergence. If Neo4j has `:SCIM` nodes too, the divergence is elsewhere.

### Category 2: Objectid duplication (do Neo4j nodes also share objectids?)

| Query | Purpose |
|---|---|
| `...cnt = 3 RETURN count(oid) AS triple_oids` | Neo4j triple-copies? If 0, Neo4j never creates 3 copies per user |
| `...cnt > 1 RETURN count(oid) AS duped_oids` | Total shared objectids — baseline for duplication |
| `...RETURN cnt AS copies, count(oid) AS num_oids ORDER BY cnt` | Full distribution — shows if Neo4j has 2-copy pattern or 1-copy |

**Key insight**: If Neo4j has 2604 objectids with 2 copies (matching kglite's Okta+Base pattern), the divergence is purely the 689 third copies. If Neo4j has fewer 2-copy objectids, the divergence is broader.

### Category 3: SCIM edge pipeline (are the edges even present in Neo4j?)

| Query | Purpose |
|---|---|
| `MATCH ()-[r:SCIM_Provisioned]->() RETURN count(r)` | Were SCIM_Provisioned edges created at all? |
| `MATCH ()-[r:SCIM_MemberOf]->() RETURN count(r)` | Were SCIM_MemberOf edges created? |
| `MATCH (s)-[:SCIM_Provisioned]->() UNWIND labels(s) AS lbl RETURN lbl, count(*) ORDER BY c DESC` | What labels do SCIM edge start nodes have? In kglite: `:SCIM`. In Neo4j: `:Okta`? `:Base`? Or are the edges absent? |

**Key insight**: If Neo4j has 0 SCIM_Provisioned edges, the SCIM edges were **dropped during ingestion** (not just the stubs). If Neo4j has SCIM_Provisioned edges but they attach to `:Okta`/`:Base` nodes, then Neo4j's MERGE coalesced the stubs differently.

### How to use these

```bash
# Generate new golden files with diagnostic queries included
docker compose -f docker-compose.testing.yml up -d
make golden-generate
docker compose -f docker-compose.testing.yml down

# Compare kglite against the new golden (diagnostic queries will show MATCH/MISMATCH)
make golden-test-all
```

The new golden values for the diagnostic queries will definitively show whether:
1. Neo4j never creates SCIM stubs (edges dropped)
2. Neo4j creates SCIM stubs but attaches them to different labels
3. Neo4j's batch buffering deduplicates before writing

## Open Items

1. ~~Update stale golden fields~~ — done: relationship count (61746→63546), relationship types (210→211). **Note**: distinct rel types test may have a formatting mismatch — needs debugging.
2. ~~Implement Option C~~ — done, zero effect. Code present but not harmful (no-op for `match_by: "id"` edges).
3. **Investigate Neo4j Go-side batch buffering** — the `nodeUpdateByBuffer` may explain the 706-node difference. This is the most likely remaining explanation.
4. **Debug distinct relationship types golden mismatch** — golden file updated but test may still mismatch due to formatting.
5. **Decide on kglite-ffi dirty state** — the property-only MERGE fallback removal should be committed or reverted.
