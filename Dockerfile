# syntax=docker/dockerfile:1

# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /build

# Install ca-certificates for HTTPS
RUN apk add --no-cache ca-certificates

# Copy go mod files first for better layer caching
COPY go.mod ./

# Download dependencies (cached if go.mod unchanged)
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy source code
COPY . .

# Build the binary
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o selfhost_s3 ./cmd/selfhost_s3

# Runtime stage
FROM alpine:3.21

# Labels
LABEL org.opencontainers.image.title="selfhost_s3"
LABEL org.opencontainers.image.description="Minimal S3-compatible object storage server"
LABEL org.opencontainers.image.source="https://github.com/Notifuse/selfhost_s3"
LABEL org.opencontainers.image.vendor="Notifuse"

# Install ca-certificates for HTTPS and create non-root user
RUN apk add --no-cache ca-certificates \
    && addgroup -g 1000 selfhosts3 \
    && adduser -u 1000 -G selfhosts3 -s /bin/sh -D selfhosts3

# Create data directory
RUN mkdir -p /data && chown selfhosts3:selfhosts3 /data

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/selfhost_s3 .

# Use non-root user
USER selfhosts3

# Expose port
EXPOSE 9000

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:9000/health || exit 1

# Default environment variables
ENV S3_PORT=9000 \
    S3_STORAGE_PATH=/data \
    S3_REGION=us-east-1

# Volume for persistent storage
VOLUME ["/data"]

# Run the server
ENTRYPOINT ["./selfhost_s3"]
