# ====
# Builder Image
# ====
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app
ENV GOBIN=/app/bin

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o /app/bin/api ./cmd/api
RUN go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

# ====
# Final Image
# ====
FROM alpine:latest

RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app

COPY --from=builder /app/bin/api /app/api
COPY --from=builder /app/bin/migrate /usr/local/bin/migrate
COPY --from=builder /app/migrations /app/migrations
COPY --from=builder /app/frontend /app/frontend

RUN mkdir -p /app/storage/originals /app/storage/variants

EXPOSE 4000
CMD ["/app/api"]