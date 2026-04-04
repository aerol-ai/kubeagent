# Build stage
FROM golang:1.25-alpine AS builder

ARG HELM_VERSION=v3.18.6

RUN apk add --no-cache git ca-certificates curl tar

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN curl -fsSL "https://get.helm.sh/helm-${HELM_VERSION}-linux-amd64.tar.gz" -o /tmp/helm.tgz \
 && tar -xzf /tmp/helm.tgz -C /tmp \
 && mv /tmp/linux-amd64/helm /helm \
 && chmod +x /helm

ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -X github.com/aerol-ai/kubeagent/pkg/ws.Version=${VERSION}" \
    -o /kubeagent ./cmd/agent/

# Runtime stage
FROM gcr.io/distroless/static:nonroot

COPY --from=builder /kubeagent /kubeagent
COPY --from=builder /helm /usr/local/bin/helm

USER nonroot:nonroot

ENTRYPOINT ["/kubeagent"]
