.PHONY: init kglite build test test-quick test-rust test-comparison test-adminer docker clean

## Initialize kglite submodule (run once after clone)
init:
	git submodule update --init --recursive

## Build kglite static library from submodule
kglite: kglite-ffi/target/release/libkglite.a

kglite-ffi/target/release/libkglite.a: kglite-ffi/src/**/*.rs kglite-ffi/Cargo.toml
	cd kglite-ffi && cargo build --release --no-default-features --features ffi

## Build the standalone binary
build: kglite
	go build -tags standalone -o bloodhound-standalone ./cmd/api/src/cmd/bhapi

## Run all non-comparison e2e tests
test: kglite
	go test -v -tags e2e -timeout 30m ./cmd/api/src/test/e2e/

## Run only the Azure attack path analysis test (quick smoke test)
test-quick: kglite
	go test -v -tags e2e -timeout 30m -run TestAzureAttackPathEdges ./cmd/api/src/test/e2e/

## Run kglite Rust unit tests
test-rust:
	cd kglite-ffi && cargo test --no-default-features --features ffi

## Run kglite vs Neo4j comparison tests (requires: docker compose -f docker-compose.testing.yml up -d)
test-comparison: kglite
	go test -v -tags 'e2e comparison' -timeout 30m -run 'TestCompareAzure|TestCompareAD$$' ./cmd/api/src/test/e2e/

## Run AD_Miner comparison tests against Neo4j
test-adminer: kglite
	go test -v -tags 'e2e comparison' -timeout 30m -run TestCompareADMiner ./cmd/api/src/test/e2e/

## Build Docker container
docker:
	docker build -f dockerfiles/standalone.Dockerfile -t bloodhound-standalone .

## Remove build artifacts
clean:
	rm -f bloodhound-standalone
	cd kglite-ffi && cargo clean 2>/dev/null || true
