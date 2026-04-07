# syntax=docker/dockerfile:1-labs

########
# Build kglite static library
################
FROM docker.io/library/rust:1-bookworm AS rust-builder

WORKDIR /build/kglite-ffi
COPY kglite-ffi/Cargo.toml kglite-ffi/Cargo.lock ./
COPY kglite-ffi/src ./src
COPY kglite-ffi/kglite ./kglite
COPY kglite-ffi/benches ./benches

RUN cargo build --release --no-default-features --features ffi

########
# Build Go binary
################
FROM docker.io/library/golang:1.25.0-bookworm AS api-builder

RUN apt-get update && apt-get install -y --no-install-recommends gcc libc6-dev && rm -rf /var/lib/apt/lists/*

ENV CGO_ENABLED=1

WORKDIR /build
COPY --parents go.mod go.sum cmd/api packages/go ./
COPY --from=rust-builder /build/kglite-ffi/target/release/libkglite.a ./kglite-ffi/target/release/libkglite.a

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -tags standalone -o /bloodhound-standalone ./cmd/api/src/cmd/bhapi

########
# Runtime image
################
FROM docker.io/library/debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
RUN mkdir -p /opt/bloodhound/work /etc/bloodhound /var/log

COPY --from=api-builder /bloodhound-standalone /bloodhound
COPY dockerfiles/configs/standalone.config.json /bloodhound.config.json

EXPOSE 8080
VOLUME ["/opt/bloodhound/work"]

ENTRYPOINT ["/bloodhound", "-configfile", "/bloodhound.config.json"]
