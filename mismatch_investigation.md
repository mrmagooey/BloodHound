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

---

## Remediation Plan (2026-05-02)

The prior investigation has produced a clear hypothesis (Neo4j-side coalescing of cross-label MERGEs on the same objectid) but no decisive Neo4j-side measurement. The existing diagnostic comparison queries were authored but never executed end-to-end against a live Neo4j to confirm where the divergence appears.

This plan is structured as a **measurement-first triage** that avoids re-tackling already-rejected approaches (Option A: cross-label oidToIdx; Option C: name-resolver snapshot). It explicitly de-prioritises further code changes until we have side-by-side per-label and per-edge evidence from Neo4j, since the last two attempts both implemented fixes that addressed the wrong code path.

### Guiding principles

- **Don't write a fix until a controlled, minimal, side-by-side reproduction reports the same divergence.** Both Option A and Option C wasted effort on plausible-but-wrong code paths because the gap was reasoned about, not measured. Running the existing scim_cross_platform repro under both backends is mandatory before any code change.
- **Shrink the dataset until one byte changes the result.** The 138 MB main `okta-graph.json` and the 34-file zip are not the right substrate. The 5-user repro is the right size class; verify it (a) actually reproduces the divergence and (b) is the smallest possible witness.
- **Keep all diagnostic Cypher in the repo, not in scratch files.** Per-label counts, copy-distribution, and edge-pipeline queries belong in `knexusPresetQueries` (already added) and the standalone repro fixtures so the next iteration starts from a known position.
- **Stop adding hypotheses without retiring old ones.** This document already contains five hypotheses; only one (transaction isolation) was firmly ruled out as the *operative* mechanism, and the property-only MERGE fallback is in an unresolved dirty state. The next investigation step must produce a **disposition** for each open hypothesis, not just add a sixth.

### Phase 1: Establish a fresh baseline (no code changes)

The objective of Phase 1 is to know exactly which queries diverge today, with the current Go and current `kglite-ffi` working tree, so subsequent phases can attribute changes correctly.

1. **Resolve the dirty kglite-ffi working tree first.** Run `git -C kglite-ffi status` and `git -C kglite-ffi diff` and either commit or revert. The property-only MERGE fallback in `try_match_merge_pattern()` materially affects whether `:SCIM` stubs collapse onto `:Okta` nodes and confounds every measurement until pinned. Record the resolved SHA in this document before continuing.
2. **Run the standalone-only KNexus suite** to capture the current kglite values for all queries in `knexusPresetQueries` (including the diagnostic ones added 2026-04-16):
   ```sh
   go test -v -tags e2e -timeout 30m -run TestLoadAndQueryKNexus ./cmd/api/src/test/e2e/
   ```
   Save the raw output to `mismatch_baseline_kglite.txt` (gitignored, local only).
3. **Bring up Neo4j and run the comparison suite** to capture the Neo4j-side values for the same queries:
   ```sh
   docker compose -f docker-compose.testing.yml up -d
   go test -v -tags 'e2e comparison' -timeout 30m -run TestCompareKNexus ./cmd/api/src/test/e2e/
   docker compose -f docker-compose.testing.yml down
   ```
   The diagnostic queries (`SCIM nodes`, `Okta nodes`, `Base nodes`, `Objectids with 3 copies`, `SCIM_Provisioned start node labels`, etc.) will print MATCH/MISMATCH for each. **This is the measurement that the prior investigation deferred.** Record the results here as a table in this document — *not* in a separate file.
4. **From those numbers, decide which scenario the divergence is in:**
   - **Scenario A** (Neo4j has 0 `:SCIM` nodes; kglite has ~689): SCIM stub MERGEs are silently dropped in Neo4j. Investigation continues at the Neo4j Go driver.
   - **Scenario B** (Neo4j has `:SCIM` nodes attached to `:Okta`/`:Base` nodes via cross-label MERGE coalescing): Neo4j coalesces the MERGE because of an existing index match; kglite does not. Investigation continues in the kglite Rust MERGE planner.
   - **Scenario C** (Neo4j has the same 689 `:SCIM` stubs but other counts diverge): The SCIM stub theory is wrong; restart hypothesis search. The 706 number was a coincidence of node-count arithmetic.

### Phase 2: Pin the minimal reproduction

The existing 5-user `scim_cross_platform` fixture is the right size, but the prior investigation never confirmed it actually reproduces the divergence under **both** backends. Without that confirmation, every fix attempted on it is unverified.

1. **Add a comparison variant of the existing repro.** Author `cmd/api/src/test/e2e/repro/scim_cross_platform_comparison_test.go` with build tag `comparison && e2e`. It mirrors `TestSCIMCrossPlatformSingleBatch` but ingests into both kglite and Neo4j (using helpers from `comparison_helpers_test.go`) and asserts the same per-label and copies-distribution queries. If 5 users in this fixture produces a 1-or-greater node-count gap in the same direction as the full dataset, the minimal repro is confirmed.
2. **If the 5-user fixture does not reproduce the gap**, expand iteratively along the dimensions known to matter:
   - File ordering: hybrid-before-SCIM vs SCIM-before-hybrid
   - Flush boundary alignment (already covered by `TestFlushBoundaryMergeDuplication` for kglite-only; needs a comparison variant)
   - sourceKind combinations: `Okta`+`(none)`+`SCIM` is what the full dataset uses; permute to find the smallest set that diverges.
   The output of this step is a fixture under `testdata/repro/` that **deterministically** produces the divergence under both backends, with a single-digit node count.
3. **Commit the minimal fixture and the comparison test together with a one-line note** in this document recording its expected divergence (e.g. "5-user fixture: kglite=15, neo4j=10, gap=5"). This becomes the regression substrate for Phase 3.

### Phase 3: Bisect the divergence mechanism

With a deterministic minimal reproducer in hand, drive the divergence to a single code path.

1. **Capture the exact MERGE statements each backend receives.** Add a logging hook (or use the existing `QueryProfiler` in `packages/go/kglite/dawgs/profiler.go`) to dump every Cypher statement issued during the repro's `BatchOperation`. Do the same on the Neo4j driver side via the bolt query log. Diff the two lists. If they're identical (expected, since both backends share the Go ingest code), the divergence is purely in execution semantics — proceed to step 2. If they differ, the divergence is in batch buffering; investigate `packages/go/dawgs/drivers/neo4j` (note: per `Bash(grep)` 2026-05-02 this directory does not exist locally; the Neo4j driver lives under `github.com/specterops/dawgs` as an external module — fetch and read that module's batch implementation).
2. **For each diverging MERGE**, write a single-statement test case directly against each backend (no ingest pipeline, no resolver, no batch). Format: `MERGE (n:LabelA {objectid:'X'}) MERGE (m:LabelB {objectid:'X'})` — check whether each backend creates 1 or 2 nodes. This isolates the divergence from the surrounding ingest machinery and is the test that should have been written before any of the prior fixes. Add results to this document.
3. **If single-statement MERGEs match but the divergence persists in batch context**, the issue is buffering or commit ordering, not MERGE semantics. The remaining suspects in priority order:
   - Neo4j Go driver's `nodeUpdateByBuffer` coalesces by objectid before send (search the external `dawgs` module).
   - kglite's pendingLookups flush ordering surfaces a node mid-batch that Neo4j only sees at commit time.
   - The Rust optimizer's predicate pushdown handles `objectid` index lookups differently from Neo4j's index-backed MERGE.

### Phase 4: Choose the intervention

Only after Phase 3 produces a one-line root cause should this section be filled in. Constrain the choice to the smallest fix that closes the divergence the test in Phase 2 reports — do not bundle in cleanup, refactors, or "while we're here" changes.

Candidate interventions (assessed against measurements, not speculation):

- **I1 — Re-enable property-only MERGE fallback in kglite-ffi.** Confirmed in prior investigation as making `MERGE (:SCIM {objectid: X})` find an existing `(:Okta {objectid: X})`. Trade-off: kglite MERGE becomes non-standard relative to Neo4j Cypher (a `:SCIM` MERGE matches non-`:SCIM` nodes). Acceptable only if the comparison shows Neo4j *does* coalesce in this scenario via index-driven match, which would mean kglite is the divergent one.
- **I2 — Coalesce cross-label stub MERGEs in kglite Go batch layer.** Different from rejected Option A: instead of collapsing the oidToIdx cache, intercept stub MERGEs (those without property updates beyond `objectid`) and skip when the objectid already has any node. Risk: still overcorrects if Neo4j *does* create the cross-label stubs. Conditional on Phase 3 Scenario A.
- **I3 — Mirror Neo4j's Go-driver batch buffering on the kglite side.** Add an analogous `nodeUpdateByBuffer` to `packages/go/kglite/dawgs/batch.go` that coalesces node MERGEs by objectid before flush. Conditional on Phase 3 confirming the Neo4j driver's buffer is doing the work.
- **I4 — Accept divergence and document.** If the 706 extra nodes are stubs that don't participate in any analysis edge or path query, the divergence is observable but operationally inert. Document the mismatch in the golden file with an annotation, mark the relevant comparison queries as expected-divergent, and stop. This is the right outcome if Phase 3 shows the gap doesn't propagate to attack-path queries.

### Anti-recurrence checklist (do not skip)

These were the prior investigation's failure modes; the next iteration should self-check against them.

- [ ] Did I run the comparison test before writing code? (Option A and C both failed this.)
- [ ] Did I update this document with the measurements before proposing a fix?
- [ ] Did I retire or update the open hypothesis list, rather than adding a new one?
- [ ] Did I confirm the kglite-ffi working tree is clean and pinned to a known SHA?
- [ ] Is the new repro test under `repro/` and using the comparison helpers, not stand-alone?
- [ ] Did I add a disposition (confirmed / ruled-out / inconclusive) for every hypothesis I touched?

### Stop conditions

Halt and reconsider if any of these occur:

- Phase 1 measurements show the divergence is no longer 706/1391 (e.g. it changed shape after the dirty kglite-ffi resolution). The investigation premise has shifted; rewrite the hypothesis section before continuing.
- Phase 2 cannot produce a minimal repro under 50 nodes after two iterations. The bug may not be a deterministic MERGE divergence; consider non-determinism (concurrent flush ordering) and add timing-controlled tests.
- Phase 3 finds that the per-statement MERGE behaviour differs between kglite and Neo4j. The intervention must then be in the engine, not the ingest pipeline; reopen the kglite-ffi work and treat this as a Cypher-spec compliance task, not an ingest task.

---

## Iteration Log (2026-05-02)

### Phase 1.1 — kglite-ffi state resolved

- Submodule freshly checked out and pinned to `b146f06` (HEAD detached). Working tree clean.
- The property-only MERGE fallback (`try_match_merge_pattern`) **is present** at this SHA — `kglite-ffi/src/graph/cypher/executor.rs:9047`. The "removed in dirty working tree" note from the prior investigation does not apply to the current repo state.
- The fallback's secondary-label scan (executor.rs:9159-9176) requires `node_matches_label(node, label)` — i.e. it only matches a node that already has the requested label as a primary or secondary label. So `MERGE (:SCIM {objectid:X})` will **not** match an existing `(:Okta {objectid:X})` node unless that node also carries `:SCIM`. The investigation's earlier claim that this fallback enables cross-label coalescing was over-stated; the label check gates it.

### Phase 1.2 — kglite baseline test (in progress, hung)

- Started `make kglite` and `go test -tags e2e -run TestLoadAndQueryKNexus` after installing Go 1.25 to `~/go-toolchain` and Rust to `~/.cargo`.
- libkglite.a built (1m31s, 17 warnings, no errors).
- KNexus ingest + analysis runs through to "Post-processing App Role Assignments" (measurement_id=157, log timestamp 05:21:31), then **hangs at 99% CPU for 8+ minutes**, hitting the 30-minute test timeout.
- This is itself a divergence signal (analysis chain hanging on kglite, not Neo4j) but is orthogonal to the 706-node count issue.
- **Disposition:** the App-Role-Assignments hang is a separate bug — flagged for after the count issue closes. For now, baseline measurements will run with `-timeout 60m` or by running the comparison test (which uses the same path through Neo4j and surfaces analysis hangs separately).

### Phase 3 prep — dawgs Neo4j driver Cypher generation (corrected)

A subagent investigated `github.com/specterops/dawgs@v0.4.10/drivers/neo4j` to characterise the Neo4j driver's batch behaviour. The subagent's report contained one **incorrect** recommendation that needs noting before it's relied on.

**Subagent's key claim (incorrect):** "Neo4j hardcodes `:Base` in the MERGE pattern; both `:Okta` and `:SCIM` MERGEs identify by `:Base {objectid}`, and differing labels are applied via `s:Okta`/`s:SCIM` in the SET clause."

**Verified ground truth:** `cypher.go:97-201` (`cypherBuildRelationshipUpdateQueryBatch`) writes the MERGE pattern using `batch.startIdentityKind` directly — same as kglite's `batch.go:734-737`. There is no hardcoded `:Base`. Both backends will issue:

```
unwind $p as p
merge (s:SCIM {objectid: p.s.objectid})
merge (e:SCIM_User {objectid: p.e.objectid})
merge (s)-[r:SCIM_Provisioned]->(e)
set s += p.s, e += p.e, r += p.r,
    s:SCIM, s:Okta_User,    -- additional kinds via SET
    e:SCIM, e:SCIM_User,
    s.lastseen = ..., e.lastseen = ...;
```

So the per-statement Cypher issued to Neo4j and to kglite for SCIM edges is **structurally identical** modulo `unwind` framing. This invalidates intervention I3's premise (mirroring Neo4j's "buffer-based coalescing") because Neo4j is not coalescing across labels — it groups by `(identityKind, identityProperties, kindsToAdd)` schema fingerprint (`cypher.go:15-29`), not by objectid.

**What this means:** If both backends issue `merge (:SCIM {objectid:X})` for SCIM edges, and the prior investigation's claim that Neo4j has 0 `:SCIM` stubs is correct, then the divergence must be in **MERGE execution semantics** — i.e., Neo4j's MERGE is finding existing `(:Okta {objectid:X})` nodes via some index-backed cross-label match that kglite does not perform. This is the opposite of what kglite's current `try_match_merge_pattern` does (kglite gates on `node_matches_label`; Neo4j may not).

**But — this is still hypothesis.** The diagnostic queries (`SCIM nodes`, `Okta nodes`, etc.) on a real Neo4j run are still un-executed. Phase 1.3 must run before any intervention.

### Subagent value assessment

The subagent's research provided strong citations and located the dawgs source quickly. Its bottom-line recommendation was wrong because it inferred from `cypher.go`'s `s:Kind1` SET clause that the MERGE pattern was using a different label than it actually was. Lesson: subagents should be asked for citations + code excerpts, not for synthesised recommendations on architectural questions where one mis-read flips the conclusion. Verify subagent claims directly when they prescribe code changes.

### Updated hypothesis dispositions

| Hypothesis | Prior status | After 2026-05-02 |
|---|---|---|
| #1 kglite MERGE creates duplicates across flush boundaries | Ruled out | Unchanged |
| #2 Stale golden file | Partially confirmed | Unchanged |
| #3 Transaction isolation difference | Confirmed as root | **Downgraded to "secondary"** — applies only to `match_by:"name"` path. The 706-node SCIM divergence uses `match_by:"id"` and goes through batch.go, not the resolver. |
| #4 endpointIdentityKind returning "SCIM" | Contributing factor | Unchanged |
| #5 kglite property-only MERGE fallback | "Removed in dirty tree" | **Disposition corrected**: present at b146f06; gated by `node_matches_label`, so does not cross-label-coalesce. |
| #6 (new) Neo4j driver buffer coalesces by objectid | Speculated | **Ruled out**: dawgs cypher.go uses schema-fingerprint key, not objectid (`cypher.go:15-29`). |
| #7 (new) Neo4j MERGE has implicit cross-label index match | Speculated | **Inconclusive — pending Phase 1.3 measurements.** This is now the leading hypothesis. |

### Next actions (for the next loop iteration)

1. Wait for kglite test to time out or send notification; pull the partial query output if any.
2. Run the comparison test against Neo4j (just started: `bloodhound-testgraph-1`) to populate diagnostic query values: `make test-knexus`. This is Phase 1.3 — the measurement that no prior iteration completed.
3. From the comparison test output, decide:
   - If Neo4j reports `(SCIM nodes) = 0`, hypothesis #7 fires: Neo4j's MERGE engine performs the cross-label coalescing that kglite's `try_match_merge_pattern` declines to. The intervention is in `executor.rs`, not in Go.
   - If Neo4j reports `(SCIM nodes) ≈ 689` matching kglite, the SCIM stub theory is wrong and the 706-node gap lives elsewhere — restart hypothesis search using per-label deltas as the signal.
4. Investigate the App-Role-Assignments analysis hang separately after the count issue closes.

---

## Iteration Log (2026-05-02, continued)

### Phase 1.3 + Phase 2 + Phase 3 — measurements taken, root cause found

**Workflow:** Added `BH_SKIP_ANALYSIS=1` env var (`shared_fixtures_test.go`, `ingest_test.go`, `repro/knexus_diag_test.go`) to bypass the Azure post.go analysis hang while keeping the full ingest path. This unblocked Phase 1.2 (kglite-only) and Phase 1.3 (kglite vs Neo4j) measurements.

#### Measurements

| Metric | kglite | Neo4j | Δ |
|---|---|---|---|
| Total nodes | 11893 | 11187 | **+706** |
| Total relationships | 43960 | 42569 | **+1391** |
| Distinct objectids | 7911 | 7911 | match |
| Objectids on >1 node | 3293 | 2588 | **+705** |
| Objectids with 3 copies | 689 | 688 | +1 |
| SCIM nodes | 1402 | 1393 | +9 |
| Base nodes | 5891 | 5887 | +4 |

#### Disposition of prior hypotheses

| Hypothesis | Prior | After 2026-05-02 |
|---|---|---|
| #7 Neo4j MERGE has implicit cross-label index match | Inconclusive | **Ruled out** — Neo4j has 1393 :SCIM nodes, 688 3-copy objectids, 701 SCIM-only stubs. Both backends create cross-label stubs via standard MERGE. |
| #4 endpointIdentityKind contributes 706 SCIM stubs | Contributing | **Ruled out** — the 706 gap is NOT in 3-copy SCIM users (kglite=689, neo4j=688, diff=1). It is in 2-copy objectids (kglite=2604, neo4j=1900, diff=704). |
| #3 Transaction isolation | Confirmed root | **Ruled out** — both backends produce the SCIM stubs; transaction isolation isn't the bottleneck. The "match_by:name" path is not what creates these. |

#### 2-copy label-pair pattern shift (the ~705 difference)

Aggregating the 2-copy objectids by label-set pair:

| Pattern | kglite | Neo4j | Δ |
|---|---|---|---|
| `{Base}` + `{GH_ExternalIdentity}` | **692** | **0** | **+692** |
| `{Base}` + `{SCIM, SCIM_User}` | 692 | 692 | 0 |
| `{Base}` + `{GH_User, GitHub}` | 641 | 641 | 0 |
| `{Base}` + `{AZBase, AZUser}` | 503 | 503 | 0 |
| `{Base, GH_User}` + `{GitHub, GH_User}` | 51 | 51 | 0 |
| `{Okta, Okta_Group}` + `{Okta_Group, SCIM}` | 9 | 0 | +9 |
| `{Base, Okta_User}` + `{Okta, Okta_User}` | 9 | 9 | 0 |
| Misc small | ~6 | ~5 | ~+1 |

Total kglite-only extra ≈ **702**. Plus 3-copy diff (+1) and miscellaneous label-count diffs. Sums to the observed +706.

In Neo4j, GH_ExternalIdentity nodes carry both `[Base, GH_ExternalIdentity]` labels on a **single** node. In kglite, the same objectids are split into one `[Base]`-only node and one `[GH_ExternalIdentity]`-only node. **This is the 692 extra nodes.**

#### Bug found — kglite `UpdateNodeBy` emits a labelled MERGE when Neo4j emits a label-less one

The KNexus dataset's `githound_enterprise_saml_E_kgDOAAiv9g.json` has no top-level metadata, so `IngestGenericData` is invoked with `sourceKind=EmptyKind`. The data file's nodes have `kinds: ["GH_ExternalIdentity"]`. In `IngestNode`:

```
baseKind = EmptyKind
nextNode.Labels = [GH_ExternalIdentity]
nodeKinds = MergeNodeKinds(EmptyKind, [GH_ExternalIdentity]) = [GH_ExternalIdentity]
update.IdentityKind = EmptyKind
update.Node.Kinds = [GH_ExternalIdentity]
```

**Neo4j** (`cypher.go:247-249`):
```go
if batch.identityKind != nil && !batch.identityKind.Is(graph.EmptyKind) {
    output.WriteString(fmt.Sprintf(":%s", batch.identityKind.String()))
}
```
Emits **label-less** MERGE: `unwind $p as p merge (n {objectid: p.objectid}) set n += p, n:GH_ExternalIdentity`.

**kglite** (`batch.go:367-369`):
```go
if kindStr == "" {
    kindStr = update.Node.Kinds[0].String()  // <-- BUG: falls back to first Kind
}
```
Emits **labelled** MERGE: `MERGE (n:GH_ExternalIdentity {objectid:X}) SET ...`.

When a hybrid edge ingests first and creates a `(:Base {objectid:X})` stub, then IngestNode runs:
- Neo4j's label-less `merge (n {objectid:X})` matches the existing `:Base` node and adds `:GH_ExternalIdentity` via SET → single node `[Base, GH_ExternalIdentity]`.
- kglite's `MERGE (n:GH_ExternalIdentity {objectid:X})` does not match the existing `:Base` node (label mismatch in the pattern) and creates a new `:GH_ExternalIdentity` node → two nodes.

The same mechanism applies to the 9 Okta_Group cases.

#### Fix

In `packages/go/kglite/dawgs/batch.go::UpdateNodeBy`:

1. When `update.IdentityKind == nil || update.IdentityKind.String() == ""`, emit `MERGE (n %s)` (label-less) instead of falling back to `Node.Kinds[0]`.
2. Add the actual Node.Kinds as `SET n:Kind1, n:Kind2, …` clauses (mirroring `cypher.go:272-277`) so the resulting node carries the right primary/secondary labels.

This requires kglite-ffi to support label-less `MERGE (n {prop:val})`. Verifying this works via a focused unit test.

#### Secondary kglite bug discovered

`MATCH (n) WHERE n.objectid = '<literal>'` returns only one of N nodes when multiple share that objectid; `MATCH (n {objectid: 'X'})` returns all. This affected the 2-copy pattern aggregation diagnostic and is filed as a separate executor bug (not part of the 706-node gap).

### Fix attempts (2026-05-02, evening)

#### Attempt 1: label-less MERGE in `UpdateNodeBy`

**Status: rejected — kglite-ffi does not support label-less MERGE.**

Modified `packages/go/kglite/dawgs/batch.go::UpdateNodeBy` to emit `MERGE (n {objectid: p.objectid}) SET ..., n:Kind1, n:Kind2` when `IdentityKind` is empty, mirroring Neo4j's `cypher.go:247-249`. The focused test `TestEmptyIdentityKindLabelLessMerge` failed: kglite produced two nodes — one `[Base]` plus one `[Node, GH_ExternalIdentity]`. Inspection of `kglite-ffi/src/graph/cypher/executor.rs:9072-9076` shows `try_match_merge_pattern` defaults to label `"Node"` when no label is provided in the MERGE pattern, then scans the `:Node` index — which never contains real nodes — and creates a new `:Node`-typed node from scratch. So label-less MERGE is structurally unsupported in kglite-ffi.

#### Attempt 2: `oidToKind` lookup in `UpdateNodeBy`, populated from `UpdateRelationshipBy`

**Status: rejected — does not change the divergence.**

Modified `UpdateNodeBy` so that when `IdentityKind` is empty, it consults `b.driver.oidToKind` (the first-seen-label cache) and uses the cached label for the MERGE pattern. Also extended `UpdateRelationshipBy` to populate `oidToKind` for both endpoint objectids using their `StartIdentityKind`/`EndIdentityKind`. The focused test `TestEmptyIdentityKindLabelLessMerge` now passes (a `:Base` stub created first via `UpdateRelationshipBy` is correctly matched by the subsequent `UpdateNodeBy` with empty `IdentityKind`).

The full comparison test, however, shows **identical** numbers to before the fix: 11893 vs 11187 nodes (Δ=+706), the same 692 `{Base} | {GH_ExternalIdentity}` 2-copy pattern in kglite, and the 14 of 48 query mismatches unchanged. The fix is a no-op in the actual KNexus ingest because the SAML file (which produces these GH_ExternalIdentity nodes) processes nodes before edges *within itself*, and no other file references those objectids — so `oidToKind` is never populated before the IngestNode runs.

#### Direct probe of Neo4j MERGE semantics

To understand why Neo4j produces a single `[Base, GH_ExternalIdentity]` node per objectid in the actual KNexus run, ran a direct Cypher probe against the running testgraph container. The probe replicated the exact UNWIND queries that `cypher.go` emits:

1. `unwind $p as p merge (n {objectid: p.objectid}) set n += p, n:GH_ExternalIdentity` — single-row UNWIND.
2. `unwind $p as p merge (s:Base {objectid: p.s.objectid}) merge (e:Base {objectid: p.e.objectid}) merge (s)-[r:GH_HasExternalIdentity]->(e) set s += p.s, e += p.e, r += p.r, s.lastseen = ..., e.lastseen = ...` — single-row UNWIND.

Run in order 1→2 (matching the IngestNode-before-edges order within the SAML file, with constraints in place), Neo4j **also splits** into two nodes: `id(n1) [GH_ExternalIdentity]` and `id(n2) [Base]`. Run in order 2→1, Neo4j produces a single merged node.

This is the same behavior kglite shows. **Neo4j's MERGE is not doing implicit cross-label coalescing.** Yet the actual KNexus ingest into Neo4j produces 692/692 merged nodes — meaning the actual ingest must execute the MERGEs in the *opposite* order from what the source files describe. Possible explanations to investigate next iteration:

- The dawgs Neo4j batch flush order is **not** strictly nodes-then-edges as `cypher.go:flush()` reads — there may be a transaction-internal reordering. (Need to enable Neo4j query log and inspect the wire-level command stream for a fresh ingest.)
- Some buffer re-ordering or coalescing happens in the Bolt driver that combines node and relationship MERGEs into one transaction whose final state is order-independent.
- A constraint-driven MATCH happens during MERGE that I haven't been able to reproduce with single-row UNWIND probes.

Until that gap closes, no Go-side fix can predict the right behavior. **Next iteration must enable Neo4j query logging and capture the actual MERGE sequence the dawgs driver emits during a fresh KNexus ingest** — that's the missing piece. The shape of the fix likely lives at the level of when the kglite Batch flushes nodes vs edges, or in `try_match_merge_pattern` getting a property-only fallback.

### Speedup: BH_REUSE_NEO4J=1

Added `BH_REUSE_NEO4J=1` env var in `cmd/api/src/test/e2e/knexus_comparison_test.go` (and helper `neo4jHasKNexusData`). When set, `TestCompareKNexus` skips `clearNeo4j` + `AssertSchema` + Neo4j ingest if the testgraph already has ≥11000 nodes. This brings the kglite-iteration cycle from ~7 min to ~90 s by reusing Neo4j data across runs (Neo4j's behavior is invariant across kglite-Go-only changes).

Set after a known-good comparison run; clear it (or the volume) if the dataset zip changes or the schema-asserting code changes.

### State of the codebase at end of this iteration

| File | Change | Keep? |
|---|---|---|
| `packages/go/kglite/dawgs/batch.go` | `UpdateNodeBy` reads `oidToKind` when IdentityKind empty; adds extra-label SETs; `UpdateRelationshipBy` populates `oidToKind` for both endpoints | **No-op for KNexus dataset, but otherwise structurally correct.** Keep — improves edge-then-node ordering scenarios. |
| `cmd/api/src/test/e2e/repro/knexus_diag_test.go` | Adds `TestKNexus2CopyPatterns`, `TestKNexusEqualityProbe`, `TestEmptyIdentityKindLabelLessMerge`, `TestSecondaryLabelMergeRepro` | Keep — diagnostic + regression fixtures. |
| `cmd/api/src/test/e2e/shared_fixtures_test.go`, `ingest_test.go` | `BH_SKIP_ANALYSIS=1` env hook | Keep — bypasses Azure post-processing hang. |
| `cmd/api/src/test/e2e/knexus_comparison_test.go` | `BH_REUSE_NEO4J=1` env hook | Keep — significant triage speedup. |
| `kglite-ffi` submodule pinned at b146f06 | Clean | Keep. |

---

## Iteration Log (2026-05-02, late evening) — wire-level evidence and root cause

A subagent instrumented the dawgs Neo4j driver (via `go.mod` `replace` to a local copy at `.local-dawgs/`) and captured the on-the-wire query stream during a fresh KNexus ingest into Neo4j.

### Wire-level findings

For the known GH_ExternalIdentity OID `45495F6C41444F41416976397332437963344372577731`:

| Time | Event |
|---|---|
| 07:31:48.198 | UpdateNodeBy queued (idKind=`<empty>`, idProps=`[objectid]`, kindsToAdd=`[GH_ExternalIdentity]`) — node buffer at 6682 entries |
| 07:31:48.983 | Three UpdateRelationshipBy queued referencing this OID (one as END for `GH_HasExternalIdentity`, one as START for `GH_MapsToUser`, one as END for `SCIM_Provisioned`) |
| 07:31:49.113 | **Relationship buffer hits 20 000 → flushRelationshipUpdates fires FIRST** |
| 07:31:49.459 → 07:31:56.628 | Three `merge (s:Base {objectid:...}) merge (e:Base {objectid:...}) merge (s)-[r:Kind]->(e)` statements run on the wire (creating the `:Base` stubs) |
| 07:32:02.931 | `BATCH_COMMIT begin` — drains residual buffers in order: nodeUpd, relCreate, relUpd, nodeDel, relDel |
| 07:32:02.940 | The label-less node MERGE is built: `unwind $p as p merge (n {objectid:p.objectid}) set n += p, n:GH_ExternalIdentity;` |
| 07:32:04.789 | Node MERGE actually runs on the wire — **13 seconds after the edge MERGEs** — finds the existing `:Base` stub via property-only match, adds `:GH_ExternalIdentity` via SET |

### Root cause

dawgs Neo4j (`.local-dawgs/drivers/neo4j/batch.go:50-106`) maintains **separate buffers** for nodes (`nodeUpdateByBuffer`) and relationships (`relationshipUpdateByBuffer`). Each fills independently. With KNexus's edge-heavy structure, the relationship buffer hits the 20 000 threshold mid-ingest and flushes by itself. The node buffer doesn't drain until `Commit()` at the end of the BatchOperation. So **edges flush first, nodes flush last** — the opposite of the in-file source order.

This is the only environment where Neo4j's label-less `merge (n {objectid:...})` finds an existing node to match: by the time it runs, the edge MERGEs have already created the `:Base` stubs.

kglite, by contrast, uses **one shared `pending` buffer** for both node MERGEs and slow-path relationship triple-MERGEs (`packages/go/kglite/dawgs/batch.go:46-47, 215-221`). They flush in append order: nodes first (from IngestNodes), then edges (from IngestRelationships). So when `MERGE (n:GH_ExternalIdentity {objectid:X})` runs, no `:Base` stub exists yet — it creates a `:GH_ExternalIdentity` node. Later the edge slow-path emits `MERGE (s:Base {objectid:X})` — kglite-ffi's `:Base` index has no entry, creates a separate `:Base` stub. Two nodes per objectid — the 692 split.

### The fix

The fix is **defer empty-IdentityKind node MERGEs in kglite** so they flush AFTER edges have created their stubs. Concretely, in `packages/go/kglite/dawgs/batch.go::UpdateNodeBy`:

1. When `update.IdentityKind == graph.EmptyKind`, **don't** call `enqueue(cypher, params)` immediately. Instead, append the NodeUpdate to a new `pendingDeferredNodes []graph.NodeUpdate` slice on the Batch.
2. In `flush()`, after the existing pending Cypher flush and `resolvePendingLookups()`, drain the deferred nodes:
   - For each: if `oidToIdx[objectid]` has any label entry, use the first such label as `kindStr` (matches the stub the edge slow-path created).
   - Otherwise use the first non-empty `Node.Kinds[i]`.
   - Emit `MERGE (n:kindStr {objectid:X}) SET n += props, n:OtherKinds...`.
   - Append to `pending`.
3. Call `b.driver.kg.CypherBatchExec(b.pending)` once more to flush the deferred queries.

This mirrors the **effective ordering** that dawgs Neo4j's separate-buffer flush produces, without changing kglite-ffi or the rest of the pipeline.

### Why prior fix attempts failed

| Attempt | Why it failed |
|---|---|
| Label-less MERGE in `UpdateNodeBy` | kglite-ffi defaults to `:Node` label for label-less patterns (executor.rs:9072-9076), creates a `:Node` stub instead of matching by property. |
| `oidToKind` lookup with population from `UpdateRelationshipBy` | Looking up `oidToKind` at `UpdateNodeBy` call time finds nothing — IngestNode runs *before* IngestRelationships within the same file, so the cache is empty. |
| Rust-side cross-label MERGE fallback (prior, by an earlier investigator) | Made `:SCIM` MERGE find any existing `:Okta` node — too aggressive, broke other invariants. |

The deferral fix sidesteps all of these because it relies on `oidToIdx` (populated by `resolvePendingLookups` after the edge slow-path flush) instead of a forward-looking heuristic.

---

## Iteration Log (2026-05-02, near midnight) — Fix landed

### The two-part fix

**Part 1**: defer empty-IdentityKind node MERGEs into `pendingDeferredNodes` (`packages/go/kglite/dawgs/batch.go`). UpdateNodeBy short-circuits at the top, appending to the slice and returning immediately. `flushDeferredNodes()` resolves each entry's MERGE label by consulting `oidToIdx` (preferred) → `oidToKind` → first `Node.Kinds` (fallback), then calls `buildAndEnqueueNodeMerge` with a synthesized non-empty IdentityKind so the existing MERGE-build code path runs unchanged.

**Part 2 (the missing piece, found via diagnostics)**: drain `pendingDeferredNodes` ONLY at `Commit()` — never from intermediate `flush()` calls. `Driver.BatchOperation` was changed from `batch.flush()` to `batch.Commit()`. The reason: intermediate flushes fire when the pending Cypher buffer hits the size threshold, and at that point only a tiny number of edges have been processed (and therefore only ~2 endpoints have populated `oidToKind`). Draining mid-stream causes 691 of 693 deferred nodes to fall through to "first own kind" and split anyway. Final-commit drain ensures ALL edges have populated `oidToIdx` first.

### Verification — comparison test results

| Metric | Pre-fix (kglite) | Post-fix (kglite) | Neo4j | Δ post-fix |
|---|---|---|---|---|
| Total nodes | 11893 | **11200** | 11187 | **+13** |
| Total relationships | 43960 | 43960 | 42569 | +1391 (unchanged) |
| Objectids on >1 node | 3293 | **2600** | 2588 | +12 |
| Objectids with 3 copies | 689 | 689 | 688 | +1 |
| SCIM nodes | 1402 | 1402 | 1393 | +9 |
| Base nodes | 5891 | 5891 | 5887 | +4 |
| Okta_User nodes | 2503 | 2503 | 2502 | +1 |
| SCIM_Provisioned edges | 1394 | 1394 | 1385 | +9 |

Diagnostic from real ingest:
```
KGLITE_DEFERRED flushed=693  fromIdx+=693 fromKind+=0 fromOwn+=0  oidToIdxSize=7911 oidToKindSize=7911
sample[0] oid="..." kinds=[GH_SamlIdentityProvider] hasIdx=true labelMap=map[Base:5820] oidToKind="Base"
sample[1] oid="..." kinds=[GH_ExternalIdentity]    hasIdx=true labelMap=map[Base:5821] oidToKind="Base"
```
All 693 deferred merges resolved from `oidToIdx` with label `Base`. Mirrors Neo4j's effective behavior precisely.

### Mismatches still open (+13 nodes / +1391 relationships)

- **+9 :SCIM stub nodes** with matching +9 SCIM_Provisioned edges. These are SCIM edges referencing OIDs that have no other earlier reference, so the edge slow-path creates a `:SCIM` stub before any `:Base` stub. In Neo4j the same OIDs end up combined; in kglite they remain split. Likely the same mechanism but with different label dynamics — needs targeted analysis on which 9 OIDs these are.
- **+4 :Base** and **+1 :Okta_User**: small tail.
- **+1391 relationships**: not affected by this fix at all — the gap is the same as pre-fix. Most likely cause: kglite produces duplicate edges where Neo4j coalesces (or vice versa). 1391 doesn't divide evenly by node-pair counts, suggesting it's a per-edge issue, not a per-node-stub issue. Needs separate investigation.

### Validation
- All existing kglite Go unit tests pass: `go test ./packages/go/kglite/dawgs/` → ok
- Focused regression tests pass: `TestEmptyIdentityKindLabelLessMerge`, `TestSecondaryLabelMergeRepro`
- 20 attack-path edge queries: all match
- 34 of 48 KNexus diagnostic queries match (was 33; +1 from `Total nodes` getting closer but still not exact)

### Cleanup remaining

- Remove `fmt.Printf("KGLITE_DEFERRED ...")` debug prints from `flushDeferredNodes` before merge.
- Remove the `deferredFlushStats` global counter (or hide behind a build tag).
- Decide whether to keep the `.local-dawgs/` workspace replace or revert it (the dawgs probe instrumentation is no longer needed for the fix itself).
- The `kglite-ffi` submodule is back at the upstream `b146f06` commit.

---

## Iteration Log (2026-05-02, after headline fix committed) — Residual gap localized

The headline fix is committed (`130a7d6d`). Investigating the residual +13 nodes / +1391 relationships.

### Per-edge-type comparison (kglite post-fix vs Neo4j)

Diffs greater than ±2:

| Edge type | kglite | Neo4j | Δ |
|---|---|---|---|
| `GH_MapsToUser` | 1381 | 692 | **+689** |
| `SCIM_Provisioned` | 1394 | 1385 | +9 |
| (small misc) | | | ~+693 ≈ +1391 ✓ |

The +1391 is dominated by **+689 extra `GH_MapsToUser` edges in kglite**. That number is strikingly close to the 689 3-copy Okta_User count. Hypothesis: kglite creates a duplicate `GH_MapsToUser` edge per 3-copy Okta_User, perhaps because the `match_by:"name"` resolver path or some endpoint duplication causes the same logical edge to land twice with different endpoint OIDs that then resolve to the same node.

### Per-2-copy-pattern post-fix comparison

| Pattern | kglite post-fix | Neo4j | Δ |
|---|---|---|---|
| `{Base}` + `{SCIM, SCIM_User}` | 692 | 692 | 0 ✓ |
| `{Base}` + `{GH_User, GitHub}` | 641 | 641 | 0 ✓ |
| `{Base}` + `{AZBase, AZUser}` | 503 | 503 | 0 ✓ |
| `{Base, GH_User}` + `{GitHub, GH_User}` | 51 | 51 | 0 ✓ |
| `{Okta, Okta_Group}` + `{Okta_Group, SCIM}` | 9 | 0 | **+9** |
| `{Base, Okta_User}` + `{Okta, Okta_User}` | 9 | 9 | 0 ✓ |
| Misc small | ~7 | ~6 | ~+1 |

The dominant residual node-divergence is **9 Okta_Group OIDs split into `(:Okta:Okta_Group) + (:Okta_Group:SCIM)`** — same pattern shape as the GH_ExternalIdentity bug (different files producing different label sets for the same objectid) but it survived the fix. Likely because the label timing differs: in the GH_ExternalIdentity case the `:Base` stub was created by edges (so `oidToIdx` got populated by `resolvePendingLookups`), but the `:Okta` and `:SCIM` paths for these 9 OIDs are both *node ingest* paths from different files with different sourceKinds, so they each get their own labelled MERGE and never end up as deferred (empty IdentityKind) merges.

### Open items for next iteration

1. **+689 `GH_MapsToUser` edges**: dump the actual edges (start OID, end OID) from kglite and Neo4j and find the duplication pattern. Likely a resolver or convertor issue, not a kglite-ffi bug.
2. **+9 Okta_Group split nodes**: subagent investigation in progress (`aea2b85c15cb43318`). Goal is to identify whether this is the same deferred-merge mechanism with different label timing, or a new bug.
3. Once both are characterized, decide whether one fix can cover both or if separate interventions are needed.

---

## Iteration Log (2026-05-02, late) — Both residual gaps closed by one fix

### The discovery

The residual +9 Okta_Group split nodes were localized via subagent investigation. Root cause: **kglite's endpoint resolver lacks read-isolation against in-batch writes**. The production code calls `endpoint.Resolver.PopulateSnapshot(ctx)` before `BatchOperation` (`cmd/api/src/services/graphify/tasks.go:234`), freezing the lower-name → objectid map so name-based edge resolution (`match_by:"name"`) only sees pre-batch state. The test ingest helpers (`doIngestZip`, `ingestZipFull`) don't do this.

In Neo4j, the resolver's `MATCH (n {name:'X'})` query runs in a separate read session and naturally doesn't see uncommitted batch writes, so even without explicit snapshotting it gets pre-batch behavior. In kglite, the resolver shares the `KnowledgeGraph` instance with the batch — every batched MERGE is immediately visible to the resolver — so `match_by:"name"` resolves to in-batch nodes that should be invisible.

This is **the same hypothesis** (#3 "Transaction isolation difference") flagged in the original investigation as "downgraded to secondary". It turned out to drive both the residual node split (+9) AND much of the relationship inflation (+1391 → 0 after fix). Hypothesis #3 is now firmly confirmed.

### The fix

In `cmd/api/src/test/e2e/ingest_test.go::doIngestZip`, call `resolver.PopulateSnapshot(ctx)` before `BatchOperation` and `resolver.ClearSnapshot()` after. This mirrors the production flow exactly.

### Verification

| Metric | Pre-fix | After deferred-merge fix | After snapshot fix | Neo4j |
|---|---|---|---|---|
| Total nodes | 11893 | 11200 | **11187** | 11187 ✓ |
| Total relationships | 43960 | 43960 | **42569** | 42569 ✓ |
| SCIM nodes | 1402 | 1402 | **1393** | 1393 ✓ |
| Objectids on >1 node | 3293 | 2600 | **2588** | 2588 ✓ |
| 3-copy objectids | 689 | 689 | **688** | 688 ✓ |
| GH_MapsToUser edges | 1381 | 1381 | **692** | 692 ✓ |
| Mismatches in 48-query suite | 14 | 14 | **1** | — |

The single remaining mismatch was tie-break order in `All labels with counts` — both backends returned identical data but ordered tied counts (e.g., 255 for AZServicePrincipal and AZApp) differently. Fixed by adding `, lbl ASC` as secondary sort.

### Hypothesis dispositions, final

| Hypothesis | Final status |
|---|---|
| #1 kglite MERGE creates duplicates across flush boundaries | Ruled out |
| #2 Stale golden file | Partially confirmed (irrelevant after fixes) |
| #3 Transaction isolation difference (resolver) | **Confirmed root cause for ~15% of the original divergence (the +9 OktaGroup + +689 GH_MapsToUser edges)** |
| #4 endpointIdentityKind returning "SCIM" instead of "Base" | Contributing factor; sourced the same ingest path that produces the deferred merges |
| #5 kglite property-only MERGE fallback | Present at b146f06; gated by `node_matches_label`, doesn't cross-label-coalesce |
| #6 Neo4j driver buffer coalesces by objectid | Ruled out (uses schema fingerprint, not objectid) |
| #7 Neo4j MERGE has implicit cross-label index match | Ruled out (Neo4j splits the same way kglite does in isolation; what differs is the order of node vs edge MERGE flushes — see headline fix) |
| #8 (new) dawgs Neo4j buffers nodes and edges separately, edges flush first | **Confirmed root cause for the headline 706-node gap (the GH_ExternalIdentity case)** |
| #9 (new) Test ingest helpers skip resolver snapshot | **Confirmed root cause for the residual +9 / +689 divergence** |

## Iteration Log (analysis-hang investigation)

**Reproduction**: `TestLoadAndQueryKNexus` (analysis enabled) hangs at
`Post-processing App Role Assignments measurement_id=88` and never completes
within a 13-minute timeout. AD pre-processing of the same suite finishes in
~500 ms; the K-Nexus dataset is the trigger.

**Goroutine dump on timeout** (key frames):

- Writer (g4396): in CGO `kg_create_edges_batch`, flushing 5000 edges
  (`packages/go/kglite/dawgs/batch.go:188` →
  `packages/go/kglite/kglite.go:280`). Reached from
  `analysis.NewPostRelationshipOperation.func1` →
  `dawgs ops/parallel.go:92`.
- Readers (g4392/4393/4394): blocked at
  `channels.Submit(... outC, AZMGAdd{Secret,Owner})` —
  `packages/go/analysis/azure/post.go:260,508,547` — sending into the
  unbuffered `readerWriterValueC` because the writer can't drain it.

So no Go/Rust deadlock — pure backpressure caused by writer-side throughput.

**Root cause** (fix shape between B and C, but on the FFI side, not the Go
flush size or buffer): each
`kg_create_edges_batch` call ran an O(degree(src)) edge-existence check
*per edge* in `ConnectionBatchProcessor::flush_chunk`
(`kglite-ffi/src/graph/batch_operations.rs::486-497`) via petgraph's
`edges_connecting`. Azure post-processing emits N×M cross-product jobs
from a small set of hub service-principal sources to thousands of
targets, so the per-flush cost grew quadratically with the source's
out-degree. Instrumenting the Go layer showed flushes climbing from
5.3 s → 10.6 s as the graph filled, processing only ~7 batches before
the test timeout fired. The same code path on the small AD fixture
finished in ~500 ms because the source out-degrees stay small.

**Fix** (single-file change, FFI layer only):
`kglite-ffi/src/graph/batch_operations.rs::flush_chunk` now builds a
`HashMap<(NodeIndex, NodeIndex), EdgeIndex>` once at the top of each
chunk by iterating each unique source's outgoing edges *once*, then
performs O(1) lookups for the per-edge existence check. The map is
kept in sync as the loop adds new edges. `add_connection` no longer
performs a redundant pre-flush existence check (kept only for
`ConflictHandling::Skip` short-circuiting).

A first attempt also added Go-side dedup of `(src,dst,kind)` in
`Batch.CreateRelationshipByIDs` to fold N×M duplicate jobs into one
EdgeSpec; while it eliminated the slow flushes too, it caused a 1800-edge
delta vs the Neo4j golden because the dedup was suppressing real
property-set updates that the analysis layer relies on emitting (e.g.
LastSeen). Removed in favor of the FFI fix alone.

**Verification**:

| Test | Before | After |
|------|--------|-------|
| `TestLoadAndQueryKNexus` (analysis on) | hang ≥13m | PASS, AppRoleAssignments 295 ms |
| `TestGoldenKNexus` | could not run (hang) | **53/53 MATCH** |
| `TestGoldenKNexusOpenGraph` | could not run (hang) | **5/5 MATCH, 97 SKIP_NONDET** (unchanged from before) |
| `TestLoadAndQueryAD`, `TestLoadAndQueryAzure` (regression check) | PASS | PASS |
| `kglite-ffi` Rust unit tests | PASS | PASS |

**Files changed**:

- `kglite-ffi/src/graph/batch_operations.rs` — pre-built per-source
  existing-edge index in `flush_chunk`; removed redundant existence
  check in `add_connection` for non-Skip modes.
- `packages/go/kglite/dawgs/batch.go` — no functional change beyond a
  reverted dedup attempt; left as-is.
