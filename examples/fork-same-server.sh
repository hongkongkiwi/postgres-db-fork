#!/bin/bash

# PostgreSQL Same-Server Fork Script
# This script sets up environment variables and forks a database within the same PostgreSQL server

set -e  # Exit on any error

# =============================================================================
# Check if postgres-db-fork tool is installed
# =============================================================================

echo "🔍 Checking for postgres-db-fork tool..."

if ! command -v postgres-db-fork >/dev/null 2>&1; then
    echo "❌ Error: postgres-db-fork command not found in PATH"
    echo ""
    echo "📥 To install postgres-db-fork:"
    echo ""
    echo "   Option 1 - Download pre-built binary:"
    echo "   curl -L -o postgres-db-fork https://github.com/your-org/postgres-db-fork/releases/latest/download/postgres-db-fork-linux-amd64"
    echo "   chmod +x postgres-db-fork"
    echo "   sudo mv postgres-db-fork /usr/local/bin/"
    echo ""
    echo "   Option 2 - Build from source (requires Go):"
    echo "   git clone https://github.com/your-org/postgres-db-fork.git"
    echo "   cd postgres-db-fork"
    echo "   go build -o postgres-db-fork ."
    echo "   sudo mv postgres-db-fork /usr/local/bin/"
    echo ""
    echo "   Option 3 - Build locally in this project:"
    echo "   go build -o postgres-db-fork ."
    echo "   export PATH=\$PATH:\$(pwd)"
    echo ""
    exit 1
fi

echo "✅ postgres-db-fork tool found"

# =============================================================================
# Configuration - Edit these values for your setup
# =============================================================================

# Option 1: Database URIs (will override individual parameters if set)
# Format: postgresql://[user[:password]@][host][:port][/dbname][?param1=value1&...]
SOURCE_DB_URI=""  # e.g., "postgresql://postgres:password@localhost:5432/myapp_prod?sslmode=prefer" # pragma: allowlist secret
TARGET_DB_URI=""  # e.g., "postgresql://postgres:password@localhost:5432/myapp_dev?sslmode=prefer" # pragma: allowlist secret

# Option 2: Individual database connection settings (used if URIs are not set)
DB_HOST="localhost"
DB_PORT="5432"
DB_USER="postgres"
DB_PASSWORD="your_password_here" # pragma: allowlist secret

# Source and target databases (used if URIs are not set)
SOURCE_DB="myapp_prod"
TARGET_DB="myapp_dev"

# Fork options
DROP_IF_EXISTS="true"
MAX_CONNECTIONS="4"
OUTPUT_FORMAT="text"  # or "json"
QUIET="false"

# =============================================================================
# Set environment variables
# =============================================================================

echo "🔧 Setting up environment variables for same-server fork..."

# Check if source URI is provided
if [ -n "$SOURCE_DB_URI" ]; then
    echo "📡 Using source database URI"
    export PGFORK_SOURCE_URI="$SOURCE_DB_URI"
else
    echo "🔧 Using individual source connection parameters"
    export PGFORK_SOURCE_HOST="$DB_HOST"
    export PGFORK_SOURCE_PORT="$DB_PORT"
    export PGFORK_SOURCE_USER="$DB_USER"
    export PGFORK_SOURCE_PASSWORD="$DB_PASSWORD"  # pragma: allowlist secret
    export PGFORK_SOURCE_DATABASE="$SOURCE_DB"
    export PGFORK_SOURCE_SSLMODE="prefer"
fi

# Check if target URI is provided
if [ -n "$TARGET_DB_URI" ]; then
    echo "📡 Using target database URI"
    export PGFORK_TARGET_URI="$TARGET_DB_URI"
else
    echo "🔧 Using individual target connection parameters"
    # For same-server fork, destination defaults to source values
    # Only set target database name
    export PGFORK_TARGET_DATABASE="$TARGET_DB"
fi

# Fork options
export PGFORK_DROP_IF_EXISTS="$DROP_IF_EXISTS"
export PGFORK_MAX_CONNECTIONS="$MAX_CONNECTIONS"
export PGFORK_OUTPUT_FORMAT="$OUTPUT_FORMAT"
export PGFORK_QUIET="$QUIET"

# =============================================================================
# Display configuration
# =============================================================================

echo "📋 Fork Configuration:"

if [ -n "$SOURCE_DB_URI" ]; then
    # Mask password in URI for display
    DISPLAY_SOURCE_URI=$(echo "$SOURCE_DB_URI" | sed 's/:\/\/[^:]*:[^@]*@/:\/\/***:***@/')
    echo "  Source URI: $DISPLAY_SOURCE_URI"
else
    echo "  Source: $DB_HOST:$DB_PORT/$SOURCE_DB"
fi

if [ -n "$TARGET_DB_URI" ]; then
    # Mask password in URI for display
    DISPLAY_TARGET_URI=$(echo "$TARGET_DB_URI" | sed 's/:\/\/[^:]*:[^@]*@/:\/\/***:***@/')
    echo "  Target URI: $DISPLAY_TARGET_URI"
else
    echo "  Target: $DB_HOST:$DB_PORT/$TARGET_DB"
fi

if [ -z "$SOURCE_DB_URI" ] && [ -z "$TARGET_DB_URI" ]; then
    echo "  User: $DB_USER"
fi

echo "  Drop if exists: $DROP_IF_EXISTS"
echo "  Max connections: $MAX_CONNECTIONS"
echo ""

# =============================================================================
# Confirm before proceeding
# =============================================================================

if [ "$QUIET" != "true" ]; then
    read -p "🚀 Proceed with fork operation? (y/N): " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "❌ Operation cancelled"
        exit 1
    fi
fi

# =============================================================================
# Execute fork command
# =============================================================================

echo "🌿 Starting same-server database fork..."
echo "⏱️  Started at: $(date)"

# Run the fork command
postgres-db-fork fork
FORK_RESULT=$?

# =============================================================================
# Display results
# =============================================================================

if [ $FORK_RESULT -eq 0 ]; then
    echo "✅ Fork completed successfully!"

    if [ -n "$TARGET_DB_URI" ]; then
        echo "🎯 Target database is ready"
        # Extract database name from URI for display (simple extraction)
        TARGET_DB_NAME=$(echo "$TARGET_DB_URI" | sed -n 's/.*\/\([^?]*\).*/\1/p')
        if [ -n "$TARGET_DB_NAME" ]; then
            echo "🔗 Target database: $TARGET_DB_NAME"
        fi
    else
        echo "🎯 Target database '$TARGET_DB' is ready"
        echo "🔗 Connection: postgresql://$DB_USER@$DB_HOST:$DB_PORT/$TARGET_DB"
    fi
else
    echo "❌ Fork operation failed (exit code: $FORK_RESULT)"
    exit $FORK_RESULT
fi

echo "⏱️  Completed at: $(date)"
