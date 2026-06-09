# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.26.4
FROM golang:${GO_VERSION} AS builder

WORKDIR /src

COPY go.mod go.sum VERSION version.go ./
COPY cmd ./cmd
COPY internal ./internal
COPY vendor ./vendor

RUN CGO_ENABLED=0 GOFLAGS="-mod=vendor" go build -o /out/s3m ./cmd/main

FROM registry.access.redhat.com/ubi9/ubi-minimal:9.7

ENV XDG_DATA_HOME=/var/lib/s3m
WORKDIR /app

RUN microdnf install -y ca-certificates \
    && microdnf clean all \
    && mkdir -p ${XDG_DATA_HOME} \
    && chown -R 65532:65532 ${XDG_DATA_HOME}

COPY --from=builder /out/s3m /usr/local/bin/s3m

USER 65532:65532

EXPOSE 5310
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/s3m"]
CMD ["mcp", "--http", "--http-addr=0.0.0.0:5310", "--stateless-auth"]
