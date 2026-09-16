# ====
# Builder Image
# ====

FROM golang:1.26-alpine AS builder

# Dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

ENV GOBIN=/app/bin

# Copy go.mod and go.sum files
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code
COPY . .
RUN go mod download

# Build API binary
RUN go build -o /app/bin/api ./cmd/api

# Install golang-migrate tool for DB config
RUN go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

# ====
# Full Image
# ====

FROM alpine:latest

# Install ca-certificates for HTTPS support and tzdata for timezone support
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Copy compiled API binary from the builder stage
COPY --from=builder /app/bin/api /app/api

# copy the migrate binary from the builder stage to /usr/local/bin
COPY --from=builder /app/bin/migrate /usr/local/bin/migrate

# Copy migrations folder
COPY --from=builder /app/migrations /app/migrations

# Copy frontend static files
COPY --from=builder /app/frontend /app/frontend

# Create local file storage
RUN mkdir -p /app/storage/originals
RUN mkdir -p /app/storage/variants

# Expose API port
EXPOSE 4000

# Default command to run the API
CMD ["/app/api"]

