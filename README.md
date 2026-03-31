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

The `standalone` branch replaces Neo4j and PostgreSQL with embedded alternatives — [kglite](https://github.com/mrmagooey/kglite) (a Rust graph engine accessed via CGO/FFI) and SQLite — enabling BloodHound to run as a single self-contained binary with zero external service dependencies.

### Quick Start

```bash
# Build the kglite FFI library
cd kglite-ffi && cargo build --release --no-default-features --features ffi && cd ..

# Build and run the ingestor
CGO_ENABLED=1 go build -o ingestor ./cmd/api/src/cmd/ingestor
./ingestor --file path/to/sharphound_data.zip --graph-path bloodhound.kgl
```

### Performance: kglite vs Neo4j

Measured against the BloodHound sample datasets using the e2e comparison test suite.

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

### Running the Tests

```bash
# E2e regression tests (no external services needed)
CGO_ENABLED=1 go test -v -tags e2e -timeout 30m ./cmd/api/src/test/e2e/

# Performance comparison against Neo4j (requires Docker)
docker compose -f docker-compose.testing.yml up -d
CGO_ENABLED=1 go test -v -tags "comparison e2e" -timeout 30m ./cmd/api/src/test/e2e/
docker compose -f docker-compose.testing.yml down
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
