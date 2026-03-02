BINARY := saas-watcher
VERSION := 0.1.0

.PHONY: build test lint clean

build:
	go build -o bin/$(BINARY) ./cmd/saas-watcher

test:
	go test ./... -v -race

lint:
	golangci-lint run

clean:
	rm -rf bin/
