GO ?= go
PKGS ?= ./...
TESTFLAGS ?=
VETFLAGS ?=
BENCHFLAGS ?= -bench=. -benchmem
GOLANGCI_LINT ?= golangci-lint
GOFMT ?= gofmt

.DEFAULT_GOAL := help

.PHONY: help fmt fmt-check vet test test-race test-cover lint tidy check ci all

help:
	@echo "Available targets:"
	@echo "  make fmt         Format Go code with go fmt"
	@echo "  make fmt-check   Check formatting (requires Go 1.27+ gofmt for ghttp/*.go with //go:build go1.27)"
	@echo "  make vet         Run go vet"
	@echo "  make test        Run unit tests"
	@echo "  make test-race   Run unit tests with the race detector"
	@echo "  make test-cover  Run unit tests and write coverage.out"
	@echo "  make lint        Run golangci-lint"
	@echo "  make tidy        Run go mod tidy"
	@echo "  make check       Run fmt, vet, and test"
	@echo "  make ci          Run vet and test without modifying files"

fmt:
	$(GO) fmt $(PKGS)

fmt-check:
	@test -z "$$($(GOFMT) -l .)" || (echo "unformatted files:"; $(GOFMT) -l .; exit 1)

vet:
	$(GO) vet $(VETFLAGS) $(PKGS)

test:
	$(GO) test $(TESTFLAGS) $(PKGS)

test-race:
	$(GO) test -race $(TESTFLAGS) $(PKGS)

test-cover:
	$(GO) test -coverprofile=coverage.out $(TESTFLAGS) $(PKGS)
	$(GO) tool cover -func=coverage.out

lint:
	$(GOLANGCI_LINT) run $(PKGS)

tidy:
	$(GO) mod tidy

check: fmt vet test

ci: vet test

all: check
