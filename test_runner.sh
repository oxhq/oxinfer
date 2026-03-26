#!/bin/bash

# Oxinfer Test Runner - Clean testing protocol
# Usage: ./test_runner.sh [manifest_file]

set -e

TEMP_DIR="./temp_test_outputs"
DEFAULT_MANIFEST="manifest_creator_catalog_simple.json"

# Create temp directory
mkdir -p "$TEMP_DIR"

# Use provided manifest or default
MANIFEST="${1:-$DEFAULT_MANIFEST}"

# Check if manifest exists
if [[ ! -f "$MANIFEST" ]]; then
    echo "❌ Manifest file not found: $MANIFEST"
    exit 1
fi

echo "🧪 Testing with: $MANIFEST"
echo "📁 Output dir: $TEMP_DIR"

# Build and run
echo "🔨 Building oxinfer..."
GOEXPERIMENT=jsonv2 go build ./cmd/oxinfer

echo "🚀 Running analysis..."
GOEXPERIMENT=jsonv2 ./oxinfer --manifest "$MANIFEST" --no-color > "$TEMP_DIR/output.json"

# Parse and display results
echo ""
echo "📊 RESULTS:"
echo "=========="

# Controllers
CONTROLLERS=$(cat "$TEMP_DIR/output.json" | jq -r '.controllers | length')
echo "✅ Controllers: $CONTROLLERS"

# Models  
MODELS=$(cat "$TEMP_DIR/output.json" | jq -r '.models | length')
echo "✅ Models: $MODELS"

# Polymorphic
POLYMORPHIC=$(cat "$TEMP_DIR/output.json" | jq -r '.polymorphic | length')
echo "🔍 Polymorphic: $POLYMORPHIC"

# Broadcast
BROADCAST=$(cat "$TEMP_DIR/output.json" | jq -r '.broadcast | length')
echo "📡 Broadcast: $BROADCAST"

echo ""
echo "🗂️  Full output saved to: $TEMP_DIR/output.json"
echo "🔍 To inspect: jq '.' $TEMP_DIR/output.json"

# Check for specific issues
if [[ "$POLYMORPHIC" == "0" ]]; then
    echo "⚠️  Polymorphic matcher returned 0 results"
fi

if [[ "$CONTROLLERS" == "0" ]]; then
    echo "❌ Controllers matcher failed - should detect Laravel controllers"
fi

if [[ "$MODELS" == "0" ]]; then
    echo "❌ Models matcher failed - should detect Laravel models"  
fi

echo ""
echo "✅ Test completed. Outputs contained in $TEMP_DIR/"