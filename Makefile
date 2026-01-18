SHELL := /bin/bash

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
EXECUTABLE := flow2apex
WINDOWS := $(EXECUTABLE)_windows_amd64.exe
LINUX := $(EXECUTABLE)_linux_amd64
LINUX_ARM64 := $(EXECUTABLE)_linux_arm64
DARWIN_AMD64 := $(EXECUTABLE)_darwin_amd64
DARWIN_ARM64 := $(EXECUTABLE)_darwin_arm64
ALL := $(WINDOWS) $(LINUX) $(LINUX_ARM64) $(DARWIN_AMD64) $(DARWIN_ARM64)
VERSIONED_ZIPS := $(addsuffix _$(VERSION).zip,$(basename $(ALL)))
RELEASE_ASSETS := $(VERSIONED_ZIPS) SHA256SUMS-$(VERSION)

GO_BUILD_FLAGS := -trimpath
GO_LDFLAGS := -X main.version=$(VERSION)

.PHONY: default install install-debug dist clean checksum release tag

default:
	go build $(GO_BUILD_FLAGS) -ldflags "$(GO_LDFLAGS)"

install:
	go install $(GO_BUILD_FLAGS) -ldflags "$(GO_LDFLAGS)"

install-debug:
	go install $(GO_BUILD_FLAGS) -ldflags "$(GO_LDFLAGS)" -gcflags="all=-N -l"

$(WINDOWS): go.mod
	env CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(GO_BUILD_FLAGS) -ldflags "$(GO_LDFLAGS)" -o $@

$(LINUX): go.mod
	env CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GO_BUILD_FLAGS) -ldflags "$(GO_LDFLAGS)" -o $@

$(LINUX_ARM64): go.mod
	env CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(GO_BUILD_FLAGS) -ldflags "$(GO_LDFLAGS)" -o $@

$(DARWIN_AMD64): go.mod
	env CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build $(GO_BUILD_FLAGS) -ldflags "$(GO_LDFLAGS)" -o $@
	rcodesign sign --for-notarization --pem-file <(pass OctoberSwimmer/aer/codesign/combined) $@

$(DARWIN_ARM64): go.mod
	env CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build $(GO_BUILD_FLAGS) -ldflags "$(GO_LDFLAGS)" -o $@
	rcodesign sign --for-notarization --pem-file <(pass OctoberSwimmer/aer/codesign/combined) $@

$(basename $(WINDOWS))_$(VERSION).zip: $(WINDOWS)
	@rm -f $@
	zip $@ $<
	7za rn $@ $< $(EXECUTABLE).exe

$(basename $(DARWIN_AMD64))_$(VERSION).zip: $(DARWIN_AMD64)
	@rm -f $@
	zip $@ $<
	7za rn $@ $< $(EXECUTABLE)
	rcodesign notary-submit --api-key-file <(pass OctoberSwimmer/aer/codesign/api-key) $@

$(basename $(DARWIN_ARM64))_$(VERSION).zip: $(DARWIN_ARM64)
	@rm -f $@
	zip $@ $<
	7za rn $@ $< $(EXECUTABLE)
	rcodesign notary-submit --api-key-file <(pass OctoberSwimmer/aer/codesign/api-key) $@

%_$(VERSION).zip: %
	@rm -f $@
	zip $@ $<
	7za rn $@ $< $(EXECUTABLE)

dist: $(VERSIONED_ZIPS)

checksum: dist
	shasum -a 256 $(VERSIONED_ZIPS) > SHA256SUMS-$(VERSION)

release: checksum
	@if ! command -v gh >/dev/null 2>&1; then \
		echo "gh CLI is required for 'make release'."; \
		exit 1; \
	fi
	@if [ "$(VERSION)" = "dev" ] || printf "%s" "$(VERSION)" | grep -q "dirty"; then \
		echo "VERSION '$(VERSION)' is not a clean tag. Tag the commit or invoke 'make release VERSION=vX.Y.Z' with a published tag."; \
		exit 1; \
	fi
	@if ! git rev-parse --verify "refs/tags/$(VERSION)" >/dev/null 2>&1; then \
		echo "Tag '$(VERSION)' does not exist. Create the tag before running 'make release'."; \
		exit 1; \
	fi
	git push octoberswimmer "$(VERSION)"
	gh release create "$(VERSION)" --title "flow2apex $(VERSION)" --notes-from-tag --verify-tag $(RELEASE_ASSETS)

tag:
	@set -euo pipefail; \
	if ! git diff --quiet || ! git diff --cached --quiet; then \
		echo "Working tree must be clean before running 'make tag'."; \
		exit 1; \
	fi; \
	latest_tag=$$(git tag --list 'v*' --sort=v:refname | tail -n1 || true); \
	if [ -z "$$latest_tag" ]; then \
		next_tag="v0.1.0"; \
	else \
		if [[ $$latest_tag =~ ^v([0-9]+)\.([0-9]+)\.([0-9]+)$$ ]]; then \
			major=$${BASH_REMATCH[1]}; \
			minor=$${BASH_REMATCH[2]}; \
			patch=$${BASH_REMATCH[3]}; \
			next_tag="v$${major}.$$((minor + 1)).0"; \
		else \
			echo "Latest tag '$$latest_tag' is not a vX.Y.Z semver tag."; \
			exit 1; \
		fi; \
	fi; \
	if git rev-parse --verify --quiet "refs/tags/$$next_tag" >/dev/null; then \
		echo "Tag $$next_tag already exists locally."; \
		exit 1; \
	fi; \
	echo "Tagging $$next_tag"; \
	git tag -s "$$next_tag"

clean:
	-rm -f $(EXECUTABLE) $(EXECUTABLE).exe $(EXECUTABLE)_* *.zip SHA256SUMS-*
