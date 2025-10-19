#!/bin/bash
# cleanup.sh - Clean up development artifacts from VC repository
#
# This script removes:
# - Sandbox directories (.sandboxes/mission-*)
# - Temporary mission branches (mission-vc-*)
# - Old log files in /tmp/
# - Build artifacts
#
# Usage: ./scripts/cleanup.sh [--dry-run]

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

DRY_RUN=false
if [[ "${1:-}" == "--dry-run" ]]; then
    DRY_RUN=true
    echo -e "${YELLOW}DRY RUN MODE - No changes will be made${NC}\n"
fi

# Track what we clean
CLEANED_COUNT=0

# Function to safely remove files/directories
safe_remove() {
    local target="$1"
    local description="$2"

    if [[ ! -e "$target" ]]; then
        return
    fi

    if [[ "$DRY_RUN" == "true" ]]; then
        echo -e "${YELLOW}[DRY RUN]${NC} Would remove: $description"
        CLEANED_COUNT=$((CLEANED_COUNT + 1))
    else
        echo -e "${GREEN}✓${NC} Removing: $description"
        rm -rf "$target"
        CLEANED_COUNT=$((CLEANED_COUNT + 1))
    fi
}

echo "=== VC Cleanup Script ==="
echo

# 1. Clean up sandbox directories
echo "Checking for sandbox directories..."
if [[ -d ".sandboxes" ]]; then
    for sandbox in .sandboxes/mission-*; do
        if [[ -d "$sandbox" ]]; then
            safe_remove "$sandbox" "Sandbox: $sandbox"
        fi
    done
    # Remove .sandboxes directory if empty
    if [[ -z "$(ls -A .sandboxes 2>/dev/null)" ]]; then
        safe_remove ".sandboxes" "Empty .sandboxes directory"
    fi
else
    echo "  No .sandboxes directory found"
fi
echo

# 2. Clean up mission branches
echo "Checking for mission branches..."
MISSION_BRANCHES=$(git branch | grep -E '^\s+mission-vc-' || true)
if [[ -n "$MISSION_BRANCHES" ]]; then
    while IFS= read -r branch; do
        branch=$(echo "$branch" | xargs) # trim whitespace
        if [[ "$DRY_RUN" == "true" ]]; then
            echo -e "${YELLOW}[DRY RUN]${NC} Would delete branch: $branch"
            CLEANED_COUNT=$((CLEANED_COUNT + 1))
        else
            echo -e "${GREEN}✓${NC} Deleting branch: $branch"
            git branch -D "$branch"
            CLEANED_COUNT=$((CLEANED_COUNT + 1))
        fi
    done <<< "$MISSION_BRANCHES"
else
    echo "  No mission branches found"
fi
echo

# 3. Clean up /tmp/ artifacts
echo "Checking for temp artifacts..."
TEMP_ARTIFACTS=$(ls -1 /tmp/vc* 2>/dev/null | grep -E '(\.log|\.md|vc-test|vc-new|vc[0-9]+-analysis)' | sort -u || true)
if [[ -n "$TEMP_ARTIFACTS" ]]; then
    echo "  Found VC temp files:"
    echo "$TEMP_ARTIFACTS" | while IFS= read -r file; do
        if [[ -n "$file" ]]; then
            echo "    - $(basename "$file")"
        fi
    done
    read -p "  Remove these temp files? (y/N) " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        echo "$TEMP_ARTIFACTS" | while IFS= read -r file; do
            if [[ -n "$file" ]]; then
                safe_remove "$file" "Temp file: $(basename "$file")"
            fi
        done
    fi
else
    echo "  No temp artifacts found"
fi
echo

# 4. Clean up test binaries and coverage files
echo "Checking for test artifacts..."
TEST_ARTIFACTS=$(find . -maxdepth 2 -type f \( -name "*.test" -o -name "coverage.out" -o -name "*.out" \) 2>/dev/null | grep -v node_modules || true)
if [[ -n "$TEST_ARTIFACTS" ]]; then
    while IFS= read -r file; do
        if [[ -n "$file" ]]; then
            safe_remove "$file" "Test artifact: $file"
        fi
    done <<< "$TEST_ARTIFACTS"
else
    echo "  No test artifacts found"
fi
echo

# 5. Clean up build artifacts
echo "Checking for build artifacts..."
if [[ -f "cmd/vc/vc" ]]; then
    safe_remove "cmd/vc/vc" "Build artifact: cmd/vc/vc"
fi
if [[ -f "vc" ]]; then
    safe_remove "vc" "Build artifact: vc (root)"
fi
echo

# 6. Clean up old daemon logs (keep last 1000 lines)
echo "Checking daemon log..."
if [[ -f ".beads/daemon.log" ]]; then
    LOG_SIZE=$(wc -l < .beads/daemon.log)
    if [[ $LOG_SIZE -gt 1000 ]]; then
        if [[ "$DRY_RUN" == "true" ]]; then
            echo -e "${YELLOW}[DRY RUN]${NC} Would trim daemon.log from $LOG_SIZE to 1000 lines"
            CLEANED_COUNT=$((CLEANED_COUNT + 1))
        else
            echo -e "${GREEN}✓${NC} Trimming daemon.log from $LOG_SIZE to 1000 lines"
            tail -n 1000 .beads/daemon.log > .beads/daemon.log.tmp
            mv .beads/daemon.log.tmp .beads/daemon.log
            CLEANED_COUNT=$((CLEANED_COUNT + 1))
        fi
    else
        echo "  Daemon log is small ($LOG_SIZE lines)"
    fi
else
    echo "  No daemon log found"
fi
echo

# 7. Clean up go build cache (optional - helps free disk space)
echo "Checking Go build cache..."
CACHE_SIZE=$(du -sh "$(go env GOCACHE)" 2>/dev/null | cut -f1 || echo "unknown")
if [[ "$CACHE_SIZE" != "unknown" ]]; then
    echo "  Current cache size: $CACHE_SIZE"
    read -p "  Clean Go build cache? (y/N) " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        if [[ "$DRY_RUN" == "true" ]]; then
            echo -e "${YELLOW}[DRY RUN]${NC} Would clean Go build cache"
        else
            go clean -cache
            echo -e "${GREEN}✓${NC} Cleaned Go build cache"
        fi
        CLEANED_COUNT=$((CLEANED_COUNT + 1))
    fi
fi
echo

# Summary
echo "=== Cleanup Summary ==="
if [[ "$DRY_RUN" == "true" ]]; then
    echo -e "${YELLOW}DRY RUN:${NC} Found $CLEANED_COUNT items to clean"
    echo "Run without --dry-run to actually remove them"
else
    echo -e "${GREEN}✓ Cleaned $CLEANED_COUNT items${NC}"
fi
