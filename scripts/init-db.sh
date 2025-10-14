#!/bin/bash
# init-db.sh - Initialize a fresh VC database
#
# This script initializes a new database for VC. The database schema is
# automatically created when connecting to a new database, so this script
# primarily serves as documentation and a convenience wrapper.
#
# Usage:
#   ./scripts/init-db.sh [sqlite|postgres]
#
# Examples:
#   ./scripts/init-db.sh sqlite           # Initialize SQLite database
#   ./scripts/init-db.sh postgres         # Initialize PostgreSQL database
#

set -euo pipefail

BACKEND="${1:-sqlite}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

init_sqlite() {
    local db_path="${VC_DB_PATH:-.beads/vc.db}"

    log_info "Initializing SQLite database at: $db_path"

    # Create directory if it doesn't exist
    local db_dir=$(dirname "$db_path")
    if [ ! -d "$db_dir" ]; then
        log_info "Creating directory: $db_dir"
        mkdir -p "$db_dir"
    fi

    # Check if database already exists
    if [ -f "$db_path" ]; then
        log_warn "Database already exists at $db_path"
        read -p "Do you want to delete and recreate it? (y/N) " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            log_info "Aborted"
            exit 0
        fi
        log_info "Removing existing database..."
        rm "$db_path"
    fi

    # The Go storage layer will automatically create the schema when connecting
    # to a new database, so we just need to trigger a connection
    log_info "Database will be initialized on first connection"
    log_info "The schema is automatically created by the storage layer"

    log_success "SQLite database ready at: $db_path"
    log_info "Connect using: sqlite3 $db_path"
}

init_postgres() {
    local host="${VC_PG_HOST:-localhost}"
    local port="${VC_PG_PORT:-5432}"
    local database="${VC_PG_DATABASE:-vc}"
    local user="${VC_PG_USER:-vc}"
    local password="${VC_PG_PASSWORD:-}"

    log_info "Initializing PostgreSQL database"
    log_info "  Host: $host"
    log_info "  Port: $port"
    log_info "  Database: $database"
    log_info "  User: $user"

    # Check if PostgreSQL is accessible
    if ! command -v psql &> /dev/null; then
        log_error "psql command not found. Please install PostgreSQL client tools."
        exit 1
    fi

    # Build connection string
    export PGPASSWORD="$password"

    # Check if database exists
    if psql -h "$host" -p "$port" -U "$user" -lqt 2>/dev/null | cut -d \| -f 1 | grep -qw "$database"; then
        log_warn "Database '$database' already exists"
        read -p "Do you want to drop and recreate it? (y/N) " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            log_info "Aborted"
            exit 0
        fi

        log_info "Dropping existing database..."
        psql -h "$host" -p "$port" -U "$user" -d postgres -c "DROP DATABASE IF EXISTS $database;"
    fi

    # Create database
    log_info "Creating database..."
    psql -h "$host" -p "$port" -U "$user" -d postgres -c "CREATE DATABASE $database;"

    # The Go storage layer will automatically create the schema when connecting
    log_info "Database will be initialized on first connection"
    log_info "The schema is automatically created by the storage layer"

    log_success "PostgreSQL database '$database' created and ready"
    log_info "Connect using: psql -h $host -p $port -U $user -d $database"
}

show_usage() {
    cat <<EOF
Usage: $0 [sqlite|postgres]

Initialize a fresh VC database.

Options:
  sqlite      Initialize SQLite database (default)
  postgres    Initialize PostgreSQL database

Environment Variables (SQLite):
  VC_DB_PATH  Path to SQLite database file (default: .beads/vc.db)

Environment Variables (PostgreSQL):
  VC_PG_HOST      PostgreSQL host (default: localhost)
  VC_PG_PORT      PostgreSQL port (default: 5432)
  VC_PG_DATABASE  Database name (default: vc)
  VC_PG_USER      Database user (default: vc)
  VC_PG_PASSWORD  Database password (default: empty)

Examples:
  # Initialize SQLite database at default location
  $0 sqlite

  # Initialize SQLite database at custom location
  VC_DB_PATH=/tmp/test.db $0 sqlite

  # Initialize PostgreSQL database with defaults
  $0 postgres

  # Initialize PostgreSQL with custom settings
  VC_PG_HOST=db.example.com VC_PG_USER=myuser $0 postgres

Notes:
  - The database schema is automatically created by the storage layer
  - For SQLite, the schema is created when the database file is first accessed
  - For PostgreSQL, the schema is created on first connection
  - You can also just start using VC and the schema will be created automatically
EOF
}

# Main
case "${BACKEND}" in
    sqlite)
        init_sqlite
        ;;
    postgres|postgresql)
        init_postgres
        ;;
    -h|--help|help)
        show_usage
        exit 0
        ;;
    *)
        log_error "Unknown backend: $BACKEND"
        echo
        show_usage
        exit 1
        ;;
esac
