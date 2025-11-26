#!/bin/bash
#
# Polecat Wrapper for Gastown Integration (vc-6gp7)
#
# A simpler shell script alternative to polecat_wrapper.py.
# Use this if you don't want Python dependencies.
#
# Usage:
#     ./polecat_wrapper.sh "Implement OAuth2 login"
#     ./polecat_wrapper.sh --lite "Fix typo in README"
#     ./polecat_wrapper.sh --issue vc-123
#
# For more advanced features (gm integration, heuristic lite mode),
# use polecat_wrapper.py instead.
#
# See docs/design/GASTOWN_INTEGRATION.md Section 7 for full specification.

set -euo pipefail

# Colors for output (disable with NO_COLOR=1)
if [[ -z "${NO_COLOR:-}" ]]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[0;33m'
    BLUE='\033[0;34m'
    NC='\033[0m' # No Color
else
    RED='' GREEN='' YELLOW='' BLUE='' NC=''
fi

# Print error message and exit
die() {
    echo -e "${RED}Error: $1${NC}" >&2
    exit 1
}

# Print info message
info() {
    echo -e "${BLUE}$1${NC}" >&2
}

# Print success message
success() {
    echo -e "${GREEN}$1${NC}" >&2
}

# Print warning message
warn() {
    echo -e "${YELLOW}$1${NC}" >&2
}

# Show usage
usage() {
    cat <<EOF
Usage: $(basename "$0") [OPTIONS] [TASK]

Execute a task via VC in polecat mode.

Options:
    -h, --help          Show this help message
    -l, --lite          Use lite mode (skip preflight/assessment)
    -i, --issue ID      Execute beads issue by ID
    -n, --dry-run       Don't commit or merge changes
    -v, --verbose       Print verbose output
    --stdin             Read task from stdin

Examples:
    $(basename "$0") "Implement user authentication"
    $(basename "$0") --lite "Fix typo in README"
    $(basename "$0") --issue vc-abc
    echo "Long task description..." | $(basename "$0") --stdin
EOF
}

# Parse arguments
LITE_MODE=""
ISSUE_ID=""
DRY_RUN=""
VERBOSE=""
FROM_STDIN=""
TASK=""

while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help)
            usage
            exit 0
            ;;
        -l|--lite)
            LITE_MODE="--lite"
            shift
            ;;
        -i|--issue)
            ISSUE_ID="$2"
            shift 2
            ;;
        -n|--dry-run)
            DRY_RUN="1"
            shift
            ;;
        -v|--verbose)
            VERBOSE="1"
            shift
            ;;
        --stdin)
            FROM_STDIN="1"
            shift
            ;;
        -*)
            die "Unknown option: $1"
            ;;
        *)
            TASK="$1"
            shift
            ;;
    esac
done

# Validate arguments
if [[ -n "$FROM_STDIN" ]]; then
    TASK=$(cat)
elif [[ -z "$ISSUE_ID" && -z "$TASK" ]]; then
    usage
    die "Either TASK, --issue, or --stdin is required"
fi

# Check for vc command
if ! command -v vc &> /dev/null; then
    die "vc command not found. Is VC installed and in PATH?"
fi

# Build VC command
VC_CMD=(vc exec --polecat-mode)

if [[ -n "$LITE_MODE" ]]; then
    VC_CMD+=("$LITE_MODE")
fi

if [[ -n "$ISSUE_ID" ]]; then
    VC_CMD+=(--issue "$ISSUE_ID")
else
    VC_CMD+=(--task "$TASK")
fi

# Run VC
info "Running VC in polecat mode..."
if [[ -n "$VERBOSE" ]]; then
    info "Command: ${VC_CMD[*]}"
fi

# Capture output
RESULT_JSON=$(mktemp)
VC_STDERR=$(mktemp)
trap "rm -f '$RESULT_JSON' '$VC_STDERR'" EXIT

if ! "${VC_CMD[@]}" > "$RESULT_JSON" 2> "$VC_STDERR"; then
    warn "VC exited with non-zero status"
    if [[ -n "$VERBOSE" ]]; then
        cat "$VC_STDERR" >&2
    fi
fi

# Print stderr if verbose
if [[ -n "$VERBOSE" && -s "$VC_STDERR" ]]; then
    cat "$VC_STDERR" >&2
fi

# Validate JSON output
if ! jq empty "$RESULT_JSON" 2>/dev/null; then
    die "VC did not produce valid JSON output"
fi

# Parse result
STATUS=$(jq -r '.status' "$RESULT_JSON")
SUCCESS=$(jq -r '.success' "$RESULT_JSON")
SUMMARY=$(jq -r '.summary // "No summary"' "$RESULT_JSON")
DURATION=$(jq -r '.duration_seconds' "$RESULT_JSON")
FILES_COUNT=$(jq '.files_modified | length' "$RESULT_JSON")
DISCOVERED_COUNT=$(jq '.discovered_issues | length' "$RESULT_JSON")

# Print result summary
echo ""
echo "=== VC Polecat Mode Result ==="
echo "Status: $STATUS"
echo "Success: $SUCCESS"
echo "Duration: ${DURATION}s"
echo "Files modified: $FILES_COUNT"
echo "Discovered issues: $DISCOVERED_COUNT"
if [[ "$SUMMARY" != "No summary" ]]; then
    echo "Summary: $SUMMARY"
fi

# Create discovered issues (if any and not dry-run)
if [[ "$DISCOVERED_COUNT" -gt 0 && -z "$DRY_RUN" ]]; then
    info "Creating discovered issues..."

    # Check for bd command
    if command -v bd &> /dev/null; then
        jq -c '.discovered_issues[]' "$RESULT_JSON" | while read -r issue; do
            TITLE=$(echo "$issue" | jq -r '.title')
            TYPE=$(echo "$issue" | jq -r '.type // "task"')
            PRIORITY=$(echo "$issue" | jq -r '.priority // 2')
            DESC=$(echo "$issue" | jq -r '.description // ""')

            bd create \
                --title "$TITLE" \
                --type "$TYPE" \
                --priority "$PRIORITY" \
                --label discovered:related \
                ${DESC:+--description "$DESC"} 2>/dev/null || true
        done
        success "Created $DISCOVERED_COUNT discovered issues"
    else
        warn "bd command not found, skipping issue creation"
    fi
fi

# Commit and merge (if successful and not dry-run)
if [[ "$SUCCESS" == "true" && "$STATUS" == "completed" && -z "$DRY_RUN" ]]; then
    if [[ "$FILES_COUNT" -gt 0 ]]; then
        info "Committing and merging changes..."

        # Get current branch
        CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD)

        # Stage and commit
        git add -A
        COMMIT_MSG="VC: ${TASK:0:50}"
        if [[ ${#TASK} -gt 50 ]]; then
            COMMIT_MSG="${COMMIT_MSG}..."
        fi

        if git commit -m "$COMMIT_MSG" 2>/dev/null; then
            # Merge to main
            if git checkout main 2>/dev/null && \
               git merge "$CURRENT_BRANCH" 2>/dev/null && \
               git push origin main 2>/dev/null; then
                success "Changes merged to main"
            else
                warn "Merge or push failed, check manually"
            fi

            # Return to polecat branch
            git checkout "$CURRENT_BRANCH" 2>/dev/null || true
        else
            info "Nothing to commit"
        fi
    fi
else
    if [[ -n "$DRY_RUN" ]]; then
        info "Dry run - skipping commit/merge"
    elif [[ "$SUCCESS" != "true" ]]; then
        warn "Task did not complete successfully, skipping commit/merge"
    fi
fi

# Exit with appropriate code
if [[ "$SUCCESS" == "true" ]]; then
    exit 0
else
    exit 1
fi
