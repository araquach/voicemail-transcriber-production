# 🛠 Build stage with Go 1.23
FROM golang:1.23 as builder

WORKDIR /app

# Cache deps first
COPY go.mod go.sum ./
RUN go mod download

# Copy everything and build
COPY . .
RUN go build -o server ./cmd/server

# 🏁 Runtime stage
FROM debian:bullseye-slim

# Install CA certificates (needed for HTTPS)
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy the binary
COPY --from=builder /app/server .

# ✅ Set the default entrypoint
CMD ["./server"]