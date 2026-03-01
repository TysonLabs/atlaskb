BINARY := atlaskb
PKG := github.com/tgeorge06/atlaskb
CMD := ./cmd/atlaskb

.PHONY: build run test lint clean web build-full dev-web dev-server

build:
	go build -o bin/$(BINARY) $(CMD)

run: build
	./bin/$(BINARY)

test:
	go test ./... -v

lint:
	golangci-lint run ./...

clean:
	rm -f bin/$(BINARY)
	go clean -testcache

web:
	cd web && npm ci && npm run build

build-full: web build

dev-web:
	cd web && npm run dev

dev-server:
	go run $(CMD) serve --port 8080
