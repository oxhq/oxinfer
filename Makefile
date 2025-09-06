SHELL := /bin/bash

.PHONY: help build test fmt vet run perf-validate perf-report perf-clean cache-clean clean

## help: Show this help
help:
	@echo "Useful commands:";
	@awk 'BEGIN {FS=":.*##"} /^[a-zA-Z0-9_\-]+:.*##/ {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST) | sort

## build: Build the oxinfer CLI
build:
	@echo "Building oxinfer..."
	GOEXPERIMENT=jsonv2 go build -o oxinfer ./cmd/oxinfer

## test: Run the full test suite
test:
	@echo "Running tests..."
	GOEXPERIMENT=jsonv2 go test ./...

## fmt: Format Go files
fmt:
	go fmt ./...

## vet: Run go vet
vet:
	go vet ./...

## run: Run oxinfer with a manifest (usage: make run MANIFEST=path/to/manifest.json [OUT=delta.json])
run:
	@if [ -z "$(MANIFEST)" ]; then \
		echo "Usage: make run MANIFEST=path/to/manifest.json [OUT=delta.json]"; \
		exit 1; \
	fi; \
	if [ -n "$(OUT)" ]; then \
		./oxinfer --manifest $(MANIFEST) --out $(OUT); \
	else \
		./oxinfer --manifest $(MANIFEST); \
	fi

## perf-validate: Run end-to-end performance validation (generates .oxinfer/performance_reports/*)
perf-validate:
	GOEXPERIMENT=jsonv2 go test ./internal/perf -run TestPerformanceValidationEnd2End -count=1 -v

## perf-report: Alias for perf-validate
perf-report: perf-validate

## perf-clean: Remove performance reports under .oxinfer/performance_reports
perf-clean:
	@mkdir -p .oxinfer/performance_reports
	@find .oxinfer/performance_reports -type f -name '*.json' -delete || true
	@echo "Cleaned .oxinfer/performance_reports"

## cache-clean: Remove cache under .oxinfer/cache (preserves README)
cache-clean:
	@rm -rf .oxinfer/cache || true
	@echo "Cleaned .oxinfer/cache"

## clean: Remove build artifacts and generated files (preserves .oxinfer/README.md)
clean: perf-clean cache-clean
	@rm -f oxinfer bench validate-determinism validate-mvp || true
	@echo "Clean complete"

