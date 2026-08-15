.PHONY: all setup clean lint test cover build deploy version version/major version/minor version/patch help

PROJECT_NAME := prometheus_exporters
VERSION_FILE := VERSION
VERSION := $(shell cat $(VERSION_FILE))

EXPORTERS := network_exporter ipsec_exporter cloudflare_exporter libvirt_exporter
REGISTRY := ghcr.io/phaseshiftdata

COMMIT_SHA := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BRANCH := $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "unknown")
GIT_TAG := $(shell git describe --tags --exact-match 2>/dev/null || echo "")

SHELL := /bin/bash

# Determine image tag based on context
ifdef GIT_TAG
  IMAGE_TAG := $(GIT_TAG)
else ifeq ($(BRANCH),main)
  IMAGE_TAG := main
else
  IMAGE_TAG := $(COMMIT_SHA)
endif

# Default target
all: build

# ============================================================================
# Setup - install dependencies
# ============================================================================
setup:
	@echo "Installing dependencies..."
	@cd site && npm install
	@echo "Setup complete."

# ============================================================================
# Clean - remove build artifacts
# ============================================================================
clean:
	@echo "Cleaning..."
	rm -rf build/ bin/ dist/ coverage/
	rm -rf site/dist/ site/node_modules/
	@echo "Clean complete."

# ============================================================================
# Lint - run all linters
# ============================================================================
lint:
	@echo "Running Go vet..."
	go vet ./...
	@echo "Running staticcheck..."
	@which staticcheck >/dev/null 2>&1 && staticcheck ./... || echo "  staticcheck not installed, skipping"
	@echo "Lint passed."

# ============================================================================
# Test - run all tests (unit, integration, e2e)
# ============================================================================
test:
	@echo "Running tests..."
	go test -v -race -coverprofile=coverage.out -covermode=atomic ./...
	@echo "Coverage summary:"
	@go tool cover -func=coverage.out | tail -1
	@echo "All tests passed."

# ============================================================================
# Cover - run test coverage and gate on 98% threshold
# ============================================================================
COVERAGE_THRESHOLD := 98.0

cover:
	@echo "Running test coverage..."
	@go test -coverprofile=coverage.out -covermode=atomic ./...
	@COVERAGE=$$(go tool cover -func=coverage.out | grep '^total:' | awk '{print $$3}' | sed 's/%//'); \
	echo "Total coverage: $${COVERAGE}%"; \
	if [ $$(echo "$${COVERAGE} < $(COVERAGE_THRESHOLD)" | bc -l) -eq 1 ]; then \
		echo "FAIL: coverage $${COVERAGE}% is below $(COVERAGE_THRESHOLD)% threshold"; \
		exit 1; \
	else \
		echo "PASS: coverage $${COVERAGE}% meets $(COVERAGE_THRESHOLD)% threshold"; \
	fi

# ============================================================================
# Build - build container images
# ============================================================================
build:
	@echo "Building $(PROJECT_NAME) v$(VERSION) (tag: $(IMAGE_TAG))..."
	@for exp in $(EXPORTERS); do \
		if [ -f "cmd/$$exp/Dockerfile" ]; then \
			echo "  Building $(REGISTRY)/$$exp:$(IMAGE_TAG)..."; \
			docker build \
				-t $(REGISTRY)/$$exp:$(IMAGE_TAG) \
				-f cmd/$$exp/Dockerfile \
				--build-arg VERSION=$(VERSION) \
				--build-arg COMMIT=$(COMMIT_SHA) \
				.; \
		else \
			echo "  Skipping $$exp (no Dockerfile)"; \
		fi; \
	done
	@echo "Build complete."

# ============================================================================
# Deploy - push container images to GHCR
# ============================================================================
deploy:
	@echo "Pushing images to $(REGISTRY) (tag: $(IMAGE_TAG))..."
	@for exp in $(EXPORTERS); do \
		if docker image inspect $(REGISTRY)/$$exp:$(IMAGE_TAG) >/dev/null 2>&1; then \
			echo "  Pushing $(REGISTRY)/$$exp:$(IMAGE_TAG)..."; \
			docker push $(REGISTRY)/$$exp:$(IMAGE_TAG); \
		else \
			echo "  Skipping $$exp (image not found)"; \
		fi; \
	done
	@echo "Deploy complete."

# ============================================================================
# Version - semantic version tagging
# ============================================================================
LATEST_TAG := $(shell git tag -l 'v*' --sort=-version:refname | head -1)

version:
ifeq ($(LATEST_TAG),)
	@echo "No semver tags found. Tagging v0.0.0..."
	@git tag -a v0.0.0 -m "v0.0.0"
	@echo "Tagged v0.0.0"
else
	@$(MAKE) --no-print-directory version/patch
endif

version/patch:
ifeq ($(LATEST_TAG),)
	@echo "Error: no existing semver tag found. Run 'make version' first." >&2; exit 1
else
	@CURRENT="$(LATEST_TAG)"; \
	CURRENT=$${CURRENT#v}; \
	IFS='.' read -r MAJOR MINOR PATCH <<< "$$CURRENT"; \
	PATCH=$$((PATCH + 1)); \
	NEW="v$$MAJOR.$$MINOR.$$PATCH"; \
	git tag -a "$$NEW" -m "$$NEW"; \
	echo "Tagged $$NEW (was $(LATEST_TAG))"
endif

version/minor:
ifeq ($(LATEST_TAG),)
	@echo "Error: no existing semver tag found. Run 'make version' first." >&2; exit 1
else
	@CURRENT="$(LATEST_TAG)"; \
	CURRENT=$${CURRENT#v}; \
	IFS='.' read -r MAJOR MINOR PATCH <<< "$$CURRENT"; \
	MINOR=$$((MINOR + 1)); \
	PATCH=0; \
	NEW="v$$MAJOR.$$MINOR.$$PATCH"; \
	git tag -a "$$NEW" -m "$$NEW"; \
	echo "Tagged $$NEW (was $(LATEST_TAG))"
endif

version/major:
ifeq ($(LATEST_TAG),)
	@echo "Error: no existing semver tag found. Run 'make version' first." >&2; exit 1
else
	@CURRENT="$(LATEST_TAG)"; \
	CURRENT=$${CURRENT#v}; \
	IFS='.' read -r MAJOR MINOR PATCH <<< "$$CURRENT"; \
	MAJOR=$$((MAJOR + 1)); \
	MINOR=0; \
	PATCH=0; \
	NEW="v$$MAJOR.$$MINOR.$$PATCH"; \
	git tag -a "$$NEW" -m "$$NEW"; \
	echo "Tagged $$NEW (was $(LATEST_TAG))"
endif

# ============================================================================
# Help
# ============================================================================
help:
	@echo "$(PROJECT_NAME) Makefile"
	@echo ""
	@echo "Targets:"
	@echo "  all (default)    Build container images"
	@echo "  setup            Install dependencies"
	@echo "  clean            Delete build artifacts"
	@echo "  lint             Run all linters (go vet, staticcheck, golangci-lint)"
	@echo "  test             Run all tests (unit, integration, e2e)"
	@echo "  build            Build container images tagged with policy"
	@echo "  deploy           Push container images to GHCR"
	@echo "  cover            Run test coverage and gate on 98%% threshold"
	@echo "  version          Tag v0.0.0 if no semver tags exist, else bump patch"
	@echo "  version/major    Bump major version and tag"
	@echo "  version/minor    Bump minor version and tag"
	@echo "  version/patch    Bump patch version and tag"
	@echo "  help             Show this help"
	@echo ""
	@echo "Image Tag Policy:"
	@echo "  Feature branch/PR:  :<commit-sha>"
	@echo "  Main branch:        :main"
	@echo "  Git tag:            :<git-tag>"
	@echo ""
	@echo "Current tag: $(IMAGE_TAG)"
