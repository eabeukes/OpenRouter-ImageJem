# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.26.7
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-trixie AS build

ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -mod=readonly -trimpath \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildDate=${BUILD_DATE}" \
    -o /out/bridge ./cmd/bridge

FROM gcr.io/distroless/static-debian13:nonroot

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
ARG SOURCE=https://github.com/eabeukes/OpenRouter-ImageJem

LABEL org.opencontainers.image.title="OpenRouter ImageJem" \
      org.opencontainers.image.description="Key-transparent OpenWebUI bridge for OpenRouter image generation and editing" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.source="${SOURCE}" \
      org.opencontainers.image.url="${SOURCE}" \
      org.opencontainers.image.documentation="${SOURCE}#readme" \
      org.opencontainers.image.licenses="MIT"

COPY --from=build /out/bridge /bridge
USER 65532:65532
STOPSIGNAL SIGTERM
ENTRYPOINT ["/bridge"]
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 CMD ["/bridge", "healthcheck"]
