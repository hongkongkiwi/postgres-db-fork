# Local CI Testing with Act

This document explains how to run GitHub Actions workflows locally using [act](https://github.com/nektos/act).

## Prerequisites

- Docker installed and running
- `act` installed (use `task act-install` to install automatically)

## Quick Start

1. **Install act** (if not already installed):

   ```bash
   task act-install
   ```

2. **Run all CI checks locally**:

   ```bash
   task act-ci
   ```

3. **Run specific jobs**:

   ```bash
   task act-ci-lint      # Only linting
   task act-ci-test      # Only unit tests
   task act-ci-validate  # Only validation
   task act-ci-fast      # Skip integration tests
   ```

## Available Act Commands

| Command | Description |
|---------|-------------|
| `task act-install` | Install act for your platform |
| `task act-check` | Verify act and Docker are ready |
| `task act-list` | List all available workflows and jobs |
| `task act-ci` | Run complete CI workflow |
| `task act-ci-job JOB=<name>` | Run specific job |
| `task act-ci-lint` | Run linting checks |
| `task act-ci-test` | Run unit tests |
| `task act-ci-validate` | Run validation checks |
| `task act-ci-integration` | Run integration tests (limited) |
| `task act-ci-fast` | Run fast checks (no integration) |
| `task act-debug` | Run with verbose debugging |

## Configuration Files

- **`.actrc`**: Act configuration for optimized local testing
- **`.env.act`**: Environment variables for simulating CI environment
- **`.gitignore`**: Contains `.env.act` (local configuration)

## Integration Tests with Act

⚠️ **Note**: Integration tests with PostgreSQL services may not work perfectly with act due to
service networking limitations. For full integration testing, use:

```bash
task test-integration  # Uses local Docker containers
task test-e2e         # Full end-to-end testing
```

## Workflow Cancellation

The CI workflow includes automatic cancellation of previous runs when pushing to the same branch:

```yaml
concurrency:
  group: ${{ github.workflow }}-${{ github.ref }}
  cancel-in-progress: true
```

This ensures only the latest push triggers CI, saving compute resources and providing faster feedback.

## Troubleshooting

### Docker Issues

```bash
# Check Docker is running
docker info

# Restart Docker service if needed
```

### Act Installation Issues

```bash
# Manual installation on macOS
brew install act

# Manual installation on Linux
curl https://raw.githubusercontent.com/nektos/act/master/install.sh | sudo bash
```

### First Run is Slow

Act downloads Docker images on first run. Subsequent runs are much faster due to image caching.

### Service Networking

If integration tests fail with act, use native testing commands:

```bash
task test-integration    # Better for services
task test-unit          # Fast unit testing
```

## CI vs Local Testing

| Feature | GitHub Actions | Act Local |
|---------|---------------|-----------|
| Unit Tests | ✅ | ✅ |
| Linting | ✅ | ✅ |
| Integration Tests | ✅ | ⚠️ Limited |
| Service Networking | ✅ | ⚠️ May fail |
| Codecov Upload | ✅ | ❌ Skipped |
| Speed | Slower | Faster |
| Cost | Uses CI minutes | Free |
