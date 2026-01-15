.DEFAULT_GOAL = build
.PHONY: build build-all build-x86 pkg clean clean-build clean-tests test-unit test-integration-image test-integration

PACKER ?= packer
GORELEASER_IMAGE = docker.io/goreleaser/goreleaser:v2.13.3
GORELEASER_CONFIG = build/goreleaser.yml
TEST_IMAGE = $(CURDIR)/tests/images/fedora-stratis.qcow2
TEST_BINARY = $(CURDIR)/tests/integration.test
PLUGIN_BINARY = $(CURDIR)/build/dist/podman-volume-stratis_linux_amd64_v1/podman-volume-stratis

# Run goreleaser via Docker
define goreleaser
	docker run --rm \
		-v $(CURDIR):/work \
		-w /work \
		-e GOOS=$(1) \
		-e GOARCH=$(2) \
		$(GORELEASER_IMAGE) $(3)
endef

# Local development: snapshot build for current platform
build:
	$(call goreleaser,$(shell go env GOOS),$(shell go env GOARCH),build --config $(GORELEASER_CONFIG) --snapshot --clean --single-target)

# Build all targets (cross-compilation)
build-all:
	docker run --rm \
		-v $(CURDIR):/work \
		-w /work \
		$(GORELEASER_IMAGE) build --config $(GORELEASER_CONFIG) --snapshot --clean

# Build x86_64 only (for integration tests - VM is x86_64)
build-x86:
	$(call goreleaser,linux,amd64,build --config $(GORELEASER_CONFIG) --snapshot --clean --single-target)

# Build packages locally (full release without publishing)
pkg:
	docker run --rm \
		-v $(CURDIR):/work \
		-w /work \
		$(GORELEASER_IMAGE) release --config $(GORELEASER_CONFIG) --snapshot --clean --skip=publish

clean-build:
	go clean
	rm -rf build/dist/

clean-tests:
	rm -rf tests/images
	rm -f $(TEST_BINARY)

clean: clean-build clean-tests

# Run unit tests
test-unit:
	go test ./...

# Build VM image for integration tests
test-integration-image: tests/images/fedora-stratis.qcow2

tests/images/fedora-stratis.qcow2:
	cd tests/packer && $(PACKER) init fedora-stratis.pkr.hcl
	cd tests/packer && $(PACKER) build fedora-stratis.pkr.hcl

# Run integration tests (VM is x86_64 only)
test-integration: build-x86 test-integration-image
	@go test -c -tags=integration -o $(TEST_BINARY) ./tests/integration
	@VM_IMAGE=$(TEST_IMAGE) PLUGIN_BINARY=$(PLUGIN_BINARY) $(TEST_BINARY) -test.v
