#!/bin/bash

# Pre-commit checks for maestro
# Runs formatting, compilation, and tests.
# NOTE: Does NOT build Docker images — that can't be tested inside containers.

set -e

echo "Running pre-commit checks..."
echo ""

# --- 1. Check Go formatting ---
echo "1. Checking gofmt..."
TMP_GOFMT=$(mktemp)
gofmt -l $(find . -name '*.go' -not -name '._*' -not -path './vendor/*') > "$TMP_GOFMT" 2>&1 || true

if [ -s "$TMP_GOFMT" ]; then
    echo "❌ gofmt found issues in the following files:"
    cat "$TMP_GOFMT"
    echo ""
    echo "Run 'gofmt -w .' to fix formatting issues"
    rm -f "$TMP_GOFMT"
    exit 1
else
    echo "✓ gofmt passed"
    rm -f "$TMP_GOFMT"
fi

# --- 2. Compile check ---
echo ""
echo "2. Checking compilation..."
TMP_BUILD=$(mktemp)
if go build ./... > "$TMP_BUILD" 2>&1; then
    echo "✓ Compilation passed"
    rm -f "$TMP_BUILD"
else
    echo "❌ Compilation failed:"
    cat "$TMP_BUILD"
    rm -f "$TMP_BUILD"
    exit 1
fi

# --- 3. Run Go tests ---
echo ""
echo "3. Running Go tests..."
TMP_TEST=$(mktemp)
if go test ./... > "$TMP_TEST" 2>&1; then
    echo "✓ Go tests passed"
    rm -f "$TMP_TEST"
else
    echo "❌ Go tests failed:"
    cat "$TMP_TEST"
    rm -f "$TMP_TEST"
    exit 1
fi

# --- 4. Run linter (if available) ---
echo ""
echo "4. Running golangci-lint..."
if command -v golangci-lint &> /dev/null; then
    TMP_LINT=$(mktemp)
    if golangci-lint run > "$TMP_LINT" 2>&1; then
        echo "✓ golangci-lint passed"
        rm -f "$TMP_LINT"
    else
        echo "❌ golangci-lint found issues:"
        cat "$TMP_LINT"
        rm -f "$TMP_LINT"
        exit 1
    fi
else
    echo "⚠️  golangci-lint not found, skipping lint check"
fi

# --- Summary ---
echo ""
echo "✅ All pre-commit checks passed!"
