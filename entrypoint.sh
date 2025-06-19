#!/bin/sh

set -e

# Function to output to GitHub Actions
github_output() {
    local name="$1"
    local value="$2"
    echo "${name}=${value}" >> "$GITHUB_OUTPUT"
}

# Function to set GitHub environment variable
github_env() {
    local name="$1"
    local value="$2"
    echo "${name}=${value}" >> "$GITHUB_ENV"
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
    # test-connection uses different flag names
    if [ -n "$PGFORK_SOURCE_HOST" ]; then
        ARGS="$ARGS --host $PGFORK_SOURCE_HOST"
    fi
    if [ -n "$PGFORK_SOURCE_PORT" ]; then
        ARGS="$ARGS --port $PGFORK_SOURCE_PORT"
    fi
    if [ -n "$PGFORK_SOURCE_USER" ]; then
        ARGS="$ARGS --user $PGFORK_SOURCE_USER"
    fi
    if [ -n "$PGFORK_SOURCE_PASSWORD" ]; then
        ARGS="$ARGS --password $PGFORK_SOURCE_PASSWORD"
    fi
    if [ -n "$PGFORK_SOURCE_DATABASE" ]; then
        ARGS="$ARGS --database $PGFORK_SOURCE_DATABASE"
    fi
    if [ -n "$PGFORK_SOURCE_SSLMODE" ]; then
        ARGS="$ARGS --sslmode $PGFORK_SOURCE_SSLMODE"
    fi
fi

log_info "Executing: postgres-db-fork $COMMAND $ARGS"

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
