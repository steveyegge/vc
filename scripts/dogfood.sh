#!/usr/bin/env bash
#
# dogfood.sh - Helper script for dogfooding VC against itself
#
# Usage:
#   ./scripts/dogfood.sh [issue-id]
#
# If no issue-id is provided, executor will pick next ready work.
# Launches VC executor and activity feed tail in parallel.
# Press Ctrl+C to stop both.
#

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Get script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VC_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$VC_ROOT"

echo -e "${BLUE}🐕 VC Dogfooding Session${NC}"
echo -e "${BLUE}========================${NC}\n"

# Check if VC binary exists
if [[ ! -f "./vc" ]]; then
    echo -e "${RED}Error: VC binary not found. Run 'go build ./cmd/vc' first.${NC}"
    exit 1
fi

# Check API key
if [[ -z "$ANTHROPIC_API_KEY" ]]; then
    echo -e "${YELLOW}Warning: ANTHROPIC_API_KEY not set. AI supervision will be disabled.${NC}"
    read -p "Continue anyway? (y/N) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
fi

# Create temp files for process management
TAIL_PID_FILE=$(mktemp)
EXEC_PID_FILE=$(mktemp)

# Cleanup function
cleanup() {
    echo -e "\n\n${YELLOW}📋 Cleaning up...${NC}"

    if [[ -f "$TAIL_PID_FILE" ]]; then
        TAIL_PID=$(cat "$TAIL_PID_FILE" 2>/dev/null || echo "")
        if [[ -n "$TAIL_PID" ]] && kill -0 "$TAIL_PID" 2>/dev/null; then
            echo "  Stopping activity feed tail (PID $TAIL_PID)..."
            kill "$TAIL_PID" 2>/dev/null || true
        fi
        rm -f "$TAIL_PID_FILE"
    fi

    if [[ -f "$EXEC_PID_FILE" ]]; then
        EXEC_PID=$(cat "$EXEC_PID_FILE" 2>/dev/null || echo "")
        if [[ -n "$EXEC_PID" ]] && kill -0 "$EXEC_PID" 2>/dev/null; then
            echo "  Stopping executor (PID $EXEC_PID)..."
            kill "$EXEC_PID" 2>/dev/null || true
        fi
        rm -f "$EXEC_PID_FILE"
    fi

    # Kill any remaining vc processes
    pkill -9 -f "vc (tail|execute)" 2>/dev/null || true

    echo -e "\n${GREEN}✓ Cleanup complete${NC}"
    echo -e "\n${BLUE}📊 Next steps:${NC}"
    echo -e "  1. Review activity feed output above"
    echo -e "  2. File any issues discovered: bd create ..."
    echo -e "  3. Export issues: bd export -o .beads/issues.jsonl"
    echo -e "  4. Commit changes: git add .beads/issues.jsonl && git commit"
}

trap cleanup EXIT INT TERM

# Start activity feed tail in background
echo -e "${BLUE}👁️  Starting activity feed monitor...${NC}"
./vc tail -f > /tmp/vc-dogfood-tail.log 2>&1 &
TAIL_PID=$!
echo "$TAIL_PID" > "$TAIL_PID_FILE"
echo -e "${GREEN}✓ Activity feed tail started (PID: $TAIL_PID)${NC}"
echo -e "  Log: /tmp/vc-dogfood-tail.log\n"

# Give tail a moment to start
sleep 1

# Start executor
echo -e "${BLUE}🚀 Starting VC executor...${NC}"
if [[ -n "$1" ]]; then
    echo -e "  Target issue: ${GREEN}$1${NC}"
    # Note: Issue-specific execution not yet supported in execute command
    # This would require additional implementation
    echo -e "${YELLOW}  Note: Issue-specific execution not yet implemented${NC}"
    echo -e "  Executor will pick next ready work instead\n"
fi

./vc execute --poll-interval 2 > /tmp/vc-dogfood-exec.log 2>&1 &
EXEC_PID=$!
echo "$EXEC_PID" > "$EXEC_PID_FILE"
echo -e "${GREEN}✓ Executor started (PID: $EXEC_PID)${NC}"
echo -e "  Log: /tmp/vc-dogfood-exec.log\n"

echo -e "${BLUE}📺 Monitoring (Ctrl+C to stop)${NC}"
echo -e "${BLUE}================================${NC}\n"

# Tail both logs in real-time
tail -f /tmp/vc-dogfood-tail.log /tmp/vc-dogfood-exec.log
