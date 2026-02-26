BINARY := atlaskb
PKG := github.com/tgeorge06/atlaskb
CMD := ./cmd/atlaskb

.PHONY: build run test lint clean

build:
	go build -o $(BINARY) $(CMD)

run: build
	./$(BINARY)

test:
	go test ./... -v

lint:
	golangci-lint run ./...

clean:
	rm -f $(BINARY)
	go clean -testcache
