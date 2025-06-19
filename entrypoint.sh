#!/bin/sh

set -e

# Clear any existing PostgreSQL environment variables that might conflict
unset PGHOST PGPORT PGUSER PGPASSWORD PGDATABASE PGSSLMODE
unset POSTGRES_HOST POSTGRES_PORT POSTGRES_USER POSTGRES_PASSWORD POSTGRES_DB

# Debug: Print environment variables for troubleshooting
echo "=== DEBUG: Environment Variables ==="
echo "COMMAND: $1"
echo "CONFIG_FILE: $CONFIG_FILE"
echo "PGFORK_SOURCE_HOST: $PGFORK_SOURCE_HOST"
echo "PGFORK_SOURCE_PORT: $PGFORK_SOURCE_PORT"
echo "PGFORK_SOURCE_USER: $PGFORK_SOURCE_USER"
echo "PGFORK_SOURCE_DATABASE: $PGFORK_SOURCE_DATABASE"
echo "PGFORK_SOURCE_SSLMODE: $PGFORK_SOURCE_SSLMODE"
echo "PGFORK_TIMEOUT: $PGFORK_TIMEOUT"
echo "=================================="

# Function to output to GitHub Actions
github_output() {
    local name="$1"
    local value="$2"
    # Only write to GITHUB_OUTPUT if it's set and the file/directory exists
    if [ -n "$GITHUB_OUTPUT" ] && [ -w "$(dirname "$GITHUB_OUTPUT" 2>/dev/null || echo /dev/null)" ]; then
        echo "${name}=${value}" >> "$GITHUB_OUTPUT"
    fi
}

# Function to set GitHub environment variable
github_env() {
    local name="$1"
    local value="$2"
    # Only write to GITHUB_ENV if it's set and the file/directory exists
    if [ -n "$GITHUB_ENV" ] && [ -w "$(dirname "$GITHUB_ENV" 2>/dev/null || echo /dev/null)" ]; then
        echo "${name}=${value}" >> "$GITHUB_ENV"
    fi
}

# Function to log info
log_info() {
    echo "::notice::$1"
}

# Function to log error
log_error() {
    echo "::error::$1"
}

# Function to log warning
log_warning() {
    echo "::warning::$1"
}

# Handle template variables
if [ -n "$TEMPLATE_VARS" ]; then
    # Parse JSON template vars and set as environment variables
    echo "$TEMPLATE_VARS" | jq -r 'to_entries[] | "PGFORK_VAR_\(.key)=\(.value)"' | while IFS= read -r line; do
        export "$line"
    done
fi

# Set GitHub context variables automatically
if [ -n "$GITHUB_EVENT_NUMBER" ]; then
    export PGFORK_VAR_PR_NUMBER="$GITHUB_EVENT_NUMBER"
fi

# Build command arguments
ARGS=""

# Add command
if [ -n "$1" ]; then
    COMMAND="$1"
else
    COMMAND="fork"
fi

# Add config file if specified
if [ -n "$CONFIG_FILE" ]; then
    ARGS="$ARGS --config $CONFIG_FILE"
fi

# Check if we're using a config file
USING_CONFIG_FILE=""
if [ -n "$CONFIG_FILE" ]; then
    USING_CONFIG_FILE="true"
fi

# Skip adding flags if command starts with -- (like --help, --version)
if [ "${COMMAND#--}" = "$COMMAND" ]; then
    # Add flags based on command support
    # --quiet is supported by: fork, validate, ps, cleanup, list
    if [ "$PGFORK_QUIET" = "true" ] && ([ "$COMMAND" = "fork" ] || [ "$COMMAND" = "validate" ] || [ "$COMMAND" = "ps" ] || [ "$COMMAND" = "cleanup" ] || [ "$COMMAND" = "list" ]); then
        ARGS="$ARGS --quiet"
    fi

    # --dry-run is supported by: fork, cleanup, branch (delete)
    if [ "$PGFORK_DRY_RUN" = "true" ] && ([ "$COMMAND" = "fork" ] || [ "$COMMAND" = "cleanup" ]); then
        ARGS="$ARGS --dry-run"
    fi

    # --drop-if-exists is supported by: fork only
    if [ "$PGFORK_DROP_IF_EXISTS" = "true" ] && [ "$COMMAND" = "fork" ]; then
        ARGS="$ARGS --drop-if-exists"
    fi

    # --schema-only is supported by: fork, diff, branch (create)
    if [ "$PGFORK_SCHEMA_ONLY" = "true" ] && [ "$COMMAND" = "fork" ]; then
        ARGS="$ARGS --schema-only"
    fi

    # --data-only is supported by: fork only
    if [ "$PGFORK_DATA_ONLY" = "true" ] && [ "$COMMAND" = "fork" ]; then
        ARGS="$ARGS --data-only"
    fi

    # Add output format flag (now standardized across all commands)
    if [ -n "$PGFORK_OUTPUT_FORMAT" ]; then
        ARGS="$ARGS --output-format $PGFORK_OUTPUT_FORMAT"
    fi
    # Add fork-specific flags only for commands that support them
    if [ "$COMMAND" = "fork" ]; then
        if [ -n "$PGFORK_MAX_CONNECTIONS" ]; then
            ARGS="$ARGS --max-connections $PGFORK_MAX_CONNECTIONS"
        fi

        if [ -n "$PGFORK_CHUNK_SIZE" ]; then
            ARGS="$ARGS --chunk-size $PGFORK_CHUNK_SIZE"
        fi

        if [ -n "$PGFORK_TIMEOUT" ]; then
            ARGS="$ARGS --timeout $PGFORK_TIMEOUT"
        fi
    elif [ "$COMMAND" = "test-connection" ]; then
        # test-connection supports timeout but not max-connections or chunk-size
        if [ -n "$PGFORK_TIMEOUT" ]; then
            ARGS="$ARGS --timeout $PGFORK_TIMEOUT"
        fi
    fi

    # Only add database connection flags if NOT using config file OR if explicitly overridden
    if [ -z "$USING_CONFIG_FILE" ]; then
        # Add database connection flags for commands that need them
        if [ "$COMMAND" = "fork" ] || [ "$COMMAND" = "validate" ]; then
            if [ -n "$PGFORK_SOURCE_HOST" ]; then
                ARGS="$ARGS --source-host $PGFORK_SOURCE_HOST"
            fi
            if [ -n "$PGFORK_SOURCE_PORT" ]; then
                ARGS="$ARGS --source-port $PGFORK_SOURCE_PORT"
            fi
            if [ -n "$PGFORK_SOURCE_USER" ]; then
                ARGS="$ARGS --source-user $PGFORK_SOURCE_USER"
            fi
            if [ -n "$PGFORK_SOURCE_PASSWORD" ]; then
                ARGS="$ARGS --source-password $PGFORK_SOURCE_PASSWORD"
            fi
            if [ -n "$PGFORK_SOURCE_DATABASE" ]; then
                ARGS="$ARGS --source-db $PGFORK_SOURCE_DATABASE"
            fi
            if [ -n "$PGFORK_SOURCE_SSLMODE" ]; then
                ARGS="$ARGS --source-sslmode $PGFORK_SOURCE_SSLMODE"
            fi

            if [ -n "$PGFORK_DEST_HOST" ]; then
                ARGS="$ARGS --dest-host $PGFORK_DEST_HOST"
            fi
            if [ -n "$PGFORK_DEST_PORT" ]; then
                ARGS="$ARGS --dest-port $PGFORK_DEST_PORT"
            fi
            if [ -n "$PGFORK_DEST_USER" ]; then
                ARGS="$ARGS --dest-user $PGFORK_DEST_USER"
            fi
            if [ -n "$PGFORK_DEST_PASSWORD" ]; then
                ARGS="$ARGS --dest-password $PGFORK_DEST_PASSWORD"
            fi
            if [ -n "$PGFORK_DEST_SSLMODE" ]; then
                ARGS="$ARGS --dest-sslmode $PGFORK_DEST_SSLMODE"
            fi

            if [ -n "$PGFORK_TARGET_DATABASE" ]; then
                ARGS="$ARGS --target-db $PGFORK_TARGET_DATABASE"
            fi
        elif [ "$COMMAND" = "cleanup" ] || [ "$COMMAND" = "list" ] || [ "$COMMAND" = "ps" ]; then
            # These commands use different flag names
            if [ -n "$PGFORK_DEST_HOST" ]; then
                ARGS="$ARGS --host $PGFORK_DEST_HOST"
            fi
            if [ -n "$PGFORK_DEST_PORT" ]; then
                ARGS="$ARGS --port $PGFORK_DEST_PORT"
            fi
            if [ -n "$PGFORK_DEST_USER" ]; then
                ARGS="$ARGS --user $PGFORK_DEST_USER"
            fi
            if [ -n "$PGFORK_DEST_PASSWORD" ]; then
                ARGS="$ARGS --password $PGFORK_DEST_PASSWORD"
            fi
            if [ -n "$PGFORK_DEST_SSLMODE" ]; then
                ARGS="$ARGS --sslmode $PGFORK_DEST_SSLMODE"
            fi
            # Cleanup command specific flags
            if [ "$COMMAND" = "cleanup" ] && [ -n "$PGFORK_TARGET_DATABASE" ]; then
                ARGS="$ARGS --pattern $PGFORK_TARGET_DATABASE"
            fi
        elif [ "$COMMAND" = "test-connection" ]; then
            # test-connection uses different flag names and does NOT support --password flag
            if [ -n "$PGFORK_SOURCE_HOST" ]; then
                ARGS="$ARGS --host $PGFORK_SOURCE_HOST"
            fi
            if [ -n "$PGFORK_SOURCE_PORT" ]; then
                ARGS="$ARGS --port $PGFORK_SOURCE_PORT"
            fi
            if [ -n "$PGFORK_SOURCE_USER" ]; then
                ARGS="$ARGS --user $PGFORK_SOURCE_USER"
            fi
            # NOTE: test-connection does NOT accept --password flag for security reasons
            # Password should be provided via environment variables or config file
            if [ -n "$PGFORK_SOURCE_DATABASE" ]; then
                ARGS="$ARGS --database $PGFORK_SOURCE_DATABASE"
            fi
            if [ -n "$PGFORK_SOURCE_SSLMODE" ]; then
                ARGS="$ARGS --sslmode $PGFORK_SOURCE_SSLMODE"
            fi
        fi
    fi
fi

log_info "Executing: postgres-db-fork $COMMAND $ARGS"

# Set password as environment variable for commands that need it
if [ -n "$PGFORK_SOURCE_PASSWORD" ]; then
    export PGPASSWORD="$PGFORK_SOURCE_PASSWORD"
fi

# Execute the command and capture output
OUTPUT_FILE=$(mktemp)
if postgres-db-fork "$COMMAND" $ARGS > "$OUTPUT_FILE" 2>&1; then
    RESULT="success"
    log_info "Command executed successfully"
else
    RESULT="failed"
    log_error "Command failed with exit code $?"
fi

# Read the output
OUTPUT=$(cat "$OUTPUT_FILE")
rm "$OUTPUT_FILE"

# Set GitHub Actions outputs
github_output "result" "$RESULT"

# Try to extract target database name from output (for fork command)
if [ "$COMMAND" = "fork" ] && [ "$RESULT" = "success" ]; then
    # This would need to be customized based on actual output format
    TARGET_DB=$(echo "$OUTPUT" | grep -o "Target database: [^[:space:]]*" | cut -d' ' -f3 || echo "")
    if [ -n "$TARGET_DB" ]; then
        github_output "target-database" "$TARGET_DB"

        # Build connection string if we have the target database
        if [ -n "$PGFORK_DEST_HOST" ] && [ -n "$PGFORK_DEST_USER" ]; then
            CONN_STRING="postgresql://$PGFORK_DEST_USER"
            if [ -n "$PGFORK_DEST_PASSWORD" ]; then
                CONN_STRING="$CONN_STRING:$PGFORK_DEST_PASSWORD"
            fi
            CONN_STRING="$CONN_STRING@$PGFORK_DEST_HOST"
            if [ -n "$PGFORK_DEST_PORT" ]; then
                CONN_STRING="$CONN_STRING:$PGFORK_DEST_PORT"
            fi
            CONN_STRING="$CONN_STRING/$TARGET_DB"
            if [ -n "$PGFORK_DEST_SSLMODE" ]; then
                CONN_STRING="$CONN_STRING?sslmode=$PGFORK_DEST_SSLMODE"
            fi
            github_output "connection-string" "$CONN_STRING"
        fi
    fi
fi

# Output the result for debugging
echo "=== Command Output ==="
echo "$OUTPUT"
echo "====================="

# Exit with appropriate code
if [ "$RESULT" = "success" ]; then
    exit 0
else
    exit 1
fi
