# Test Speedup Plan

The slowest paths are the Go e2e tests (`make test`, `golden-test`, `test-comparison`), so that's where the wins are.

## What's already good

- `shared_fixtures_test.go:91-201` — TestMain loads AD/Azure/Combined/KNexus once and shares them across tests. This is the biggest optimization already in place.
- `BH_SKIP_KNEXUS=1` / `BH_SKIP_COMBINED=1` env gates for development.
- `make test-quick` for smoke testing.

## Easy wins

### 1. Add `t.Parallel()` everywhere — currently zero use across 196 tests

```
$ grep -c "t.Parallel" cmd/api/src/test/e2e/*.go → 0 in all files
```

Two clear tiers:

- **Read-only tests over shared fixtures** (safe trivially — graphs are not mutated): `TestGoldenAD/Azure/KNexus/KNexusOpenGraph` (`golden_test.go:137-170`), `TestADAttackPathEdges` / `TestAzureAttackPathEdges` (`analysis_test.go:74-101`), `TestADIngestAssertions` / `TestAzureIngestAssertions` / `TestCombinedIngestAssertions` (`ingest_assertions_test.go:93-131`), `TestLoadAndQueryAD/Azure/All` (`ingest_test.go:458-488`). Just add `t.Parallel()` at the top of each. The 4 golden tests alone serialize ~420 queries today.
- **Fresh-graph tests** (`openGraph(t)` → `t.TempDir()` → independent DB): the 30+ tests in `batch_coverage_test.go`, 13+ in `transaction_coverage_test.go`, plus `dawgs_driver_test.go`, `cypher_test.go`, `driver_coverage_test.go`, `error_edge_case_test.go`. Each opens its own kglite file, so they're embarrassingly parallel.

This alone should give 3–8× wall-clock speedup on a multi-core box.

### 2. Parallelize fixture loading in TestMain (`shared_fixtures_test.go:108-192`)

AD, Azure, and KNexus are loaded sequentially today. Load them concurrently with a `sync.WaitGroup` — no cross-dependencies. KNexus is the heaviest fixture, so this directly cuts startup time.

### 3. Avoid the Combined fixture's redundant ingest (`shared_fixtures_test.go:148-169`)

It re-ingests AD then Azure into a third kglite — duplicating work already done for `fixtureADGraph` and `fixtureAzureGraph`. kglite has a save/load round-trip (`save_load_test.go`); copying the AD `.kgl` file and ingesting only Azure on top would roughly halve the Combined-build time, or check if `db.Save()` then load-as-merge is feasible.

### 4. Cache fixtures across runs

TestMain rebuilds graphs from zips every invocation. Persist post-analysis `.kgl` files keyed by `(zip mtime, ingest schema hash, code hash)` under `testdata/.cache/` and skip ingestion + analysis when fresh. With shared fixtures dominating runtime, this turns repeat runs into seconds.

### 5. Use `go test -p N` and `-parallel N` in the Makefile

Currently the `test` target has neither; bumping `-parallel` is a no-op until step 1 lands, but pairing them gives the scheduler room.

## Build-side wins

### 6. Cache `libkglite.a` across `make` invocations

`make` already has the dependency (`Makefile:9`), but CI rebuilds from scratch. Use `actions/cache` keyed on `kglite-ffi/Cargo.lock` + Rust source hashes — saves the cargo release build (often the longest single step).

### 7. Use `cargo build` (debug) instead of `--release` for `test-rust`

Test runs typically don't need `--release` artifacts; only the final binary build does. (Currently `test-rust` already drops `--release`, but verify the e2e Go tests don't pull in the release static lib unnecessarily — the `kglite` Make target builds release.)

## Selective execution

### 8. Split `make test` into focused targets

Today `make test` runs everything under `./cmd/api/src/test/e2e/`. Add e.g. `make test-driver` (`-run 'TestDriver|TestTx|TestBatch'`), `make test-golden`, `make test-ingest` so dev iteration runs only the relevant slice. Pairs naturally with `t.Parallel()`.

### 9. Run repro tests as their own short target

`cmd/api/src/test/e2e/repro/` is in a sub-package and can be run independently with `-run` filters — these are fast regression tests that shouldn't be coupled to the full e2e timeout.

## Highest-leverage order

1. Add `t.Parallel()` to every read-only-shared test and every `openGraph(t)` test. (~1 hour, biggest single speedup.)
2. Parallelize the three fixture loads in TestMain. (~30 min.)
3. Cache `.kgl` fixtures by input hash. (~half day, dwarfs all other gains on repeated local runs.)
4. CI cache for `libkglite.a` + cargo registry.
