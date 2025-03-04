# docker build . -t cosmwasm/wasmd:latest
# docker run --rm -it cosmwasm/wasmd:latest /bin/sh
FROM golang:1.20-alpine3.18 AS go-builder
ARG BUILDPLATFORM=linux/amd64

# NOTE: add libusb-dev to run with LEDGER_ENABLED=true
RUN set -eux &&\
    apk update &&\
    apk add --no-cache \
    ca-certificates \
    linux-headers \
    build-base \
    cmake \
    git

# use mimalloc for musl
WORKDIR ${GOPATH}/src/mimalloc
RUN set -eux &&\
    git clone --depth 1 \
        https://github.com/microsoft/mimalloc . &&\
    mkdir -p build &&\
    cd build &&\
    cmake .. &&\
    make -j$(nproc) &&\
    make install

WORKDIR /code
COPY . /code/

# Cosmwasm - Download correct libwasmvm version and verify checksum
RUN set -eux &&\
    WASMVM_VERSION=$(go list -m github.com/CosmWasm/wasmvm | cut -d ' ' -f 5) && \
    WASMVM_DOWNLOADS="github.com/classic-terra/wasmvm/releases/download/${WASMVM_VERSION}"; \
    wget ${WASMVM_DOWNLOADS}/checksums.txt -O /tmp/checksums.txt; \
    if [ ${BUILDPLATFORM} = "linux/amd64" ]; then \
        WASMVM_URL="${WASMVM_DOWNLOADS}/libwasmvm_muslc.x86_64.a"; \
    elif [ ${BUILDPLATFORM} = "linux/arm64" ]; then \
        WASMVM_URL="${WASMVM_DOWNLOADS}/libwasmvm_muslc.aarch64.a"; \
    else \
        echo "Unsupported Build Platfrom ${BUILDPLATFORM}"; \
        exit 1; \
    fi; \
    wget ${WASMVM_URL} -O /lib/libwasmvm_muslc.a; \
    CHECKSUM=`sha256sum /lib/libwasmvm_muslc.a | cut -d" " -f1`; \
    grep ${CHECKSUM} /tmp/checksums.txt; \
    rm /tmp/checksums.txt

# force it to use static lib (from above) not standard libgo_cosmwasm.so file
RUN LEDGER_ENABLED=false \
  go build \
    -mod=readonly \
    -tags "muslc,linux" \
    -ldflags " \
      -w -s -linkmode=external -extldflags \
      '-L/go/src/mimalloc/build -lmimalloc -Wl,-z,muldefs -static' \
    " \
    -trimpath \
    -o build/mantlemint ./sync.go

FROM alpine:3.18

RUN apk update && apk add wget lz4 aria2 curl jq gawk coreutils "zlib>1.2.12-r2" "libssl1.1>1.1.1q-r0"

WORKDIR /root

COPY --from=go-builder /code/build/mantlemint /usr/local/bin/mantlemint

# rest server
EXPOSE 1317
# grpc
EXPOSE 9090

CMD ["/usr/local/bin/mantlemint"]
