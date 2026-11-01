.DEFAULT_GOAL = build
.PHONY: build build-all build-x86 pkg clean clean-build clean-tests test-unit test-integration-image test-integration toggle-prerelease

PACKER ?= packer
GORELEASER_IMAGE = docker.io/goreleaser/goreleaser:v2.13.3
GORELEASER_CONFIG = build/goreleaser.yml
RELEASE_PLEASE_CONFIG = build/release-please-config.json
TEST_IMAGE = $(CURDIR)/tests/images/fedora-stratis.qcow2
TEST_BINARY = $(CURDIR)/tests/integration.test
TEST_PKG_BINARY = $(CURDIR)/tests/packaging.test
# The last release whose removal script undid everything even during an upgrade, to test upgrading
# from it. Checked against its sha256, from the release's checksums.txt.
OLD_RPM = $(CURDIR)/build/old-release/podman-volume-stratis_1.1.4_linux_amd64.rpm
OLD_RPM_SHA256 = e32e29f3eab18b07b7e11cc41e5b926ea3763c8068564f1b45825f24a50cb1e3
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

# Build the RPMs only, which is what the integration tests install, without the archives pkg makes
# too. goreleaser cannot build a release for one architecture only.
pkg-rpm:
	docker run --rm \
		-v $(CURDIR):/work \
		-w /work \
		$(GORELEASER_IMAGE) release --config $(GORELEASER_CONFIG) --snapshot --clean --skip=publish,archive

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
	rm -f $(TEST_BINARY) $(TEST_PKG_BINARY)

clean: clean-build clean-tests

# Run unit tests. With the race detector, because several of them check what runs side by side.
test-unit:
	go test -race ./...

# The image is rebuilt whenever what it is built from changes, so one built from older packer files
# is never used. That is judged by content rather than file times, which change without the
# content: CI restores a cached image older than its fresh checkout. The stamp is a file named
# after a hash of the packer files, written next to the image once it is built.
PACKER_FILES = $(sort $(wildcard tests/packer/*.pkr.hcl tests/packer/scripts/* tests/packer/http/*))
IMAGE_STAMP = tests/images/.built-from-$(shell sha256sum /dev/null $(PACKER_FILES) | sha256sum | cut -c1-16)

# Build VM image for integration tests
test-integration-image: $(IMAGE_STAMP)

# packer refuses to write into an existing output directory, hence the rm
$(IMAGE_STAMP):
	rm -rf tests/images
	cd tests/packer && $(PACKER) init fedora-stratis.pkr.hcl
	cd tests/packer && $(PACKER) build fedora-stratis.pkr.hcl
	touch $@

# Run integration tests (VM is x86_64 only): the plugin's own, then the RPM's install, upgrade and
# removal scripts. pkg-rpm builds the plugin binary too. Both suites always run, so a failure in the
# first does not hide the second's results; the target fails at the end if either did. The package
# tests run from their own directory, where their script tests find the scripts.
test-integration: pkg-rpm test-integration-image $(OLD_RPM)
	@go test -c -tags=integration -o $(TEST_BINARY) ./tests/integration
	@go test -c -tags=integration -o $(TEST_PKG_BINARY) ./tests/packaging
	@rc=0; \
	VM_IMAGE=$(TEST_IMAGE) PLUGIN_BINARY=$(PLUGIN_BINARY) $(TEST_BINARY) -test.v -test.timeout 30m || rc=1; \
	cd tests/packaging && VM_IMAGE=$(TEST_IMAGE) PLUGIN_RPM=$$(ls $(CURDIR)/build/dist/*_linux_amd64.rpm) \
		PLUGIN_RPM_OLD=$(OLD_RPM) $(TEST_PKG_BINARY) -test.v -test.timeout 15m || rc=1; \
	exit $$rc

# Downloaded next to its final name and checked before it is moved there, so an interrupted or
# corrupted download is never taken for the real file.
$(OLD_RPM):
	mkdir -p $(@D)
	curl -fsSL -o $@.tmp https://github.com/kriansa/podman-volume-stratis/releases/download/v1.1.4/$(@F)
	echo "$(OLD_RPM_SHA256)  $@.tmp" | sha256sum --check --quiet
	mv $@.tmp $@

# Toggle pre-release mode in release-please config.
#
# When disabling pre-release, the manifest version is reverted to the last
# stable git tag. This is necessary because release-please doesn't natively
# support transitioning from prerelease to stable — it treats the manifest
# version as "already released" and bumps past it. Simply stripping the
# prerelease suffix (e.g. 1.1.0-beta.1 -> 1.1.0) would cause it to skip
# that version entirely. See:
#   https://github.com/googleapis/release-please/issues/2515
#   https://github.com/googleapis/release-please/issues/2447
define TOGGLE_PRERELEASE_SCRIPT
import json, pathlib, subprocess
config_path = "$(RELEASE_PLEASE_CONFIG)"
manifest_path = "build/release-please-manifest.json"
p = pathlib.Path(config_path)
c = json.loads(p.read_text())
pkg = c["packages"]["."]
pkg["prerelease"] = not pkg.get("prerelease", False)
pkg["versioning"] = "prerelease" if pkg["prerelease"] else "default"
p.write_text(json.dumps(c, indent=2) + "\n")
if not pkg["prerelease"]:
    tags = subprocess.check_output(["git", "tag", "--sort=-v:refname"], text=True).splitlines()
    stable = next((t.lstrip("v") for t in tags if "v" in t and "-" not in t.lstrip("v")), None)
    if stable:
        m = pathlib.Path(manifest_path)
        d = json.loads(m.read_text())
        d["."] = stable
        m.write_text(json.dumps(d, indent=2) + "\n")
print("Pre-release", "enabled" if pkg["prerelease"] else "disabled")
endef
export TOGGLE_PRERELEASE_SCRIPT

toggle-prerelease:
	@python3 -c "$$TOGGLE_PRERELEASE_SCRIPT"
