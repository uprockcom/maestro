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

# Docker image for code signing Windows binaries with Azure Trusted Signing
# This image includes Azure CLI and Jsign for cross-platform signing

FROM azul/zulu-openjdk-debian:21-latest

# Version information
LABEL org.opencontainers.image.title="Maestro Signing Tools"
LABEL org.opencontainers.image.description="Code signing tools for Maestro releases"
LABEL org.opencontainers.image.version="1.0.0"
LABEL signing-tools.jsign.version="7.0"
LABEL signing-tools.azure-cli.version="2.79.0"
LABEL signing-tools.base-image="azul/zulu-openjdk-debian:21-latest"

# Avoid interactive prompts during package installation
ENV DEBIAN_FRONTEND=noninteractive

# Install prerequisites
RUN apt-get update -qq && \
    apt-get install -y -qq \
        wget \
        curl \
        ca-certificates \
        apt-transport-https \
        lsb-release \
        gnupg \
        osslsigncode \
    && rm -rf /var/lib/apt/lists/*

# Install Microsoft root certificates for signature verification
RUN mkdir -p /usr/local/share/ca-certificates/microsoft && \
    curl -sSL "http://www.microsoft.com/pkiops/certs/microsoft%20identity%20verification%20root%20certificate%20authority%202020.crt" \
        -o /tmp/ms-root.crt && \
    openssl x509 -inform DER -in /tmp/ms-root.crt \
        -out /usr/local/share/ca-certificates/microsoft/ms-identity-root-2020.crt && \
    update-ca-certificates && \
    rm /tmp/ms-root.crt

# Install Azure CLI
RUN mkdir -p /etc/apt/keyrings && \
    curl -sLS https://packages.microsoft.com/keys/microsoft.asc | \
    gpg --dearmor -o /etc/apt/keyrings/microsoft.gpg && \
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/microsoft.gpg] https://packages.microsoft.com/repos/azure-cli/ $(lsb_release -cs) main" > \
    /etc/apt/sources.list.d/azure-cli.list && \
    apt-get update -qq && \
    apt-get install -y -qq azure-cli && \
    rm -rf /var/lib/apt/lists/*

# Install Jsign (cross-platform code signing tool with Azure Trusted Signing support)
RUN wget -q https://github.com/ebourg/jsign/releases/download/7.0/jsign_7.0_all.deb && \
    apt-get update -qq && \
    apt-get install -y -qq ./jsign_7.0_all.deb && \
    rm jsign_7.0_all.deb && \
    rm -rf /var/lib/apt/lists/*

# Verify installations
RUN az --version && \
    jsign --help > /dev/null && \
    osslsigncode --version

WORKDIR /workspace

# Default entrypoint for signing
# Usage: docker run --rm -v $(pwd):/workspace signing-tools maestro.exe
ENTRYPOINT ["/bin/bash"]
