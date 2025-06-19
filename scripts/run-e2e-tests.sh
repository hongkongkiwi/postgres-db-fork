#!/bin/bash

# E2E Test Runner for postgres-db-fork
# This script sets up the environment and runs comprehensive e2e tests

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if Docker is available
check_docker() {
    # In CI, we assume Docker is available and managed by the environment.
    if [ -n "$CI" ]; then
        print_info "CI environment detected. Skipping Docker check."
        return
    fi
    print_info "Checking Docker availability..."

    if ! command -v docker &> /dev/null; then
        print_error "Docker is not installed or not in PATH"
        print_error "Please install Docker to run e2e tests"
        exit 1
    fi

    if ! docker info &> /dev/null; then
        print_error "Docker daemon is not running"
        print_error "Please start Docker and try again"
        exit 1
    fi

    print_success "Docker is available and running"
}

# Clean up any existing test containers
cleanup_containers() {
    # In CI, service containers are managed by the workflow, so we shouldn't interfere.
    if [ -n "$CI" ]; then
        print_warning "CI environment detected. Skipping container cleanup to preserve service containers."
        return
    fi
    print_info "Cleaning up existing test containers..."

    # Remove any containers with postgres image that might be left over
    docker ps -a --filter "ancestor=postgres" --format "{{.ID}}" | xargs -r docker rm -f

    # Clean up volumes
    docker volume prune -f &> /dev/null || true

    print_success "Cleanup completed"
}

# Run e2e tests
run_e2e_tests() {
    print_info "Running E2E tests with real PostgreSQL containers..."

    # Set environment variables for the test
    export POSTGRES_E2E_TEST=true
    export DOCKER_API_VERSION=1.40

    # Run the tests with verbose output and test-by-test timing
    if go test -v -tags=e2e ./... -timeout=30m | while IFS= read -r line; do
        if [[ "$line" == "=== RUN"* ]]; then
            printf "[%s] %s\\n" "$(date '+%Y-%m-%d %H:%M:%S')" "$line"
        else
            echo "$line"
        fi
    done; then
        print_success "All E2E tests passed!"
        return 0
    else
        print_error "Some E2E tests failed"
        return 1
    fi
}

# Generate coverage report for e2e tests
generate_coverage() {
    print_info "Generating coverage report for E2E tests..."

    go test -v -tags=e2e -coverprofile=coverage-e2e.out ./... -timeout=30m | while IFS= read -r line; do
        if [[ "$line" == "=== RUN"* ]]; then
            printf "[%s] %s\\n" "$(date '+%Y-%m-%d %H:%M:%S')" "$line"
        else
            echo "$line"
        fi
    done
    go tool cover -html=coverage-e2e.out -o coverage-e2e.html
    go tool cover -func=coverage-e2e.out

    print_success "Coverage report generated at coverage-e2e.html"
}

# Main execution
main() {
    print_info "Starting E2E test execution..."
    print_info "=================================="

    # Check prerequisites
    check_docker

    # Clean up before starting
    cleanup_containers

    # Run tests based on arguments
    case "${1:-run}" in
        "run")
            run_e2e_tests
            ;;
        "coverage")
            generate_coverage
            ;;
        "clean")
            cleanup_containers
            print_success "Cleanup completed"
            ;;
        "help"|"-h"|"--help")
            echo "Usage: $0 [run|coverage|clean|help]"
            echo ""
            echo "Commands:"
            echo "  run      Run E2E tests (default)"
            echo "  coverage Run E2E tests with coverage report"
            echo "  clean    Clean up Docker containers and volumes"
            echo "  help     Show this help message"
            exit 0
            ;;
        *)
            print_error "Unknown command: $1"
            print_error "Use '$0 help' for usage information"
            exit 1
            ;;
    esac

    # Final cleanup
    cleanup_containers

    print_success "E2E test execution completed!"
}

# Run main function with all arguments
main "$@"
