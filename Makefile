# Copyright 2025 Christopher O'Connell
# All rights reserved

.PHONY: build install install-completion docker signing-image clean test help release release-snapshot license-check

# Default target
help:
	@echo "MCL Build Targets:"
	@echo "  make build              - Build the maestro binary"
	@echo "  make docker             - Build the Docker image locally"
	@echo "  make signing-image      - Build the code signing Docker image"
	@echo "  make install            - Install maestro to /usr/local/bin (requires sudo)"
	@echo "  make install-completion - Install shell completion for current shell"
	@echo "  make test               - Run tests"
	@echo "  make clean              - Remove built binaries"
	@echo "  make all                - Build everything (binary + docker)"
	@echo "  make license-check      - Check/add Apache 2.0 headers to source files"
	@echo ""
	@echo "Release Targets:"
	@echo "  make release-preflight           - Check release prerequisites"
	@echo "  make release-preflight-snapshot  - Check snapshot prerequisites"
	@echo "  make release VERSION=vX.Y.Z      - Create a new release (runs preflight)"
	@echo "  make release-snapshot            - Test release build without publishing"

# Version information for dev builds
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -X github.com/uprockcom/maestro/pkg/version.Version=$(VERSION) \
           -X github.com/uprockcom/maestro/pkg/version.Commit=$(COMMIT) \
           -X github.com/uprockcom/maestro/pkg/version.Date=$(DATE) \
           -X github.com/uprockcom/maestro/pkg/version.BuiltBy=make

# Build the Go binary
build:
	mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/maestro .

# Build the Docker image
docker:
	docker build -t maestro:latest docker/

# Build the code signing Docker image
# Tagged with version and latest
SIGNING_IMAGE_VERSION ?= 1.0.0
signing-image:
	@echo "Building signing tools container..."
	docker build \
		-t maestro-signing:$(SIGNING_IMAGE_VERSION) \
		-t maestro-signing:latest \
		-f docker/signing/Dockerfile \
		docker/signing/
	@echo "✓ Built: maestro-signing:$(SIGNING_IMAGE_VERSION)"
	@echo "✓ Tagged: maestro-signing:latest"

# Install to system PATH (run 'make build' first, then 'sudo make install')
install:
	cp bin/maestro /usr/local/bin/
	@echo ""
	@echo "Run 'make install-completion' to enable shell autocompletion"

# Install shell completion for current shell
install-completion:
	@if [ ! -f bin/maestro ]; then \
		echo "Error: bin/maestro not found. Run 'make build' first."; \
		exit 1; \
	fi
	@SHELL_NAME=$$(basename "$$SHELL"); \
	case "$$SHELL_NAME" in \
		bash) \
			echo "Installing bash completion..."; \
			mkdir -p ~/.local/share/bash-completion/completions; \
			bin/maestro completion bash > ~/.local/share/bash-completion/completions/maestro; \
			echo "Installed to ~/.local/share/bash-completion/completions/maestro"; \
			echo "Run 'source ~/.local/share/bash-completion/completions/maestro' or restart your shell"; \
			;; \
		zsh) \
			echo "Installing zsh completion..."; \
			mkdir -p ~/.zsh/completions; \
			bin/maestro completion zsh > ~/.zsh/completions/_maestro; \
			echo "Installed to ~/.zsh/completions/_maestro"; \
			echo "Add 'fpath=(~/.zsh/completions \$$fpath)' to ~/.zshrc if not already present"; \
			echo "Then run 'autoload -U compinit && compinit' or restart your shell"; \
			;; \
		fish) \
			echo "Installing fish completion..."; \
			mkdir -p ~/.config/fish/completions; \
			bin/maestro completion fish > ~/.config/fish/completions/maestro.fish; \
			echo "Installed to ~/.config/fish/completions/maestro.fish"; \
			;; \
		*) \
			echo "Unknown shell: $$SHELL_NAME"; \
			echo "Run 'maestro completion --help' for manual installation instructions"; \
			;; \
	esac

# Run tests
test:
	go test ./...

# Clean build artifacts
clean:
	rm -rf bin

# Build everything
all: build docker

# Release targets
release-preflight:
	@chmod +x scripts/release-preflight.sh
	@./scripts/release-preflight.sh --release

release-preflight-snapshot:
	@chmod +x scripts/release-preflight.sh
	@./scripts/release-preflight.sh --snapshot

release:
ifndef VERSION
	@echo "Error: VERSION is required"
	@echo "Usage: make release VERSION=v1.2.3"
	@exit 1
endif
	@chmod +x scripts/release.sh
	@./scripts/release.sh $(VERSION)

release-snapshot:
	@echo "Building snapshot release (no publish)..."
	goreleaser release --snapshot --clean --skip=publish

# Check and add Apache 2.0 license headers
license-check:
	@chmod +x scripts/add-license-headers.sh
	@./scripts/add-license-headers.sh