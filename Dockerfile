# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-s -w" -o cake-autortt .

# Final stage
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache \
    iproute2 \
    iptables \
    ca-certificates

# Create non-root user
RUN addgroup -g 1001 cake && \
    adduser -D -u 1001 -G cake cake

# Copy binary from builder
COPY --from=builder /app/cake-autortt /usr/bin/cake-autortt

# Make binary executable
RUN chmod +x /usr/bin/cake-autortt

# Create necessary directories
RUN mkdir -p /etc /var/run

# Copy default YAML config
COPY etc/cake-autortt.yaml /etc/cake-autortt.yaml

# Set proper permissions
RUN chown -R cake:cake /etc/cake-autortt.yaml /var/run

# Switch to non-root user
USER cake

# Expose any necessary ports (none for this application)
# EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD pgrep cake-autortt || exit 1

# Run the application
ENTRYPOINT ["/usr/bin/cake-autortt"]
CMD ["--debug"]