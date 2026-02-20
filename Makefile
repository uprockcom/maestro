# Copyright 2025 Christopher O'Connell
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

.PHONY: build install install-completion docker signing-image clean test check help release release-snapshot license-check build-relay docker-relay

# Default target
help:
	@echo "Maestro Build Targets:"
	@echo "  make check              - Compile all binaries and run all tests (no Docker needed)"
	@echo "  make build              - Build the maestro binary"
	@echo "  make docker             - Build the Docker image locally"
	@echo "  make signing-image      - Build the code signing Docker image"
	@echo "  make install            - Install maestro to /usr/local/bin (requires sudo)"
	@echo "  make install-completion - Install shell completion for current shell"
	@echo "  make test               - Run tests"
	@echo "  make clean              - Remove built binaries"
	@echo "  make all                - Build everything (binary + docker)"
	@echo "  make build-relay        - Build the signal-relay binary"
	@echo "  make docker-relay       - Build the signal-relay Docker image"
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
	docker build -t maestro:latest -f docker/Dockerfile .

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

# Compile all binaries and run all tests (works inside containers — no Docker needed)
check:
	@echo "==> Compiling maestro (main module)..."
	go build ./...
	@echo "==> Compiling maestro-request (docker/maestro-request-go)..."
	cd docker/maestro-request-go && go build -o /dev/null .
	@echo "==> Compiling signal-relay (cmd/signal-relay)..."
	cd cmd/signal-relay && go build -o /dev/null .
	@echo "==> Running tests (main module)..."
	go test ./...
	@echo ""
	@echo "All binaries compile and tests pass."

# Clean build artifacts
clean:
	rm -rf bin

# Build the signal-relay binary (separate Go module)
build-relay:
	cd cmd/signal-relay && go build -o ../../bin/signal-relay .

# Build the signal-relay Docker image
docker-relay:
	docker build -t maestro-signal-relay:latest -f deploy/signal-relay/Dockerfile .

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