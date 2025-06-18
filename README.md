# PostgreSQL Database Fork Tool

A high-performance CLI tool for forking (copying) PostgreSQL databases, supporting both same-server and cross-server operations with intelligent optimization.

## Features

- **Smart Forking Strategy**: Automatically detects same-server vs cross-server scenarios
- **Same-Server Optimization**: Uses PostgreSQL's efficient template-based database cloning
- **Cross-Server Support**: Parallel data transfer with configurable concurrency
- **Progress Monitoring**: Real-time feedback and size reporting
- **Flexible Configuration**: CLI flags, configuration files, or environment variables
- **Table Filtering**: Include/exclude specific tables from the fork operation
- **Schema-Only/Data-Only Options**: Transfer just schema or just data as needed
- **Robust Error Handling**: Comprehensive validation and error recovery

## Installation

### From Source

```bash
git clone <repository-url>
cd postgres-db-fork

# Using Task (recommended)
task build

# Or directly with Go
go build -o postgres-db-fork main.go
```

### Prerequisites

- Go 1.21 or later
- [Task](https://taskfile.dev/#/installation) (optional but recommended)
- Access to PostgreSQL database(s)

### Pre-built Binaries

Download the latest release for your platform from the [releases page](releases).

## Quick Start

### Same-Server Fork (Most Efficient)

Fork a database within the same PostgreSQL server:

```bash
postgres-db-fork fork \
  --source-host localhost \
  --source-user myuser \
  --source-password mypass \
  --source-db production_app \
  --target-db development_app
```

### Cross-Server Fork

Fork a database from one server to another:

```bash
postgres-db-fork fork \
  --source-host prod.example.com \
  --source-user produser \
  --source-password prodpass \
  --source-db myapp_prod \
  --dest-host dev.example.com \
  --dest-user devuser \
  --dest-password devpass \
  --target-db myapp_dev \
  --drop-if-exists
```

### Configuration File

Create a configuration file for repeated operations:

```bash
postgres-db-fork fork --config my-fork-config.yaml
```

## Command Reference

### Global Options

- `--config`: Configuration file path
- `--log-level`: Log level (debug, info, warn, error)
- `--verbose`: Enable verbose output

### Fork Command Options

#### Source Database
- `--source-host`: Source database host (default: localhost)
- `--source-port`: Source database port (default: 5432)
- `--source-user`: Source database username
- `--source-password`: Source database password
- `--source-db`: Source database name (required)
- `--source-sslmode`: SSL mode (default: prefer)

#### Destination Database
- `--dest-host`: Destination host (defaults to source-host)
- `--dest-port`: Destination port (defaults to source-port)
- `--dest-user`: Destination username (defaults to source-user)
- `--dest-password`: Destination password (defaults to source-password)
- `--dest-sslmode`: Destination SSL mode (defaults to source-sslmode)

#### Target Database
- `--target-db`: Target database name (required)
- `--drop-if-exists`: Drop target database if it exists

#### Transfer Options
- `--max-connections`: Maximum parallel connections (default: 4)
- `--chunk-size`: Rows per batch (default: 1000)
- `--timeout`: Operation timeout (default: 30m)
- `--exclude-tables`: Tables to exclude (comma-separated)
- `--include-tables`: Tables to include (if specified, only these are transferred)
- `--schema-only`: Transfer schema only, no data
- `--data-only`: Transfer data only, no schema

## Configuration File Format

Create a YAML configuration file for complex scenarios:

```yaml
source:
  host: prod.example.com
  port: 5432
  username: produser
  password: prodpass
  database: myapp_production
  sslmode: require

destination:
  host: dev.example.com
  port: 5432
  username: devuser
  password: devpass
  sslmode: prefer

target_database: myapp_development
drop_if_exists: true
max_connections: 8
chunk_size: 2000
timeout: 45m

exclude_tables:
  - audit_logs
  - temp_data
  - session_data

# Or use include_tables to only transfer specific tables
# include_tables:
#   - users
#   - products
#   - orders
```

## How It Works

### Same-Server Fork (Template-Based)

When source and destination are on the same PostgreSQL server, the tool uses PostgreSQL's `CREATE DATABASE ... WITH TEMPLATE` feature:

1. Connects to the PostgreSQL server
2. Validates source database exists
3. Handles target database (drop if exists, if requested)
4. Creates new database using source as template
5. Reports completion with size information

This approach is extremely fast as PostgreSQL handles the copy at the storage level.

### Cross-Server Fork (Data Transfer)

When source and destination are on different servers:

1. **Schema Transfer**: Extracts and recreates table structures, indexes, and constraints
2. **Data Transfer**: Transfers data table-by-table with configurable parallelism
3. **Progress Monitoring**: Real-time feedback on transfer progress
4. **Error Recovery**: Robust handling of connection issues and data conflicts

## Performance Considerations

### Same-Server Performance
- **Speed**: Near-instantaneous for small databases, scales with storage I/O
- **Resources**: Minimal CPU/memory overhead  
- **Method**: Uses PostgreSQL's `CREATE DATABASE ... WITH TEMPLATE` for maximum efficiency
- **Limitations**: Both databases must be on same PostgreSQL instance

### Cross-Server Performance Optimizations
The tool implements several performance optimizations for cross-server forks:

#### Database-Level Optimizations
- **WAL Optimization**: Temporarily disables synchronous commits during bulk loading
- **Memory Settings**: Increases buffer sizes for faster writes
- **Checkpoint Tuning**: Optimizes checkpoint behavior for bulk operations

#### Data Transfer Optimizations  
- **COPY Protocol**: Uses PostgreSQL's native COPY for maximum throughput (10-50x faster than INSERT)
- **Streaming Transfer**: Data streams directly from source to destination without intermediate storage
- **Parallel Processing**: Multiple tables transferred simultaneously with configurable concurrency
- **Binary Format**: Uses efficient CSV format with optimized delimiters

#### Network Optimizations
- **Connection Pooling**: Reuses database connections across table transfers
- **Pipelining**: Overlaps read and write operations for continuous data flow
- **Batching**: Large chunk sizes reduce network round trips

### Recommended Settings

| Database Size | Max Connections | Chunk Size | Expected Speed* |
|---------------|----------------|------------|----------------|
| < 1GB        | 2-4            | 1000       | 2-10 minutes   |
| 1-10GB       | 4-8            | 2000       | 5-30 minutes   |
| 10-100GB     | 8-16           | 5000       | 30-180 minutes |
| > 100GB      | 16-32          | 10000      | 3-12 hours     |

*Speed estimates for cross-server transfers on typical cloud infrastructure (1Gbps network)

#### Performance Tips
- **Use Read-Only Source User**: Creates a dedicated read-only user for maximum safety
- **High-Performance Config**: See `examples/performance-config.yaml` for optimized settings
- **Network Bandwidth**: Performance scales linearly with network speed between servers
- **Exclude Large Tables**: Use `exclude_tables` to skip audit logs and temporary data
- **Monitor Progress**: Use `--log-level debug` to see detailed transfer progress

## Use Cases

### Development Workflows
- Create development copies of production data
- Set up staging environments
- Isolate feature development with real data

### Testing and QA
- Create test databases with production-like data
- Validate migrations before production deployment
- Performance testing with realistic datasets

### Data Analytics
- Create analytical copies without affecting production
- Historical snapshots for reporting
- Data science experimentation

### Disaster Recovery
- Create backup copies on different infrastructure
- Geographic distribution of data
- Migration between cloud providers

## Troubleshooting

### Common Issues

**Connection Errors**
```bash
# Test connectivity first
psql -h hostname -U username -d database_name
```

**Permission Errors**
- Ensure user has `CREATEDB` privilege for same-server forks
- Verify user can connect to both source and destination

**Memory Issues**
- Reduce `--chunk-size` for large tables
- Decrease `--max-connections` to reduce memory usage

**Timeout Issues**
- Increase `--timeout` for very large databases
- Consider using `--schema-only` first, then `--data-only`

### Logging

Enable debug logging for detailed operation information:

```bash
postgres-db-fork fork --log-level debug --source-db mydb --target-db mycopy
```

## Security Considerations

### Source Database Access
- **Read-Only Required**: The tool only needs **SELECT** permissions on the source database
- **No Write Operations**: Source database is never modified during fork operations
- **Dedicated User**: Create a dedicated read-only user for maximum security:

```sql
-- Create read-only user for database forking
CREATE USER fork_reader WITH PASSWORD 'secure_password';
GRANT CONNECT ON DATABASE your_database TO fork_reader;
GRANT USAGE ON SCHEMA public TO fork_reader;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO fork_reader;
GRANT SELECT ON ALL SEQUENCES IN SCHEMA public TO fork_reader;

-- For future tables
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO fork_reader;
```

### General Security
- Store passwords in configuration files with appropriate file permissions (600)
- Use environment variables for sensitive credentials
- Consider using PostgreSQL's `.pgpass` file for password management
- Enable SSL connections in production environments
- Use connection limits and timeouts to prevent resource exhaustion

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests for new functionality
5. Submit a pull request

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Acknowledgments

Inspired by database forking implementations from:
- [Heroku Postgres Fork](https://devcenter.heroku.com/articles/heroku-postgres-fork)
- [DoltHub Database Forks](https://www.dolthub.com/blog/2022-07-29-database-forks/)
- [Cybertec PostgreSQL Forking](https://www.cybertec-postgresql.com/en/forking-databases-the-art-of-copying-without-copying/)

Designed to work with DigitalOcean Managed PostgreSQL and other hosted PostgreSQL services. 