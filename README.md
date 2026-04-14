<p align="center">
    <picture>
        <img src="cmd/ui/public/img/BHCE_Vertical_RedField.svg" alt="BloodHound Community Edition" width='400' />
    </picture>
</p>

<hr />

BloodHound is a monolithic web application composed of an embedded React frontend with [Sigma.js](https://www.sigmajs.org/) and a [Go](https://go.dev/) based REST API backend. It is deployed with a [Postgresql](https://www.postgresql.org/) application database and a [Neo4j](https://neo4j.com/) graph database, and is fed by the [SharpHound](https://github.com/SpecterOps/SharpHound) and [AzureHound](https://github.com/SpecterOps/AzureHound) data collectors.

BloodHound leverages graph theory to reveal hidden and often unintended relationships across identity and access management systems. Powered by [OpenGraph](https://specterops.io/opengraph/?utm_campaign=Direct_DemoRequest_2025_09_01_GitHub&utm_medium=DemoRequest&utm_source=Direct&Latest_Campaign=701Uw00000X36PF), BloodHound now supports comprehensive analysis beyond Active Directory and Azure environments, enabling users to map complex privilege relationships across [diverse identity platforms](https://bloodhound.specterops.io/opengraph/library). Attackers can utilize BloodHound to rapidly discover sophisticated attack paths otherwise impossible to identify manually, while defenders can proactively identify and mitigate these risks. Both red and blue teams benefit from BloodHound's expanded capabilities, gaining deeper insights into identity and privilege structures across their entire security landscape.

BloodHound CE is created and maintained by the [SpecterOps](https://specterops.io/?utm_campaign=Direct_DemoRequest_2025_09_01_GitHub&utm_medium=DemoRequest&utm_source=Direct&Latest_Campaign=701Uw00000X36PF) team who also brought you [BloodHound Enterprise](https://specterops.io/bloodhound-overview/?utm_campaign=Direct_DemoRequest_2025_09_01_GitHub&utm_medium=DemoRequest&utm_source=Direct&Latest_Campaign=701Uw00000X36PF). The original BloodHound was created by [@\_wald0](https://www.twitter.com/_wald0), [@CptJesus](https://twitter.com/CptJesus), and [@harmj0y](https://twitter.com/harmj0y).

## Standalone Mode (kglite + SQLite)

The `standalone` branch replaces Neo4j and PostgreSQL with embedded alternatives — [kglite](https://github.com/mrmagooey/kglite) (a Rust graph engine accessed via CGO/FFI) and SQLite — enabling BloodHound to run as a single self-contained binary with zero external service dependencies. The kglite static library adds approximately 18 MB to the final binary. This eliminates 2.9 GiB of RAM (Neo4j JVM), 8.2 GB of disk (Neo4j + PostgreSQL containers), and the 15-second JVM startup delay. All 63 core BloodHound Cypher queries produce identical results to Neo4j.

### Building from Source

Prerequisites: [Rust toolchain](https://rustup.rs/) (stable), [Go](https://go.dev/) 1.25+, a C compiler (for CGO).

```bash
# Clone with submodules (kglite lives at kglite-ffi/)
git clone --recursive https://github.com/SpecterOps/BloodHound.git
cd BloodHound && git checkout standalone

# Build the kglite FFI static library
cd kglite-ffi && cargo build --release --no-default-features --features ffi && cd ..

# Build the standalone binary
CGO_ENABLED=1 go build -tags standalone -o bhapi ./cmd/api/src/cmd/bhapi

# Run
./bhapi -configfile dockerfiles/configs/standalone.config.json
```

### Docker

```bash
# Build the container image
docker build -f dockerfiles/standalone.Dockerfile -t bloodhound-standalone .

# Run — port 8080, persistent graph data in a volume
docker run -p 8080:8080 -v bh-data:/opt/bloodhound/work bloodhound-standalone
```

The container exposes port 8080 and stores its graph database under `/opt/bloodhound/work`.

### Performance: kglite vs Neo4j

Measured against the BloodHound sample datasets using the e2e comparison test suite.

**Summary:**
- AD dataset: **4.2x** faster end-to-end (ingest + analysis) vs Neo4j
- Azure dataset: **2.5x** faster analysis, **1.6x** faster total
- Attack path queries: **67-524x** faster (in-memory, no network overhead)

#### AD Dataset (1,519 nodes, 16,367 relationships)

| Phase | kglite | Neo4j | Speedup |
|---|---|---|---|
| Ingest | 2.3s | 14.1s | **6.3x** |
| Analysis (attack path computation) | 7.5s | 8.9s | **1.2x** |
| **Total pipeline** | **9.8s** | **23.0s** | **2.4x** |

| Query Benchmark | kglite | Neo4j | Speedup |
|---|---|---|---|
| 20 preset Cypher queries | 95ms | 580ms | **6.1x** |
| 29 attack path edge queries | 16ms | 319ms | **20x** |

Per-query highlights (kglite vs Neo4j):
- Variable-length path queries (e.g., domain admin membership): **2.4ms vs 176ms (73x)**
- Property-filtered counts (e.g., kerberoastable users): **1.1ms vs 46ms (42x)**
- Simple relationship counts (e.g., DCSync edges): **0.3ms vs 12ms (31x)**

#### Azure/Entra Dataset (13,555 nodes, 27,388 relationships)

| Phase | kglite | Neo4j | Speedup |
|---|---|---|---|
| Ingest | 42s | 14s | 0.3x (slower) |
| Analysis (attack path computation) | 6.7s | 1m 13s | **10.9x** |
| **Total pipeline** | **49s** | **1m 27s** | **1.8x** |

> **Note:** kglite ingest is slower on the larger Azure dataset due to CGO/FFI serialization overhead per batch. Analysis and query performance remain significantly faster since all graph traversals execute in-process without network round-trips.

### Testing

The test suite lives in `cmd/api/src/test/e2e/` and validates that kglite produces identical results to Neo4j across ingestion, analysis, and Cypher queries. Tests are gated by build tags: `e2e` for all end-to-end tests, `comparison` for tests that additionally require a live Neo4j instance.

#### E2E Tests

Ingest sample datasets (AD, Azure, KNexus) into kglite, run attack-path analysis, and validate node/edge counts and query results. No external services needed.

```bash
make test          # full e2e suite
make test-quick    # smoke test (Azure attack path analysis only)
```

Set `BH_SKIP_KNEXUS=1` or `BH_SKIP_COMBINED=1` to skip loading expensive fixtures during development.

#### Golden Tests

Compare kglite query results against pre-generated reference files. **Neo4j is the source of truth** — golden files are generated by running queries against Neo4j and saving the results. kglite is expected to match these results exactly. The golden files cover AD (49 queries), Azure (14 queries), KNexus (53 queries), and KNexus OpenGraph (305 queries).

```bash
make golden-test       # compare kglite against golden files (AD + Azure, no Neo4j needed)
make golden-test-all   # include KNexus datasets

# regenerate golden files from Neo4j (requires Docker)
docker compose -f docker-compose.testing.yml up -d
make golden-generate
docker compose -f docker-compose.testing.yml down
```

Golden files are stored in `cmd/api/src/test/e2e/testdata/golden/`. When kglite behavior changes, mismatches should be investigated against Neo4j before updating the golden files.

#### Comparison Tests

Run the same queries against both kglite and a live Neo4j instance side-by-side, reporting matches/mismatches with timing and speedup metrics. Requires a running Neo4j container.

```bash
docker compose -f docker-compose.testing.yml up -d
make test-comparison   # kglite vs Neo4j (AD, Azure, KNexus)
make test-adminer      # AD_Miner query compatibility
docker compose -f docker-compose.testing.yml down
```

#### Reproduction Tests

Isolated tests in `cmd/api/src/test/e2e/repro/` that target specific kglite/Neo4j behavioral divergences: MERGE label isolation, idempotency across batch flushes, cross-platform stub deduplication, and batch flush boundary behavior.

```bash
go test -v -tags e2e -timeout 5m ./cmd/api/src/test/e2e/repro/
```

#### Rust Tests

Unit tests for the kglite FFI library (Cypher parsing, graph operations).

```bash
make test-rust
```

#### Make Target Reference

| Target | Neo4j Required | Description |
|---|---|---|
| `test` | No | Full e2e suite (ingest + analysis + queries) |
| `test-quick` | No | Smoke test (Azure attack paths only) |
| `golden-test` | No | kglite vs golden files (AD + Azure) |
| `golden-test-all` | No | kglite vs golden files (all datasets) |
| `golden-generate` | Yes | Generate golden files from Neo4j |
| `test-comparison` | Yes | Side-by-side kglite vs Neo4j |
| `test-adminer` | Yes | AD_Miner query compatibility |
| `test-knexus` | Yes | KNexus dataset comparison |
| `test-rust` | No | kglite Rust unit tests |

### kglite Submodule

The embedded graph database source lives at `kglite-ffi/` as a git submodule from [github.com/mrmagooey/kglite](https://github.com/mrmagooey/kglite). After cloning, ensure submodules are initialized:

```bash
git submodule update --init --recursive
```

## Running BloodHound Community Edition
Please refer to the [Quickstart Guide for BloodHound Community Edition](https://bloodhound.specterops.io/get-started/quickstart/community-edition-quickstart), which is part of the [BloodHound documentation](https://bloodhound.specterops.io).

## Useful Links

- [BloodHound Documentation](https://bloodhound.specterops.io/)
- [BloodHound Community Edition Quickstart Guide](https://bloodhound.specterops.io/get-started/quickstart/community-edition-quickstart)
- [BloodHound Slack](https://slack.specterops.io)
- [OpenGraph Documentation](https://bloodhound.specterops.io/opengraph/overview)
- [Wiki](https://github.com/SpecterOps/BloodHound/wiki)
- [Docker Compose Example](./examples/docker-compose/README.md)
- [Developer Quick Start Guide](https://github.com/SpecterOps/BloodHound/wiki/Development)
- [Contributing Guide](https://github.com/SpecterOps/BloodHound/wiki/Contributing)
- [Contributors](./CONTRIBUTORS.md)

## Contact

Please check out the [Contact page](https://github.com/SpecterOps/BloodHound/wiki/Contact) in our wiki for details on how to reach out with questions and suggestions.

## Licensing

```
Copyright 2025 Specter Ops, Inc.

Licensed under the Apache License, Version 2.0
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
```

Unless otherwise annotated by a lower-level LICENSE file or license header, all files in this repository are released
under the `Apache-2.0` license. A full copy of the license may be found in the top-level [LICENSE](LICENSE) file.
