# syntax=docker/dockerfile:1.7
# One build stage, two small runtime images: `--target ingest` and `--target simulator`.
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ingest ./cmd/ingest && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/simulator ./cmd/simulator && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/loadtest ./cmd/loadtest

FROM gcr.io/distroless/static-debian12:nonroot AS simulator
COPY --from=build /out/simulator /simulator
COPY --from=build /out/loadtest /loadtest
ENTRYPOINT ["/simulator"]

FROM gcr.io/distroless/static-debian12:nonroot AS ingest
COPY --from=build /out/ingest /ingest
EXPOSE 9090 8081
# Distroless has no shell or curl, so the binary probes itself.
HEALTHCHECK --interval=10s --timeout=3s --start-period=30s --retries=3 CMD ["/ingest", "-healthcheck"]
ENTRYPOINT ["/ingest"]
