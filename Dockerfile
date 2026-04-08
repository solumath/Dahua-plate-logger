FROM --platform=$BUILDPLATFORM golang:1.25-bookworm AS builder
ARG TARGETARCH

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -ldflags="-s -w" -o plate_logger .

# ---- final image ----
FROM debian:bookworm-slim

COPY --from=builder /build/plate_logger /plate_logger

VOLUME ["/config", "/data"]

ENTRYPOINT ["/plate_logger"]
