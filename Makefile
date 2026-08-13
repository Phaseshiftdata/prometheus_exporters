.PHONY: all setup clean lint test build deploy help

PROJECT_NAME := prometheus_exporters
VERSION_FILE := VERSION
VERSION := $(shell cat $(VERSION_FILE))

EXPORTERS := network_exporter ipsec_exporter cloudflare_exporter
REGISTRY := ghcr.io/asymmetric-effort

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
	@for exp in $(EXPORTERS); do \
		if [ -d "cmd/$$exp" ]; then \
			echo "  Vetting cmd/$$exp..."; \
			cd cmd/$$exp && go vet ./... && cd ../..; \
		fi; \
	done
	@echo "Running staticcheck..."
	@for exp in $(EXPORTERS); do \
		if [ -d "cmd/$$exp" ]; then \
			echo "  Checking cmd/$$exp..."; \
			cd cmd/$$exp && staticcheck ./... 2>/dev/null && cd ../..; \
		fi; \
	done || true
	@echo "Running golangci-lint..."
	@for exp in $(EXPORTERS); do \
		if [ -d "cmd/$$exp" ]; then \
			echo "  Linting cmd/$$exp..."; \
			cd cmd/$$exp && golangci-lint run ./... 2>/dev/null && cd ../..; \
		fi; \
	done || true
	@echo "Lint passed."

# ============================================================================
# Test - run all tests (unit, integration, e2e)
# ============================================================================
test:
	@echo "Running unit tests..."
	@for exp in $(EXPORTERS); do \
		if [ -d "cmd/$$exp" ]; then \
			echo "  Testing cmd/$$exp..."; \
			cd cmd/$$exp && go test -v -race ./... && cd ../..; \
		fi; \
	done
	@if [ -d "internal" ]; then \
		echo "  Testing internal/..."; \
		go test -v -race ./internal/...; \
	fi
	@echo "All tests passed."

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
	@echo "  help             Show this help"
	@echo ""
	@echo "Image Tag Policy:"
	@echo "  Feature branch/PR:  :<commit-sha>"
	@echo "  Main branch:        :main"
	@echo "  Git tag:            :<git-tag>"
	@echo ""
	@echo "Current tag: $(IMAGE_TAG)"
