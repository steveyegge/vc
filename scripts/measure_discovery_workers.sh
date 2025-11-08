#!/bin/bash
# Measure actual cost and performance of discovery workers
# Usage: ./scripts/measure_discovery_workers.sh [codebase_path]

set -e

CODEBASE_PATH="${1:-.}"
OUTPUT_FILE="discovery_workers_analysis.txt"

echo "=== Discovery Workers Performance Analysis ===" > "$OUTPUT_FILE"
echo "Codebase: $CODEBASE_PATH" >> "$OUTPUT_FILE"
echo "Date: $(date)" >> "$OUTPUT_FILE"
echo "" >> "$OUTPUT_FILE"

# Count lines of code
echo "--- Codebase Size ---" >> "$OUTPUT_FILE"
find "$CODEBASE_PATH" -name "*.go" -not -path "*/vendor/*" -not -path "*/.git/*" -not -name "*_test.go" | \
  xargs wc -l | tail -1 >> "$OUTPUT_FILE"
echo "" >> "$OUTPUT_FILE"

# Count packages
echo "--- Package Count ---" >> "$OUTPUT_FILE"
find "$CODEBASE_PATH" -type d -name "*.go" -not -path "*/vendor/*" -not -path "*/.git/*" | wc -l | \
  awk '{print "Packages: " $1}' >> "$OUTPUT_FILE"
echo "" >> "$OUTPUT_FILE"

# Run tests with timing
echo "--- Worker Performance ---" >> "$OUTPUT_FILE"
echo "Running integration tests..." >> "$OUTPUT_FILE"
go test -v ./internal/discovery/... -run Integration 2>&1 | \
  grep -E "(Architecture|Bug Hunter|Duration|Issues found|Files analyzed)" | \
  tee -a "$OUTPUT_FILE"

echo "" >> "$OUTPUT_FILE"
echo "Analysis complete. Results saved to $OUTPUT_FILE"
cat "$OUTPUT_FILE"
