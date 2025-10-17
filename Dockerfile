# Multi-stage Dockerfile for J.E.E.V.E.S. agents
FROM golang:1.23-alpine AS builder

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
# Allow Go to auto-download newer toolchain if needed
ENV GOTOOLCHAIN=auto
RUN go mod download

# Copy source code
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY pkg/ ./pkg/

# Build all agents
RUN go build -o collector-agent ./cmd/collector-agent
RUN go build -o illuminance-agent ./cmd/illuminance-agent
RUN go build -o light-agent ./cmd/light-agent
RUN go build -o occupancy-agent ./cmd/occupancy-agent
RUN go build -o behavior-agent ./cmd/behavior-agent
RUN go build -o observer-agent ./cmd/observer-agent

# Collector agent
FROM alpine:latest AS collector
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /build/collector-agent .
ENTRYPOINT ["./collector-agent"]

# Illuminance agent
FROM alpine:latest AS illuminance-agent
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /build/illuminance-agent .
ENTRYPOINT ["./illuminance-agent"]

# Light agent
FROM alpine:latest AS light-agent
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /build/light-agent .
ENTRYPOINT ["./light-agent"]

# Occupancy agent
FROM alpine:latest AS occupancy-agent
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /build/occupancy-agent .
ENTRYPOINT ["./occupancy-agent"]

FROM alpine:latest AS behavior-agent
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /build/behavior-agent .
ENTRYPOINT ["./behavior-agent"]

FROM alpine:latest AS observer-agent
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /build/observer-agent .
ENTRYPOINT ["./observer-agent"]
