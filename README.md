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

The agent is best deployed using the provided Helm chart, which is available as an OCI artifact in the GitHub Container Registry.

### Installation via OCI (Recommended)

1. Install the agent directly from the OCI registry:

```bash
helm install kube-agent oci://ghcr.io/penify-dev/charts/kube-agent \
  --version 0.1.0 \
  --namespace aerol-system \
  --create-namespace \
  --set platform.token="YOUR_AGENT_TOKEN"
```

### Installation from Source

If you have the repository cloned locally, you can install using the local chart folder:

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
| `platform.token` | Authentication token for the agent. **Required.** | `""` |
| `platform.url` | Aerol.ai platform WebSocket URL | `wss://ws.aerol.ai` |
| `logLevel` | Logging level (`debug`, `info`, `warn`, `error`) | `info` |
| `rbac.create` | Whether to create RBAC resources (see [RBAC Configuration](#rbac-configuration) below) | `true` |
| `rbac.scope` | Scope of the RBAC role: `ClusterRole` (cluster-wide) or `Role` (namespace-only). See [RBAC Configuration](#rbac-configuration) | `ClusterRole` |
| `rbac.namespaces` | Only used when `rbac.scope=Role`. List of namespaces the agent can access. Defaults to the install namespace if empty. | `[]` |

> **Secrets are managed automatically.** When you install the chart, a Kubernetes Secret is created in the target namespace containing the `platform.token` and `platform.url`. You do not need to create or manage secrets manually. The namespace is either `aerol-system` (the recommended default) or whatever namespace you specify with `--namespace`.

### RBAC Configuration

The agent requires Kubernetes RBAC permissions to discover and manage resources. The chart creates the necessary RBAC resources automatically when `rbac.create` is `true` (the default).

#### Understanding `rbac.scope`

| Scope | Kind Created | Access Level | Use Case |
|-------|-------------|--------------|----------|
| `ClusterRole` (default) | `ClusterRole` + `ClusterRoleBinding` | Agent can read and manage resources **across all namespaces** in the cluster | Full cluster visibility - recommended for most installations |
| `Role` | `Role` + `RoleBinding` | Agent can only access resources **within the namespace it is deployed to** | Restricted environments where the agent should not see or touch resources outside its namespace |

#### Option 1: Full Cluster Access (default)

This is the default. The agent gets a `ClusterRole` covering everything needed to deploy and manage full Helm chart stacks (including complex ones like server environments, operators, and monitoring stacks):

| API Group | Resources | Access | Why |
|-----------|-----------|--------|-----|
| Core (`""`) | pods, services, configmaps, secrets, PVCs, events, namespaces, serviceaccounts | Full read/write | Core workload lifecycle |
| `apps` | deployments, statefulsets, daemonsets, replicasets | Full read/write | Workload management |
| `batch` | jobs, cronjobs | Full read/write | Batch workloads |
| `networking.k8s.io` | ingresses, networkpolicies | Full read/write | Traffic routing |
| `autoscaling`, `policy` | HPAs, PDBs | Full read/write | Scaling and availability |
| `rbac.authorization.k8s.io` | roles, rolebindings, clusterroles, clusterrolebindings | Full read/write | Required for Helm charts that create their own ServiceAccount bindings (nearly every production chart does this - without it, installs fail) |
| `apiextensions.k8s.io` | customresourcedefinitions | Full read/write | Required for charts that ship CRDs (cert-manager, operators, monitoring stacks) |
| `admissionregistration.k8s.io` | validating/mutatingwebhookconfigurations | Full read/write | Required for charts that install admission webhooks |
| `storage.k8s.io` | storageclasses | Read + patch/update | Allows enabling `allowVolumeExpansion` on a StorageClass so PVC resize works |
| `storage.k8s.io` | volumeattachments, csinodes, csidrivers | Read-only | Inspect cluster storage drivers and attachment state |

> **On "reading storage content":** Kubernetes RBAC controls access to API resources - it does not control what is inside a mounted volume or a running pod's filesystem. The agent has no mechanism to read application data from PVCs. That data is only accessible from within a pod that mounts the volume.

> **PVC resize flow:** To expand a PVC, the agent patches `spec.resources.requests.storage` on the PVC (a core resource, always writable). For this to succeed, the StorageClass used by that PVC must have `allowVolumeExpansion: true`. The agent can now patch StorageClasses to enable this if it isn't already set.

```bash
# Default install - full cluster access
helm install kube-agent oci://ghcr.io/penify-dev/charts/kube-agent \
  --namespace aerol-system \
  --create-namespace \
  --set platform.token="YOUR_AGENT_TOKEN"
```

#### Option 2: Namespace-Scoped Access (restricted)

If your security policy does not allow cluster-wide access, set `rbac.scope=Role`. The agent will only manage resources within the namespaces you specify. A `Role` + `RoleBinding` is created in **each** target namespace, with the agent's `ServiceAccount` (in its own install namespace) as the subject.

**Single namespace (defaults to the install namespace):**

```bash
helm install kube-agent oci://ghcr.io/penify-dev/charts/kube-agent \
  --namespace aerol-system \
  --create-namespace \
  --set platform.token="YOUR_AGENT_TOKEN" \
  --set rbac.scope="Role"
```

**Multiple specific namespaces (via `--set`):**

> **Helm array syntax:** When passing a list on the command line with `--set`, wrap values in curly braces `{}` and separate with commas — **no spaces**. Do not use square brackets.
> ```
> --set rbac.namespaces="{production,staging,dev}"   ✅ correct
> --set rbac.namespaces="production,staging,dev"     ❌ wrong — treated as a string
> --set rbac.namespaces=["production","staging"]     ❌ wrong — invalid syntax
> ```

```bash
helm install kube-agent oci://ghcr.io/penify-dev/charts/kube-agent \
  --namespace aerol-system \
  --create-namespace \
  --set platform.token="YOUR_AGENT_TOKEN" \
  --set rbac.scope="Role" \
  --set "rbac.namespaces={production,staging,dev}"
```

**Multiple namespaces via a values file (recommended — easier to read and maintain):**

Create a file (e.g. `my-values.yaml`) with a YAML list under `rbac.namespaces`. Each namespace is a separate `- item` entry:

```yaml
# my-values.yaml
rbac:
  scope: "Role"
  namespaces:
    - production
    - staging
    - dev
```

Then pass the file with `-f`:

```bash
helm install kube-agent oci://ghcr.io/penify-dev/charts/kube-agent \
  --namespace aerol-system \
  --create-namespace \
  --set platform.token="YOUR_AGENT_TOKEN" \
  -f my-values.yaml
```

To update the namespaces after install (e.g. add `qa`):

```bash
helm upgrade kube-agent oci://ghcr.io/penify-dev/charts/kube-agent \
  --namespace aerol-system \
  --set platform.token="YOUR_AGENT_TOKEN" \
  --set "rbac.namespaces={production,staging,dev,qa}" \
  --set rbac.scope="Role"
```

With `Role` scope, the agent has the same resource permissions (pods, deployments, services, secrets, PVCs, etc.) but strictly bounded to the listed namespaces. Cluster-wide resources like nodes, persistent volumes, storage classes, and CRDs are not accessible.

#### Option 3: Bring Your Own RBAC

If you want full control over what the agent can access, disable RBAC creation entirely and bind the agent's ServiceAccount to your own Role or ClusterRole:

```bash
helm install kube-agent oci://ghcr.io/penify-dev/charts/kube-agent \
  --namespace aerol-system \
  --create-namespace \
  --set platform.token="YOUR_AGENT_TOKEN" \
  --set rbac.create=false \
  --set serviceAccount.name="my-custom-sa"
```

Then create your own RBAC rules. For example, a minimal read-only ClusterRole:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kube-agent-readonly
rules:
  - apiGroups: [""]
    resources: ["pods", "services", "configmaps", "events", "namespaces"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["apps"]
    resources: ["deployments", "replicasets", "statefulsets", "daemonsets"]
    verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: kube-agent-readonly
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: kube-agent-readonly
subjects:
  - kind: ServiceAccount
    name: my-custom-sa
    namespace: aerol-system
```

> **Tip for DevOps teams:** Start with `rbac.scope=Role` for the most restrictive default. If the agent needs to discover resources across namespaces, switch to `ClusterRole`. If you need fine-grained control (e.g., read-only in some namespaces, read-write in others), use `rbac.create=false` and manage RBAC separately.

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
