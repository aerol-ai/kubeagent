# Aerol.ai Kubernetes Agent

The Aerol.ai Kubernetes Agent is a robust, asynchronous orchestration engine designed to connect your Kubernetes clusters to the Aerol.ai platform. It handles deployment synchronization, health monitoring, and drift detection.

## Features

- **Asynchronous Orchestration**: Efficiently manages Kubernetes resources without blocking.
- **WebSocket Connectivity**: Real-time communication with the Aerol.ai platform.
- **Helm Integration**: Easy deployment and management via Helm.
- **Health Monitoring**: Continuous monitoring of deployed resources.

## Prerequisites

- **Go**: 1.24 or later (for local development).
- **Kubernetes Cluster**: Access to a running Kubernetes cluster.
- **Helm**: 3.x or later.
- **Docker**: For containerization.

## Local Development

### Build from source

```bash
go build -o kube-agent ./cmd/agent/
```

### Run locally

```bash
./kube-agent --config config.yaml
```

## Helm Deployment

The agent is best deployed using the provided Helm chart.

### Installation

1. Install the agent using the local chart:

```bash
helm install kube-agent ./deploy/helm/kube-agent \
  --namespace aerol-system \
  --create-namespace \
  --set platform.token="YOUR_AGENT_TOKEN"
```

### Configuration

Key configuration options in `values.yaml`:

| Parameter | Description | Default |
|-----------|-------------|---------|
| `platform.url` | Aerol.ai platform WebSocket URL | `wss://app.aerol.ai/agent/connect` |
| `platform.token` | Authentication token for the agent | `""` |
| `replicaCount` | Number of agent replicas | `1` |
| `image.repository` | Docker image repository | `ghcr.io/penify-dev/kube-agent` |
| `image.tag` | Docker image tag | `0.1.0` |
| `logLevel` | Logging level (`debug`, `info`, `warn`, `error`) | `info` |
| `rbac.create` | Whether to create RBAC resources | `true` |
| `rbac.scope` | Scope of the RBAC role (`ClusterRole` or `Role`) | `ClusterRole` |

### Uninstallation

To uninstall the agent:

```bash
helm uninstall kube-agent --namespace aerol-system
```

## CI/CD

This project uses GitHub Actions for continuous integration and deployment. The workflow includes:
- **Go Build & Test**: Automatically builds the app and runs tests on every pull request and push to `main`.
- **Docker Build & Push**: Builds and pushes the Docker image to GHCR on every push to `main`.
- **Helm Build & Lint**: Lints the Helm chart and packages it for deployment.

## License

This project is licensed under the Apache License 2.0.
