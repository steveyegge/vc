#!/bin/bash
# find-storage-mocks.sh
# Finds all mockStorage implementations in test files
# Usage: ./scripts/find-storage-mocks.sh

set -euo pipefail

echo "Finding all mockStorage implementations in test files..."
echo ""

# Find all test files with mockStorage struct definitions
mock_files=$(find . -name "*_test.go" -type f -exec grep -l "type.*mockStorage.*struct" {} \; | sort)

if [ -z "$mock_files" ]; then
    echo "No mockStorage implementations found."
    exit 0
fi

echo "Found mockStorage implementations in the following files:"
echo ""
for file in $mock_files; do
    echo "  $file"
done

echo ""
echo "Total: $(echo "$mock_files" | wc -l | tr -d ' ') files"
echo ""
echo "When updating the storage.Storage interface, ALL of these files must be updated."
echo "Use the following command to check if they compile:"
echo ""
echo "  go test -c \$(echo "$mock_files" | tr '\n' ' ')"
