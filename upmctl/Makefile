SHELL := /bin/sh

.PHONY: fmt fmt-check vet test test-race test-cover test-contract test-legacy test-skill test-delivery-validation test-cli-audit \
	build build-linux-amd64 release release-artifacts release-darwin-arm64 \
	release-linux-amd64 release-linux-arm64 release-checksums release-smoke clean check

BINARY ?= bin/upmctl
DIST_DIR ?= dist
BUILDINFO_PACKAGE := github.com/upmio/kubespray-upm/upmctl/internal/buildinfo

VERSION ?= $(shell ./scripts/build-metadata.sh version)
GIT_COMMIT ?= $(shell ./scripts/build-metadata.sh commit)
SOURCE_DATE_EPOCH ?= $(shell ./scripts/build-metadata.sh epoch)
BUILD_DATE ?= $(shell SOURCE_DATE_EPOCH='$(SOURCE_DATE_EPOCH)' ./scripts/build-metadata.sh date)

LDFLAGS := -s -w \
	-X $(BUILDINFO_PACKAGE).Version=$(VERSION) \
	-X $(BUILDINFO_PACKAGE).GitCommit=$(GIT_COMMIT) \
	-X $(BUILDINFO_PACKAGE).BuildDate=$(BUILD_DATE)
GO_BUILD_FLAGS := -trimpath -buildvcs=false -ldflags "$(LDFLAGS)"

fmt:
	gofmt -w $$(find . -name '*.go' -type f)

fmt-check:
	@test -z "$$(gofmt -l $$(find . -name '*.go' -type f))"

vet:
	go vet ./...

test:
	go test ./...

test-race:
	go test -race ./...

test-cover:
	go test -coverprofile=coverage.out ./...

test-contract:
	go test ./internal/output ./internal/cli ./internal/contract -run 'JSON|Error|Contract|Capabilities|Timeout|Schema'

test-legacy:
	bash ../vagrant_setup_scripts/tests/test_network_wizard.sh

# Separately runnable source gate for the packaged Codex Skill. The validator
# is provided by the official skill-creator bundled with the local Codex SDK.
test-skill:
	./scripts/validate-skill.sh ./skills/upmctl-environment

test-delivery-validation:
	./scripts/tests/test_validate_test_environment.sh

test-cli-audit: build
	sh -n ./scripts/tests/audit-cli-contract.sh ./scripts/tests/host-safe-cli-coverage.sh
	UPMCTL_BIN='$(abspath $(BINARY))' ./scripts/tests/audit-cli-contract.sh

build:
	mkdir -p $$(dirname $(BINARY))
	CGO_ENABLED=0 go build $(GO_BUILD_FLAGS) -o $(BINARY) ./cmd/upmctl

build-linux-amd64:
	mkdir -p $$(dirname $(BINARY))
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(BINARY)-linux-amd64 ./cmd/upmctl

# Build release archives through a recursive target so dist is cleaned exactly
# once before the platform targets are allowed to run in parallel.
release:
	rm -rf $(DIST_DIR)
	$(MAKE) release-artifacts VERSION='$(VERSION)' GIT_COMMIT='$(GIT_COMMIT)' SOURCE_DATE_EPOCH='$(SOURCE_DATE_EPOCH)' BUILD_DATE='$(BUILD_DATE)'
	./scripts/write-checksums.sh '$(DIST_DIR)' '$(DIST_DIR)/SHA256SUMS'
	./scripts/release-smoke.sh '$(DIST_DIR)' '$(VERSION)' '$(GIT_COMMIT)' '$(BUILD_DATE)'

release-artifacts: release-darwin-arm64 release-linux-amd64 release-linux-arm64

release-darwin-arm64:
	./scripts/build-release.sh darwin arm64 '$(VERSION)' '$(GIT_COMMIT)' '$(BUILD_DATE)' '$(SOURCE_DATE_EPOCH)' '$(DIST_DIR)'

release-linux-amd64:
	./scripts/build-release.sh linux amd64 '$(VERSION)' '$(GIT_COMMIT)' '$(BUILD_DATE)' '$(SOURCE_DATE_EPOCH)' '$(DIST_DIR)'

release-linux-arm64:
	./scripts/build-release.sh linux arm64 '$(VERSION)' '$(GIT_COMMIT)' '$(BUILD_DATE)' '$(SOURCE_DATE_EPOCH)' '$(DIST_DIR)'

release-checksums: release-artifacts
	./scripts/write-checksums.sh '$(DIST_DIR)' '$(DIST_DIR)/SHA256SUMS'

release-smoke: release-checksums
	./scripts/release-smoke.sh '$(DIST_DIR)' '$(VERSION)' '$(GIT_COMMIT)' '$(BUILD_DATE)'

clean:
	rm -rf bin dist coverage.out

check: fmt-check vet test test-race test-contract test-legacy test-delivery-validation build
