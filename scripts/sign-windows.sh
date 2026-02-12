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

# Sign Windows binary with Azure Trusted Signing
# Usage: sign-windows.sh /path/to/binary.exe

set -e

BINARY="$1"

if [ -z "$BINARY" ]; then
  echo "Usage: $0 <binary-path>"
  exit 1
fi

# Only sign Windows binaries (skip macOS/Linux)
if [[ ! "$BINARY" =~ windows ]] && [[ ! "$BINARY" =~ \.exe$ ]]; then
  echo "⏭️  Skipping non-Windows binary: $(basename $BINARY)"
  exit 0
fi

# Check if Azure credentials are set
if [ -z "${AZURE_CLIENT_ID}" ] || [ -z "${AZURE_CLIENT_SECRET}" ] || [ -z "${AZURE_TENANT_ID}" ]; then
  echo "⚠️  Azure credentials not set, skipping Windows signing for: $(basename $BINARY)"
  exit 0
fi

if [ -z "${AZURE_CODE_SIGNING_ACCOUNT_NAME}" ] || [ -z "${AZURE_CERTIFICATE_PROFILE_NAME}" ]; then
  echo "⚠️  Azure Trusted Signing settings not set, skipping Windows signing for: $(basename $BINARY)"
  exit 0
fi

echo "🔏 Signing Windows binary: $(basename $BINARY)"

# Get absolute path for Docker mount
BINARY_ABS=$(cd "$(dirname "$BINARY")" && pwd)/$(basename "$BINARY")
WORKSPACE_DIR=$(dirname "$BINARY_ABS")

# Use Jsign via custom Docker image with Azure Trusted Signing support
docker run --rm \
  -v "$WORKSPACE_DIR:/workspace" \
  -w /workspace \
  -e AZURE_CLIENT_ID \
  -e AZURE_CLIENT_SECRET \
  -e AZURE_TENANT_ID \
  -e AZURE_ENDPOINT \
  -e AZURE_CODE_SIGNING_ACCOUNT_NAME \
  -e AZURE_CERTIFICATE_PROFILE_NAME \
  maestro-signing:latest \
  -c "
    set -e

    # Authenticate with Azure (credentials from env vars)
    az login --service-principal \
      -u \"\$AZURE_CLIENT_ID\" \
      -p \"\$AZURE_CLIENT_SECRET\" \
      --tenant \"\$AZURE_TENANT_ID\" > /dev/null

    # Get access token for Azure Trusted Signing
    AZURE_TOKEN=\$(az account get-access-token \
      --resource https://codesigning.azure.net \
      --query accessToken -o tsv)

    # Sign the binary with Jsign
    jsign --storetype TRUSTEDSIGNING \
      --keystore \${AZURE_ENDPOINT:-neu.codesigning.azure.net} \
      --storepass \"\${AZURE_TOKEN}\" \
      --alias \"\$AZURE_CODE_SIGNING_ACCOUNT_NAME/\$AZURE_CERTIFICATE_PROFILE_NAME\" \
      --name \"Maestro - Multi-Container Claude Manager\" \
      --url \"https://github.com/uprockcom/maestro\" \
      /workspace/$(basename "$BINARY")

    # Verify signature
    osslsigncode verify /workspace/$(basename "$BINARY") > /dev/null 2>&1 || {
      echo \"ERROR: Signature verification failed\"
      exit 1
    }

    echo \"✅ Signed and verified: $(basename "$BINARY")\"
  "

echo "✅ Windows binary signed: $(basename $BINARY)"
