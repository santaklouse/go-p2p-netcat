GO ?= go
BINARY ?= p2p-nc
ALIAS ?= pnc

.PHONY: build test test-docker vet fmt check clean

build:
	GOTOOLCHAIN=auto $(GO) build -o $(BINARY) ./cmd/p2p-nc
	GOTOOLCHAIN=auto $(GO) build -o $(ALIAS) ./cmd/pnc

test:
	GOTOOLCHAIN=auto $(GO) test ./...
	bash deploy/deploy_test.sh
	bash deploy/wireguard-full-tunnel_test.sh

test-docker:
	bash scripts/docker_test.sh

vet:
	GOTOOLCHAIN=auto $(GO) vet ./...

fmt:
	GOTOOLCHAIN=auto $(GO) fmt ./...

check: fmt vet test

clean:
	$(RM) $(BINARY) $(ALIAS)
