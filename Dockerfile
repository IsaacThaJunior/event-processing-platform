# Dockerfile.dev (development)
FROM golang:1.26-alpine

WORKDIR /app

# Install git and bash
RUN apk add --no-cache git bash

# Add Go bin to PATH
ENV PATH=$PATH:/go/bin

# Install Air
RUN go install github.com/air-verse/air@latest

# Copy go.mod and go.sum, download deps. pulse/ is a sibling module
# (referenced via a local `replace` in go.mod), so it needs to be present
# at /pulse — matching the ../pulse relative path — before `go mod download`.
COPY go-mid-int-project/go.mod go-mid-int-project/go.sum ./
COPY pulse /pulse
RUN go mod download

# Copy all source code
COPY go-mid-int-project/. .

# Expose port
EXPOSE 8080

# Run Air for hot reload
CMD ["air", "-c", ".air.toml"]