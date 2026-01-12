.DEFAULT_GOAL = compile
.PHONY: compile clean clean-build clean-tests test-unit test-integration-image test-integration pkg-rhel

PREFIX ?= /usr
LIBEXECDIR ?= $(PREFIX)/libexec
PACKER ?= packer
NFPM_IMAGE = goreleaser/nfpm:v2.44.1
TEST_IMAGE = $(CURDIR)/tests/images/fedora-stratis.qcow2
TEST_BINARY = $(CURDIR)/tests/integration.test
PLUGIN_BINARY = $(CURDIR)/build/dist/podman-volume-stratis

# Version detection
VERSION ?= $(shell ./build/scripts/version.sh)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

# ldflags for version injection
VERSION_PKG = github.com/kriansa/podman-volume-stratis/internal/version
LDFLAGS = -s -w \
	-X $(VERSION_PKG).Version=$(VERSION) \
	-X $(VERSION_PKG).Commit=$(COMMIT) \
	-X $(VERSION_PKG).BuildTime=$(BUILD_TIME)

build/dist:
	mkdir -p build/dist

build/dist/podman-volume-stratis-$(VERSION): build/dist
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o build/dist/podman-volume-stratis-$(VERSION) ./cmd/podman-volume-stratis

compile: build/dist/podman-volume-stratis-$(VERSION)

# Build RHEL package (RPM)
pkg-rhel: compile
	docker run --rm \
		-v $(CURDIR):/work \
		-w /work \
		-e VERSION=$(VERSION) \
		-e ARCH=$(shell go env GOARCH) \
		$(NFPM_IMAGE) package \
		--config build/nfpm/RHEL.yml \
		--packager rpm \
		--target build/dist/

clean-build:
	go clean
	rm -rf build/dist

clean-tests: clean-build
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

# Run integration tests (compiles and runs directly for streaming output)
test-integration: compile test-integration-image
	@go test -c -tags=integration -o $(TEST_BINARY) ./tests/integration
	@VM_IMAGE=$(TEST_IMAGE) PLUGIN_BINARY=$(PLUGIN_BINARY) $(TEST_BINARY) -test.v
