# syntax=docker/dockerfile:1.7

FROM node:22-bookworm AS web-builder
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26-bookworm AS go-builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web-builder /src/web/dist ./web/dist

ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown

RUN CGO_ENABLED=1 go build -ldflags "-s -w \
  -X github.com/tgeorge06/atlaskb/internal/version.Version=${VERSION} \
  -X github.com/tgeorge06/atlaskb/internal/version.Commit=${COMMIT} \
  -X github.com/tgeorge06/atlaskb/internal/version.Date=${DATE}" \
  -o /out/atlaskb ./cmd/atlaskb

FROM debian:bookworm-slim
RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates tzdata git wget \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /root
COPY --from=go-builder /out/atlaskb /usr/local/bin/atlaskb
COPY scripts/container-entrypoint.sh /usr/local/bin/container-entrypoint.sh
RUN chmod +x /usr/local/bin/container-entrypoint.sh

EXPOSE 3000

ENTRYPOINT ["/usr/local/bin/container-entrypoint.sh"]
CMD ["serve", "--port", "3000"]
