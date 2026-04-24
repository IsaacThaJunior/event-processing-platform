# Dockerfile.dev (development)
FROM golang:1.26-alpine

WORKDIR /app

# Install git and bash
RUN apk add --no-cache git bash

# Add Go bin to PATH
ENV PATH=$PATH:/go/bin

# Install Air
RUN go install github.com/air-verse/air@latest

# Copy go.mod and go.sum, download deps
COPY go.mod ./
COPY go.sum ./
RUN go mod download

# Copy all source code
COPY . .

# Expose port
EXPOSE 8080

# Run Air for hot reload
CMD ["air", "-c", ".air.toml"]