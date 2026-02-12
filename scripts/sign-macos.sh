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

# Sign macOS binary with quill
# Usage: sign-macos.sh /path/to/binary

set -e

BINARY="$1"

if [ -z "$BINARY" ]; then
  echo "Usage: $0 <binary-path>"
  exit 1
fi

# Only sign macOS binaries (skip Windows/Linux)
if [[ ! "$BINARY" =~ darwin ]]; then
  echo "⏭️  Skipping non-macOS binary: $(basename $BINARY)"
  exit 0
fi

# Check if quill credentials are set
if [ -z "${QUILL_SIGN_P12}" ]; then
  echo "⚠️  QUILL_SIGN_P12 not set, skipping macOS signing for: $(basename $BINARY)"
  exit 0
fi

echo "🔏 Signing macOS binary: $(basename $BINARY)"

# Check if we have notarization credentials
if [ -n "${QUILL_NOTARY_KEY}" ] && [ -n "${QUILL_NOTARY_KEY_ID}" ] && [ -n "${QUILL_NOTARY_ISSUER}" ]; then
  echo "  → Signing and notarizing with Apple..."
  QUILL_LOG_LEVEL=info quill sign-and-notarize "$BINARY"
else
  echo "  → Signing only (notarization credentials not set)..."
  QUILL_LOG_LEVEL=info quill sign "$BINARY"
  echo "  ⚠️  Binary signed but NOT notarized"
fi

# Verify signature
codesign --verify --deep --strict --verbose=2 "$BINARY" 2>&1 || {
  echo "❌ Signature verification failed for: $(basename $BINARY)"
  exit 1
}

echo "✅ Signed and verified: $(basename $BINARY)"
