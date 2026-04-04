# Build stage
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -X github.com/aerol-ai/kubeagent/pkg/ws.Version=${VERSION}" \
    -o /kubeagent ./cmd/agent/

# Runtime stage
FROM gcr.io/distroless/static:nonroot

COPY --from=builder /kubeagent /kubeagent

USER nonroot:nonroot

ENTRYPOINT ["/kubeagent"]
