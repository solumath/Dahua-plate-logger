# syntax=docker/dockerfile:1
FROM golang:1.25-bookworm AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o plate_logger .

# ---- final image ----
FROM gcr.io/distroless/static-debian12:nonroot

# Copy timezone data (needed for time.Local() calls)
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

COPY --from=builder /build/plate_logger /plate_logger

WORKDIR /config

VOLUME ["/config", "/data"]

EXPOSE 8080

ENTRYPOINT ["/plate_logger"]
