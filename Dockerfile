# Build stage
FROM golang:1.25.5-alpine3.20 AS builder

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
FROM alpine:3.20

RUN apk --no-cache add ca-certificates sqlite curl tzdata

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/whatsapp-mcp .
COPY schema.sql .

# Expose MCP server port
EXPOSE 8080

# Run application
CMD ["./whatsapp-mcp"]
