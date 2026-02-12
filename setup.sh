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

set -e

echo "Maestro Setup Script"
echo "===================="
echo ""

# Check for Go
if ! command -v go &> /dev/null; then
    echo "❌ Go is not installed. Please install Go 1.24+ first."
    exit 1
fi

# Check for Docker
if ! command -v docker &> /dev/null; then
    echo "❌ Docker is not installed. Please install Docker first."
    exit 1
fi

echo "✓ Prerequisites found"
echo ""

# Build the maestro binary
echo "Building maestro binary..."
go build -o maestro .
echo "✓ maestro binary built"
echo ""

# Build Docker image
echo "Building Docker image..."
docker build -t maestro:latest docker/
echo "✓ Docker image built"
echo ""

# Set up config directory if it doesn't exist
if [ ! -d "$HOME/.maestro" ]; then
    mkdir -p "$HOME/.maestro"
fi

if [ ! -f "$HOME/.maestro/config.yml" ]; then
    echo "Creating default config at ~/.maestro/config.yml..."
    cp config.yml.example "$HOME/.maestro/config.yml"
    echo "✓ Config file created"
    echo ""
    echo "📝 Please edit ~/.maestro/config.yml to add your custom domains and folders"
else
    echo "✓ Config file already exists at ~/.maestro/config.yml"
fi
echo ""

# Ask about PATH installation
echo "Would you like to install maestro to /usr/local/bin? (requires sudo)"
echo "This will make 'maestro' available system-wide."
read -p "Install to /usr/local/bin? [y/N]: " -n 1 -r
echo ""

if [[ $REPLY =~ ^[Yy]$ ]]; then
    sudo cp maestro /usr/local/bin/
    echo "✓ maestro installed to /usr/local/bin"
else
    echo "To use maestro, either:"
    echo "  1. Add $(pwd) to your PATH:"
    echo "     export PATH=\"\$PATH:$(pwd)\""
    echo "  2. Or run it directly:"
    echo "     $(pwd)/maestro"
fi
echo ""

echo "✅ Setup complete!"
echo ""
echo "Quick Start:"
echo "  maestro new \"your first task\"     # Create a new container"
echo "  maestro list                       # List containers"
echo "  maestro connect <name>             # Connect to a container"
echo ""
echo "For more info, see README.md"
