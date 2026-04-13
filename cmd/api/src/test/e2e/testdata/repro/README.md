# Minimal Reproduction Datasets

These datasets are minimal test cases that reproduce the k-nexus golden test
mismatches between kglite and Neo4j. All three k-nexus mismatches (total nodes
+706, total relationships +3191, extra AZExecuteCommand edge type) trace back
to a single root cause: **cross-platform duplicate node creation**.

## Root cause

When Okta nodes are ingested first (source_kind=Okta), then SCIM edges
reference those same objectids (source_kind=SCIM), the SCIM relationship
MERGE creates stub nodes with label `:SCIM` instead of reusing the existing
`:Okta` nodes. This creates duplicate nodes for each cross-referenced objectid.

The 706 extra nodes in the k-nexus dataset are these duplicate stubs. The 3191
extra relationships come from analysis post-processing creating edges against
both the original and duplicate nodes. AZExecuteCommand appears as an "extra"
edge type in kglite because the duplicate nodes change which analysis paths
fire.

## Scenarios

1. **Cross-platform duplicate nodes** (`cross_platform_dupes/`)
   Okta nodes ingested, then SCIM edges reference existing Okta objectids.
   Expects 6 nodes, gets 8 (2 duplicates).

2. **AZExecuteCommand edge creation** (`az_execute_command/`)
   Azure tenant + Windows devices + Intune role. Verifies AZExecuteCommand
   edges are created correctly. This test PASSES — the edge type appearing
   in kglite but not Neo4j golden files is a side effect of the duplicate
   nodes, not a separate bug.

3. **Combined scenario** (`combined/`)
   AD + Azure + Okta + SCIM together, confirming duplicates persist
   across mixed-platform ingest.

## Running

```bash
go test -v -tags e2e -timeout 5m ./cmd/api/src/test/e2e/repro/
```
