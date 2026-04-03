# Build stage
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -X github.com/penify-dev/kube-agent/pkg/ws.Version=${VERSION}" \
    -o /kube-agent ./cmd/agent/

# Runtime stage
FROM gcr.io/distroless/static:nonroot

COPY --from=builder /kube-agent /kube-agent

USER nonroot:nonroot

ENTRYPOINT ["/kube-agent"]
