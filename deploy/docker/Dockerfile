ARG GO_VERSION=1.22

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-bookworm AS builder

WORKDIR /src

ARG TARGETOS
ARG TARGETARCH

ENV CGO_ENABLED=0

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
  go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
  --mount=type=cache,target=/root/.cache/go-build \
  GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
  go build -trimpath -ldflags="-s -w" -o /out/cidr-api .

FROM scratch AS runtime

ARG APP_VERSION=dev

LABEL org.opencontainers.image.title="go-cidr-api" \
  org.opencontainers.image.description="China province/city CIDR query API" \
  org.opencontainers.image.version="${APP_VERSION}"

WORKDIR /app

ENV RUN_MODE=http \
  ADDR=:30662 \
  DATA_FILE=/app/china_city_cidrs.compact.json

COPY --from=builder /out/cidr-api /cidr-api
COPY --chown=65532:65532 china_city_cidrs.compact.json /app/china_city_cidrs.compact.json

USER 65532:65532

EXPOSE 30662

ENTRYPOINT ["/cidr-api"]
