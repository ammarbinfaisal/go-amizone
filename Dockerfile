# syntax=docker/dockerfile:1.3-labs
FROM golang:1.24-alpine as builder

WORKDIR /app

# Copy dependency files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o amizone-api-server cmd/amizone-api-server/server.go

# Final stage
FROM alpine:latest

WORKDIR /app

# Install CA certificates for HTTPS requests
RUN apk --no-cache add ca-certificates

# Copy the binary from builder
COPY --from=builder /app/amizone-api-server .

# Set environment variables
ENV GRPC_GO_LOG_SEVERITY_LEVEL=info
ENV GRPC_GO_LOG_VERBOSITY_LEVEL=99
ENV AMIZONE_API_ADDRESS=0.0.0.0:8081

# Expose the API port
EXPOSE 8081

# Run the server
CMD ["./amizone-api-server"]
