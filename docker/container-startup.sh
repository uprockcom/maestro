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