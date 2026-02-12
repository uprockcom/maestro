#!/bin/bash
#
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

# Maestro installation script
# Usage: curl -fsSL https://raw.githubusercontent.com/uprockcom/maestro/main/install.sh | bash

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
REPO="uprockcom/maestro"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="$HOME/.maestro"
DOCKER_IMAGE="ghcr.io/uprockcom/maestro"

# Functions
info() {
    echo -e "${BLUE}ℹ${NC} $1"
}

success() {
    echo -e "${GREEN}✓${NC} $1"
}

warning() {
    echo -e "${YELLOW}⚠${NC} $1"
}

error() {
    echo -e "${RED}✗${NC} $1"
    exit 1
}

# Detect platform
detect_platform() {
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    ARCH=$(uname -m)

    case "$OS" in
        linux)
            OS="Linux"
            ;;
        darwin)
            OS="Darwin"
            ;;
        *)
            error "Unsupported operating system: $OS"
            ;;
    esac

    case "$ARCH" in
        x86_64)
            ARCH="x86_64"
            ;;
        arm64|aarch64)
            ARCH="arm64"
            ;;
        *)
            error "Unsupported architecture: $ARCH"
            ;;
    esac

    info "Detected platform: $OS $ARCH"
}

# Check prerequisites
check_prerequisites() {
    # Check for Docker
    if ! command -v docker &> /dev/null; then
        error "Docker is required but not installed. Please install Docker first: https://docs.docker.com/get-docker/"
    fi

    # Check if Docker daemon is running
    if ! docker ps &> /dev/null; then
        error "Docker daemon is not running. Please start Docker."
    fi

    success "Docker is available"

    # Check for Claude CLI (optional but recommended)
    if ! command -v claude &> /dev/null; then
        warning "Claude CLI not found. Some features (AI branch naming) won't work."
        warning "Install from: https://claude.com/claude-code"
    else
        success "Claude CLI is available"
    fi
}

# Get latest release version
get_latest_version() {
    info "Fetching latest version..."
    VERSION=$(curl -s "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')

    if [ -z "$VERSION" ]; then
        error "Failed to fetch latest version"
    fi

    info "Latest version: $VERSION"
}

# Download and install binary
install_binary() {
    # Strip 'v' prefix from version for filename (GoReleaser uses version without 'v')
    VERSION_NO_V="${VERSION#v}"
    BINARY_NAME="maestro_${VERSION_NO_V}_${OS}_${ARCH}.tar.gz"
    DOWNLOAD_URL="https://github.com/$REPO/releases/download/$VERSION/$BINARY_NAME"

    info "Downloading $BINARY_NAME..."

    TMP_DIR=$(mktemp -d)
    cd "$TMP_DIR"

    if ! curl -fsSL -o "$BINARY_NAME" "$DOWNLOAD_URL"; then
        error "Failed to download binary from $DOWNLOAD_URL"
    fi

    info "Extracting archive..."
    tar -xzf "$BINARY_NAME"

    # Try to install to /usr/local/bin, fall back to ~/bin
    if [ -w "$INSTALL_DIR" ]; then
        info "Installing to $INSTALL_DIR..."
        mv maestro "$INSTALL_DIR/"
        chmod +x "$INSTALL_DIR/maestro"
    elif sudo -n true 2>/dev/null; then
        info "Installing to $INSTALL_DIR (with sudo)..."
        sudo mv maestro "$INSTALL_DIR/"
        sudo chmod +x "$INSTALL_DIR/maestro"
    else
        warning "$INSTALL_DIR is not writable and sudo not available"
        INSTALL_DIR="$HOME/bin"
        mkdir -p "$INSTALL_DIR"
        info "Installing to $INSTALL_DIR instead..."
        mv maestro "$INSTALL_DIR/"
        chmod +x "$INSTALL_DIR/maestro"

        # Check if ~/bin is in PATH
        if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
            warning "Add $INSTALL_DIR to your PATH:"
            echo ""
            echo "  export PATH=\"\$PATH:$INSTALL_DIR\""
            echo ""
            warning "Add the above line to your ~/.bashrc, ~/.zshrc, or ~/.profile"
        fi
    fi

    # Cleanup
    cd - > /dev/null
    rm -rf "$TMP_DIR"

    success "Binary installed: $(which maestro)"
}

# Pull Docker image
pull_docker_image() {
    info "Pulling Docker image..."

    # Try to pull versioned image first, fall back to latest
    if docker pull "${DOCKER_IMAGE}:${VERSION#v}" 2>/dev/null; then
        success "Docker image pulled: ${DOCKER_IMAGE}:${VERSION#v}"
    elif docker pull "${DOCKER_IMAGE}:latest" 2>/dev/null; then
        success "Docker image pulled: ${DOCKER_IMAGE}:latest"
        warning "Versioned image not found, using latest"
    else
        warning "Could not pull Docker image from registry"
        warning "You'll need to build it locally: cd maestro && make docker"
    fi
}

# Create initial configuration
create_config() {
    if [ -f "$CONFIG_DIR/config.yml" ]; then
        warning "Config already exists at $CONFIG_DIR/config.yml - skipping"
        return
    fi

    info "Creating initial configuration..."
    mkdir -p "$CONFIG_DIR"
    mkdir -p "$CONFIG_DIR/.claude"

    cat > "$CONFIG_DIR/config.yml" <<EOF
# Maestro Configuration
# See: https://github.com/uprockcom/maestro

claude:
  auth_path: ~/.maestro/.claude

containers:
  prefix: maestro-
  image: ${DOCKER_IMAGE}:latest
  resources:
    memory: "4g"
    cpus: "2.0"

firewall:
  enabled: true
  allowed_domains:
    - "github.com"
    - "githubusercontent.com"
    - "npmjs.org"
    - "registry.npmjs.org"
    - "pypi.org"
    - "files.pythonhosted.org"
    - "anthropic.com"
    - "claude.ai"

sync:
  additional_folders: []

daemon:
  show_nag: true
  token_check_interval: 3600
  token_warning_threshold: 86400
EOF

    success "Config created at $CONFIG_DIR/config.yml"
}

# Main installation flow
main() {
    echo ""
    echo "╔═══════════════════════════════════════╗"
    echo "║     Maestro Installation Script      ║"
    echo "║  Multi-Container Claude Management   ║"
    echo "╚═══════════════════════════════════════╝"
    echo ""

    detect_platform
    check_prerequisites
    get_latest_version
    install_binary
    pull_docker_image
    create_config

    echo ""
    success "Maestro installed successfully!"
    echo ""
    info "Next steps:"
    echo "  1. Authenticate: maestro auth"
    echo "  2. Create container: maestro new \"your task description\""
    echo "  3. List containers: maestro list"
    echo ""
    info "Documentation: https://github.com/uprockcom/maestro"
    echo ""
}

# Run installation
main
