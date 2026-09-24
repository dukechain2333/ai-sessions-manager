BINARY=sm
BINDIR ?= $(HOME)/.local/bin

.PHONY: build test install vet

build:
	go build -o "$(BINARY)" ./cmd/sm

test:
	go test ./...
	python3 -m unittest discover -s scripts/iterm2
	python3 -m unittest discover -s scripts/tests

vet:
	go vet ./...

install: build
	install -d "$(BINDIR)"
	install -m 755 "$(BINARY)" "$(BINDIR)/"
