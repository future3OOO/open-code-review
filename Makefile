.PHONY: build test clean run help

BINARY_NAME := argus
GO := go
BUILD_DIR := ./bin

build:
	$(GO) build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/argus

test:
	$(GO) test -v -race -count=1 ./...

clean:
	rm -rf $(BUILD_DIR)

run: build
	$(BUILD_DIR)/$(BINARY_NAME) --staged

help:
	$(BUILD_DIR)/$(BINARY_NAME) -h

# Cross-platform builds
build-linux:
	GOOS=linux GOARCH=amd64 $(GO) build -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/argus

build-darwin:
	GOOS=darwin GOARCH=arm64 $(GO) build -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/argus

all: build-linux build-darwin
