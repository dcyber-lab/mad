.PHONY: build test lint

build:
	go build -o reachable .

test:
	go vet ./...
	go test ./...

lint:
	shellcheck -s bash -S warning internal/probe/scripts/*.sh
