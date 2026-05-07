.PHONY: build build-all run test clean dist checksums

BINARY_NAME=evoduck
BUILD_DIR=build
DIST_DIR=build/dist
MODULE=github.com/chawuciren/evoduck

VERSION ?= $(shell git describe --tags --abbrev=0 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS=-ldflags "-s -w -X $(MODULE)/cmd/$(BINARY_NAME).Version=$(VERSION) -X $(MODULE)/cmd/$(BINARY_NAME).Commit=$(COMMIT) -X $(MODULE)/cmd/$(BINARY_NAME).BuildTime=$(BUILD_TIME)"

PLATFORMS=\
	linux/amd64 \
	linux/arm64 \
	darwin/amd64 \
	darwin/arm64 \
	windows/amd64 \
	windows/arm64

build:
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/$(BINARY_NAME)

build-all:
	@rm -rf $(DIST_DIR)
	@mkdir -p $(DIST_DIR)
	@for platform in $(PLATFORMS); do \
		OS=$$(echo $$platform | cut -d/ -f1); \
		ARCH=$$(echo $$platform | cut -d/ -f2); \
		EXT=""; \
		if [ "$$OS" = "windows" ]; then EXT=".exe"; fi; \
		OUT="$(DIST_DIR)/$(BINARY_NAME)-$$OS-$$ARCH$$EXT"; \
		echo "Building $$OS/$$ARCH..."; \
		GOOS=$$OS GOARCH=$$ARCH go build $(LDFLAGS) -o "$$OUT" ./cmd/$(BINARY_NAME); \
	done

dist: build-all
	@for platform in $(PLATFORMS); do \
		OS=$$(echo $$platform | cut -d/ -f1); \
		ARCH=$$(echo $$platform | cut -d/ -f2); \
		EXT=""; \
		if [ "$$OS" = "windows" ]; then EXT=".exe"; fi; \
		BIN="$(BINARY_NAME)-$$OS-$$ARCH$$EXT"; \
		if [ "$$OS" = "windows" ]; then \
			cd $(DIST_DIR) && zip "$$BIN.zip" "$$BIN" && cd - > /dev/null; \
		else \
			cd $(DIST_DIR) && tar -czf "$$BIN.tar.gz" "$$BIN" && cd - > /dev/null; \
		fi; \
	done
	@echo "Dist packages created in $(DIST_DIR)/"

checksums: dist
	cd $(DIST_DIR) && sha256sum * > checksums.txt
	@echo "Checksums written to $(DIST_DIR)/checksums.txt"

run: build
	$(BUILD_DIR)/$(BINARY_NAME) run

test:
	go test -v ./...

clean:
	rm -rf $(BUILD_DIR)

install-deps:
	go mod tidy
