# PostgreSQL Database Fork Tool

A high-performance command-line tool for forking (copying) PostgreSQL databases with optimization for speed and CI/CD integration.

## Features

- **High Performance**: Uses PostgreSQL COPY protocol for 10-50x faster data transfer than INSERT-based methods
- **Dual Mode Operation**: Template-based same-server cloning and cross-server data transfer
- **Read-Only Source Access**: Safely fork from production databases without write permissions
- **CI/CD Integration**: Environment variables, JSON output, template naming, and cleanup automation
- **Parallel Processing**: Configurable concurrent connections for optimal performance
- **Comprehensive Transfer**: Tables, indexes, foreign keys, sequences, and data with progress monitoring

## Quick Start

### Installation

```bash
# Download the latest release
curl -L -o postgres-db-fork https://github.com/your-org/postgres-db-fork/releases/latest/download/postgres-db-fork-linux-amd64
chmod +x postgres-db-fork

# Or build from source
go build -o postgres-db-fork ./cmd/main.go
```

### Basic Usage

```bash
# Fork database on same server (fast template-based)
postgres-db-fork fork \
  --source-db myapp_prod \
  --target-db myapp_dev \
  --source-user readonly_user \
  --source-password secret

# Fork database cross-server (with data transfer)
postgres-db-fork fork \
  --source-host prod.example.com \
  --source-db myapp \
  --dest-host staging.example.com \
  --target-db myapp_staging
```

## CI/CD Integration

### Environment Variables

All configuration can be provided via environment variables with the `PGFORK_` prefix:

```bash
# Source database
export PGFORK_SOURCE_HOST=staging-db.example.com
export PGFORK_SOURCE_USER=readonly_user
export PGFORK_SOURCE_PASSWORD=secure_password
export PGFORK_SOURCE_DATABASE=myapp_staging

# Destination database
export PGFORK_DEST_HOST=preview-db.example.com
export PGFORK_DEST_USER=admin_user
export PGFORK_DEST_PASSWORD=admin_password

# Template variables
export PGFORK_VAR_PR_NUMBER=123
export PGFORK_VAR_BRANCH=feature_api

# CI/CD settings
export PGFORK_OUTPUT_FORMAT=json
export PGFORK_QUIET=true
export PGFORK_DROP_IF_EXISTS=true
```

### Template Naming

Use Go templates for dynamic database naming:

```bash
# PR-based databases
postgres-db-fork fork --target-db "myapp_pr_{{.PR_NUMBER}}"

# Branch-based databases
postgres-db-fork fork --target-db "{{.APP_NAME}}_{{.BRANCH}}_{{.COMMIT_SHORT}}"

# Custom variables
postgres-db-fork fork \
  --target-db "{{.APP_NAME}}_{{.ENVIRONMENT}}_{{.PR_NUMBER}}" \
  --template-var APP_NAME=myapp \
  --template-var ENVIRONMENT=preview
```

**Available template variables:**

- `{{.PR_NUMBER}}` - GitHub PR number or GitLab MR IID
- `{{.BRANCH}}` - Sanitized branch name (safe for database identifiers)
- `{{.COMMIT_SHORT}}` - First 8 characters of commit SHA
- `{{.VAR_NAME}}` - Custom variables via `--template-var` or `PGFORK_VAR_*`

### JSON Output

Perfect for CI/CD automation:

```bash
postgres-db-fork fork --output-format json --quiet
```

```json
{
  "format": "json",
  "success": true,
  "message": "Database fork completed successfully",
  "database": "myapp_pr_123",
  "duration": "2m30s"
}
```

### Cleanup Command

Automatically clean up old PR databases:

```bash
# Delete databases matching pattern older than 7 days
postgres-db-fork cleanup \
  --pattern "myapp_pr_*" \
  --older-than 7d \
  --user admin_user

# Force delete specific database
postgres-db-fork cleanup \
  --pattern "myapp_pr_123" \
  --force

# Dry run to see what would be deleted
postgres-db-fork cleanup \
  --pattern "myapp_pr_*" \
  --older-than 3d \
  --dry-run
```

## GitHub Actions Integration

### Using as a GitHub Action

This repository can be used directly as a GitHub Action in your workflows:

```yaml
- name: Fork database for PR
  uses: your-org/postgres-db-fork@v1
  with:
    command: fork
    source-host: ${{ secrets.DB_HOST }}
    source-user: ${{ secrets.DB_USER }}
    source-password: ${{ secrets.DB_PASSWORD }}
    source-database: myapp_staging
    dest-host: ${{ secrets.PREVIEW_DB_HOST }}
    dest-user: ${{ secrets.PREVIEW_DB_USER }}
    dest-password: ${{ secrets.PREVIEW_DB_PASSWORD }}
    target-database: "myapp_pr_{{.PR_NUMBER}}"
    drop-if-exists: true
    output-format: json
  id: fork-db

- name: Get database info
  run: |
    echo "Target database: ${{ steps.fork-db.outputs.target-database }}"
    echo "Connection string: ${{ steps.fork-db.outputs.connection-string }}"
```

#### Action Inputs

| Input | Description | Required | Default |
|-------|-------------|----------|---------|
| `command` | Command to run (fork, validate, cleanup, etc.) | Yes | `fork` |
| `config-file` | Path to configuration file | No | |
| `source-host` | Source database host | No | |
| `source-port` | Source database port | No | `5432` |
| `source-user` | Source database username | No | |
| `source-password` | Source database password | No | |
| `source-database` | Source database name | No | |
| `dest-host` | Destination database host | No | |
| `dest-user` | Destination database username | No | |
| `dest-password` | Destination database password | No | |
| `target-database` | Target database name (supports templates) | No | |
| `drop-if-exists` | Drop target database if it exists | No | `false` |
| `max-connections` | Maximum number of connections | No | `4` |
| `timeout` | Operation timeout | No | `30m` |
| `output-format` | Output format (text, json) | No | `text` |
| `quiet` | Quiet mode | No | `false` |
| `dry-run` | Dry run mode | No | `false` |

#### Action Outputs

| Output | Description |
|--------|-------------|
| `result` | Command execution result (success/failed) |
| `target-database` | The actual target database name (after template processing) |
| `connection-string` | Connection string for the target database |

#### Template Variables

The action automatically provides GitHub context variables:

- `{{.PR_NUMBER}}` - Pull request number
- `{{.BRANCH}}` - Branch name
- `{{.COMMIT_SHA}}` - Full commit SHA
- `{{.COMMIT_SHORT}}` - Short commit SHA (first 8 chars)
- `{{.RUN_ID}}` - GitHub Actions run ID
- `{{.RUN_NUMBER}}` - GitHub Actions run number

#### Complete Workflow Example

```yaml
name: PR Database Management

on:
  pull_request:
    types: [opened, synchronize]
  pull_request_target:
    types: [closed]

jobs:
  create-pr-database:
    if: github.event.action != 'closed'
    runs-on: ubuntu-latest
    steps:
      - name: Create PR preview database
        uses: your-org/postgres-db-fork@v1
        with:
          command: fork
          source-host: ${{ secrets.STAGING_DB_HOST }}
          source-user: ${{ secrets.DB_USER }}
          source-password: ${{ secrets.DB_PASSWORD }}
          source-database: myapp_staging
          dest-host: ${{ secrets.PREVIEW_DB_HOST }}
          dest-user: ${{ secrets.PREVIEW_DB_USER }}
          dest-password: ${{ secrets.PREVIEW_DB_PASSWORD }}
          target-database: "myapp_pr_{{.PR_NUMBER}}"
          drop-if-exists: true
          max-connections: 8
          output-format: json
        id: fork

      - name: Comment on PR
        uses: actions/github-script@v6
        with:
          script: |
            github.rest.issues.createComment({
              issue_number: context.issue.number,
              owner: context.repo.owner,
              repo: context.repo.repo,
              body: `🗄️ Preview database created: \`${{ steps.fork.outputs.target-database }}\`

              Connection string: \`${{ steps.fork.outputs.connection-string }}\``
            })

  cleanup-pr-database:
    if: github.event.action == 'closed'
    runs-on: ubuntu-latest
    steps:
      - name: Cleanup PR database
        uses: your-org/postgres-db-fork@v1
        with:
          command: cleanup
          host: ${{ secrets.PREVIEW_DB_HOST }}
          user: ${{ secrets.PREVIEW_DB_USER }}
          password: ${{ secrets.PREVIEW_DB_PASSWORD }}
          pattern: "myapp_pr_${{ github.event.number }}"
          force: true
```

### Traditional CI/CD Integration

See [`examples/github-actions.yml`](examples/github-actions.yml) for a complete workflow that:

- Creates PR preview databases automatically
- Uses template naming for consistent database names
- Comments on PRs with database information
- Cleans up databases when PRs are closed
- Provides scheduled cleanup for orphaned databases

Example workflow step using the binary directly:

```yaml
- name: Create PR preview database
  run: |
    export PGFORK_TARGET_DATABASE="myapp_pr_{{.PR_NUMBER}}"
    export PGFORK_OUTPUT_FORMAT=json
    export PGFORK_VAR_PR_NUMBER=${{ github.event.pull_request.number }}
    ./postgres-db-fork fork > result.json

    DB_NAME=$(jq -r '.database' result.json)
    echo "Database created: $DB_NAME"
```

## Configuration

### Command Line Flags

#### Fork Command

```bash
postgres-db-fork fork [flags]

# Database connection
--source-host        Source database host (default: localhost)
--source-port        Source database port (default: 5432)
--source-user        Source database username
--source-password    Source database password
--source-db          Source database name (required)
--source-sslmode     Source SSL mode (default: prefer)

--dest-host          Destination host (defaults to source-host)
--dest-user          Destination username (defaults to source-user)
--dest-password      Destination password (defaults to source-password)
--dest-sslmode       Destination SSL mode (defaults to source-sslmode)

--target-db          Target database name (required, supports templates)

# Fork options
--drop-if-exists     Drop target database if it exists
--max-connections    Parallel connections (default: 4)
--chunk-size         Rows per batch (default: 1000)
--timeout            Operation timeout (default: 30m)
--exclude-tables     Tables to exclude
--include-tables     Tables to include (if specified, only these)
--schema-only        Transfer schema only
--data-only          Transfer data only

# CI/CD integration
--output-format      Output format: text or json (default: text)
--quiet              Suppress output except errors
--dry-run            Preview without making changes
--template-var       Template variables (--template-var PR_NUMBER=123)
--env-vars           Load from environment variables (default: true)

# Progress monitoring & resumption
--progress-file      Write progress updates to file (for CI/CD monitoring)
--no-progress        Disable progress reporting
--job-id             Job ID for resumption (auto-generated if not provided)
--resume             Resume interrupted job
--state-dir          Directory for job state files

# Error handling
--retry-attempts     Maximum retry attempts (default: 3)
--retry-delay        Initial retry delay (default: 1s)
```

#### Cleanup Command Flags

```bash
postgres-db-fork cleanup [flags]

# Database connection
--host               Database host (required)
--user               Database username (required)
--password           Database password
--sslmode            SSL mode (default: prefer)

# Cleanup criteria
--pattern            Database name pattern (required, supports wildcards)
--older-than         Delete databases older than duration (e.g., 7d, 24h)
--exclude            Database names to exclude
--force              Force deletion without age requirement

# Output options
--output-format      Output format: text or json
--quiet              Suppress output except errors
--dry-run            Show what would be deleted
```

#### List Command

Essential for CI/CD scripts to discover databases:

```bash
postgres-db-fork list [flags]

# Database connection
--host               Database host (default: localhost)
--user               Database username (required)
--password           Database password

# Filtering
--pattern            Database name pattern (default: *)
--exclude            Database names to exclude
--older-than         Only show databases older than duration
--newer-than         Only show databases newer than duration

# Display options
--show-size          Include database size information
--show-age           Include database age information
--show-owner         Include database owner information
--sort-by            Sort by: name, size, age (default: name)

# Output options
--output-format      Output format: text or json
--quiet              Only output database names
--count-only         Only output count of matching databases

# Examples
postgres-db-fork list --pattern "myapp_pr_*" --show-size --output-format json
postgres-db-fork list --pattern "myapp_pr_123" --quiet  # Check if database exists
```

#### Validate Command

Pre-flight validation for CI/CD workflows:

```bash
postgres-db-fork validate [flags]

# Uses same connection flags as fork command

# Validation options
--quick              Only test basic connectivity
--skip-permissions   Skip permission checks
--check-resources    Check available disk space and resources

# Output options
--output-format      Output format: text or json
--quiet              Only output errors and final result

# Examples
postgres-db-fork validate --source-host prod.example.com --quick
postgres-db-fork validate --output-format json --check-resources
```

### Configuration File

See [`examples/ci-cd-config.yaml`](examples/ci-cd-config.yaml) for full configuration examples.

```yaml
# Basic configuration
source:
  host: staging-db.example.com
  database: myapp_staging
  username: readonly_user
  password: ${PGFORK_SOURCE_PASSWORD}

target_database: "myapp_pr_{{.PR_NUMBER}}"
drop_if_exists: true
output_format: json

# Performance settings
max_connections: 8
exclude_tables:
  - "audit_logs"
  - "performance_metrics"
```

## Performance Optimization

### Database Settings

For optimal performance, configure your destination PostgreSQL server:

```sql
-- Temporary settings for faster data loading
ALTER SYSTEM SET wal_level = minimal;
ALTER SYSTEM SET max_wal_senders = 0;
ALTER SYSTEM SET checkpoint_completion_target = 0.9;
ALTER SYSTEM SET wal_buffers = '32MB';
ALTER SYSTEM SET shared_buffers = '1GB';
SELECT pg_reload_conf();
```

### Hardware Recommendations

- **CPU**: Multi-core for parallel processing (4+ cores recommended)
- **Memory**: 4GB+ RAM, more for large databases
- **Storage**: SSD storage for both source and destination
- **Network**: High bandwidth for cross-server transfers (1Gbps+ recommended)

### Performance Tuning

```bash
# High-performance configuration for large databases
postgres-db-fork fork \
  --max-connections 8 \
  --chunk-size 5000 \
  --exclude-tables "audit_logs,temp_*" \
  --timeout 2h
```

## Use Cases

### 1. PR Preview Databases

```bash
# Automatically create preview databases for each PR
export PGFORK_TARGET_DATABASE="myapp_pr_{{.PR_NUMBER}}"
postgres-db-fork fork --output-format json
```

### 2. Development Environment Setup

```bash
# Copy production to local development
postgres-db-fork fork \
  --source-host prod.example.com \
  --source-db myapp_prod \
  --dest-host localhost \
  --target-db myapp_dev \
  --exclude-tables "audit_logs,user_sessions"
```

### 3. Staging Refresh

```bash
# Update staging with latest production data
postgres-db-fork fork \
  --source-db myapp_prod \
  --target-db myapp_staging \
  --drop-if-exists
```

### 4. Testing Database Setup

```bash
# Create test database with schema only
postgres-db-fork fork \
  --source-db myapp_staging \
  --target-db myapp_test \
  --schema-only \
  --drop-if-exists
```

## Advanced Features

### Progress Monitoring

Monitor long-running transfers with real-time progress and ETA:

```bash
# Write progress to file for CI/CD monitoring
postgres-db-fork fork \
  --source-db large_prod_db \
  --target-db dev_copy \
  --progress-file /tmp/transfer-progress.json \
  --output-format json
```

Progress file format:

```json
{
  "phase": "data",
  "overall": {
    "percent_complete": 45.2,
    "tables_completed": 12,
    "tables_total": 25,
    "rows_completed": 1250000,
    "rows_total": 2765000,
    "duration": "5m30s"
  },
  "current_table": {
    "name": "user_events",
    "percent_complete": 78.5,
    "rows_completed": 785000,
    "rows_total": 1000000,
    "speed": "15000 rows/sec"
  },
  "estimated_time_remaining": "2m15s"
}
```

### Job Resumption

Automatically resume interrupted transfers for large databases:

```bash
# Start transfer with specific job ID
postgres-db-fork fork \
  --job-id "prod-to-staging-$(date +%Y%m%d)" \
  --source-db prod_db \
  --target-db staging_db

# Resume if interrupted
postgres-db-fork fork --resume --job-id "prod-to-staging-20241201"
```

Job state includes:

- Completed tables
- Failed tables with error details
- Current transfer phase
- Configuration validation

### Configuration Validation

Prevent CI/CD failures with pre-flight checks:

```bash
# Validate before running in CI/CD
postgres-db-fork validate \
  --source-host $PROD_HOST \
  --source-db $SOURCE_DB \
  --dest-host $STAGING_HOST \
  --target-db $TARGET_DB \
  --output-format json

# Quick connectivity test
postgres-db-fork validate --quick --source-host prod.example.com
```

### Database Discovery

Find and manage databases programmatically:

```bash
# List all PR databases with sizes
postgres-db-fork list \
  --pattern "myapp_pr_*" \
  --show-size \
  --show-age \
  --output-format json

# Check if specific database exists (exit code 0 if found)
postgres-db-fork list --pattern "myapp_pr_123" --quiet

# Count databases matching pattern
postgres-db-fork list --pattern "temp_*" --count-only
```

### Enhanced Error Handling

Intelligent retry logic with exponential backoff:

- **Connection errors**: Retry with increasing delays
- **Resource limits**: Wait for resources to become available
- **Timeout errors**: Retry with longer timeouts
- **Permission errors**: Fail immediately (non-retryable)

```bash
# Custom retry configuration
postgres-db-fork fork \
  --retry-attempts 5 \
  --retry-delay 2s \
  --timeout 60m
```

## Safety Features

- **Read-Only Source Access**: Tool only requires SELECT permissions on source database
- **Connection Validation**: Validates database connections before starting operations
- **Atomic Operations**: Template-based same-server cloning is atomic
- **Progress Monitoring**: Real-time progress reporting for long-running operations
- **Error Recovery**: Robust error handling with detailed error messages

## Performance Benchmarks

| Database Size | Same-Server (Template) | Cross-Server (COPY) | Traditional pg_dump/restore |
|---------------|------------------------|---------------------|---------------------------|
| 100MB         | 5-15 seconds          | 30-60 seconds       | 2-5 minutes              |
| 1GB           | 30-60 seconds         | 2-10 minutes        | 10-30 minutes            |
| 10GB          | 2-5 minutes           | 15-45 minutes       | 1-3 hours                |
| 100GB+        | 10-30 minutes         | 3-12 hours          | 6-24+ hours              |

*Performance varies based on hardware, network, and database complexity.*

## Exit Codes

- `0` - Success
- `1` - Error (configuration, connection, or operation failure)

Perfect for CI/CD automation and error handling.

## Contributing

1. Fork the repository
2. Create a feature branch
3. Add tests for new functionality
4. Submit a pull request

## License

MIT License - see LICENSE file for details.

## Acknowledgments

Inspired by database forking implementations from:

- [Heroku Postgres Fork](https://devcenter.heroku.com/articles/heroku-postgres-fork)
- [DoltHub Database Forks](https://www.dolthub.com/blog/2022-07-29-database-forks/)
- [Cybertec PostgreSQL Forking](https://www.cybertec-postgresql.com/en/forking-databases-the-art-of-copying-without-copying/)

Designed to work with DigitalOcean Managed PostgreSQL and other hosted PostgreSQL services.
