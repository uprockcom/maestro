#!/bin/bash
# Copyright 2025 Christopher O'Connell
# All rights reserved

# Container startup script for Maestro
# This script is run when the container starts

echo "Maestro container starting..."

# Ensure proper ownership of home directory
sudo chown -R node:node /home/node 2>/dev/null || true

# Install custom CA certificate if mounted (for corporate proxies like Zscaler)
if [ -f /usr/local/share/ca-certificates/custom-ca.crt ]; then
    echo "Installing custom CA certificate..."
    sudo update-ca-certificates 2>/dev/null || true
    echo "✓ CA certificate installed"
fi

# Set container hostname in prompt
export PS1="[maestro] \w $ "

# Update Claude Code to latest version
echo "Checking for Claude Code updates..."
npm update -g @anthropic-ai/claude-code || echo "Warning: Could not update Claude Code"
claude --version

# Note: Firewall will be initialized by Maestro after container is set up
# This avoids timing issues where the firewall script hasn't been copied yet

# Ensure proper permissions on workspace
if [ -d /workspace ]; then
    cd /workspace
fi

# Keep container running
echo "Container ready. This container is managed by Maestro."
echo "Connect using: maestro connect <container-name>"

# Sleep infinity to keep container alive
exec sleep infinity