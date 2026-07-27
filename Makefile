GO ?= go
BINARY ?= p2p-nc

.PHONY: build test vet fmt check clean

build:
	GOTOOLCHAIN=auto $(GO) build -o $(BINARY) ./cmd/p2p-nc

test:
	GOTOOLCHAIN=auto $(GO) test ./...

vet:
	GOTOOLCHAIN=auto $(GO) vet ./...

fmt:
	GOTOOLCHAIN=auto $(GO) fmt ./...

check: fmt vet test

clean:
	$(RM) $(BINARY)
