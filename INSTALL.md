# Kube-Agent Installation Guide

The Aerol.ai agent is a lightweight Go binary that runs inside your Kubernetes cluster. It connects outward to the Aerol.ai platform via WebSocket, enabling real-time cluster management without exposing any inbound ports.

## Prerequisites

- Kubernetes cluster (K8s 1.19+ or K3s)
- Helm 3.x installed
- `kubectl` configured with cluster access
- An account on Aerol.ai (vibedoctor.dev)

## Quick Start

### 1. Generate an Agent Token

1. Log into Aerol.ai at https://vibedoctor.dev
2. Navigate to **Kubernetes** > **Add Cluster**
3. Select the **Agent Connection** tab
4. Enter a name for your cluster (e.g., "production", "staging")
5. Click **Generate Token**
6. **Copy the token immediately** — it's only shown once!

### 2. Install via Helm

```bash
# Add the Aerol.ai Helm repository
helm repo add vibedoctor oci://ghcr.io/penify-dev/kube-agent/charts

# Install the agent
helm install vibedoctor-agent vibedoctor/kube-agent \
  --set platform.url="wss://ws.aerol.ai/" \
  --set platform.token="YOUR_TOKEN_HERE" \
  --create-namespace \
  --namespace vibedoctor-agent
```

### 3. Verify Installation

```bash
# Check pod status
kubectl get pods -n vibedoctor-agent

# Check logs
kubectl logs -n vibedoctor-agent -l app.kubernetes.io/name=kube-agent
```

You should see:
```
Agent connected to platform
Kubernetes version: v1.28.x
Nodes: 3
```

The cluster will appear as "Online" in the Aerol.ai dashboard within seconds.

---

## Configuration Options

The Helm chart supports the following values:

| Parameter | Description | Default |
|-----------|-------------|---------|
| `platform.url` | Gateway WebSocket URL | `wss://ws.aerol.ai/` |
| `platform.token` | Agent authentication token | (required) |
| `rbac.scope` | RBAC scope: `cluster` or `namespace` | `cluster` |
| `rbac.namespaces` | Namespaces to access (if scope=namespace) | `["default"]` |
| `resources.requests.cpu` | CPU request | `50m` |
| `resources.requests.memory` | Memory request | `64Mi` |
| `resources.limits.cpu` | CPU limit | `200m` |
| `resources.limits.memory` | Memory limit | `128Mi` |
| `logLevel` | Log verbosity: `debug`, `info`, `warn`, `error` | `info` |

### Example: Namespace-Scoped Access

For multi-tenant clusters, limit the agent to specific namespaces:

```bash
helm install vibedoctor-agent vibedoctor/kube-agent \
  --set platform.url="wss://ws.aerol.ai/" \
  --set platform.token="YOUR_TOKEN" \
  --set rbac.scope="namespace" \
  --set rbac.namespaces="{app-team-a,app-team-b}" \
  --create-namespace \
  --namespace vibedoctor-agent
```

---

## Upgrading

To upgrade to a newer agent version:

```bash
helm repo update vibedoctor
helm upgrade vibedoctor-agent vibedoctor/kube-agent --namespace vibedoctor-agent
```

---

## Uninstalling

```bash
helm uninstall vibedoctor-agent --namespace vibedoctor-agent
kubectl delete namespace vibedoctor-agent
```

**Important:** After uninstalling, revoke the agent token in the Aerol.ai dashboard.

---

## Troubleshooting

### Agent shows "Offline" in dashboard

1. Check pod status:
   ```bash
   kubectl get pods -n vibedoctor-agent
   ```

2. Check logs for connection errors:
   ```bash
   kubectl logs -n vibedoctor-agent -l app.kubernetes.io/name=kube-agent --tail=100
   ```

3. Verify network connectivity:
   ```bash
   kubectl exec -n vibedoctor-agent deploy/vibedoctor-agent -- \
     wget -q -O- --timeout=5 https://ws.aerol.ai/healthz
   ```

### "Unauthorized" or "Invalid token" errors

- Token may have been revoked — generate a new one in the dashboard
- Check that the token was copied correctly (no extra spaces)
- Ensure you're using the correct cluster in the dashboard

### RBAC permission errors

If the agent can't list resources:

1. Check ClusterRole/Role bindings:
   ```bash
   kubectl get clusterrolebinding -l app.kubernetes.io/name=kube-agent
   ```

2. Verify ServiceAccount token:
   ```bash
   kubectl auth can-i list pods --as=system:serviceaccount:vibedoctor-agent:vibedoctor-agent
   ```

### High memory usage

The agent is designed to be lightweight (<64MB). If memory is high:
- Check for many concurrent commands
- Increase limits if needed:
  ```bash
  helm upgrade vibedoctor-agent vibedoctor/kube-agent \
    --set resources.limits.memory=256Mi \
    --namespace vibedoctor-agent
  ```

---

## Security Considerations

### Network Security

- The agent only makes **outbound** connections (WebSocket to the gateway)
- No inbound ports are exposed
- All communication is encrypted (TLS/WSS)
- Works behind NAT/firewalls without configuration

### RBAC Permissions

By default, the agent has cluster-wide read access plus limited write access for deployments and pods. Review the ClusterRole before installation:

```bash
helm template vibedoctor-agent vibedoctor/kube-agent \
  --set platform.token="dummy" | grep -A100 "kind: ClusterRole"
```

### Token Management

- Tokens are hashed (SHA-256) before storage — raw tokens are never persisted
- Revoke tokens immediately when decommissioning clusters
- Use separate tokens per cluster (don't share tokens)
- Rotate tokens periodically for long-lived clusters

---

## Architecture

```
┌────────────────────────────────────────────────┐
│  Your Cluster                                   │
│                                                 │
│  ┌─────────────────────────────────────────┐   │
│  │  vibedoctor-agent pod                    │   │
│  │  - WebSocket client (outbound only)      │   │
│  │  - K8s API client (ServiceAccount)       │   │
│  │  - Executes commands, returns results    │   │
│  └──────────────┬──────────────────────────┘   │
│                 │                               │
│                 │ K8s API (in-cluster)          │
│                 ▼                               │
│  ┌─────────────────────────────────────────┐   │
│  │  Kubernetes API Server                   │   │
│  └─────────────────────────────────────────┘   │
└────────────────────────────────────────────────┘
                  │
                  │ WSS (outbound, port 443/8080)
                  ▼
┌────────────────────────────────────────────────┐
│  Aerol.ai Platform                          │
│                                                 │
│  ws.aerol.ai                         │
│  - WebSocket gateway                           │
│  - Command routing via Redis                   │
│  - Token validation                            │
└────────────────────────────────────────────────┘
```

---

## Support

- Documentation: https://docs.vibedoctor.dev
- Issues: https://github.com/penify-dev/kube-agent/issues
- Community: https://discord.gg/vibedoctor
