GO ?= go
BINARY ?= p2p-nc
ALIAS ?= pnc
SOAK_PROFILE ?= smoke
SOAK_REPORT ?= artifacts/webrtc-soak-local.json

.PHONY: build test test-docker soak-webrtc vet fmt check clean

build:
	GOTOOLCHAIN=auto $(GO) build -o $(BINARY) ./cmd/p2p-nc
	GOTOOLCHAIN=auto $(GO) build -o $(ALIAS) ./cmd/pnc

test:
	GOTOOLCHAIN=auto $(GO) test ./...
	bash deploy/deploy_test.sh
	bash deploy/wireguard-full-tunnel_test.sh

test-docker:
	bash scripts/docker_test.sh

soak-webrtc:
	GOTOOLCHAIN=auto $(GO) run ./cmd/webrtc-soak \
		--profile $(SOAK_PROFILE) \
		--report $(SOAK_REPORT)

vet:
	GOTOOLCHAIN=auto $(GO) vet ./...

fmt:
	GOTOOLCHAIN=auto $(GO) fmt ./...

check: fmt vet test

clean:
	$(RM) $(BINARY) $(ALIAS)
