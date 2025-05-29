# Build stage
FROM golang:1.22-alpine AS builder

# Add CA certificates and necessary tools
RUN apk --no-cache add ca-certificates git

WORKDIR /app

# Leverage caching for go.mod dependencies first
COPY go.mod go.sum ./
RUN go mod download

# Copy the full source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o voicemail-transcriber ./cmd

# Final runtime stage
FROM scratch

# Import the compiled Go binary and CA certificates
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/voicemail-transcriber /voicemail-transcriber

# Set a non-root user (optional but recommended)
USER 1001:1001

# Set execution entry
ENTRYPOINT ["/voicemail-transcriber"]

# Expose the port used by your Go app
EXPOSE 8080