# ----------------------------------------------------
# STAGE 1: BUILDER
# This stage compiles the Go application into a static binary.
# ----------------------------------------------------
FROM golang:1.24.5 AS builder

# Set the working directory inside the container
WORKDIR /app

# Assuming the build context is the 'server/' directory, 
# 'go.mod' and 'go.sum' are directly accessible here.
COPY go.mod ./
COPY go.sum ./

# Download dependencies first to leverage Docker's layer caching.
# This step only runs if go.mod or go.sum changes.
RUN go mod download

# Copy the rest of the application source code
COPY . .

# Compile the static binary. CGO_ENABLED=0 creates a binary with no external C dependencies,
# allowing it to run on minimal base images like Alpine or Scratch.
# We output the binary as 'server_app'
RUN CGO_ENABLED=0 GOOS=linux go build -o server_app .

# ----------------------------------------------------
# STAGE 2: FINAL (Minimal Runtime)
# This stage copies only the compiled binary for a tiny, secure image.
# ----------------------------------------------------
FROM alpine:latest

# Install ca-certificates needed for secure connections (HTTPS)
RUN apk --no-cache add ca-certificates

# Set the working directory
WORKDIR /app

# Copy the compiled binary from the 'builder' stage
# The binary is named 'server_app'
COPY --from=builder /app/server_app .

# Expose both the primary application port (8080) and the newly requested port (7000)
# NOTE: The NATS port (4222) is an OUTBOUND connection and does not need to be exposed here.
EXPOSE 8080 7000

# Execute the compiled binary directly. 
# We replace the inefficient 'go run' with the executable binary itself.
CMD ["./server_app", "--id", "2", "--peers", "10.65.128.217:7000"]
