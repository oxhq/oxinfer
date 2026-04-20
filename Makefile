SHELL := /bin/bash

.PHONY: help build test fmt vet run clean

## help: Show this help
help:
	@echo "Useful commands:";
	@awk 'BEGIN {FS=":.*##"} /^[a-zA-Z0-9_\-]+:.*##/ {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST) | sort

## build: Build the release CLI and copy it to ./oxinfer
build:
	@echo "Building oxinfer..."
	cargo build --locked --release
	cp target/release/oxinfer ./oxinfer

## test: Run the Rust test suite
test:
	@echo "Running tests..."
	cargo test --locked

## fmt: Format Rust files
fmt:
	cargo fmt --all

## vet: Run cargo check across all targets
vet:
	cargo check --all-targets --locked

## run: Run oxinfer with a manifest (usage: make run MANIFEST=path/to/manifest.json [OUT=delta.json])
run:
	@if [ -z "$(MANIFEST)" ]; then \
		echo "Usage: make run MANIFEST=path/to/manifest.json [OUT=delta.json]"; \
		exit 1; \
	fi
	@if [ -n "$(OUT)" ]; then \
		cargo run -- --manifest $(MANIFEST) --out $(OUT); \
	else \
		cargo run -- --manifest $(MANIFEST); \
	fi

## clean: Remove build artifacts and generated files
clean:
	rm -rf target
	rm -f oxinfer
	rm -rf temp_test_outputs
