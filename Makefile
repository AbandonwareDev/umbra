BINARY := umbra
BUILD_DIR := build

.PHONY: all build build-notray clean install install-notray help

all: build

# Default build: tray enabled (pure Go, no CGo needed)
build:
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY) .

# Build without tray support (headless/server mode)
build-notray:
	@mkdir -p $(BUILD_DIR)
	go build -tags notray -o $(BUILD_DIR)/$(BINARY)-headless .

clean:
	rm -rf $(BUILD_DIR)

install: build
	@echo "Installing $(BINARY) to /usr/local/bin/"
	sudo cp $(BUILD_DIR)/$(BINARY) /usr/local/bin/$(BINARY)

install-notray: build-notray
	@echo "Installing $(BINARY) (headless) to /usr/local/bin/"
	sudo cp $(BUILD_DIR)/$(BINARY)-headless /usr/local/bin/$(BINARY)

help:
	@echo "Targets:"
	@echo "  build           - Build with tray support (default)"
	@echo "  build-notray    - Build without tray (headless/server)"
	@echo "  install         - Install to /usr/local/bin/"
	@echo "  install-notray  - Install headless build"
	@echo "  clean           - Remove build artifacts"
