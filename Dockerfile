# Builder stage using Go 1.23
FROM golang:1.23 AS builder

# Set the working directory inside the builder stage
WORKDIR /build

# Copy package files first (to leverage Docker cache)
COPY go.mod go.sum ./
RUN go mod download

# Now copy the full source code into the builder image
COPY . .

# Build the Go application targeting Linux (explicitly for amd64 to address potential platform differences)
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /build/server ./cmd/server

# Verify the binary was built successfully during the builder phase
RUN echo "DEBUG: Builder binary check:" && ls -l /build/server

# Runtime stage using a minimal Linux distribution
FROM debian:bullseye-slim

# Install CA certificates (for cloud libraries or similar HTTPS requirements)
RUN apt-get update -y && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

# Set the working directory in the runtime stage
WORKDIR /app

# Copy the binary from the builder stage to the runtime image
COPY --from=builder /build/server /app/server

# Ensure the binary is executable
RUN chmod +x /app/server

# Verify the runtime binary exists and is executable
RUN echo "DEBUG: Runtime binary check:" && ls -l /app/server

ENTRYPOINT ["/app/server"]
CMD ["--port=8080"]