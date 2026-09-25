BIN ?= $(HOME)/.local/bin/mad

build:
	go build -o mad .

install:
	go build -o $(BIN) .

test:
	go vet ./...
	go test ./...

.PHONY: build install test
