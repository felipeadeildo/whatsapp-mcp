# Build stage
FROM golang:alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache gcc musl-dev

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build application
RUN CGO_ENABLED=1 GOOS=linux go build -o whatsapp-mcp

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates sqlite curl

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/whatsapp-mcp .
COPY schema.sql .

# Create data directory
RUN mkdir -p ./data

# Expose MCP server port
EXPOSE 8080

# Run application
CMD ["./whatsapp-mcp"]
