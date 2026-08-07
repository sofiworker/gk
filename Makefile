GO ?= go
PKGS ?= ./...
TESTFLAGS ?=
VETFLAGS ?=
BENCHFLAGS ?= -bench=. -benchmem
GOLANGCI_LINT ?= golangci-lint
GOFMT ?= gofmt

.DEFAULT_GOAL := help

.PHONY: help fmt fmt-check vet test test-race test-cover bench lint tidy check ci all webbench webbench-sanity

help:
	@echo "Available targets:"
	@echo "  make fmt         Format Go code with go fmt"
	@echo "  make fmt-check   Check formatting (requires Go 1.27+ gofmt for ghttp/1.27 files)"
	@echo "  make vet         Run go vet"
	@echo "  make test        Run unit tests"
	@echo "  make test-race   Run unit tests with the race detector"
	@echo "  make test-cover  Run unit tests and write coverage.out"
	@echo "  make bench       Run benchmarks"
	@echo "  make webbench        Run ghttp-vs-frameworks web benchmarks (benchmarks/)"
	@echo "  make webbench-sanity Verify all web benchmark adapters answer correctly"
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

bench:
	$(GO) test $(BENCHFLAGS) $(PKGS)

WEBBENCH_FLAGS ?= -bench=. -benchmem -count=1

webbench:
	cd benchmarks && $(GO) test -run xxx $(WEBBENCH_FLAGS)

webbench-sanity:
	cd benchmarks && $(GO) test -run TestScenarioSanity -v

lint:
	$(GOLANGCI_LINT) run $(PKGS)

tidy:
	$(GO) mod tidy

check: fmt vet test

ci: vet test

all: check
