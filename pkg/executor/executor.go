package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/penify-dev/kube-agent/pkg/config"
	"github.com/penify-dev/kube-agent/pkg/k8s"
	"github.com/penify-dev/kube-agent/pkg/tools"
	"github.com/penify-dev/kube-agent/pkg/topology"
)

// Executor dispatches commands to the appropriate tool.
type Executor struct {
	pods        *tools.PodTools
	deployments *tools.DeploymentTools
	services    *tools.ServiceTools
	cluster     *tools.ClusterTools
	apply       *tools.ApplyTools
	topology    *topology.Builder
	traffic     *topology.TrafficMapper
	timeout     time.Duration
}

// NewExecutor creates a new command executor.
func NewExecutor(clients *k8s.Clients, cfg *config.Config) *Executor {
	return &Executor{
		pods:        tools.NewPodTools(clients.Clientset),
		deployments: tools.NewDeploymentTools(clients.Clientset),
		services:    tools.NewServiceTools(clients.Clientset),
		cluster:     tools.NewClusterTools(clients.Clientset),
		apply:       tools.NewApplyTools(clients.Clientset, clients.DynamicClient, clients.RestConfig),
		topology:    topology.NewBuilder(clients.Clientset),
		traffic:     topology.NewTrafficMapper(clients.Clientset),
		timeout:     cfg.CommandTimeout,
	}
}

// Execute runs a command and returns the result.
func (e *Executor) Execute(cmd config.Command) config.Result {
	ctx, cancel := context.WithTimeout(context.Background(), e.timeout)
	defer cancel()

	data, err := e.dispatch(ctx, cmd.Tool, cmd.Input)
	if err != nil {
		return config.Result{
			RequestID: cmd.RequestID,
			Success:   false,
			Error:     err.Error(),
		}
	}
	return config.Result{
		RequestID: cmd.RequestID,
		Success:   true,
		Data:      data,
	}
}

func (e *Executor) dispatch(ctx context.Context, tool string, input json.RawMessage) (interface{}, error) {
	switch tool {
	// --- Pods ---
	case "pods_list":
		var in config.NamespaceInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		return e.pods.ListPods(ctx, in.Name)
	case "pods_get":
		var in config.PodInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		return e.pods.GetPod(ctx, in.Namespace, in.Name)
	case "pods_logs":
		var in config.PodLogsInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		return e.pods.GetPodLogs(ctx, in.Namespace, in.Name, in.Container, in.TailLines, in.Previous)
	case "pods_delete":
		var in config.PodInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		return nil, e.pods.DeletePod(ctx, in.Namespace, in.Name)

	// --- Deployments ---
	case "deployments_list":
		var in config.NamespaceInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		return e.deployments.ListDeployments(ctx, in.Name)
	case "deployments_get":
		var in config.DeploymentInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		return e.deployments.GetDeployment(ctx, in.Namespace, in.Name)
	case "deployments_scale":
		var in config.ScaleInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		return nil, e.deployments.ScaleDeployment(ctx, in.Namespace, in.Name, in.Replicas)
	case "deployments_restart":
		var in config.DeploymentInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		return nil, e.deployments.RestartDeployment(ctx, in.Namespace, in.Name)
	case "deployments_delete":
		var in config.DeploymentInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		return nil, e.deployments.DeleteDeployment(ctx, in.Namespace, in.Name)
	case "statefulsets_list":
		var in config.NamespaceInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		return e.deployments.ListStatefulSets(ctx, in.Name)
	case "daemonsets_list":
		var in config.NamespaceInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		return e.deployments.ListDaemonSets(ctx, in.Name)

	// --- Services ---
	case "services_list":
		var in config.NamespaceInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		return e.services.ListServices(ctx, in.Name)
	case "services_get":
		var in config.DeploymentInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		return e.services.GetService(ctx, in.Namespace, in.Name)
	case "services_delete":
		var in config.DeploymentInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		return nil, e.services.DeleteService(ctx, in.Namespace, in.Name)
	case "endpoints_list":
		var in config.NamespaceInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		return e.services.ListEndpoints(ctx, in.Name)
	case "ingresses_list":
		var in config.NamespaceInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		return e.services.ListIngresses(ctx, in.Name)

	// --- Cluster ---
	case "namespaces_list":
		return e.cluster.ListNamespaces(ctx)
	case "namespaces_create":
		var in config.NamespaceInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		return nil, e.cluster.CreateNamespace(ctx, in.Name)
	case "namespaces_delete":
		var in config.NamespaceInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		return nil, e.cluster.DeleteNamespace(ctx, in.Name)
	case "nodes_list":
		return e.cluster.ListNodes(ctx)
	case "nodes_get":
		var in config.NamespaceInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		return e.cluster.GetNode(ctx, in.Name)
	case "cluster_info":
		return e.cluster.GetClusterInfo(ctx)
	case "events_list":
		var in config.NamespaceInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		return e.cluster.ListEvents(ctx, in.Name)
	case "configmaps_list":
		var in config.NamespaceInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		return e.cluster.ListConfigMaps(ctx, in.Name)
	case "secrets_list":
		var in config.NamespaceInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		return e.cluster.ListSecrets(ctx, in.Name)
	case "pvcs_list":
		var in config.NamespaceInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		return e.cluster.ListPVCs(ctx, in.Name)

	// --- Apply / Delete / Exec ---
	case "apply":
		var in config.ApplyInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		return e.apply.Apply(ctx, in.Manifest, in.Namespace)
	case "delete":
		var in config.DeleteInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		return nil, e.apply.DeleteResource(ctx, in.Kind, in.Name, in.Namespace)
	case "exec":
		var in config.ExecInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		return e.apply.ExecInPod(ctx, in.Namespace, in.Pod, in.Container, in.Command)

	// --- Topology ---
	case "topology":
		var in config.TopologyInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		return e.topology.BuildTopology(ctx, in.Namespace)
	case "traffic_routes":
		var in config.TrafficInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		return e.traffic.MapTrafficRoutes(ctx, in.Namespace)

	default:
		return nil, fmt.Errorf("unknown tool: %s", tool)
	}
}

// ToolInfo describes an available tool.
type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ListTools returns the catalog of available tools.
func (e *Executor) ListTools() []ToolInfo {
	return []ToolInfo{
		{Name: "pods_list", Description: "List pods in a namespace"},
		{Name: "pods_get", Description: "Get pod details"},
		{Name: "pods_logs", Description: "Get pod logs"},
		{Name: "pods_delete", Description: "Delete a pod"},
		{Name: "deployments_list", Description: "List deployments"},
		{Name: "deployments_get", Description: "Get deployment details"},
		{Name: "deployments_scale", Description: "Scale a deployment"},
		{Name: "deployments_restart", Description: "Restart a deployment"},
		{Name: "deployments_delete", Description: "Delete a deployment"},
		{Name: "statefulsets_list", Description: "List statefulsets"},
		{Name: "daemonsets_list", Description: "List daemonsets"},
		{Name: "services_list", Description: "List services"},
		{Name: "services_get", Description: "Get service details"},
		{Name: "services_delete", Description: "Delete a service"},
		{Name: "endpoints_list", Description: "List endpoints"},
		{Name: "ingresses_list", Description: "List ingresses"},
		{Name: "namespaces_list", Description: "List namespaces"},
		{Name: "namespaces_create", Description: "Create a namespace"},
		{Name: "namespaces_delete", Description: "Delete a namespace"},
		{Name: "nodes_list", Description: "List nodes"},
		{Name: "nodes_get", Description: "Get node details"},
		{Name: "cluster_info", Description: "Get cluster info"},
		{Name: "events_list", Description: "List events"},
		{Name: "configmaps_list", Description: "List config maps"},
		{Name: "secrets_list", Description: "List secrets (names only)"},
		{Name: "pvcs_list", Description: "List PVCs"},
		{Name: "apply", Description: "Apply YAML manifests"},
		{Name: "delete", Description: "Delete a resource"},
		{Name: "exec", Description: "Execute command in a pod"},
		{Name: "topology", Description: "Build network topology graph"},
		{Name: "traffic_routes", Description: "Map traffic routes"},
	}
}
