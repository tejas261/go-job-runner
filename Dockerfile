# Stage 1: Build the binary
FROM golang:1.26.2 AS builder

WORKDIR /app

# Copy everything
COPY . .
RUN go mod download

# FIX: Point the build command to the actual package directory
# We output the binary to /app/server to keep the final copy command simple
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/server ./cmd/server

# Stage 2: Minimal runtime image
FROM gcr.io/distroless/static-debian12

# Copy the binary from the path defined in the build stage
COPY --from=builder /app/server /server

# Good practice for distroless
USER nonroot:nonroot
EXPOSE 5000

ENTRYPOINT ["/server"]
