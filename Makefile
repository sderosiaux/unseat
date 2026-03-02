BINARY := unseat
VERSION := 0.1.0

.PHONY: build test lint clean

build:
	go build -o bin/$(BINARY) ./cmd/unseat

test:
	go test ./... -v -race

lint:
	golangci-lint run

clean:
	rm -rf bin/
