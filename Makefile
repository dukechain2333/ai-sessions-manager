BINARY=sm

.PHONY: build test install vet

build:
	go build -o $(BINARY) ./cmd/sm

test:
	go test ./...

vet:
	go vet ./...

install: build
	install -Dm755 $(BINARY) $(HOME)/.local/bin/$(BINARY)
