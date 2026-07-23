SHELL := /bin/sh

BINARY := agx
PACKAGE := ./cmd/agx
BIN_DIR := bin
DIST_DIR := dist

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || printf 'dev')
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
CGO_ENABLED ?= 0

LDFLAGS := -s -w -X main.version=$(VERSION)
EXT :=
ifeq ($(GOOS),windows)
EXT := .exe
endif

DIST_NAME := $(BINARY)_$(VERSION)_$(GOOS)_$(GOARCH)
STAGE_DIR := $(DIST_DIR)/$(DIST_NAME)

.PHONY: all build install test test-race vet check package checksums clean

all: build

build:
	@mkdir -p "$(BIN_DIR)"
	CGO_ENABLED=$(CGO_ENABLED) go build -trimpath -ldflags '$(LDFLAGS)' -o "$(BIN_DIR)/$(BINARY)$(EXT)" $(PACKAGE)

install:
	CGO_ENABLED=$(CGO_ENABLED) go install -trimpath -ldflags '$(LDFLAGS)' $(PACKAGE)

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

check: test-race vet

package:
	@mkdir -p "$(STAGE_DIR)"
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) go build -trimpath -ldflags '$(LDFLAGS)' -o "$(STAGE_DIR)/$(BINARY)$(EXT)" $(PACKAGE)
	@if [ "$(GOOS)" = "windows" ]; then \
		(cd "$(STAGE_DIR)" && zip -q "../$(DIST_NAME).zip" "$(BINARY)$(EXT)"); \
	else \
		tar -C "$(STAGE_DIR)" -czf "$(DIST_DIR)/$(DIST_NAME).tar.gz" "$(BINARY)$(EXT)"; \
	fi
	@rm -rf "$(STAGE_DIR)"

checksums:
	@mkdir -p "$(DIST_DIR)"
	@cd "$(DIST_DIR)" && { \
		: > checksums.txt; \
		for file in $(BINARY)_*.tar.gz $(BINARY)_*.zip; do \
			[ -f "$$file" ] || continue; \
			if command -v sha256sum >/dev/null 2>&1; then \
				sha256sum "$$file"; \
			else \
				shasum -a 256 "$$file"; \
			fi; \
		done >> checksums.txt; \
	}

clean:
	@rm -rf "$(BIN_DIR)" "$(DIST_DIR)"
