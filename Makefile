BINARY=cs

.PHONY: build test install vet

build:
	go build -o $(BINARY) ./cmd/cs

test:
	go test ./...

vet:
	go vet ./...

install: build
	install -Dm755 $(BINARY) $(HOME)/.local/bin/$(BINARY)
