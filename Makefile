.PHONY: dev build test

dev:
	go run .

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/pingu .

test:
	go test -race ./...
