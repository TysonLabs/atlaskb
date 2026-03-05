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

RUN CGO_ENABLED=0 go build -ldflags "-s -w \
  -X github.com/tgeorge06/atlaskb/internal/version.Version=${VERSION} \
  -X github.com/tgeorge06/atlaskb/internal/version.Commit=${COMMIT} \
  -X github.com/tgeorge06/atlaskb/internal/version.Date=${DATE}" \
  -o /out/atlaskb ./cmd/atlaskb

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata git \
  && addgroup -S atlaskb \
  && adduser -S atlaskb -G atlaskb

WORKDIR /home/atlaskb
COPY --from=go-builder /out/atlaskb /usr/local/bin/atlaskb

USER atlaskb
EXPOSE 3000

ENTRYPOINT ["/usr/local/bin/atlaskb"]
CMD ["serve", "--port", "3000"]
