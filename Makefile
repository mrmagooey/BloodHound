.PHONY: init kglite ui build build-windows test test-full test-quick test-rust test-comparison test-adminer test-knexus golden-generate golden-test golden-test-all docker clean

## Initialize kglite submodule (run once after clone)
init:
	git submodule update --init --recursive

## Build kglite static library from submodule
kglite: kglite-ffi/target/release/libkglite.a

kglite-ffi/target/release/libkglite.a: kglite-ffi/src/**/*.rs kglite-ffi/Cargo.toml
	cd kglite-ffi && cargo build --release --no-default-features --features ffi

## Build the UI and copy assets to the embed directory
ui:
	yarn install && yarn workspace bloodhound-ui build
	cp -r cmd/ui/dist/. cmd/api/src/api/static/assets/

## Build the standalone binary (includes UI)
build: kglite ui
	go build -tags standalone -o bloodhound-standalone ./cmd/api/src/cmd/bhapi

## Build the Windows standalone binary (cross-compile from Linux using MinGW)
build-windows: ui
	cd kglite-ffi && cargo build --release --no-default-features --features ffi --target x86_64-pc-windows-gnu
	GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc \
		CGO_LDFLAGS="kglite-ffi/target/x86_64-pc-windows-gnu/release/libkglite.a -lm -lws2_32 -luserenv -lntdll -lbcrypt" \
		go build -tags standalone -o bloodhound-standalone.exe ./cmd/api/src/cmd/bhapi

## Run all non-comparison e2e tests (KNexus fixture skipped — adds ~115s)
test: kglite
	BH_SKIP_KNEXUS=1 go test -v -tags e2e -timeout 30m ./cmd/api/src/test/e2e/

## Run all e2e tests including the KNexus fixture
test-full: kglite
	go test -v -tags e2e -timeout 30m ./cmd/api/src/test/e2e/

## Run only the Azure attack path analysis test (quick smoke test)
test-quick: kglite
	go test -v -tags e2e -timeout 30m -run TestAzureAttackPathEdges ./cmd/api/src/test/e2e/

## Run kglite Rust unit tests
test-rust:
	cd kglite-ffi && cargo test --no-default-features --features ffi

## Run kglite vs Neo4j comparison tests (requires: docker compose -f docker-compose.testing.yml up -d)
test-comparison: kglite
	go test -v -tags 'e2e comparison' -timeout 30m -run 'TestCompareAzure|TestCompareAD$$|TestCompareKNexus' ./cmd/api/src/test/e2e/

## Run AD_Miner comparison tests against Neo4j
test-adminer: kglite
	go test -v -tags 'e2e comparison' -timeout 30m -run TestCompareADMiner ./cmd/api/src/test/e2e/

## Run k-nexus-global comparison tests against Neo4j
test-knexus: kglite
	go test -v -tags 'e2e comparison' -timeout 30m -run TestCompareKNexus ./cmd/api/src/test/e2e/

## Generate golden files from Neo4j (requires: docker compose -f docker-compose.testing.yml up -d)
golden-generate: kglite
	go test -v -tags 'e2e comparison' -timeout 30m -run 'TestGenerateGolden' ./cmd/api/src/test/e2e/

## Run golden comparison tests for AD + Azure (kglite vs golden files, no Neo4j needed)
golden-test: kglite
	BH_SKIP_KNEXUS=1 BH_SKIP_COMBINED=1 go test -v -tags e2e -timeout 30m -run 'TestGoldenAD|TestGoldenAzure' ./cmd/api/src/test/e2e/

## Run golden comparison tests for all datasets including KNexus
golden-test-all: kglite
	go test -v -tags e2e -timeout 30m -run 'TestGolden' ./cmd/api/src/test/e2e/

## Build Docker container
docker:
	docker build -f dockerfiles/standalone.Dockerfile -t bloodhound-standalone .

## Remove build artifacts
clean:
	rm -f bloodhound-standalone bloodhound-standalone.exe
	cd kglite-ffi && cargo clean 2>/dev/null || true
	find cmd/api/src/api/static/assets -not -name 'keep' -delete 2>/dev/null || true
