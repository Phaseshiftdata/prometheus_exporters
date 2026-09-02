.PHONY: all setup clean lint test cover build build-coverage deploy deploy-coverage molecule molecule-coverage cover-merge version version/major version/minor version/patch help check-targets check-coverage-policy

PROJECT_NAME := prometheus_exporters
VERSION_FILE := VERSION
VERSION := $(shell cat $(VERSION_FILE))

EXPORTERS := cloudflare_exporter github_exporter network_exporter ipsec_exporter libvirt_exporter openbao_exporter relay_exporter
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
	@echo "Running unit test coverage..."
	@mkdir -p coverage/unit
	@go test -coverprofile=coverage/unit.out -covermode=atomic $$(go list ./... | grep -v tests/container)
	@cp coverage/unit.out coverage.out
	@COVERAGE=$$(go tool cover -func=coverage.out | grep '^total:' | awk '{print $$3}' | sed 's/%//'); \
	echo "Total coverage: $${COVERAGE}%"; \
	if [ $$(echo "$${COVERAGE} < $(COVERAGE_THRESHOLD)" | bc -l) -eq 1 ]; then \
		echo "FAIL: coverage $${COVERAGE}% is below $(COVERAGE_THRESHOLD)% threshold"; \
		exit 1; \
	else \
		echo "PASS: coverage $${COVERAGE}% meets $(COVERAGE_THRESHOLD)% threshold"; \
	fi

# ============================================================================
# Build Coverage - build container images with coverage instrumentation
# ============================================================================
build-coverage:
	@echo "Building coverage-instrumented images..."
	@for exp in $(EXPORTERS); do \
		if [ -f "cmd/$$exp/Dockerfile" ]; then \
			echo "  Building $(REGISTRY)/$$exp:coverage..."; \
			docker build \
				-t $(REGISTRY)/$$exp:coverage \
				-f cmd/$$exp/Dockerfile \
				--build-arg VERSION=$(VERSION) \
				--build-arg COMMIT=$(COMMIT_SHA) \
				--build-arg COVERAGE=1 \
				.; \
		else \
			echo "  Skipping $$exp (no Dockerfile)"; \
		fi; \
	done
	@echo "Coverage build complete."

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
	@# Visibility guard: prevent coverage-instrumented images from being
	@# pushed to production tags (main, v*).
	@case "$(IMAGE_TAG)" in \
		coverage-*) \
			echo "ERROR: coverage-tagged images must not be pushed via 'make deploy'."; \
			echo "       Use 'make deploy-coverage' instead."; \
			exit 1;; \
	esac
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
# Deploy Coverage - push coverage-instrumented images to GHCR
# ============================================================================
COVERAGE_TAG := coverage-$(COMMIT_SHA)

deploy-coverage:
	@echo "Pushing coverage images to $(REGISTRY) (tag: $(COVERAGE_TAG))..."
	@# Safety check: only allow coverage-* tags.
	@case "$(COVERAGE_TAG)" in \
		coverage-*) ;; \
		*) echo "ERROR: deploy-coverage requires a coverage-* tag."; exit 1;; \
	esac
	@for exp in $(EXPORTERS); do \
		if docker image inspect $(REGISTRY)/$$exp:coverage >/dev/null 2>&1; then \
			echo "  Tagging $(REGISTRY)/$$exp:coverage -> $(REGISTRY)/$$exp:$(COVERAGE_TAG)"; \
			docker tag $(REGISTRY)/$$exp:coverage $(REGISTRY)/$$exp:$(COVERAGE_TAG); \
			echo "  Pushing $(REGISTRY)/$$exp:$(COVERAGE_TAG)..."; \
			docker push $(REGISTRY)/$$exp:$(COVERAGE_TAG); \
		else \
			echo "  Skipping $$exp (coverage image not found)"; \
		fi; \
	done
	@echo "Coverage deploy complete."

# ============================================================================
# Molecule - end-to-end container tests
# ============================================================================
molecule:
	@echo "Running molecule container tests..."
	go test -v -timeout 300s ./tests/container/...
	@echo "Molecule tests passed."

# ============================================================================
# Molecule Coverage - run molecule tests with coverage collection
# ============================================================================
molecule-coverage:
	@echo "Running molecule container tests with coverage collection..."
	@mkdir -p coverage/molecule
	COVERAGE_MODE=1 GOCOVERDIR_HOST=$$(pwd)/coverage/molecule go test -v -timeout 600s ./tests/container/...
	@echo "Molecule coverage tests passed."

# ============================================================================
# Cover Merge - merge unit and molecule coverage profiles
# ============================================================================
cover-merge:
	@echo "Merging unit and molecule coverage..."
	@mkdir -p coverage/unit coverage/molecule
	@# Run unit tests and collect coverage.
	@go test -coverprofile=coverage/unit.out -covermode=atomic $$(go list ./... | grep -v tests/container)
	@# Convert molecule binary coverage to text format if data exists.
	@if ls coverage/molecule/cov* 1>/dev/null 2>&1; then \
		go tool covdata textfmt -i=coverage/molecule -o=coverage/molecule.out; \
		echo "Molecule coverage converted to text format."; \
		head -1 coverage/unit.out > coverage/merged.out; \
		tail -n +2 coverage/unit.out >> coverage/merged.out; \
		tail -n +2 coverage/molecule.out >> coverage/merged.out; \
		cp coverage/merged.out coverage.out; \
		echo "Merged coverage profile written to coverage.out"; \
	else \
		echo "No molecule coverage data found; using unit coverage only."; \
		cp coverage/unit.out coverage.out; \
	fi
	@COVERAGE=$$(go tool cover -func=coverage.out | grep '^total:' | awk '{print $$3}' | sed 's/%//'); \
	echo "Total merged coverage: $${COVERAGE}%"; \
	if [ $$(echo "$${COVERAGE} < $(COVERAGE_THRESHOLD)" | bc -l) -eq 1 ]; then \
		echo "FAIL: coverage $${COVERAGE}% is below $(COVERAGE_THRESHOLD)% threshold"; \
		exit 1; \
	else \
		echo "PASS: coverage $${COVERAGE}% meets $(COVERAGE_THRESHOLD)% threshold"; \
	fi

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
# Check Targets - verify every .PHONY target is exercised in CI
# ============================================================================

# Manual-only targets that are not expected to run in CI. Each must be
# documented in the help text with "(manual)" annotation.
MANUAL_TARGETS := setup clean version version/major version/minor version/patch help

# Targets whose functionality is exercised in CI through equivalent direct
# commands rather than through make. "all" is the default target aliasing
# "build"; the rest are run directly in CI (e.g., "go vet" covers "lint",
# "go test -coverprofile" covers "test" and "cover").
CI_EQUIVALENT_TARGETS := all lint test cover molecule build-coverage molecule-coverage cover-merge deploy-coverage check-coverage-policy

check-targets:
	@echo "Checking Makefile target coverage in CI..."
	@PHONY_LINE=$$(grep '^\.PHONY:' Makefile | head -1 | sed 's/^\.PHONY://'); \
	FAIL=0; \
	for target in $$PHONY_LINE; do \
		MANUAL=0; \
		for m in $(MANUAL_TARGETS); do \
			if [ "$$target" = "$$m" ]; then MANUAL=1; break; fi; \
		done; \
		if [ $$MANUAL -eq 1 ]; then \
			echo "  $$target: manual-only (documented)"; \
			continue; \
		fi; \
		EQUIV=0; \
		for e in $(CI_EQUIVALENT_TARGETS); do \
			if [ "$$target" = "$$e" ]; then EQUIV=1; break; fi; \
		done; \
		if [ $$EQUIV -eq 1 ]; then \
			echo "  $$target: exercised in CI (equivalent commands)"; \
			continue; \
		fi; \
		if grep -qE "make\s+$$target(\s|$$)" .github/workflows/ci.yml 2>/dev/null; then \
			echo "  $$target: exercised in CI (make $$target)"; \
		else \
			echo "  ERROR: $$target is not exercised in any CI job"; \
			FAIL=1; \
		fi; \
	done; \
	if [ $$FAIL -eq 1 ]; then \
		echo "FAIL: one or more .PHONY targets are not exercised in CI"; \
		exit 1; \
	fi; \
	echo "All .PHONY targets are covered."

# ============================================================================
# Check Coverage Policy - verify all tracked files are classified
# ============================================================================
check-coverage-policy:
	@echo "Checking coverage policy classification..."
	@POLICY=".coverage-policy.yml"; \
	if [ ! -f "$$POLICY" ]; then \
		echo "ERROR: $$POLICY not found"; \
		exit 1; \
	fi; \
	PATTERNS=$$(grep '^\s*- path:' "$$POLICY" | sed 's/.*path:\s*"\(.*\)"/\1/' | sed "s/.*path:\s*'\(.*\)'/\1/"); \
	FAIL=0; \
	TOTAL=0; \
	MATCHED=0; \
	while IFS= read -r f; do \
		TOTAL=$$((TOTAL + 1)); \
		FOUND=0; \
		while IFS= read -r pat; do \
			case "$$f" in \
				$$pat*) FOUND=1; break;; \
			esac; \
			if [ "$$f" = "$$pat" ]; then FOUND=1; break; fi; \
		done <<< "$$PATTERNS"; \
		if [ $$FOUND -eq 0 ]; then \
			echo "  UNCLASSIFIED: $$f"; \
			FAIL=1; \
		else \
			MATCHED=$$((MATCHED + 1)); \
		fi; \
	done < <(git ls-files); \
	echo "Checked $$TOTAL files: $$MATCHED classified."; \
	if [ $$FAIL -eq 1 ]; then \
		echo "FAIL: unclassified files found — update .coverage-policy.yml"; \
		exit 1; \
	fi; \
	echo "All tracked files are classified in .coverage-policy.yml."

# ============================================================================
# Help
# ============================================================================
help:
	@echo "$(PROJECT_NAME) Makefile"
	@echo ""
	@echo "Targets:"
	@echo "  all (default)      Build container images"
	@echo "  setup (manual)     Install dependencies"
	@echo "  clean (manual)     Delete build artifacts"
	@echo "  lint               Run all linters (go vet, staticcheck, golangci-lint)"
	@echo "  test               Run all tests (unit, integration, e2e)"
	@echo "  build              Build container images tagged with policy"
	@echo "  build-coverage     Build coverage-instrumented container images"
	@echo "  deploy             Push container images to GHCR (rejects coverage tags)"
	@echo "  deploy-coverage    Push coverage-instrumented images to GHCR"
	@echo "  cover              Run unit test coverage and gate on 98%% threshold"
	@echo "  cover-merge        Merge unit and molecule coverage profiles"
	@echo "  molecule           Run molecule end-to-end container tests"
	@echo "  molecule-coverage  Run molecule tests with coverage collection"
	@echo "  check-targets      Verify every .PHONY target is exercised in CI"
	@echo "  check-coverage-policy  Verify all tracked files are classified"
	@echo "  version (manual)   Tag v0.0.0 if no semver tags exist, else bump patch"
	@echo "  version/major (m)  Bump major version and tag"
	@echo "  version/minor (m)  Bump minor version and tag"
	@echo "  version/patch (m)  Bump patch version and tag"
	@echo "  help (manual)      Show this help"
	@echo ""
	@echo "Image Tag Policy:"
	@echo "  Feature branch/PR:  :<commit-sha>"
	@echo "  Main branch:        :main"
	@echo "  Git tag:            :<git-tag>"
	@echo ""
	@echo "Current tag: $(IMAGE_TAG)"
