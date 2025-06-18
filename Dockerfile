# Build stage
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git=2.43.0-r0 ca-certificates=20230506-r0 tzdata=2023d-r0

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download && go mod verify

# Copy source code
COPY . .

# Build the application
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o postgres-db-fork \
    main.go

# Runtime stage
FROM alpine:3.19

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    postgresql-client \
    tzdata \
    && rm -rf /var/cache/apk/*

# Create non-root user
RUN addgroup -g 1001 -S appgroup && \
    adduser -u 1001 -S appuser -G appgroup

# Set working directory
WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /app/postgres-db-fork /usr/local/bin/postgres-db-fork

# Make binary executable
RUN chmod +x /usr/local/bin/postgres-db-fork

# Create directories with proper permissions
RUN mkdir -p /app/config /app/logs && \
    chown -R appuser:appgroup /app

# Switch to non-root user
USER appuser

# Set environment variables
ENV PATH="/usr/local/bin:${PATH}"

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD postgres-db-fork --version || exit 1

# Default command
ENTRYPOINT ["postgres-db-fork"]
CMD ["--help"]

# Labels
LABEL maintainer="postgres-db-fork"
LABEL description="PostgreSQL Database Fork Tool"
LABEL version="${VERSION}"
